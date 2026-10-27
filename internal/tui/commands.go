package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/greenthread-ai/klaudia/pkg/api"
	"github.com/greenthread-ai/klaudia/pkg/compaction"
	"github.com/greenthread-ai/klaudia/pkg/permission"
)

// cmdInfo describes one slash command — the single source of truth for both
// /help and the type-ahead suggestions, so the two can never drift.
type cmdInfo struct{ name, args, desc string }

var commandList = []cmdInfo{
	{"/help", "", "Show this help"},
	{"/model", "[name]", "Pick a model from the provider (no arg), or set one by alias/ID"},
	{"/effort", "[level]", "Show or set how hard the model works per response (" + api.EffortNames() + "; bare `off` restores the provider default)"},
	{"/theme", "[name]", "Change Markdown render theme (no arg = picker)"},
	{"/mode", "[name]", "Change how Klaudia asks permission (no arg = picker)"},
	{"!<command>", "", "Run a shell command directly; its output becomes context for Klaudia"},
	{"/stop", "", "Ask Klaudia to finish the current step and stop, keeping what it has done"},
	{"/changes", "", "Show the working tree split into your changes and Klaudia's"},
	{"/undo", "", "Undo Klaudia's last change, leaving anything you also touched alone"},
	{"/jobs", "", "List background jobs: what's running, on what port, and where"},
	{"/logs", "[-f|--errors] <job>", "Page a job's log ($PAGER), tail it (-f), or pull just its errors into the conversation"},
	{"/restart", "<job>", "Restart a background job in place, keeping its name and log"},
	{"/stopjob", "<job|agent_N|all>", "Stop a background job (and its process group) or a background agent"},
	{"/trust", "[revoke <id>|revoke all]", "Show what Klaudia may change on this machine, and what it already may"},
	{"/goal", "[run N|stop|text]", "No arg: goal-setting (draft/load a spec). run [N]: iterate to the goal. stop: halt. text: standing reminder"},
	{"/memory", "[add|recent|stale|tag|promote|supersede]", "Show / audit / curate memory; no args views the index"},
	{"/mcp", "", "List MCP servers; reconnect or disconnect them"},
	{"/stats", "", "Show session stats (turns, tokens)"},
	{"/status", "", "Show the current session settings"},
	{"/config", "", "Show resolved provider/model/sandbox settings"},
	{"/agents", "", "List available sub-agent types"},
	{"/context", "", "Show what Klaudia has in context: pinned, changed, active, recently inspected"},
	{"/pin", "[path]", "Keep a file in context every turn (survives compaction)"},
	{"/unpin", "<path>", "Stop pinning a file"},
	{"/forget", "<path>", "Drop a file from the tracked context"},
	{"/compact", "", "Summarize and compact the conversation history now"},
	{"/add-dir", "<path>", "Add a directory to the prompt context"},
	{"/plan", "[run|off]", "Read-only plan mode · run [N]: work the approved plan to done · stop: halt"},
	{"/doctor", "", "Run environment diagnostics"},
	{"/diff", "[args]", "Show git diff of the working tree"},
	{"/commit", "<msg>", "Commit the files Klaudia changed — or what you staged, if anything (asks first)"},
	{"/export", "", "Export the conversation to a Markdown file"},
	{"/last", "[n|list]", "Show a tool output in full (no arg = latest; list = index)"},
	{"/copy", "[target]", "Copy to the clipboard: answer (default) | code [N] | out | all"},
	{"/search", "<query>", "Search the session (--mine, --answers, --tools, --errors; /regex/)"},
	{"/outline", "", "Show a session outline of prompts, tool calls, and errors"},
	{"/show", "<n>", "Show one entry from /search or /outline in full"},
	{"/errors", "[n]", "List the most recent errors"},
	{"/open", "<path:line>", "Open a file reference in $EDITOR (paths from stack traces work)"},
	{"/clear", "", "Clear the screen and conversation history"},
	{"/quit", "", "Exit Klaudia (alias /exit)"},
}

