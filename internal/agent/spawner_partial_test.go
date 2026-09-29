package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread-ai/klaudia/internal/api"
)

// textThenFailProvider reports a finding and calls a tool on its first turn,
// then fails the way an overloaded service does.
type textThenFailProvider struct {
	turn  anthropic.BetaMessage
	calls int
}

func (p *textThenFailProvider) StreamTurn(_ context.Context, _ anthropic.BetaMessageNewParams, _ api.StreamSink) (anthropic.BetaMessage, error) {
	p.calls++
	if p.calls == 1 {
		return p.turn, nil
	}
	return anthropic.BetaMessage{}, errors.New("overloaded_error: Overloaded")
}

// A sub-agent that failed mid-run returned nothing, so everything it had
// found was lost with it.
func TestSpawnFailureKeepsPartialWork(t *testing.T) {
	dir, path := fixtureFile(t)
	inB, _ := json.Marshal(map[string]any{"file_path": path})
	raw := fmt.Sprintf(`{"role":"assistant","stop_reason":"tool_use","content":[
		{"type":"text","text":"The config lives in notes.txt; checking it next."},
		{"type":"tool_use","id":"tu1","name":"Read","input":%s}]}`, inB)
	var turn anthropic.BetaMessage
	if err := json.Unmarshal([]byte(raw), &turn); err != nil {
		t.Fatal(err)
	}
	provider := &textThenFailProvider{turn: turn}

	got, err := readOnlySpawner(t, provider, dir, 0).Spawn(context.Background(), "Explore", "find the config", nil)
	if err == nil {
		t.Fatal("want the child's error returned")
	}
	if !strings.Contains(got, "notes.txt") || !strings.Contains(got, "may be incomplete") {
		t.Errorf("result = %q, want the child's last reply, marked partial", got)
	}
}
