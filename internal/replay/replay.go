// Package replay drives today's agent loop through a recorded session, to
// catch regressions in what the loop sends before a user does.
//
// The model's replies and the tools' results come from the recording; the
// loop, compaction, output caps and the tool definitions are today's. Nothing
// is executed, so a replay is deterministic and safe on any session — which
// is the reason for replaying recorded results rather than re-running tools
// in a scratch copy: the transcript does not capture the tree a session began
// in, and Bash output depends on the machine, the clock and the network, so a
// re-run would mostly report noise.
//
// Two kinds of check come out of it. Findings (check.go) are things that are
// wrong in any session: a request the API would reject, a reply the loop
// asked for that the recording does not have, a recorded call today's schema
// refuses. And the normalised requests themselves, which the tests snapshot
// for a small checked-in corpus, so any change in how the conversation is
// assembled is a reviewed change rather than a surprise.
package replay

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread-ai/klaudia/internal/session"
	"github.com/greenthread-ai/klaudia/pkg/agent"
	"github.com/greenthread-ai/klaudia/pkg/compaction"
	"github.com/greenthread-ai/klaudia/pkg/permission"
)

// Finding is one thing a replay found wrong.
type Finding struct {
	Turn    int    // 1-based user turn
	Request int    // 1-based model request within the replay, 0 if none
	Kind    string // a short slug: "orphan-tool-result", "schema", …
	Detail  string
}

func (f Finding) String() string {
	return fmt.Sprintf("turn %d, request %d: %s: %s", f.Turn, f.Request, f.Kind, f.Detail)
}

// Result is what a replay produced.
type Result struct {
	Turns        int
	Requests     []Request // every model request the loop made, normalised (KeepRequests)
	RequestCount int
	Findings     []Finding
}

// step is one line of a recording, in order.
type step struct {
	msg      *anthropic.BetaMessageParam // a user or assistant message
	role     string
	subtype  string // a system line: compact_boundary, partial_compact, rewind
	content  string
	kept     int
	maxWin   int // the context window the recording ran with, when stamped
	newTurn  bool
	toolUses []toolUse // assistant: the calls it made
}

type toolUse struct {
	id, name string
	input    json.RawMessage
}

// Recording is a transcript read for replay: its messages and system lines,
// in order.
type Recording []step

// Load reads a transcript for replay.
func Load(path string) (Recording, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var steps []step
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 64*1024*1024)
	for sc.Scan() {
		var e session.Entry
		if json.Unmarshal(sc.Bytes(), &e) != nil {
			continue
		}
		switch e.Type {
		case "system":
			steps = append(steps, step{subtype: e.Subtype, content: e.Content, kept: e.Kept})
		case "user", "assistant":
			if e.IsSidechain {
				continue
			}
			var m anthropic.BetaMessageParam
			if err := json.Unmarshal(e.Message, &m); err != nil {
				return nil, fmt.Errorf("%s: %w", e.UUID, err)
			}
			s := step{msg: &m, role: e.Type, maxWin: e.ContextWindow}
			for _, b := range m.Content {
				switch {
				case b.OfToolUse != nil:
					in, _ := json.Marshal(b.OfToolUse.Input)
					s.toolUses = append(s.toolUses, toolUse{b.OfToolUse.ID, b.OfToolUse.Name, in})
				case e.Type == "user" && b.OfToolResult == nil:
					s.newTurn = true
				}
			}
			steps = append(steps, s)
		}
	}
	return steps, sc.Err()
}

// Options tune a replay.
type Options struct {
	// KeepRequests keeps every normalised request in the Result, for a
	// snapshot. Off for a sweep: a long session makes tens of thousands of
	// requests, each carrying the whole conversation.
	KeepRequests bool
}

