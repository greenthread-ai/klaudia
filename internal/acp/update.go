package acp

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/greenthread-ai/klaudia/pkg/agent"
	"github.com/greenthread-ai/klaudia/pkg/plan"
)

// Translating Klaudia's event stream into ACP session/update notifications.
//
// The two models line up well except in four places, and each of those is a
// judgement call rather than a gap:
//
//   - Klaudia emits a "usage" event per inner LLM call carrying that call's
//     token deltas. ACP's usage_update is not a delta — it is "tokens resident
//     in the context, out of this many" — so forwarding the deltas would report
//     a growing total against a fixed window and show a session at 300% of its
//     context. The resident figure is only knowable from the whole history, so
//     Agent.prompt sends one usage_update per turn instead (see postUsage).
//   - Klaudia distinguishes a tool the host gate *refused* from a tool that
//     *failed* (Event.HostBlocked), because a refused `2>/dev/null` the model
//     routes around is a non-event and showing it as a red failure made Klaudia
//     look broken. ACP v1 has only pending/in_progress/completed/failed, so a
//     refusal maps to "failed" — wrong in flavour but not in fact, and the
//     refusal's own text goes out as the tool's content so the user can read
//     what happened. The alternative, "completed", would claim the tool ran.
//   - Compaction and notices have no ACP update. They go out as thought chunks;
//     see agentThought.
//   - TodoWrite is not reported as a tool call at all. ACP has a first-class
//     plan update and clients render it as a checklist, which is the whole
//     point of the tool; leaving it as a tool call would show the user a raw
//     JSON array instead. See translator.suppressed for what that costs.

// notifier is the half of a session the translator needs: somewhere to post
// updates. The Agent satisfies it; tests use a recorder.
type notifier interface {
	update(sessionID string, u any)
}

// translator turns one session's events into notifications.
//
// It carries state, where this used to be a plain function, because a tool call
// that is reported as something other than a tool call has to be remembered:
// tool_use and tool_result arrive as two events and the second one patches the
// first by id, so suppressing only the first would leave the client patching a
// call it was never told about.
type translator struct {
	n         notifier
	sessionID string

	mu sync.Mutex
	// suppressed holds the ids of tool calls reported as some other update
	// (TodoWrite becomes a plan). Guarded because tool batches run
	// concurrently, so two tools' events can reach Emit from two goroutines.
	suppressed map[string]bool

	// readFile supplies a file's pre-edit contents for a Write diff. A field so
	// tests do not have to touch the disk; nil means os.ReadFile.
	readFile func(path string) ([]byte, error)
}

func newTranslator(n notifier, sessionID string) *translator {
	return &translator{n: n, sessionID: sessionID, suppressed: map[string]bool{}}
}

func (t *translator) suppress(id string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.suppressed[id] = true
}

func (t *translator) isSuppressed(id string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.suppressed[id]
}

func (t *translator) post(u any) { t.n.update(t.sessionID, u) }

