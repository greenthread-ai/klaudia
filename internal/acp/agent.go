// Package acp serves the Agent Client Protocol over stdio, so an editor —
// Zed, Neovim's acp.nvim, the JetBrains plugin — can drive Klaudia as its
// coding agent while keeping its own UI for messages, tool calls and
// permission prompts.
//
// It is the third frontend over the same agent.RunFunc the TUI and the
// stream-json driver use (see agent.Turn). The protocol differs from
// stream-json in two ways that matter: it is JSON-RPC rather than a bespoke
// envelope, so clients already have a library for it; and it is a standard, so
// support is written once per editor rather than once per agent.
//
// Scope, and what is deliberately left out:
//
//   - fs/read_text_file is used; fs/write_text_file is declined. Reading through
//     the client is a correctness fix (the editor hands back unsaved buffers);
//     writing through it is not safe for Klaudia's edit tools. internal/acp/fs.go
//     has the reasoning.
//   - terminal/*. Declined, not deferred. Routing Bash through the editor's
//     terminal would hand over execution and with it Klaudia's sandbox, its
//     trust gate, its job control and its output clamping. A prettier widget is
//     not worth any of those.
//   - session/delete. Not advertised. A transcript is the user's record of what
//     an agent did on their machine, and an editor's "close tab" should not be
//     able to erase it; `rm` and a file browser are right there.
//   - One prompt at a time, across all sessions. A client may open several
//     threads, but this process has one tool registry, one job store, one
//     executor and one sandbox: running two turns at once would interleave
//     them. A prompt arriving while another is running waits, and a cancel
//     while it waits is still honoured. Transcripts, by contrast, are per
//     session — see Options.Transcript.
package acp

import (
	"context"
	"encoding/json"
	"errors"
	sessions "github.com/greenthread-ai/klaudia/internal/session"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/google/uuid"

	"github.com/greenthread-ai/klaudia/internal/config"
	"github.com/greenthread-ai/klaudia/pkg/agent"
	"github.com/greenthread-ai/klaudia/pkg/compaction"
	"github.com/greenthread-ai/klaudia/pkg/permission"
	"github.com/greenthread-ai/klaudia/pkg/plan"
)

// Transcript is a session's persistent record: the agent.Recorder the loop
// writes through, plus the handle to close it.
//
// *session.Transcript satisfies it. It is an interface here so this package does
// not depend on the session store, and so tests can assert what was recorded.
type Transcript interface {
	agent.Recorder
	io.Closer
}

// Options configures an Agent.
type Options struct {
	// Run executes one turn. Required — the CLI supplies it, closing over the
	// provider, tools, model and system prompt.
	Run agent.RunFunc
	// CWD is the project root this process was started in. A session/new for a
	// different directory is refused: the tool registry, system prompt and
	// sandbox are all bound to it.
	CWD string
	// Mode is the permission mode a new session starts in.
	Mode permission.Mode
	// History seeds the first session's conversation (a resumed transcript).
	// Consumed once, by whichever session/new arrives first — resuming is about
	// the process, and a second thread in the same editor is a new conversation.
	History []anthropic.BetaMessageParam
	// SessionID is the id the first session/new takes, so that the editor's
	// first thread *is* the session the CLI resolved — including a --resume or
	// --continue id, whose transcript it then keeps appending to. Empty, and for
	// every session after the first, means a fresh id from NewSessionID.
	SessionID string
	// Transcript opens the persistent record for a session. Nil, or a nil
	// return, means that session is not persisted — which is what a transcript
	// that could not be opened has always meant, since losing the record is not
	// a reason to refuse to work.
	//
	// Per session, not per process: ACP is the one frontend with more than one
	// conversation at a time, and a single shared transcript had two threads
	// appending to the same file, so session/load replayed an interleaving that
	// never happened.
	Transcript func(sessionID string) Transcript
	// Plan opens a session's plan store. Nil gives each session an in-memory
	// one with no file. Per session for the transcript's reason: with the
	// process-wide store two threads shared one step list, and each approval
	// rewrote the other's plan file.
	Plan func(sessionID string) *plan.Store
	// Commands are the /commands offered to the client (Klaudia's skills). See
	// commands.go for why the built-in slash commands are not among them.
	Commands []Command
	// ContextWindow is the model's input-token limit, for the usage_update that
	// drives a client's context gauge. 0 suppresses the update rather than
	// reporting a percentage of an unknown total.
	ContextWindow int
	// Log receives diagnostics that have no place in the protocol: a failed
	// write, a notification for a session that has gone. The CLI points it at
	// stderr. Nil discards them.
	Log func(string)
	// NewSessionID generates session ids. Nil uses a UUID; tests set it.
	NewSessionID func() string
	// LoadHistory reads a persisted session back for session/load. Nil disables
	// loadSession, which is how a caller with no transcript store says so.
	LoadHistory func(sessionID string) ([]anthropic.BetaMessageParam, error)
	// ListSessions enumerates the project's persisted sessions, newest first.
	// Nil disables session/list.
	ListSessions func() []SessionSummary
}