// commandAliases are names handleSlash accepts that commandList does not list,
// because they are second spellings of a command already in it.
var commandAliases = map[string]string{"?": "help", "exit": "quit"}

// IsBuiltinCommand reports whether /name is a built-in slash command, and so
// whether a skill of that name would be shadowed by it.
//
// Derived from commandList — the table that already drives /help and
// type-ahead — rather than being written out again. The hand-kept copy this
// replaced lived in internal/cli and had drifted both ways: it still reserved
// /allow and /deny after the rule model was removed, and had never learned
// about /trust, /undo, /jobs and fifteen others, so a skill named "undo" was
// silently unreachable instead of warned about.
func IsBuiltinCommand(name string) bool {
	name = strings.TrimPrefix(name, "/")
	if _, ok := commandAliases[name]; ok {
		return true
	}
	for _, c := range commandList {
		if strings.TrimPrefix(c.name, "/") == name {
			return true
		}
	}
	return false
}

// slashHelp renders the command reference from commandList, plus the key
// reference rendered from the live bindings. keys is passed in rather than
// rebuilt here because the Return chords depend on config: the help has to
// describe the bindings this session actually installed.
func slashHelp(keys string) string {
	var b strings.Builder
	b.WriteString("Available commands:")
	for _, c := range commandList {
		left := c.name
		if c.args != "" {
			left += " " + c.args
		}
		fmt.Fprintf(&b, "\n  %-16s %s", left, c.desc)
	}
	b.WriteString("\n\n")
	b.WriteString(keys)
	return b.String()
}

// builtinCommands returns the canonical command names for type-ahead.
func builtinCommands() []string {
	out := make([]string, len(commandList))
	for i, c := range commandList {
		out[i] = c.name
	}
	return out
}

// slashSuggestions returns commands (built-ins + skills) that start with the
// current "/partial" token, when the user is typing a command on a fresh line.
func (m *Model) slashSuggestions() []string {
	value := m.input.Value()
	if !strings.HasPrefix(value, "/") || strings.ContainsAny(value, " \t") {
		return nil
	}
	var out []string
	for _, c := range builtinCommands() {
		if strings.HasPrefix(c, value) {
			out = append(out, c)
		}
	}
	if m.sess != nil {
		for _, sk := range m.sess.Skills {
			if name := "/" + sk.Name; strings.HasPrefix(name, value) {
				out = append(out, name)
			}
		}
	}
	sort.Strings(out)
	return out
}

// slashSuggestionLine renders up to 8 type-ahead command suggestions, or "" when
// there is nothing to suggest (or the only match is exactly what's typed).
func (m *Model) slashSuggestionLine() string {
	sug := m.slashSuggestions()
	if len(sug) == 0 || (len(sug) == 1 && sug[0] == m.input.Value()) {
		return ""
	}
	if len(sug) > 8 {
		sug = append(sug[:8], "…")
	}
	return suggestStyle.Render(strings.Join(sug, "  ")) + hintStyle.Render("  (Tab to complete)")
}

// startChoice opens a settings picker: a live dropdown in the bottom region
// that arrows select, typing filters and digits still pick (see choice.go).
func (m *Model) startChoice(title string, items []choiceItem) {
	m.choiceItems = items
	m.choicePrompt = title
	m.choiceFilter = ""
	m.refilterChoices()
	m.setState(stateAwaitingChoice)
}

// currentMode returns the live permission mode, defaulting to ModeAutonomous.
func (m *Model) currentMode() permission.Mode {
	if m.sess != nil && m.sess.PermissionMode != "" {
		return permission.Mode(m.sess.PermissionMode)
	}
	return permission.ModeAutonomous
}

