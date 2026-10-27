//go:build darwin || linux

package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

// mockModel is the model id the mock serves and the configs name.
const mockModel = "mock-model"

// ToolCall is one tool call the scripted model makes.
type ToolCall struct {
	Name string
	Args map[string]any
}

// Reply is one scripted assistant step: text, tool calls, or both.
type Reply struct {
	Text  string
	Calls []ToolCall
	// ChunkDelay paces the text stream; zero means a few milliseconds.
	ChunkDelay time.Duration
}

// Script returns the reply for a prompt, given how many assistant steps have
// already followed it (0 on the first request of a turn, 1 after the first
// round of tool results, …).
type Script func(prompt string, step int) Reply

// Mock is a scripted OpenAI-compatible Chat Completions server: GET
// /v1/models and streaming POST /v1/chat/completions over SSE.
type Mock struct {
	srv    *httptest.Server
	script Script

	mu        sync.Mutex
	completes int      // POST /chat/completions received
	prompts   []string // the prompt each completion request was answered for
	aborted   int      // streams the client hung up on
}

func newMock(t *testing.T, script Script) *Mock {
	m := &Mock{script: script}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/models", m.models)
	mux.HandleFunc("POST /v1/chat/completions", m.complete)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("mock: unexpected request %s %s", r.Method, r.URL.Path)
		http.NotFound(w, r)
	})
	m.srv = httptest.NewServer(mux) // binds 127.0.0.1
	t.Cleanup(m.srv.Close)
	return m
}

// BaseURL is what klaudia's baseURL setting should be.
func (m *Mock) BaseURL() string { return m.srv.URL + "/v1" }

// Prompts returns the prompt each completion request was answered for.
func (m *Mock) Prompts() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.prompts...)
}

// Requests is the number of completion requests received so far.
func (m *Mock) Requests() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.completes
}

// Aborted is the number of streams the client disconnected from mid-reply.
func (m *Mock) Aborted() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.aborted
}