// SessionSummary is one entry in a session/list response.
type SessionSummary struct {
	ID string
	// Title is a one-line label (Klaudia uses the session's first prompt).
	Title string
	// UpdatedAt is RFC3339; empty omits the field.
	UpdatedAt string
}

// Agent serves ACP on a reader/writer pair.
type Agent struct {
	opts Options
	conn *conn

	// turn is the one-prompt-at-a-time gate. Buffered to one: holding it is
	// permission to run.
	turn chan struct{}

	mu          sync.Mutex
	sessions    map[string]*session
	caps        clientCapabilities
	initialized bool
	// firstUsed records that the process's seeded history and session id have
	// been handed to a session. One flag for both, because they are two halves
	// of the same thing: the first thread the editor opens is the session the
	// CLI resolved.
	firstUsed bool
}

// New builds an Agent that writes to out.
func New(opts Options, out io.Writer) *Agent {
	a := &Agent{
		opts:     opts,
		turn:     make(chan struct{}, 1),
		sessions: map[string]*session{},
	}
	a.turn <- struct{}{}
	a.conn = newConn(out, a.dispatch)
	return a
}

// Serve reads ACP requests from in until EOF.
//
// Transcripts opened for sessions the client never closed are closed here: a
// client exiting is the normal way an ACP connection ends, and leaving the
// files open would lose buffered writes.
func (a *Agent) Serve(ctx context.Context, in io.Reader) error {
	defer a.closeAll()
	return a.conn.serve(ctx, in)
}

func (a *Agent) closeAll() {
	a.mu.Lock()
	open := make([]*session, 0, len(a.sessions))
	for _, s := range a.sessions {
		open = append(open, s)
	}
	a.sessions = map[string]*session{}
	a.mu.Unlock()
	for _, s := range open {
		s.close()
	}
}

// session is one ACP conversation.
type session struct {
	id  string
	cwd string

	mu      sync.Mutex
	history []anthropic.BetaMessageParam
	mode    permission.Mode
	// transcript is this conversation's own persistent record. May be nil.
	transcript Transcript
	// plan is this conversation's step list. Never nil.
	plan *plan.Store
	// cancel aborts the in-flight prompt; nil when idle.
	cancel context.CancelFunc
	// cancelled records that the client asked to stop. The spec requires the
	// pending session/prompt to answer "cancelled", and the agent loop's own
	// stop reason does not reliably say so — where the cancel lands decides
	// whether it comes back as user_halt or as a plain end_turn.
	cancelled bool
}

func (s *session) currentMode() permission.Mode {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mode
}

// close stops any in-flight prompt and releases the transcript.
func (s *session) close() {
	s.mu.Lock()
	cancel, tr := s.cancel, s.transcript
	s.transcript = nil
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if tr != nil {
		_ = tr.Close()
	}
}

