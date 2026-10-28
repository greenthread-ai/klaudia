package replay

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread-ai/klaudia/internal/session"
	"github.com/greenthread-ai/klaudia/internal/tools"
	"github.com/greenthread-ai/klaudia/pkg/agent"
	"github.com/greenthread-ai/klaudia/pkg/api"
	"github.com/greenthread-ai/klaudia/pkg/permission"
)

// scripted replies in order: a Read of path (plus extra input), then a
// closing text.
type scripted struct {
	path  string
	extra map[string]any
	calls int
}

func (s *scripted) StreamTurn(_ context.Context, _ anthropic.BetaMessageNewParams, _ api.StreamSink) (anthropic.BetaMessage, error) {
	s.calls++
	body := `{"role":"assistant","stop_reason":"end_turn","content":[{"type":"text","text":"done"}]}`
	if s.calls == 1 {
		input := map[string]any{"file_path": s.path}
		for k, v := range s.extra {
			input[k] = v
		}
		in, _ := json.Marshal(input)
		body = `{"role":"assistant","stop_reason":"tool_use","content":[{"type":"tool_use","id":"toolu_1","name":"Read","input":` + string(in) + `}]}`
	}
	var m anthropic.BetaMessage
	err := json.Unmarshal([]byte(body), &m)
	return m, err
}

// A session recorded by the real loop — a tool call, and a message the user
// sent while it ran — replays with no findings, as one turn, with the
// message landing where it did live: after the tool's result, before the
// next request.
func TestRecordThenReplay(t *testing.T) {
	t.Setenv("KLAUDIA_CONFIG_DIR", t.TempDir())
	dir := t.TempDir()
	file := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(file, []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tr, err := session.NewTranscript(session.Meta{SessionID: "rt", CWD: dir})
	if err != nil {
		t.Fatal(err)
	}
	read, err := tools.NewRead()
	if err != nil {
		t.Fatal(err)
	}
	polls := 0
	_, err = agent.New(&scripted{path: file}, tools.NewRegistry(read)).Run(context.Background(), agent.Options{
		Prompt: "read my notes", Model: "test", WorkingDir: dir, Recorder: tr,
		Permission: permission.Context{Mode: permission.StaticMode(permission.ModeBypassPermissions)},
		Interject: func() agent.Interjection {
			polls++
			if polls == 2 { // after the Read's batch
				return agent.Interjection{Text: "only the first line matters"}
			}
			return agent.Interjection{}
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	rec, err := Load(session.Path(dir, "rt"))
	if err != nil {
		t.Fatal(err)
	}
	res, err := Run(context.Background(), rec, Options{KeepRequests: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range res.Findings {
		t.Error(f)
	}
	if res.Turns != 1 || res.RequestCount != 2 {
		t.Fatalf("replayed %d turns, %d requests; want 1 and 2 (the mid-turn message is not a prompt)", res.Turns, res.RequestCount)
	}
	// In the second request: the Read's recorded result, then the message.
	var order []string
	for _, m := range res.Requests[1].Messages {
		for _, b := range m.Blocks {
			switch {
			case b.Type == "tool_result" && strings.Contains(b.Result, "hello"):
				order = append(order, "result")
			case strings.Contains(b.Text, "only the first line matters"):
				order = append(order, "message")
			}
		}
	}
	if strings.Join(order, ",") != "result,message" {
		t.Errorf("second request has %v, want the recorded result then the mid-turn message", order)
	}
}

// A model that sends a number as a string ("offset":"1") has it repaired by
// the loop before the tool runs; replay must find the recorded result for
// the repaired call, not report it as one the recording never saw.
func TestCoercedCallFindsItsResult(t *testing.T) {
	t.Setenv("KLAUDIA_CONFIG_DIR", t.TempDir())
	dir := t.TempDir()
	file := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(file, []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tr, err := session.NewTranscript(session.Meta{SessionID: "co", CWD: dir})
	if err != nil {
		t.Fatal(err)
	}
	read, err := tools.NewRead()
	if err != nil {
		t.Fatal(err)
	}
	_, err = agent.New(&scripted{path: file, extra: map[string]any{"offset": "1"}}, tools.NewRegistry(read)).Run(context.Background(), agent.Options{
		Prompt: "read", Model: "test", WorkingDir: dir, Recorder: tr,
		Permission: permission.Context{Mode: permission.StaticMode(permission.ModeBypassPermissions)},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	rec, err := Load(session.Path(dir, "co"))
	if err != nil {
		t.Fatal(err)
	}
	res, err := Run(context.Background(), rec, Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range res.Findings {
		t.Error(f)
	}
}
