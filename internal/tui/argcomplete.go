package tui

import (
	"strconv"
	"strings"

	"github.com/greenthread-ai/klaudia/pkg/permission"
)

// Tab after "/<cmd> " used to fall through to @path completion, which is the
// wrong question for nearly every command that takes an argument: /theme wants
// a theme, /stopjob a job, /trust revoke an approval id. A command listed in
// argCompleters completes its own next argument; the rest keep @path
// completion, as does an argument typed as "@...". (From PR #86.)

// argCompleter returns the candidates for the argument being typed. prev holds
// the arguments already complete before it; partial is the word under the
// cursor, which the caller filters on; a completer needs it only to decide
// what kind of word is wanted (a "-flag" versus a name). Completers read state
// the session already holds and never fetch: Tab has to answer at once.
type argCompleter func(m *Model, prev []string, partial string) []string

var argCompleters = map[string]argCompleter{
	"/model":   completeModelArg,
	"/theme":   completeThemeArg,
	"/mode":    completeModeArg,
	"/logs":    completeLogsArg,
	"/restart": completeJobArg,
	"/stopjob": completeStopJobArg,
	"/last":    completeLastArg,
	"/unpin":   completeUnpinArg,
	"/trust":   completeTrustArg,
}

// argCandidatesShown caps the candidate banner.
const argCandidatesShown = 12

// argCycle is Tab's place among several candidates: when there is no common
// prefix left to add, each Tab moves to the next one.
type argCycle struct {
	hits []string
	idx  int
	last string // the line as Tab left it; any other text starts afresh
}

// completeSlashArg completes the argument of a slash command that has a
// completer, reporting whether it handled the key. It reports false, leaving
// Tab to @path completion, for a command without one and for an argument that
// starts with "@".
//
// One candidate is filled in with a trailing space, so the next Tab moves on
// to the following argument. Several are listed and the input extended to
// their common prefix; with no prefix left to add, Tab cycles through them.
func (m *Model) completeSlashArg() bool {
	value := m.input.Value()
	fields := strings.Fields(value)
	if !strings.HasPrefix(value, "/") || len(fields) == 0 {
		return false
	}
	complete, ok := argCompleters[fields[0]]
	if !ok {
		return false
	}
	cut := strings.LastIndexAny(value, " \t\n") + 1
	if cut == 0 {
		return false // still typing the command itself
	}
	partial := value[cut:]
	if strings.HasPrefix(partial, "@") {
		return false
	}

	if len(m.argCycle.hits) > 0 && m.argCycle.last == value {
		m.argCycle.idx = (m.argCycle.idx + 1) % len(m.argCycle.hits)
		m.setArgCompletion(value[:cut] + m.argCycle.hits[m.argCycle.idx])
		return true
	}

	prev := strings.Fields(value[:cut])[1:]
	hits := filterPrefix(complete(m, prev, partial), partial)
	switch {
	case len(hits) == 0:
		// Handled, with nothing to offer: this argument is not a path, so
		// @path completion would only offer wrong answers.
		m.argCycle = argCycle{}
	case len(hits) == 1:
		m.argCycle = argCycle{}
		m.setArgCompletion(value[:cut] + hits[0] + " ")
	default:
		m.listCandidates(hits)
		if cp := commonPrefix(hits); len(cp) > len(partial) {
			// Extend to the shared prefix; a further Tab starts the cycle at
			// the first candidate rather than listing them again.
			m.argCycle = argCycle{hits: hits, idx: -1}
			m.setArgCompletion(value[:cut] + cp)
			return true
		}
		m.argCycle = argCycle{hits: hits}
		m.setArgCompletion(value[:cut] + hits[0])
	}
	return true
}

// setArgCompletion replaces the input with a completed line, remembering it so
// the next Tab can tell "cycle on" from "complete this fresh text".
func (m *Model) setArgCompletion(completed string) {
	m.argCycle.last = completed
	m.input.SetValue(completed)
	m.input.CursorEnd()
	m.syncInputHeight()
}

func (m *Model) listCandidates(hits []string) {
	show, more := hits, ""
	if len(show) > argCandidatesShown {
		more = "  (+" + strconv.Itoa(len(show)-argCandidatesShown) + " more)"
		show = show[:argCandidatesShown]
	}
	m.appendLine(hintStyle.Render("  candidates: " + strings.Join(show, "  ") + more))
}