func (a *Agent) dispatch(ctx context.Context, method string, params json.RawMessage) (any, error) {
	switch method {
	case "initialize":
		return a.initialize(params)
	case "authenticate":
		// No auth methods are advertised, so a well-behaved client never calls
		// this. Answering null rather than "method not found" keeps a client
		// that calls it unconditionally working.
		return nil, nil
	case "session/new":
		return a.newSession(params)
	case "session/load":
		return a.loadSession(params)
	case "session/list":
		return a.listSessions(params)
	case "session/close":
		return nil, a.closeSession(params)
	case "session/prompt":
		return a.prompt(ctx, params)
	case "session/cancel":
		return nil, a.cancelPrompt(params)
	case "session/set_mode":
		return nil, a.setMode(params)
	}
	return nil, errorf(codeMethodNotFound, "Klaudia does not implement "+method)
}

func (a *Agent) initialize(raw json.RawMessage) (any, error) {
	var p initializeParams
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, errorf(codeInvalidParams, "initialize: "+err.Error())
		}
	}
	a.mu.Lock()
	a.caps = p.ClientCapabilities
	a.initialized = true
	a.mu.Unlock()

	caps := agentCapabilities{
		// Advertised only when there is a store behind it. A client told
		// loadSession is supported and then refused every id has no way to tell
		// that apart from a broken agent.
		LoadSession:        a.opts.LoadHistory != nil,
		PromptCapabilities: promptCapabilities{EmbeddedContext: true},
		MCPCapabilities:    mcpCapabilities{},
	}
	sc := &sessionCapabilities{Close: &sessionMethodCapability{}}
	if a.opts.ListSessions != nil {
		sc.List = &sessionMethodCapability{}
	}
	caps.SessionCapabilities = sc

	// The spec: answer with the requested version when it is supported, and
	// with the newest supported version otherwise. Either way the client
	// decides whether it can continue.
	return initializeResult{
		ProtocolVersion:   protocolVersion,
		AgentCapabilities: caps,
		AuthMethods:       []authMethod{},
	}, nil
}

func (a *Agent) newSession(raw json.RawMessage) (any, error) {
	var p newSessionParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, errorf(codeInvalidParams, "session/new: "+err.Error())
	}
	if err := a.checkCWD("session/new", p.CWD); err != nil {
		return nil, err
	}
	a.noteEditorMCP(p.MCPServers)

	s, first := a.open("")
	if first {
		s.history = a.opts.History
	}
	a.register(s)

	// The commands go out *after* the response: the client learns this
	// session's id from it, so a notification sent first names a session it
	// cannot route to. See answer.after.
	return answer{
		result: newSessionResult{SessionID: s.id, Modes: modeState(s.mode)},
		after:  func() { a.postCommands(s.id) },
	}, nil
}