// Run replays a recording through today's loop.
func Run(ctx context.Context, steps Recording, opts Options) (Result, error) {
	reg, store, err := newTools(steps)
	if err != nil {
		return Result{}, err
	}
	p := &provider{window: 200_000, keep: opts.KeepRequests}
	loop := agent.New(p, reg)

	var res Result
	var hist []anthropic.BetaMessageParam
	for i := 0; i < len(steps); i++ {
		s := steps[i]
		switch {
		case s.subtype != "":
			hist = applyBoundary(hist, s)
			continue
		case !s.newTurn:
			continue // a reply or a tool result: consumed by the turn below
		}
		// One user turn: the prompt, then every reply and tool result up to
		// the next prompt. System lines inside it are the loop's own
		// autocompactions; their summaries answer its summary requests.
		res.Turns++
		p.turn, p.replies, p.next, p.summaries, p.servedStop = res.Turns, nil, 0, nil, false
		j, lastReply := i+1, i
		for ; j < len(steps) && !steps[j].newTurn; j++ {
			if steps[j].role == "assistant" {
				lastReply = j
			}
		}
		for k := i + 1; k < j; k++ {
			switch {
			case steps[k].role == "assistant":
				p.replies = append(p.replies, *steps[k].msg)
				if steps[k].maxWin > 0 {
					p.window = steps[k].maxWin
				}
			case steps[k].subtype == session.SubtypeCompactBoundary && k < lastReply:
				// Mid-turn: the loop's own autocompact. Its summary answers
				// the replayed loop's summary request, if it makes one.
				p.summaries = append(p.summaries, steps[k].content)
			}
		}
		store.turn = res.Turns
		// A recording ends a turn early when the user interrupted it: no
		// reply at all, or a last reply that called tools whose results
		// were never answered. The live history kept what was there; so
		// does the replay, without running a loop that would ask for more.
		if len(p.replies) == 0 {
			hist = append(hist, *s.msg)
			i = j - 1
			continue
		}
		p.cutShort = steps[lastReply].toolUses != nil
		out, err := loop.Run(ctx, agent.Options{
			Model:           "replay",
			MaxTokens:       8192,
			ContextWindow:   p.window,
			Permission:      permission.Context{Mode: permission.StaticMode(permission.ModeBypassPermissions)},
			InitialMessages: append(append([]anthropic.BetaMessageParam(nil), hist...), *s.msg),
		}, nil)
		if err != nil {
			p.findings = append(p.findings, Finding{Turn: res.Turns, Kind: "loop-error", Detail: err.Error()})
		}
		if p.next < len(p.replies) {
			p.findings = append(p.findings, Finding{Turn: res.Turns, Kind: "stopped-early",
				Detail: fmt.Sprintf("the loop ended the turn with %d of %d recorded replies unused", len(p.replies)-p.next, len(p.replies))})
		}
		if out.Messages != nil {
			hist = out.Messages
			if p.servedStop && len(hist) > 0 && hist[len(hist)-1].Role == anthropic.BetaMessageParamRoleAssistant {
				hist = hist[:len(hist)-1] // the stop replay invented, which the live session never had
			}
		}
		// After the turn's last reply: a /compact, a rewind or a summarise,
		// made between turns, applied before the next prompt.
		for k := lastReply + 1; k < j; k++ {
			if steps[k].subtype != "" {
				hist = applyBoundary(hist, steps[k])
			}
		}
		i = j - 1
	}
	res.Requests, res.RequestCount = p.requests, p.count
	res.Findings = collapse(append(p.findings, store.findings...))
	return res, nil
}

// collapse reports a finding that recurs in later requests once, with a
// count: a bad message stays in the conversation, so it is found again in
// every request after it.
func collapse(fs []Finding) []Finding {
	var out []Finding
	seen := map[string]int{}
	repeats := map[string]int{}
	for _, f := range fs {
		k := f.Kind + "\x00" + f.Detail
		if _, ok := seen[k]; ok {
			repeats[k]++
			continue
		}
		seen[k] = len(out)
		out = append(out, f)
	}
	for k, n := range repeats {
		out[seen[k]].Detail += fmt.Sprintf(" (and in %d later requests)", n)
	}
	return out
}

// applyBoundary does to the replayed history what a recorded system line did
// to the live one. The counts are the live conversation's; when the replay's
// history is a different length (its own compaction differed), the cut is
// skipped rather than made somewhere meaningless.
func applyBoundary(hist []anthropic.BetaMessageParam, s step) []anthropic.BetaMessageParam {
	switch s.subtype {
	case session.SubtypeCompactBoundary:
		if strings.TrimSpace(s.content) != "" {
			return compaction.ReplaceWithSummary(s.content)
		}
	case session.SubtypePartialCompact:
		if s.kept <= len(hist) {
			return append(compaction.ReplaceWithSummary(s.content), hist[len(hist)-s.kept:]...)
		}
	case session.SubtypeRewind:
		if keep, err := strconv.Atoi(strings.TrimSpace(s.content)); err == nil && keep <= len(hist) {
			return hist[:keep]
		}
	}
	return hist
}
