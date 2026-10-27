package replay

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
)

// Request is one model request, reduced to what decides the conversation:
// each message's role and blocks. Cache markers, thinking signatures and the
// system prompt and tool list are left out — they change for reasons that
// are not regressions, and a snapshot that churns on every description edit
// stops being read.
type Request struct {
	Turn     int       `json:"turn"`
	Messages []Message `json:"messages"`
}

// Message is one normalised message.
type Message struct {
	Role   string  `json:"role"`
	Blocks []Block `json:"blocks"`
}

// Block is one normalised content block.
type Block struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	Result    string          `json:"result,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
}

func normalise(msgs []anthropic.BetaMessageParam) (Request, error) {
	raw, err := json.Marshal(msgs)
	if err != nil {
		return Request{}, err
	}
	var in []struct {
		Role    string `json:"role"`
		Content []struct {
			Type      string          `json:"type"`
			Text      string          `json:"text"`
			Thinking  string          `json:"thinking"`
			ID        string          `json:"id"`
			ToolUseID string          `json:"tool_use_id"`
			Name      string          `json:"name"`
			Input     json.RawMessage `json:"input"`
			Content   json.RawMessage `json:"content"`
			IsError   bool            `json:"is_error"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return Request{}, err
	}
	var r Request
	for _, m := range in {
		nm := Message{Role: m.Role}
		for _, b := range m.Content {
			nb := Block{Type: b.Type, Text: b.Text, ID: b.ID, ToolUseID: b.ToolUseID, Name: b.Name, IsError: b.IsError}
			switch b.Type {
			case "thinking":
				nb.Text = b.Thinking
			case "tool_use":
				nb.Input = b.Input
			case "tool_result":
				nb.Result = resultText(b.Content)
			}
			nm.Blocks = append(nm.Blocks, nb)
		}
		r.Messages = append(r.Messages, nm)
	}
	return r, nil
}

// resultText flattens a tool_result's content, a string or a list of blocks,
// to its text; an image is noted rather than inlined.
func resultText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) != nil {
		return string(raw)
	}
	var parts []string
	for _, b := range blocks {
		if b.Type == "text" {
			parts = append(parts, b.Text)
		} else {
			parts = append(parts, "["+b.Type+"]")
		}
	}
	return strings.Join(parts, "\n")
}

// check finds what the API would reject, or what would degrade the session,
// in one request. Each rule is a bug Klaudia has had or the API's own
// validation.
func check(r Request) []Finding {
	var out []Finding
	add := func(kind, format string, a ...any) {
		out = append(out, Finding{Kind: kind, Detail: fmt.Sprintf(format, a...)})
	}
	if len(r.Messages) == 0 {
		add("empty-request", "no messages")
		return out
	}
	if r.Messages[0].Role != "user" {
		add("first-not-user", "the conversation starts with %s", r.Messages[0].Role)
	}
	if last := r.Messages[len(r.Messages)-1]; last.Role != "user" {
		add("last-not-user", "the request ends with an %s message", last.Role)
	}
	for i, m := range r.Messages {
		if len(m.Blocks) == 0 {
			add("empty-message", "message %d (%s) has no content", i, m.Role)
		}
		if i > 0 && m.Role == "assistant" && r.Messages[i-1].Role == "assistant" {
			add("consecutive-assistant", "messages %d and %d are both assistant", i-1, i)
		}
		// Only in what Klaudia composes: an assistant reply goes back as the
		// API sent it, and a web-search reply can carry an empty text block
		// the API accepts back (seen in a recorded session).
		if m.Role == "user" {
			for _, b := range m.Blocks {
				if b.Type == "text" && strings.TrimSpace(b.Text) == "" {
					add("empty-text", "message %d has an empty text block", i)
				}
			}
		}
		// Every tool_use is answered in the next message, and every
		// tool_result answers a tool_use in the previous one.
		if m.Role == "assistant" {
			uses := map[string]bool{}
			for _, b := range m.Blocks {
				if b.Type == "tool_use" {
					uses[b.ID] = true
				}
			}
			if len(uses) > 0 && i+1 < len(r.Messages) {
				for _, b := range r.Messages[i+1].Blocks {
					if b.Type == "tool_result" {
						delete(uses, b.ToolUseID)
					}
				}
				for id := range uses {
					add("unanswered-tool-use", "message %d calls %s with no tool_result after it", i, id)
				}
			}
		}
		if m.Role == "user" {
			prev := map[string]bool{}
			if i > 0 && r.Messages[i-1].Role == "assistant" {
				for _, b := range r.Messages[i-1].Blocks {
					if b.Type == "tool_use" {
						prev[b.ID] = true
					}
				}
			}
			for _, b := range m.Blocks {
				if b.Type == "tool_result" && !prev[b.ToolUseID] {
					add("orphan-tool-result", "message %d answers %s, which the message before it did not call", i, b.ToolUseID)
				}
			}
		}
	}
	return out
}