// loadSession reopens a persisted conversation and replays it to the client.
func (a *Agent) loadSession(raw json.RawMessage) (any, error) {
	if a.opts.LoadHistory == nil {
		return nil, errorf(codeMethodNotFound, "Klaudia does not implement session/load")
	}
	var p loadSessionParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, errorf(codeInvalidParams, "session/load: "+err.Error())
	}
	if p.SessionID == "" {
		return nil, errorf(codeInvalidParams, "session/load: sessionId is required")
	}
	if !sessions.ValidID(p.SessionID) {
		return nil, errorf(codeInvalidParams, "session/load: sessionId is not a session id")
	}
	if err := a.checkCWD("session/load", p.CWD); err != nil {
		return nil, err
	}
	a.noteEditorMCP(p.MCPServers)

	// Already open: the client is reconnecting to a live thread rather than
	// reading one off disk. Replay what is in memory — it is strictly newer
	// than the transcript, whose last turn may not be flushed.
	if existing, err := a.session(p.SessionID); err == nil {
		existing.mu.Lock()
		history := existing.history
		mode := existing.mode
		existing.mu.Unlock()
		newTranslator(a, existing.id).replay(history, existing.plan)
		a.postCommands(existing.id)
		return loadSessionResult{Modes: modeState(mode)}, nil
	}

	history, err := a.opts.LoadHistory(p.SessionID)
	if err != nil {
		// invalid_params, not internal: the id came from the client and this is
		// the client's mistake to correct.
		return nil, errorf(codeInvalidParams, "session/load: "+err.Error())
	}

	s, _ := a.open(p.SessionID)
	s.history = history
	a.register(s)

	// Inline, before the response. The spec requires the conversation to have
	// been sent by the time session/load returns, and unlike session/new the
	// client already has the id, so there is nothing to wait for.
	if shown := newTranslator(a, s.id).replay(history, s.plan); shown == 0 && len(history) > 0 {
		a.logf("session/load " + p.SessionID + ": " + itoa(len(history)) +
			" message(s) replayed to nothing — the transcript holds no displayable content")
	}
	a.postCommands(s.id)
	return loadSessionResult{Modes: modeState(s.mode)}, nil
}

func (a *Agent) listSessions(raw json.RawMessage) (any, error) {
	if a.opts.ListSessions == nil {
		return nil, errorf(codeMethodNotFound, "Klaudia does not implement session/list")
	}
	var p listSessionsParams
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, errorf(codeInvalidParams, "session/list: "+err.Error())
		}
	}
	// A cwd filter for somewhere else is answered with nothing rather than
	// refused: "no sessions there" is true, and this process genuinely has none.
	if p.CWD != nil && !samePath(*p.CWD, a.opts.CWD) {
		return listSessionsResult{Sessions: []sessionInfo{}}, nil
	}
	out := []sessionInfo{}
	for _, s := range a.opts.ListSessions() {
		info := sessionInfo{SessionID: s.ID, CWD: a.opts.CWD}
		if s.Title != "" {
			info.Title = &s.Title
		}
		if s.UpdatedAt != "" {
			info.UpdatedAt = &s.UpdatedAt
		}
		out = append(out, info)
	}
	return listSessionsResult{Sessions: out}, nil
}

// closeSession drops a session: any running prompt is cancelled and the
// transcript is closed. An unknown id is not an error — a client closing a
// thread twice, or closing one this process never had, has got what it asked
// for.
func (a *Agent) closeSession(raw json.RawMessage) error {
	var p closeSessionParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return errorf(codeInvalidParams, "session/close: "+err.Error())
	}
	a.mu.Lock()
	s := a.sessions[p.SessionID]
	delete(a.sessions, p.SessionID)
	a.mu.Unlock()
	if s != nil {
		s.close()
	}
	return nil
}

// open builds a session. id is the one the client asked for (session/load) or
// empty to allocate one. first reports that this session took the process's
// seeded identity, which the caller uses to decide about History.
func (a *Agent) open(id string) (s *session, first bool) {
	mode := a.opts.Mode
	if !mode.Valid() {
		mode = permission.ModeAutonomous
	}

	a.mu.Lock()
	if !a.firstUsed {
		a.firstUsed = true
		first = true
		if id == "" {
			id = a.opts.SessionID
		}
	}
	a.mu.Unlock()

	if id == "" {
		newID := a.opts.NewSessionID
		if newID == nil {
			newID = uuid.NewString
		}
		id = newID()
	}
	s = &session{id: id, cwd: a.opts.CWD, mode: mode}
	if a.opts.Transcript != nil {
		s.transcript = a.opts.Transcript(id)
	}
	if a.opts.Plan != nil {
		s.plan = a.opts.Plan(id)
	}
	if s.plan == nil {
		s.plan = plan.New("")
	}
	return s, first
}

func (a *Agent) register(s *session) {
	a.mu.Lock()
	a.sessions[s.id] = s
	a.mu.Unlock()
}