// event turns one agent.Event into zero or more session/update notifications
// and posts them.
func (t *translator) event(ev agent.Event) {
	switch ev.Type {
	case "assistant":
		if ev.Text == "" {
			return
		}
		t.post(agentMessage(ev.Text))

	case "tool_use":
		// A plan, not a tool call. The id is remembered so its tool_result does
		// not arrive as a patch to a call the client never saw. Nothing is
		// posted here: this event fires before the tool validates or stores
		// anything, so a TodoWrite the tool rejected used to show the client a
		// plan that never existed. The "plan" event below is the stored list.
		if ev.ToolName == "TodoWrite" {
			t.suppress(ev.ToolUseID)
			return
		}
		// status pending, not in_progress. ACP defines pending as "has not
		// started because it is awaiting approval or generation", and that is
		// exactly where this event sits: the loop emits tool_use before the
		// host gate and the permission check run. The approver flips it to
		// in_progress once the call is cleared (see approver.Approve).
		t.post(toolCallStart{
			Kind:       "tool_call",
			ToolCallID: ev.ToolUseID,
			Name:       ev.ToolName,
			Title:      toolTitle(ev.ToolName, ev.Input),
			ToolKind:   toolKind(ev.ToolName),
			Status:     statusPending,
			Content:    t.diffContent(ev.ToolName, ev.Input),
			Locations:  toolLocations(ev.ToolName, ev.Input),
			RawInput:   ev.Input,
		})

	case "tool_progress":
		if ev.Text == "" || t.isSuppressed(ev.ToolUseID) {
			return
		}
		t.post(toolCallPatch{
			Kind:       "tool_call_update",
			ToolCallID: ev.ToolUseID,
			Status:     statusInProgress,
			Content:    []toolCallContent{toolText(ev.Text)},
		})

	case "tool_result":
		if t.isSuppressed(ev.ToolUseID) {
			return
		}
		status := statusCompleted
		if ev.IsError {
			status = statusFailed
		}
		patch := toolCallPatch{
			Kind:       "tool_call_update",
			ToolCallID: ev.ToolUseID,
			Status:     status,
		}
		// FullContent is the untruncated output the tool withheld from the
		// model to protect the context window. The editor is a local frontend
		// with a scrollbar, so it gets the whole thing — this is the one place
		// where showing more than the model saw is correct.
		if text := ev.FullContent; text != "" {
			patch.Content = []toolCallContent{toolText(text)}
		} else if ev.Content != "" {
			patch.Content = []toolCallContent{toolText(ev.Content)}
		}
		t.post(patch)

	case "notice", "compaction":
		if text := noticeText(ev); text != "" {
			t.post(agentThought(text))
		}

	case "plan":
		if ev.Plan != nil {
			t.post(planUpdate{Kind: "plan", Entries: snapshotEntries(*ev.Plan)})
		}

	case "permission_mode":
		if ev.Content == "" {
			return
		}
		t.post(modeUpdate{Kind: "current_mode_update", CurrentModeID: ev.Content})
	}
}

// planEntries renders TodoWrite's input as ACP plan entries, reporting false for
// any other tool. Only session/load replay uses it now: a reloaded history has
// the call's input and no store to read, so the input is all there is.
//
// Status is passed through rather than mapped: TodoWrite's three statuses are
// spelled exactly as ACP's, and anything else the model invents becomes pending
// so a malformed status cannot produce an entry a client will reject.
func planEntries(name string, input any) ([]planEntry, bool) {
	if name != "TodoWrite" {
		return nil, false
	}
	var in struct {
		Todos []struct {
			Content string `json:"content"`
			Status  string `json:"status"`
		} `json:"todos"`
	}
	if !decodeInput(input, &in) {
		return nil, false
	}
	entries := make([]planEntry, 0, len(in.Todos))
	for _, td := range in.Todos {
		if strings.TrimSpace(td.Content) == "" {
			continue
		}
		status := planPending
		switch td.Status {
		case planInProgress, planCompleted:
			status = td.Status
		}
		entries = append(entries, planEntry{
			Content:  td.Content,
			Priority: planPriorityMedium,
			Status:   status,
		})
	}
	// An empty todos array is still a plan — it is how the model clears one —
	// so ok is true even with no entries. Returning false there would leak the
	// call back out as a raw tool call.
	return entries, true
}

// snapshotEntries renders the stored plan as ACP plan entries. The store
// only holds the three valid statuses (TodoWrite rejects anything else), but
// the same unknown-becomes-pending rule as planEntries is kept so the two
// paths cannot disagree about what a client is allowed to receive.
func snapshotEntries(snap plan.Snapshot) []planEntry {
	entries := make([]planEntry, 0, len(snap.Items))
	for _, it := range snap.Items {
		if strings.TrimSpace(it.Content) == "" {
			continue
		}
		status := planPending
		switch it.Status {
		case planInProgress, planCompleted:
			status = it.Status
		}
		entries = append(entries, planEntry{Content: it.Content, Priority: planPriorityMedium, Status: status})
	}
	return entries
}