// modeChoices builds the permission-mode picker, marking the current mode.
func (m *Model) modeChoices() []choiceItem {
	cur := permission.Mode(m.sess.PermissionMode)
	items := make([]choiceItem, 0, len(permission.SelectableModes()))
	for _, mode := range permission.SelectableModes() {
		mode := mode
		label := mode.Label()
		if mode == cur {
			label += "  (current)"
		}
		items = append(items, choiceItem{
			label: label,
			apply: func() string {
				m.sess.PermissionMode = string(mode)
				return "Permission mode: " + mode.Label()
			},
		})
	}
	return items
}

// busyGuard reports whether a slash command must be refused because a turn is
// in flight. Display/config commands run fine while the model works, but ones
// that mutate the conversation history, change the run state, or start another
// turn would race the active turn — those call this first. what is the command
// label used in the hint (e.g. "/clear").
func (m *Model) busyGuard(what string) bool {
	if m.state == stateRunning {
		m.appendLine(toolStyle.Render("  " + what + " isn't available while Klaudia is working — press Esc to interrupt first."))
		return true
	}
	return false
}

// formatStats renders the /stats line. When the context limit is known, the
// caller passes the live estimated resident size (via compaction.EstimateTokens
// over current history) and we surface it as "context: ~R/L (P%, source)".
// Unknown limits omit the suffix rather than show a misleading percentage.
// Extracted so the test can pin the format without standing up a Model.
func formatStats(turns int, inTok, outTok int64, resident, ctxLimit int, ctxSource string) string {
	base := fmt.Sprintf("Session: turns=%d  input_tokens=%d  output_tokens=%d", turns, inTok, outTok)
	if ctxLimit <= 0 {
		return base
	}
	pct := float64(resident) / float64(ctxLimit) * 100
	return fmt.Sprintf("%s  context: ~%d/%d (%.0f%%, %s)", base, resident, ctxLimit, pct, ctxSource)
}