// checkCWD refuses a session rooted anywhere but this process's project.
//
// Refused rather than quietly ignored: the registry, system prompt, sandbox and
// trust zones are all built around the directory Klaudia started in, so a
// session claiming a different root would read and write somewhere the client is
// not looking.
func (a *Agent) checkCWD(method, cwd string) error {
	if !filepath.IsAbs(cwd) {
		return errorf(codeInvalidParams, method+": cwd must be an absolute path")
	}
	if !samePath(cwd, a.opts.CWD) {
		return errorf(codeInvalidParams, method+": this Klaudia process serves "+
			a.opts.CWD+", not "+cwd+". Start Klaudia in the project root the editor has open.")
	}
	return nil
}

func (a *Agent) noteEditorMCP(servers []json.RawMessage) {
	if len(servers) > 0 {
		a.logf("ignoring " + itoa(len(servers)) + " MCP server(s) from the editor: " +
			"Klaudia connects the servers in .mcp.json instead")
	}
}

// postCommands tells a session's client what it may offer as a /command.
// Always sent, even when the list is empty: "this agent has no commands" is
// information, and a client cannot infer it from silence.
func (a *Agent) postCommands(sessionID string) {
	a.update(sessionID, availableCommandsUpdate{
		Kind:     "available_commands_update",
		Commands: commandList(a.opts.Commands),
	})
}

func (a *Agent) setMode(raw json.RawMessage) error {
	var p setModeParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return errorf(codeInvalidParams, "session/set_mode: "+err.Error())
	}
	s, err := a.session(p.SessionID)
	if err != nil {
		return err
	}
	mode := permission.Mode(p.ModeID)
	if !mode.Valid() {
		return errorf(codeInvalidParams, "session/set_mode: unknown mode "+p.ModeID)
	}
	s.mu.Lock()
	s.mode = mode
	s.mu.Unlock()
	// Echo it back. The client already knows what it asked for, but a mode can
	// also change without it asking (ExitPlanMode), so there is one code path
	// that reports the current mode rather than two.
	a.update(s.id, modeUpdate{Kind: "current_mode_update", CurrentModeID: string(mode)})
	return nil
}

func (a *Agent) cancelPrompt(raw json.RawMessage) error {
	var p cancelParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return errorf(codeInvalidParams, "session/cancel: "+err.Error())
	}
	s, err := a.session(p.SessionID)
	if err != nil {
		return err
	}
	s.mu.Lock()
	cancel := s.cancel
	s.cancelled = true
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return nil
}