// filterPrefix keeps the candidates that start with partial, ignoring case,
// in their original order and without duplicates.
func filterPrefix(cands []string, partial string) []string {
	want := strings.ToLower(partial)
	seen := map[string]bool{}
	var out []string
	for _, c := range cands {
		if c == "" || seen[c] || !strings.HasPrefix(strings.ToLower(c), want) {
			continue
		}
		seen[c] = true
		out = append(out, c)
	}
	return out
}

// completeModelArg offers the ids from the list /model last fetched. Before
// that there is nothing to offer, and a hint says how to get the list rather
// than leaving Tab silently dead.
func completeModelArg(m *Model, prev []string, _ string) []string {
	if len(prev) > 0 {
		return nil
	}
	if len(m.knownModels) == 0 {
		hint := "  no model list yet — /model on its own fetches it, and Tab completes from it after that"
		if m.sess == nil || m.sess.ListModels == nil {
			hint = "  this provider cannot list its models — type the id: /model <id>"
		}
		m.appendLine(hintStyle.Render(hint))
		return nil
	}
	out := make([]string, 0, len(m.knownModels))
	for _, mi := range m.knownModels {
		out = append(out, mi.ID)
	}
	return out
}

func completeThemeArg(_ *Model, prev []string, _ string) []string {
	if len(prev) > 0 {
		return nil
	}
	out := make([]string, 0, len(renderThemes))
	for _, t := range renderThemes {
		out = append(out, t.id)
	}
	return out
}

// completeModeArg offers the modes the /mode picker offers.
func completeModeArg(_ *Model, prev []string, _ string) []string {
	if len(prev) > 0 {
		return nil
	}
	var out []string
	for _, mode := range permission.SelectableModes() {
		out = append(out, string(mode))
	}
	return out
}

// jobRefs names the session's background jobs, by name where they have one.
func (m *Model) jobRefs(runningOnly bool) []string {
	if m.sess == nil || m.sess.Jobs == nil {
		return nil
	}
	var out []string
	for _, j := range m.sess.Jobs.List() {
		if runningOnly && !j.Running {
			continue
		}
		ref := j.Name
		if ref == "" {
			ref = j.ID
		}
		out = append(out, ref)
	}
	return out
}

// completeJobArg completes /restart <job>: any job, since a crashed one is
// exactly what gets restarted.
func completeJobArg(m *Model, prev []string, _ string) []string {
	if len(prev) > 0 {
		return nil
	}
	return m.jobRefs(false)
}

// completeStopJobArg completes /stopjob <job|all> with the jobs still running.
func completeStopJobArg(m *Model, prev []string, _ string) []string {
	if len(prev) > 0 {
		return nil
	}
	return withAll(m.jobRefs(true))
}

// withAll appends "all" to two or more refs, so a lone one fills in on the
// first Tab.
func withAll(refs []string) []string {
	if len(refs) > 1 {
		refs = append(refs, "all")
	}
	return refs
}

// completeLogsArg completes /logs [-f|--errors] <job>. Flags are offered only
// once a "-" is typed, so /logs <Tab> with one job fills that job in.
func completeLogsArg(m *Model, prev []string, partial string) []string {
	used := map[string]bool{}
	for _, a := range prev {
		switch a {
		case "-f", "--follow":
			used["-f"] = true
		case "--errors", "-e":
			used["--errors"] = true
		default:
			return nil // the job is already named
		}
	}
	if strings.HasPrefix(partial, "-") {
		var out []string
		for _, f := range []string{"-f", "--errors"} {
			if !used[f] {
				out = append(out, f)
			}
		}
		return out
	}
	return m.jobRefs(false)
}

// completeLastArg completes /last [n|list] with the held results, newest first.
func completeLastArg(m *Model, prev []string, _ string) []string {
	if len(prev) > 0 {
		return nil
	}
	items := m.results.items
	out := make([]string, 0, len(items)+1)
	for i := len(items) - 1; i >= 0; i-- {
		out = append(out, strconv.Itoa(items[i].seq))
	}
	return append(out, "list")
}

func completeUnpinArg(m *Model, prev []string, _ string) []string {
	if len(prev) > 0 {
		return nil
	}
	return append([]string(nil), m.pinned...)
}

// completeTrustArg completes /trust revoke, then the live approval ids.
func completeTrustArg(m *Model, prev []string, _ string) []string {
	switch {
	case len(prev) == 0:
		return []string{"revoke"}
	case len(prev) == 1 && strings.EqualFold(prev[0], "revoke"):
		if m.sess == nil || m.sess.Trust == nil {
			return nil
		}
		var out []string
		for _, g := range m.sess.Trust.Grants() {
			out = append(out, g.ID)
		}
		return withAll(out)
	}
	return nil
}