// handleSlash dispatches a slash command. Commands run locally and never reach
// the model. Most are safe to run while a turn is in flight; the destructive
// ones guard with busyGuard.
func (m *Model) handleSlash(input string) (tea.Model, tea.Cmd) {
	fields := strings.Fields(input)
	cmd := fields[0]
	args := fields[1:]

	switch cmd {
	case "/help", "/?":
		m.appendLine(bannerStyle.Render(slashHelp(m.keyHints()) + m.skillHelpLines()))
	case "/quit", "/exit":
		return m, tea.Quit
	case "/clear":
		if m.busyGuard("/clear") {
			break
		}
		m.transcript.Reset()
		m.history = nil
		m.pastes.reset()
		m.results.reset()
		m.nav = nil
		// Erase the visible screen, but deliberately not the scrollback: ESC[3J
		// would destroy whatever the user had in the terminal before Klaudia
		// started, and in tmux it wipes the whole pane's history. Earlier output
		// stays scrollable, which is the point of rendering inline.
		m.appendLine(bannerStyle.Render("Cleared conversation. Earlier output remains in terminal scrollback."))
		return m, tea.Sequence(tea.ClearScreen, m.out.drainCmd())
	case "/search":
		return m, m.searchConversation(args)
	case "/outline":
		return m, m.outline()
	case "/show":
		return m, m.showEntry(args)
	case "/errors":
		return m, m.listErrors(args)
	case "/open":
		return m, m.openInEditor(args)
	case "/copy":
		m.appendLine(bannerStyle.Render(m.copyToClipboard(args)))
	case "/last":
		return m, m.showResult(args)
	case "/model":
		if len(args) > 0 {
			// The confirmation was being computed and dropped here, so
			// `/model <id>` was the one path that changed the model and said
			// nothing — including when the id is one the table can't size and
			// the turn drops to the 8192 floor, which is exactly when the user
			// needs to be told.
			m.appendLine(bannerStyle.Render(m.setModel(args[0], 0, 0)))
			break
		}
		cur := m.sess.Model
		if cur == "" {
			cur = m.sess.ResolvedModel // show the concrete default, by name
		}
		if cur == "" {
			cur = "(default)"
		}
		if m.sess.ListModels == nil {
			// Provider can't enumerate — report the current model as before.
			m.appendLine(bannerStyle.Render("Model: " + cur))
			break
		}
		m.appendLine(bannerStyle.Render("Model: " + cur + " — fetching available models…"))
		return m, m.fetchModels()
	case "/effort":
		m.setEffort(args)
	case "/theme":
		if len(args) == 0 {
			if m.state == stateRunning {
				m.appendLine(toolStyle.Render("  use /theme <name> while Klaudia is working (the picker needs an idle session). Names: " + themeNames()))
				break
			}
			m.startChoice("Theme — choose Markdown render colours:", m.themeChoices())
			return m, nil
		}
		theme, ok := lookupTheme(strings.Join(args, " "))
		if !ok {
			m.appendLine(errStyle.Render("unknown theme " + strings.Join(args, " ") + ". Available: " + themeNames()))
			break
		}
		m.setTheme(theme.id)
		m.appendLine(bannerStyle.Render("Theme: " + theme.name))
	case "/goal":
		switch {
		case len(args) == 0:
			if m.busyGuard("/goal") {
				break
			}
			return m.toggleGoalSetting()
		case strings.EqualFold(args[0], "run"):
			if m.busyGuard("/goal run") {
				break
			}
			return m.startGoalLoop(args[1:])
		case strings.EqualFold(args[0], "stop"):
			m.stopLoop()
		case strings.EqualFold(args[0], "clear"):
			m.sess.Goal = ""
			m.appendLine(bannerStyle.Render("Standing goal cleared."))
		default:
			m.sess.Goal = strings.Join(args, " ")
			m.appendLine(bannerStyle.Render("Standing goal set; it will be re-stated each turn:\n" + m.sess.Goal))
		}
	case "/memory":
		// Memory is always an interface value; headless mode gets memory.Disabled()
		// which returns "" for reads and ErrDisabled for writes.
		m.appendLine(m.handleMemoryCommand(args))
	case "/mcp":
		var servers []MCPServerInfo
		if m.sess.MCP != nil {
			servers = m.sess.MCP.Servers()
		}
		if len(servers) == 0 {
			m.appendLine(bannerStyle.Render("No MCP servers configured. Add them in .mcp.json or .klaudia/.mcp.json."))
			break
		}
		var b strings.Builder
		b.WriteString("MCP servers:")
		items := make([]choiceItem, 0, len(servers))
		for _, s := range servers {
			s := s
			status := "● connected"
			if !s.Connected {
				status = "○ disconnected"
			}
			fmt.Fprintf(&b, "\n  %s  %s (%d tools)", status, s.Name, s.Tools)
			if s.Connected {
				items = append(items, choiceItem{label: "Disconnect " + s.Name, apply: func() string {
					if err := m.sess.MCP.Disconnect(s.Name); err != nil {
						return "disconnect failed: " + err.Error()
					}
					return "Disconnected " + s.Name
				}})
			} else {
				items = append(items, choiceItem{label: "Reconnect " + s.Name, apply: func() string {
					if err := m.sess.MCP.Reconnect(s.Name); err != nil {
						return "reconnect failed: " + err.Error()
					}
					return "Reconnected " + s.Name + " (its tools work again this session)"
				}})
			}
		}
		m.appendLine(bannerStyle.Render(b.String()))
		m.startChoice("Manage MCP servers (Esc to leave as-is):", items)
		return m, nil
	case "/stats":
		resident := compaction.EstimateTokens(m.history)
		m.appendLine(bannerStyle.Render(formatStats(m.statTurns, m.statIn, m.statOut, resident, m.sess.ContextWindow, m.sess.ContextWindowSource)))
	case "/status":
		model := m.sess.Model
		if model == "" {
			model = "(default)"
		}
		resume := ""
		if m.sess.SessionID != "" {
			resume = "\nresume: klaudia --resume " + m.sess.SessionID
		}
		m.appendLine(bannerStyle.Render(fmt.Sprintf("model=%s  permissions=%s  messages=%d%s",
			model, m.currentMode().Label(), len(m.history), resume)))
	case "/mode":
		if len(args) > 0 {
			want := permission.Mode(args[0])
			if !want.Valid() {
				m.appendLine(errStyle.Render("unknown mode " + args[0] + ". Try /mode with no argument to pick one."))
				break
			}
			m.sess.PermissionMode = string(want)
			m.appendLine(bannerStyle.Render("Permission mode: " + want.Label()))
			break
		}
		m.startChoice("Permission mode — choose how Klaudia asks before acting:", m.modeChoices())
		return m, nil
	case "/stop":
		if m.state != stateRunning {
			m.appendLine(bannerStyle.Render("Klaudia isn't working on anything."))
			break
		}
		m.steer.requestHalt()
		m.appendLine(bannerStyle.Render(
			"Will stop after the current step and report what's done. Esc to interrupt immediately instead."))
	case "/changes":
		m.changesCommand()
	case "/undo":
		m.undoCommand()
	case "/jobs":
		m.jobsCommand()
	case "/logs":
		if len(args) > 0 && strings.EqualFold(args[0], "stop") {
			if !m.stopFollow() {
				m.appendLine(bannerStyle.Render("not following anything"))
			}
			break
		}
		return m.logsCommand(args)
	case "/restart":
		m.restartCommand(args)
	case "/stopjob":
		m.stopJobCommand(args)
	case "/trust":
		m.trustCommand(args)
	case "/config":
		m.appendLine(bannerStyle.Render(m.renderConfig()))
	case "/agents":
		m.appendLine(bannerStyle.Render(m.renderAgents()))
	case "/context":
		m.appendLine(bannerStyle.Render(m.renderContext()))
	case "/pin":
		m.pinCommand(args, true)
	case "/unpin":
		m.pinCommand(args, false)
	case "/forget":
		m.forgetCommand(args)
	case "/add-dir":
		if len(args) == 0 {
			if len(m.sess.ExtraDirs) == 0 {
				m.appendLine(bannerStyle.Render("No extra directories added. /add-dir <path> to add one."))
			} else {
				m.appendLine(bannerStyle.Render("Extra directories:\n  " + strings.Join(m.sess.ExtraDirs, "\n  ")))
			}
			break
		}
		dir := strings.Join(args, " ")
		m.sess.ExtraDirs = append(m.sess.ExtraDirs, dir)
		m.appendLine(bannerStyle.Render("Added directory (referenced in the prompt context next turn): " + dir))
	case "/compact":
		if m.busyGuard("/compact") {
			break
		}
		if m.sess.Compact == nil {
			m.appendLine(errStyle.Render("compaction is not available"))
			break
		}
		if len(m.history) == 0 {
			m.appendLine(bannerStyle.Render("Nothing to compact yet."))
			break
		}
		m.appendLine(bannerStyle.Render("Compacting conversation…"))
		m.setState(stateRunning)
		go func(hist []anthropic.BetaMessageParam) {
			newHist, summary, err := m.sess.Compact(m.ctx, hist)
			m.events <- compactDoneMsg{history: newHist, summary: summary, err: err}
		}(m.history)
		return m, m.spin.Tick
	case "/plan":
		if len(args) > 0 && strings.EqualFold(args[0], "run") {
			if m.busyGuard("/plan run") {
				break
			}
			return m.runPlan(args[1:])
		}
		if len(args) > 0 && strings.EqualFold(args[0], "stop") {
			m.stopLoop()
			break
		}
		if len(args) > 0 && strings.ToLower(args[0]) == "off" {
			// Autonomous, not a mode that asks per action: leaving plan means
			// getting on with it, and the host gate is what stops a change to
			// this machine either way.
			m.sess.PermissionMode = string(permission.ModeAutonomous)
			m.appendLine(bannerStyle.Render("Left plan mode (autonomous)."))
		} else {
			m.sess.PermissionMode = string(permission.ModePlan)
			m.appendLine(bannerStyle.Render("Entered plan mode: read-only exploration; mutations are blocked. /plan off to leave."))
		}
	case "/doctor":
		if m.sess.Doctor == nil {
			m.appendLine(errStyle.Render("doctor is not available"))
		} else {
			m.appendLine(bannerStyle.Render(m.sess.Doctor()))
		}
	case "/diff":
		out, err := gitOutput(m.sess.CWD, append([]string{"diff"}, args...)...)
		switch {
		case err != nil:
			m.appendLine(errStyle.Render("git diff: " + err.Error()))
		case strings.TrimSpace(out) == "":
			m.appendLine(bannerStyle.Render("No changes."))
		default:
			m.appendLine(out)
		}
	case "/export":
		path, err := m.exportTranscript()
		if err != nil {
			m.appendLine(errStyle.Render("export: " + err.Error()))
		} else {
			m.appendLine(bannerStyle.Render("Exported transcript to " + path))
		}
	case "/commit":
		if m.busyGuard("/commit") {
			break
		}
		if len(args) == 0 {
			m.appendLine(errStyle.Render("usage: /commit <message>"))
			break
		}
		message := strings.Join(args, " ")
		status, err := gitOutput(m.sess.CWD, "status", "--short")
		if err != nil {
			m.appendLine(errStyle.Render("git: " + err.Error()))
			break
		}
		if strings.TrimSpace(status) == "" {
			m.appendLine(bannerStyle.Render("Nothing to commit (working tree clean)."))
			break
		}
		cwd := m.sess.CWD
		// Only Klaudia's own files are stageable. A file it wrote that the user
		// then edited by hand is deliberately left out: the two changes cannot
		// be separated without hunk-level surgery, and sweeping the user's edit
		// into a commit describing Klaudia's work is the bug /commit already
		// stopped doing once. It is listed rather than silently dropped.
		safe := map[string]bool{}
		for _, f := range m.classify(status) {
			if f.Owner == ownerKlaudia {
				safe[f.Path] = true
			}
		}
		plan := planCommit(status, safe)
		if plan.empty() {
			m.appendLine(bannerStyle.Render(plan.describe() +
				"\nStage the changes you want with git add, then run /commit again."))
			break
		}
		m.confirmAction = func() string {
			// A user's own `git add` is a statement about what belongs in the
			// commit; adding to it would be the same mistake as `git add -A`.
			if !plan.Staged {
				args := append([]string{"add", "--"}, plan.Add...)
				if _, err := gitOutput(cwd, args...); err != nil {
					return "git add failed: " + err.Error()
				}
			}
			if out, err := gitOutput(cwd, "commit", "-m", message); err != nil {
				return "git commit failed: " + strings.TrimSpace(out) + " " + err.Error()
			}
			return "Committed."
		}
		m.setState(stateAwaitingConfirm)
		// The y/n lives in the persistent bottom view (stateAwaitingConfirm);
		// scrollback just records the question and what is being committed.
		m.appendLine(askStyle.Render(plan.describe()))
	default:
		// A /<skill> matching a user-defined skill renders its body and submits it
		// as the turn prompt. Built-in commands above always win (a skill that
		// shadows one is unreachable here).
		if sk, ok := m.lookupSkill(strings.TrimPrefix(cmd, "/")); ok {
			if m.busyGuard("/" + sk.Name) {
				break
			}
			rendered := sk.Render(strings.Join(args, " "))
			m.appendLine(bannerStyle.Render("Running skill /" + sk.Name))
			m.setState(stateRunning)
			return m, m.startTurn(rendered)
		}
		m.appendLine(errStyle.Render("Unknown command " + cmd + ". Try /help."))
	}
	return m, nil
}