// diffContent builds the diff a client renders as a side-by-side change, for the
// two tools whose input describes one.
//
// Edit sends its own old_string/new_string as the diff rather than a
// reconstructed whole file. Reconstructing would mean reimplementing Edit's
// replacement rules (uniqueness, replace_all) here, and a preview that computed
// the change slightly differently from the tool is worse than a narrower one:
// this is the text the permission prompt asks the user to approve.
//
// Write sends the file's current contents as oldText, read now — before the tool
// runs, which is the only moment they still exist. nil oldText is not "unknown",
// it is "no such file", and a client draws that as a new file.
func (t *translator) diffContent(name string, input any) []toolCallContent {
	var in struct {
		FilePath  string  `json:"file_path"`
		Content   *string `json:"content"`
		OldString *string `json:"old_string"`
		NewString *string `json:"new_string"`
	}
	if !decodeInput(input, &in) || in.FilePath == "" {
		return nil
	}
	switch name {
	case "Edit":
		if in.OldString == nil || in.NewString == nil {
			return nil
		}
		return []toolCallContent{toolDiff(in.FilePath, in.OldString, *in.NewString)}
	case "Write":
		if in.Content == nil {
			return nil
		}
		read := t.readFile
		if read == nil {
			read = os.ReadFile
		}
		var old *string
		if b, err := read(in.FilePath); err == nil {
			s := string(b)
			old = &s
		}
		return []toolCallContent{toolDiff(in.FilePath, old, *in.Content)}
	}
	return nil
}

// decodeInput re-decodes a tool input into a typed struct. Event.Input is an
// `any` the model's JSON was decoded into, so a round-trip through JSON is the
// only way to read it with field types intact — which inputFields, flattening
// everything to strings, cannot do: it cannot tell an absent "content" from an
// empty one, and truncating a file to nothing is a real edit.
func decodeInput(input any, out any) bool {
	b, err := json.Marshal(input)
	if err != nil {
		return false
	}
	return json.Unmarshal(b, out) == nil
}

// noticeText is the human line for a notice or compaction event. A compaction
// event with no content is the "started compacting" marker, which deserves a
// word rather than nothing: a long pause with no explanation is the thing
// notices exist to prevent.
func noticeText(ev agent.Event) string {
	if ev.Content != "" {
		return ev.Content
	}
	if ev.Type == "compaction" {
		return "compacting the conversation…"
	}
	return ""
}

// salientKey names the one input field worth putting in a tool call's title,
// per tool. Deliberately a copy of the TUI's equivalent rather than a shared
// helper: the TUI's version returns a lipgloss-styled suffix for a terminal
// header, and the two will want to diverge (an editor has room for a path that
// a 40-column status line does not).
var salientKey = map[string]string{
	"Bash":            "command",
	"Read":            "file_path",
	"Write":           "file_path",
	"Edit":            "file_path",
	"NotebookEdit":    "notebook_path",
	"Diagnostics":     "file",
	"Definition":      "file",
	"References":      "file",
	"Glob":            "pattern",
	"Grep":            "pattern",
	"BrowserSearch":   "query",
	"BrowserFetch":    "url",
	"BrowserNavigate": "url",
	"Skill":           "name",
	"ToolSearch":      "query",
	"Memory":          "operation",
	"BashOutput":      "bash_id",
	"KillShell":       "shell_id",
	"RestartJob":      "job",
	"ReadMcpResource": "uri",
}

// maxTitle bounds a tool call title. Long enough for a full path or a real
// command, short enough that a client rendering it on one line does not have
// to.
const maxTitle = 120

// toolTitle is the human-readable label for a tool call: the tool's name plus
// the one input that says which call this is.
func toolTitle(name string, input any) string {
	f := inputFields(input)
	if name == "Agent" {
		sub, desc := f["subagent_type"], oneline(f["description"])
		switch {
		case sub != "" && desc != "":
			return "Agent (" + sub + "): " + desc
		case sub != "":
			return "Agent: " + sub
		}
	}
	if key, ok := salientKey[name]; ok {
		if v := oneline(f[key]); v != "" {
			return name + " " + v
		}
	}
	// An MCP tool is named mcp__<server>__<tool>; the raw form is unreadable.
	if server, tool, ok := mcpToolName(name); ok {
		return tool + " (" + server + ")"
	}
	return name
}

// mcpToolName splits Klaudia's mcp__<server>__<tool> naming.
func mcpToolName(name string) (server, tool string, ok bool) {
	rest, ok := strings.CutPrefix(name, "mcp__")
	if !ok {
		return "", "", false
	}
	server, tool, ok = strings.Cut(rest, "__")
	if !ok || server == "" || tool == "" {
		return "", "", false
	}
	return server, tool, true
}