func (a *Agent) prompt(ctx context.Context, raw json.RawMessage) (any, error) {
	var p promptParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, errorf(codeInvalidParams, "session/prompt: "+err.Error())
	}
	s, err := a.session(p.SessionID)
	if err != nil {
		return nil, err
	}
	text := promptText(p.Prompt)
	if text == "" {
		return nil, errorf(codeInvalidParams, "session/prompt: no text in the prompt")
	}
	// A client's command picker sends the command as ordinary prompt text, so
	// "/review foo.go" arrives here and nothing else would expand it.
	if expanded, ok := expandCommand(a.opts.Commands, text); ok {
		text = expanded
	}

	// One prompt per session, which the spec requires the agent to enforce.
	s.mu.Lock()
	if s.cancel != nil {
		s.mu.Unlock()
		return nil, errorf(codeInvalidRequest, "session/prompt: a prompt is already running in this session")
	}
	s.cancelled = false
	turnCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.mu.Unlock()
	defer func() {
		cancel()
		s.mu.Lock()
		s.cancel = nil
		s.mu.Unlock()
	}()

	// Wait for the process-wide turn slot. Selecting on turnCtx is the point:
	// a cancel that arrives while this prompt is queued ends it here, rather
	// than running a turn the user has already abandoned.
	select {
	case <-turnCtx.Done():
		return promptResult{StopReason: stopCancelled}, nil
	case <-a.turn:
	}
	defer func() { a.turn <- struct{}{} }()

	req := &sessionRequester{agent: a, sessionID: s.id}
	s.mu.Lock()
	history := s.history
	recorder := s.transcript
	s.mu.Unlock()

	tr := newTranslator(a, s.id)

	// streamed records whether any assistant text reached the client. The loop
	// has early-return paths that put their explanation in Result.Text without
	// ever emitting it, and without this the editor shows an empty reply.
	var streamed atomic.Bool

	res, runErr := a.opts.Run(turnCtx, agent.Turn{
		Prompt:  text,
		History: history,
		Emit: func(ev agent.Event) {
			if ev.Type == "assistant" && ev.Text != "" {
				streamed.Store(true)
			}
			tr.event(ev)
		},
		Approver: &approver{req: req, sessionID: s.id},
		Asker:    &asker{req: req, sessionID: s.id},
		Planner: &planner{req: req, sessionID: s.id, approved: func() {
			// Approving a plan means "go and do this", so the session leaves
			// plan mode for autonomous — the same landing spot the TUI uses,
			// and for the same reason: a halfway mode leaves the session asking
			// about every step of a plan the user just approved.
			s.mu.Lock()
			s.mode = permission.ModeAutonomous
			s.mu.Unlock()
			a.update(s.id, modeUpdate{
				Kind:          "current_mode_update",
				CurrentModeID: string(permission.ModeAutonomous),
			})
		}},
		Mode:     s.currentMode,
		Recorder: recorder,
		Plan:     s.plan,
		ReadText: func(ctx context.Context, path string, line, limit int) (string, error) {
			return a.readTextFile(ctx, s.id, path, line, limit)
		},
	})

	if res.Messages != nil {
		s.mu.Lock()
		s.history = res.Messages
		s.mu.Unlock()
	}
	a.postUsage(s.id, res.Messages)

	s.mu.Lock()
	cancelled := s.cancelled
	s.mu.Unlock()
	if cancelled {
		// The spec is explicit: a cancelled turn answers the pending prompt
		// with stopReason "cancelled" rather than an error. The client asked
		// for this, so it is not a failure.
		return promptResult{StopReason: stopCancelled}, nil
	}
	if runErr != nil {
		if errors.Is(runErr, context.Canceled) {
			return promptResult{StopReason: stopCancelled}, nil
		}
		return nil, errorf(codeInternalError, config.Hints.FriendlyError(runErr))
	}

	// Two things leave an editor with nothing to show, and ACP has no status
	// for either.
	//
	// A turn can end cleanly and say nothing — a refusal, the output limit, a
	// context window that no longer fits — so the explanation goes out as text.
	// And the loop has early returns (a hook that blocked the prompt) that put
	// their reason in Result.Text without streaming it, so nothing emitted it.
	// An empty reply is indistinguishable from a bug and was reported as one.
	hadText := !agent.TurnEndedEmpty(res.Text)
	if hadText && !streamed.Load() {
		a.update(s.id, agentMessage(res.Text))
	}
	if note := agent.TurnNote(res.StopReason, hadText); note != "" {
		a.update(s.id, agentMessage(note))
	}
	return promptResult{StopReason: stopReasonFor(res.StopReason)}, nil
}

// session looks a session up by id.
func (a *Agent) session(id string) (*session, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	s, ok := a.sessions[id]
	if !ok {
		return nil, errorf(codeInvalidParams, "unknown session "+id)
	}
	return s, nil
}

// update posts a session/update notification. It satisfies notifier.
func (a *Agent) update(sessionID string, u any) {
	a.conn.notify("session/update", sessionNotification{SessionID: sessionID, Update: u})
}