// renderConfig shows the resolved provider/model/sandbox/permission settings.
func (m *Model) renderConfig() string {
	provider := m.sess.Provider
	if provider == "" {
		provider = "anthropic"
	}
	model := m.sess.ResolvedModel
	if model == "" {
		model = m.sess.Model
	}
	if model == "" {
		model = "(default)"
	}
	sandbox := m.sess.SandboxMode
	if sandbox == "" {
		sandbox = "local"
	}
	effort := string(m.sess.Effort)
	if effort == "" {
		effort = "(provider default)"
	}
	return fmt.Sprintf("Configuration:\n  provider=%s\n  model=%s\n  effort=%s\n  sandbox=%s\n  permissions=%s\n\n  /mode to change permissions · /model to change the model · /effort to change effort",
		provider, model, effort, sandbox, m.currentMode().Label())
}

// renderAgents lists the available sub-agent types (Agent tool subagent_type).
func (m *Model) renderAgents() string {
	if len(m.sess.Agents) == 0 {
		return "No sub-agent types available."
	}
	var b strings.Builder
	b.WriteString("Available sub-agent types (Agent tool):")
	for _, a := range m.sess.Agents {
		fmt.Fprintf(&b, "\n  %-16s %s", a.Name, a.Description)
	}
	return b.String()
}