func (m *Mock) models(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"object":"list","data":[{"id":%q,"object":"model"}]}`, mockModel)
}

type chatRequest struct {
	Model    string `json:"model"`
	Stream   bool   `json:"stream"`
	Messages []struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} `json:"messages"`
}

// promptAndStep finds the last user prompt and counts the assistant steps
// since it. Tool results are role "tool" and do not end the search.
func (req chatRequest) promptAndStep() (string, int) {
	step := 0
	for i := len(req.Messages) - 1; i >= 0; i-- {
		msg := req.Messages[i]
		switch msg.Role {
		case "assistant":
			step++
		case "user":
			text := strings.TrimSpace(reminderRe.ReplaceAllString(contentText(msg.Content), ""))
			if text == "" {
				continue // a reminder on its own is not the prompt
			}
			return text, step
		}
	}
	return "", step
}

var reminderRe = regexp.MustCompile(`(?s)<system-reminder>.*?</system-reminder>`)

func contentText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	_ = json.Unmarshal(raw, &parts)
	var b strings.Builder
	for _, p := range parts {
		b.WriteString(p.Text)
		b.WriteString("\n")
	}
	return b.String()
}

func (m *Mock) complete(w http.ResponseWriter, r *http.Request) {
	var req chatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	prompt, step := req.promptAndStep()
	m.mu.Lock()
	m.completes++
	m.prompts = append(m.prompts, prompt)
	m.mu.Unlock()
	reply := m.script(prompt, step)

	if !req.Stream {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model": mockModel,
			"choices": []any{map[string]any{
				"message":       map[string]any{"role": "assistant", "content": reply.Text},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 10},
		})
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	send := func(v any) bool {
		if r.Context().Err() != nil {
			return false
		}
		b, _ := json.Marshal(v)
		if _, err := fmt.Fprintf(w, "data: %s\n\n", b); err != nil {
			return false
		}
		if flusher != nil {
			flusher.Flush()
		}
		return true
	}
	abort := func() {
		m.mu.Lock()
		m.aborted++
		m.mu.Unlock()
	}

	delay := reply.ChunkDelay
	if delay == 0 {
		delay = 3 * time.Millisecond
	}
	const chunk = 16
	text := []rune(reply.Text)
	for i := 0; i < len(text); i += chunk {
		piece := string(text[i:min(i+chunk, len(text))])
		if !send(map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"content": piece}}}}) {
			abort()
			return
		}
		select {
		case <-time.After(delay):
		case <-r.Context().Done():
			abort()
			return
		}
	}
	for i, c := range reply.Calls {
		args, _ := json.Marshal(c.Args)
		send(map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"tool_calls": []any{map[string]any{
			"index": i, "id": fmt.Sprintf("call_%d_%d", step, i), "type": "function",
			"function": map[string]any{"name": c.Name, "arguments": string(args)},
		}}}}}})
	}
	finish := "stop"
	if len(reply.Calls) > 0 {
		finish = "tool_calls"
	}
	send(map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": finish}}})
	send(map[string]any{"choices": []any{}, "usage": map[string]any{"prompt_tokens": 1200, "completion_tokens": 80}})
	if r.Context().Err() == nil {
		fmt.Fprint(w, "data: [DONE]\n\n")
		if flusher != nil {
			flusher.Flush()
		}
	}
}

// slowSentence repeats in the "slow" reply; the test counts how many arrived.
// Each is its own paragraph: the TUI shows a paragraph once it is complete,
// so one long paragraph would not appear until the stream ended.
const slowSentence = "This reply streams slowly so that it can be interrupted."

// scriptKeywords are the words defaultScript answers to.
var scriptKeywords = []string{"hello", "tools", "fail", "slow", "links", "question", "wrapme", "planit", "longpara", "halfplan", "bgjob", "bgagent", "childtask"}

// keyword returns the scripted keyword that appears last in the prompt, or "".
//
// Last, not first: after an interrupted turn the next prompt reaches the model
// in the same user message as the interrupted one (separated by a blank line
// since the shim stopped joining them bare), so the newest words are at the
// end.
func keyword(prompt string) string {
	p := strings.ToLower(prompt)
	best, at := "", -1
	for _, k := range scriptKeywords {
		if i := strings.LastIndex(p, k); i > at {
			best, at = k, i
		}
	}
	return best
}

// defaultScript answers by keyword in the prompt. Add a keyword to
// scriptKeywords and a case here for a new scenario; a prompt with none of
// them, or "hello", gets a plain greeting.
func defaultScript(prompt string, step int) Reply {
	// /goal run's prompts are fixed text naming the spec file; answer them by
	// working through its checkboxes (goalStep) rather than by keyword.
	if strings.Contains(prompt, "autonomously iterating") || strings.Contains(prompt, "final independent review") {
		return goalStep(prompt, step)
	}
	// A background sub-agent's result arriving as its own turn.
	if strings.Contains(prompt, "<background-agent") {
		return Reply{Text: "Got the background result."}
	}
	// A plan run's prompts: finish the open step, then confirm on review.
	if strings.Contains(prompt, "Continue the plan the user approved") {
		if step == 0 {
			return Reply{Calls: []ToolCall{{"TodoWrite", halfPlanTodos("completed", "completed")}}}
		}
		return Reply{Text: "Finished the second step."}
	}
	if strings.Contains(prompt, "Every step of the approved plan") {
		return Reply{Text: "Both steps verified.\n<goal-complete/>"}
	}
	switch keyword(prompt) {
	case "question":
		if step == 0 {
			return Reply{Calls: []ToolCall{{"AskUserQuestion", map[string]any{
				"question": "Which greeting should main.go print?",
				"options": []any{
					map[string]any{"label": "hello, world", "description": "The classic"},
					map[string]any{"label": "hi", "description": "Short"},
				}}}}}
		}
		return Reply{Text: "Noted your choice."}
	case "longpara":
		var words []string
		for i := 1; i <= 300; i++ {
			words = append(words, fmt.Sprintf("w%03d", i))
		}
		return Reply{Text: strings.Join(words, " "), ChunkDelay: 15 * time.Millisecond}
	case "halfplan":
		// Approved, does one of two steps, then stops: the case a plan run
		// exists for.
		steps := []Reply{
			{Calls: []ToolCall{{"ExitPlanMode", map[string]any{"plan": "## Plan\n\n1. Change the greeting\n2. Add a test\n"}}}},
			{Calls: []ToolCall{{"TodoWrite", halfPlanTodos("in_progress", "pending")}}},
			{Calls: []ToolCall{{"TodoWrite", halfPlanTodos("completed", "pending")}}},
		}
		if step < len(steps) {
			return steps[step]
		}
		return Reply{Text: "Pausing after the first step."}
	case "bgjob":
		if step == 0 {
			return Reply{Calls: []ToolCall{{"Bash", map[string]any{"command": "sleep 30", "description": "Background sleeper", "run_in_background": true}}}}
		}
		return Reply{Text: "Started it in the background."}
	case "bgagent":
		if step == 0 {
			return Reply{Calls: []ToolCall{{"Agent", map[string]any{
				"description": "survey", "subagent_type": "Explore", "run_in_background": true,
				"prompt": "childtask: survey the project"}}}}
		}
		return Reply{Text: "Started it; carrying on."}
	case "childtask":
		// The sub-agent's own turn: slow enough to be seen running.
		return Reply{Text: strings.Repeat("Child finished: three findings. ", 20), ChunkDelay: 40 * time.Millisecond}
	case "wrapme":
		if step == 0 {
			return Reply{Calls: []ToolCall{{"Bash", map[string]any{"command": "echo " + wrapSentence + "; exit 3", "description": "A long failure"}}}}
		}
		return Reply{Text: "Done wrapping."}
	case "planit":
		todos := func(statuses ...string) map[string]any {
			names := []string{"Change the greeting", "Add a test"}
			var list []any
			for i, s := range statuses {
				list = append(list, map[string]any{"content": names[i], "status": s})
			}
			return map[string]any{"todos": list}
		}
		steps := []Reply{
			{Text: "Here is my plan.\n", Calls: []ToolCall{{"ExitPlanMode", map[string]any{"plan": "## Plan\n\n1. Change the greeting\n2. Add a test\n"}}}},
			{Calls: []ToolCall{{"TodoWrite", todos("in_progress", "pending")}}},
			{Calls: []ToolCall{{"Edit", map[string]any{"file_path": "main.go", "old_string": `"hello"`, "new_string": `"hello, plan"`}}}},
			{Calls: []ToolCall{{"TodoWrite", todos("completed", "completed")}}},
		}
		if step < len(steps) {
			return steps[step]
		}
		return Reply{Text: "Plan done."}
	case "tools":
		steps := []Reply{
			{Text: "Let me look.\n", Calls: []ToolCall{{"TodoWrite", map[string]any{"todos": []any{
				map[string]any{"content": "Read the project", "status": "in_progress", "activeForm": "Reading the project"},
				map[string]any{"content": "Fix the greeting", "status": "pending", "activeForm": "Fixing the greeting"},
			}}}}},
			{Calls: []ToolCall{
				{"Read", map[string]any{"file_path": "main.go"}},
				{"Glob", map[string]any{"pattern": "**/*.go"}},
			}},
			{Calls: []ToolCall{{"Bash", map[string]any{"command": "echo built-ok", "description": "Pretend to build"}}}},
			{Calls: []ToolCall{{"Edit", map[string]any{"file_path": "main.go", "old_string": `"hello"`, "new_string": `"hello, e2e"`}}}},
			{Calls: []ToolCall{{"TodoWrite", map[string]any{"todos": []any{
				map[string]any{"content": "Read the project", "status": "completed", "activeForm": "Reading the project"},
				map[string]any{"content": "Fix the greeting", "status": "completed", "activeForm": "Fixing the greeting"},
			}}}}},
		}
		if step < len(steps) {
			return steps[step]
		}
		return Reply{Text: "Changed the greeting in main.go."}
	case "fail":
		if step == 0 {
			return Reply{Calls: []ToolCall{{"Bash", map[string]any{"command": "ls ./no-such-dir-e2e", "description": "List a missing directory"}}}}
		}
		return Reply{Text: "That failed, as expected."}
	case "slow":
		return Reply{Text: strings.Repeat(slowSentence+"\n\n", 40), ChunkDelay: 60 * time.Millisecond}
	case "links":
		return Reply{Text: "The entry point is main.go and the docs are at https://go.dev/doc/. " +
			"There is no nowhere/missing.go.\n"}
	}
	return Reply{Text: "Hello from the mock model."}
}

// wrapSentence is distinctive words, long enough to wrap at 100 columns, so a
// scenario can check none of them was split across rows.
const wrapSentence = "alphaword bravoword charlieword deltaword echoword foxtrotword golfword " +
	"hotelword indiaword julietword kiloword limaword mikeword novemberword oscarword papaword"

// goalStep plays a model working a /goal run: each iteration ticks the first
// unchecked box in the spec with Edit, and claims completion once none are
// left; the final review confirms it.
func goalStep(prompt string, step int) Reply {
	m := regexp.MustCompile(`(?:defined in|Re-read) (\S+?\.md)`).FindStringSubmatch(prompt)
	if m == nil {
		return Reply{Text: "no spec path in the prompt"}
	}
	data, err := os.ReadFile(m[1])
	if err != nil {
		return Reply{Text: "cannot read " + m[1]}
	}
	var unchecked []string
	for _, ln := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(strings.TrimSpace(ln), "- [ ]") {
			unchecked = append(unchecked, ln)
		}
	}
	switch {
	case strings.Contains(prompt, "final independent review"):
		return Reply{Text: "Reviewed every item.\n<goal-complete/>"}
	case step == 0 && len(unchecked) > 0:
		return Reply{Calls: []ToolCall{{"Edit", map[string]any{
			"file_path": m[1], "old_string": unchecked[0],
			"new_string": strings.Replace(unchecked[0], "- [ ]", "- [x]", 1)}}}}
	case len(unchecked) == 0:
		return Reply{Text: "All ticked.\n<goal-complete/>"}
	}
	return Reply{Text: "Ticked one."}
}

// halfPlanTodos is the halfplan scenario's two-step list at the given statuses.
func halfPlanTodos(first, second string) map[string]any {
	return map[string]any{"todos": []any{
		map[string]any{"content": "Change the greeting", "status": first},
		map[string]any{"content": "Add a test", "status": second},
	}}
}