// postUsage reports how much of the context window the conversation now
// occupies.
//
// Once per turn, from the resulting history, because that is what ACP's
// usage_update means: resident tokens out of the window, not the deltas the
// loop's "usage" events carry. The estimate is the same one the TUI's status
// line shows and autocompact triggers on, so the editor's gauge and Klaudia's
// own agree — a second estimator here would be a second answer to the same
// question.
func (a *Agent) postUsage(sessionID string, history []anthropic.BetaMessageParam) {
	if a.opts.ContextWindow <= 0 || len(history) == 0 {
		return
	}
	a.update(sessionID, usageUpdate{
		Kind: "usage_update",
		Used: compaction.EstimateTokens(history),
		Size: a.opts.ContextWindow,
	})
}

func (a *Agent) logf(msg string) {
	if a.opts.Log != nil {
		a.opts.Log(msg)
	}
}

// sessionRequester binds the outbound client calls to one session, so an ask
// cannot be answered against the wrong conversation.
type sessionRequester struct {
	agent     *Agent
	sessionID string
}

func (r *sessionRequester) update(sessionID string, u any) { r.agent.update(sessionID, u) }

func (r *sessionRequester) requestPermission(ctx context.Context, p requestPermissionParams) (permissionOutcome, error) {
	var res requestPermissionResult
	if err := r.agent.conn.call(ctx, "session/request_permission", p, &res); err != nil {
		return permissionOutcome{}, err
	}
	return res.Outcome, nil
}

// promptText flattens an ACP prompt into the string the agent loop takes.
//
// Resource links and embedded resources are not dropped: a link becomes its
// path so the model can Read it, and an embedded resource's text is inlined,
// which is the whole purpose of an editor attaching the open buffer. Image and
// audio blocks are skipped — promptCapabilities says Klaudia does not accept
// them, so a client that sends one is already outside what it was told.
func promptText(blocks []contentBlock) string {
	var b strings.Builder
	add := func(s string) {
		if s == "" {
			return
		}
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(s)
	}
	for _, blk := range blocks {
		switch blk.Type {
		case "text":
			add(blk.Text)
		case "resource_link":
			name := blk.URI
			if name == "" {
				name = blk.Name
			}
			add(name)
		case "resource":
			if blk.Resource == nil {
				continue
			}
			if blk.Resource.Text != "" {
				add("<" + blk.Resource.URI + ">\n" + blk.Resource.Text + "\n</" + blk.Resource.URI + ">")
			} else {
				add(blk.Resource.URI)
			}
		}
	}
	return strings.TrimSpace(b.String())
}

// modeState describes Klaudia's permission modes for the client's mode picker.
func modeState(current permission.Mode) *sessionModeState {
	st := &sessionModeState{CurrentModeID: string(current)}
	for _, m := range permission.SelectableModes() {
		st.AvailableModes = append(st.AvailableModes, sessionMode{
			ID:          string(m),
			Name:        modeName(m),
			Description: m.Label(),
		})
	}
	return st
}

// modeName is the short label for a mode picker; Mode.Label is the sentence
// that explains it.
func modeName(m permission.Mode) string {
	switch m {
	case permission.ModeAutonomous:
		return "Autonomous"
	case permission.ModePlan:
		return "Plan"
	case permission.ModeBypassPermissions:
		return "Bypass permissions"
	}
	return string(m)
}

// samePath compares two directories as the filesystem sees them. Cleaning the
// strings is not enough on macOS, where the editor's /var and Klaudia's
// /private/var are the same directory spelled differently.
func samePath(a, b string) bool {
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	ra, err1 := filepath.EvalSymlinks(a)
	rb, err2 := filepath.EvalSymlinks(b)
	return err1 == nil && err2 == nil && ra == rb
}

// askSeq numbers the synthetic tool-call ids for asks that have no tool call of
// their own (AskUserQuestion, ExitPlanMode). The client uses the id to match a
// permission request to what it is displaying, so it has to be unique.
var askSeq atomic.Int64

func nextAskID() int64 { return askSeq.Add(1) }

func itoa(n int) string { return strconv.Itoa(n) }