// renderContext shows the working directory, git branch, and model.

// gitOutput runs `git <args>` in dir and returns combined output. A non-zero
// exit returns the output plus the error so callers can surface git's message.
func gitOutput(dir string, args ...string) (string, error) {
	c := exec.Command("git", args...)
	if dir != "" {
		c.Dir = dir
	}
	out, err := c.CombinedOutput()
	return string(out), err
}

// exportTranscript writes the conversation history to a timestamped Markdown
// file in the working directory and returns its path.
func (m *Model) exportTranscript() (string, error) {
	dir := m.sess.CWD
	if dir == "" {
		dir = "."
	}
	name := fmt.Sprintf("klaudia-export-%s.md", time.Now().Format("20060102-150405"))
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(exportMarkdown(m.history)), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// exportMarkdown renders the conversation history as Markdown (role headers +
// text blocks; tool calls/results are summarized).
func exportMarkdown(history []anthropic.BetaMessageParam) string {
	var b strings.Builder
	b.WriteString("# Klaudia conversation\n\n")
	b.WriteString("_Exported " + time.Now().Format(time.RFC3339) + "_\n")
	for _, msg := range history {
		fmt.Fprintf(&b, "\n## %s\n\n", capitalize(string(msg.Role)))
		for _, block := range msg.Content {
			switch {
			case block.OfText != nil:
				b.WriteString(block.OfText.Text + "\n")
			case block.OfToolUse != nil:
				fmt.Fprintf(&b, "_→ tool: %s_\n", block.OfToolUse.Name)
			case block.OfToolResult != nil:
				b.WriteString("_← tool result_\n")
			}
		}
	}
	return b.String()
}

// lookupSkill finds a loaded skill by name (case-sensitive).
func (m *Model) lookupSkill(name string) (SkillCommand, bool) {
	if m.sess == nil {
		return SkillCommand{}, false
	}
	for _, sk := range m.sess.Skills {
		if sk.Name == name {
			return sk, true
		}
	}
	return SkillCommand{}, false
}

// skillHelpLines renders the user-defined skills section of /help (empty when
// none are loaded).
func (m *Model) skillHelpLines() string {
	if m.sess == nil || len(m.sess.Skills) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\nSkills:")
	for _, sk := range m.sess.Skills {
		desc := sk.Description
		if desc == "" {
			desc = "(user-defined skill)"
		}
		fmt.Fprintf(&b, "\n  /%-14s %s", sk.Name, desc)
	}
	return b.String()
}