func oneline(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > maxTitle {
		s = string(r[:maxTitle-1]) + "…"
	}
	return s
}

// toolKinds maps Klaudia's tools onto ACP's fixed kind vocabulary, which is
// what a client uses to pick an icon and decide how prominent a call is.
// Anything unlisted — every MCP tool included — is "other".
var toolKinds = map[string]string{
	"Read":             kindRead,
	"BashOutput":       kindRead,
	"Jobs":             kindRead,
	"Diagnostics":      kindRead,
	"Definition":       kindRead,
	"References":       kindRead,
	"ListMcpResources": kindRead,
	"ReadMcpResource":  kindRead,
	"BrowserSnapshot":  kindRead,

	"Write":        kindEdit,
	"Edit":         kindEdit,
	"NotebookEdit": kindEdit,
	"TodoWrite":    kindEdit,
	"Memory":       kindEdit,

	"Glob":          kindSearch,
	"Grep":          kindSearch,
	"ToolSearch":    kindSearch,
	"BrowserSearch": kindSearch,

	"Bash":       kindExecute,
	"KillShell":  kindExecute,
	"RestartJob": kindExecute,
	"Skill":      kindExecute,

	"BrowserFetch":    kindFetch,
	"BrowserNavigate": kindFetch,

	"ExitPlanMode": kindSwitchMode,
}

func toolKind(name string) string {
	if k, ok := toolKinds[name]; ok {
		return k
	}
	return kindOther
}

// locationKey names the input field holding the file a tool acts on. Locations
// are the one translation an editor really uses: Zed follows them to keep the
// open buffer on whatever Klaudia is looking at.
var locationKey = map[string]string{
	"Read":         "file_path",
	"Write":        "file_path",
	"Edit":         "file_path",
	"NotebookEdit": "notebook_path",
	"Diagnostics":  "file",
	"Definition":   "file",
	"References":   "file",
}

func toolLocations(name string, input any) []toolCallLocation {
	key, ok := locationKey[name]
	if !ok {
		return nil
	}
	f := inputFields(input)
	path := f[key]
	if path == "" {
		return nil
	}
	loc := toolCallLocation{Path: path}
	// Read takes "offset"; the LSP tools take "line". Either one lets the
	// editor scroll to the right place instead of the top of the file.
	for _, k := range []string{"line", "offset"} {
		if n, err := strconv.Atoi(f[k]); err == nil && n > 0 {
			loc.Line = &n
			break
		}
	}
	return []toolCallLocation{loc}
}

// inputFields flattens a tool input into string values. Event.Input is an
// `any` decoded from the model's JSON, so a round-trip through JSON is the only
// way to read it without knowing each tool's struct.
func inputFields(input any) map[string]string {
	out := map[string]string{}
	b, err := json.Marshal(input)
	if err != nil {
		return out
	}
	var raw map[string]any
	if json.Unmarshal(b, &raw) != nil {
		return out
	}
	for k, v := range raw {
		switch s := v.(type) {
		case string:
			out[k] = s
		case float64:
			out[k] = strconv.FormatFloat(s, 'f', -1, 64)
		case bool:
			out[k] = strconv.FormatBool(s)
		}
	}
	return out
}

// stopReasonFor maps a Klaudia stop reason onto ACP's five.
//
// cancelled is reported from the session rather than inferred here: a turn the
// client cancelled may well come back with stop reason "user_halt" *or*
// "end_turn" depending on where the cancel landed, and the client that asked
// for it is owed the same answer either way.
func stopReasonFor(reason string) string {
	switch reason {
	case "max_tokens":
		return stopMaxTokens
	case "refusal":
		return stopRefusal
	case "max_turns":
		return stopMaxTurnRequests
	case "user_halt":
		return stopCancelled
	default:
		// end_turn, stop_sequence, tool_use, blocked_by_hook,
		// model_context_window_exceeded and "" all land here. The last two are
		// not end_turn in spirit, but ACP has no code for either and the reason
		// reaches the user as text (see Agent.prompt) rather than as a status
		// the client would have to guess at.
		return stopEndTurn
	}
}
