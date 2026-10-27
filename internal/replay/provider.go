package replay

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread-ai/klaudia/pkg/api"
	"github.com/greenthread-ai/klaudia/pkg/compaction"
)

// provider answers the loop's model calls from the recording: the turn's
// recorded replies in order, and its recorded summaries for a compaction.
// Every request is normalised, kept and checked on the way.
type provider struct {
	turn      int
	replies   []anthropic.BetaMessageParam
	next      int
	summaries []string
	window    int
	// cutShort marks a turn the recording ends mid-work (interrupted): the
	// loop's request after the last reply is expected, and is answered with
	// a stop rather than reported. servedStop records that it was.
	cutShort   bool
	servedStop bool

	keep     bool
	count    int
	requests []Request
	findings []Finding
}

func (p *provider) StreamTurn(_ context.Context, params anthropic.BetaMessageNewParams, _ api.StreamSink) (anthropic.BetaMessage, error) {
	req, err := normalise(params.Messages)
	if err != nil {
		return anthropic.BetaMessage{}, err
	}
	req.Turn = p.turn
	p.count++
	n := p.count
	if p.keep {
		p.requests = append(p.requests, req)
	}
	found := check(req)
	// Measured with the loop's own estimator, so this fires only when the
	// loop let a request past the window it compacts against.
	if est := compaction.EstimateTokens(params.Messages); p.window > 0 && est > p.window {
		found = append(found, Finding{Kind: "over-window", Detail: fmt.Sprintf("about %d tokens against a %d-token window", est, p.window)})
	}
	for _, f := range found {
		f.Turn, f.Request = p.turn, n
		p.findings = append(p.findings, f)
	}

	if isSummaryRequest(req) {
		text := "[replay: the recording has no summary here]"
		if len(p.summaries) > 0 {
			text, p.summaries = p.summaries[0], p.summaries[1:]
		}
		return assistantMessage([]any{map[string]any{"type": "text", "text": text}}, "end_turn")
	}
	if p.next >= len(p.replies) && p.cutShort {
		p.servedStop = true
		return assistantMessage([]any{map[string]any{"type": "text", "text": "[replay: the recording was interrupted here]"}}, "end_turn")
	}
	if p.next >= len(p.replies) {
		p.findings = append(p.findings, Finding{Turn: p.turn, Request: n, Kind: "unrecorded-request",
			Detail: "the loop asked for a reply after the recording's last one in this turn"})
		return assistantMessage([]any{map[string]any{"type": "text", "text": "[replay: no recorded reply]"}}, "end_turn")
	}
	reply := p.replies[p.next]
	p.next++
	raw, err := json.Marshal(reply.Content)
	if err != nil {
		return anthropic.BetaMessage{}, err
	}
	var blocks []any
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return anthropic.BetaMessage{}, err
	}
	stop := "end_turn"
	for _, b := range blocks {
		if m, ok := b.(map[string]any); ok && m["type"] == "tool_use" {
			stop = "tool_use"
		}
	}
	return assistantMessage(blocks, stop)
}

// assistantMessage builds the response the loop would have assembled from a
// stream, by way of the same JSON the API returns.
func assistantMessage(content []any, stop string) (anthropic.BetaMessage, error) {
	raw, err := json.Marshal(map[string]any{
		"id": "msg_replay", "type": "message", "role": "assistant", "model": "replay",
		"content": content, "stop_reason": stop,
		"usage": map[string]any{"input_tokens": 0, "output_tokens": 0},
	})
	if err != nil {
		return anthropic.BetaMessage{}, err
	}
	var m anthropic.BetaMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return anthropic.BetaMessage{}, fmt.Errorf("replay: building a reply: %w", err)
	}
	return m, nil
}

// isSummaryRequest reports whether the loop is asking for a compaction
// summary: its last message is the summary instruction.
func isSummaryRequest(r Request) bool {
	if len(r.Messages) == 0 {
		return false
	}
	last := r.Messages[len(r.Messages)-1]
	return last.Role == "user" && len(last.Blocks) > 0 && last.Blocks[len(last.Blocks)-1].Text == compaction.SummaryInstruction
}
