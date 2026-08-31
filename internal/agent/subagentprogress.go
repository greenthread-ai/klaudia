package agent

import (
	"encoding/json"
	"strings"
)

// Turning a sub-agent's events into something a person can watch.
//
// Only tool calls are surfaced. The child's streamed prose is its working-out,
// and relaying it would interleave a second voice into the parent's transcript;
// what the user needs while waiting is evidence of movement and its shape —
// which files, which searches — so the line is the tool name plus its most
// identifying argument.

const progressFieldLimit = 60

// subagentProgressLine renders one child event as a short display line, or ""
// for events not worth surfacing.
func subagentProgressLine(ev Event) string {
	if ev.Type != "tool_use" || ev.ToolName == "" {
		return ""
	}
	// Ordered by how well the field identifies the call, not by how common it is.
	if v := firstToolField(ev.Input,
		"file_path", "notebook_path", "path", "command", "query", "url", "pattern", "description",
	); v != "" {
		return ev.ToolName + " " + clipProgress(v, progressFieldLimit)
	}
	return ev.ToolName
}

// firstToolField returns the first non-empty string field among keys. Tool input
// is `any` off the wire — usually a decoded map, but it round-trips through JSON
// when it is not, so a typed input works too.
func firstToolField(input any, keys ...string) string {
	fields, ok := input.(map[string]any)
	if !ok {
		b, err := json.Marshal(input)
		if err != nil {
			return ""
		}
		fields = map[string]any{}
		if json.Unmarshal(b, &fields) != nil {
			return ""
		}
	}
	for _, k := range keys {
		if s, ok := fields[k].(string); ok {
			if s = strings.TrimSpace(s); s != "" {
				return s
			}
		}
	}
	return ""
}

// clipProgress flattens a value to one line and bounds its width, so a pasted
// heredoc or a long prompt can't take over the display.
//
// Which end to keep depends on where the meaning is. A bare path is identified
// by its tail — clipping the front of a deep temp path still leaves the
// filename, while clipping the end leaves an unreadable prefix and throws away
// the only part that says which file. Anything else (a command, a query) reads
// front-first, so it keeps its head.
func clipProgress(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if strings.Contains(s, "/") && !strings.Contains(s, " ") {
		return "…" + string(r[len(r)-(n-1):])
	}
	return string(r[:n-1]) + "…"
}
