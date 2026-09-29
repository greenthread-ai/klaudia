package tui

import (
	"strconv"
	"strings"

	"github.com/greenthread-ai/klaudia/internal/permission"
)

// Tab after "/<cmd> " used to fall through to @path completion, which is the
// wrong question for nearly every command that takes an argument: /theme wants
// a theme, /stopjob a job, /trust revoke an approval id. Each entry in
// commandList may now carry a completer that names the candidates for its next
// argument; commands without one keep the @path behaviour.

// argCompleter returns the candidates for the argument being typed. prev holds
// the arguments already complete before it; partial is the word under the
// cursor, which the caller filters on — a completer only needs it to decide
// what kind of word is wanted (a "-flag" versus a name). Completers read
// state the session already holds. They never fetch: Tab has to answer at
// once, and a keystroke that waits on the network is worse than no answer.
type argCompleter func(m *Model, prev []string, partial string) []string

// argCandidatesShown caps the candidate banner, as @path completion does.
const argCandidatesShown = 12

// lookupCommand finds a command-table entry by its exact name.
func lookupCommand(name string) (cmdInfo, bool) {
	for _, c := range commandList {
		if c.name == name {
			return c, true
		}
	}
	return cmdInfo{}, false
}

// completeSlashArg completes the argument of a slash command that has a
// completer, reporting whether it handled the key. It reports false — leaving
// Tab to @path completion — for a command without a completer, and for an
// argument that starts with "@", which is the user asking for a path.
//
// One candidate is filled in with a trailing space, so the next Tab moves on
// to the following argument. Several are listed, and the input is extended to
// their common prefix; when there is no prefix left to add, Tab cycles through
// them instead, the way @path completion does.
func (m *Model) completeSlashArg() bool {
	value := m.input.Value()
	if !strings.HasPrefix(value, "/") {
		return false
	}
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return false
	}
	info, ok := lookupCommand(fields[0])
	if !ok || info.complete == nil {
		return false
	}
	cut := strings.LastIndexAny(value, " \t\n") + 1
	partial := value[cut:]
	if strings.HasPrefix(partial, "@") {
		return false
	}

	if len(m.cycle.hits) > 0 && m.cycle.last == value {
		m.cycle.idx = (m.cycle.idx + 1) % len(m.cycle.hits)
		m.setCompletion(value[:cut] + m.cycle.hits[m.cycle.idx])
		return true
	}

	prev := strings.Fields(value[:cut])[1:]
	hits := filterPrefix(info.complete(m, prev, partial), partial)
	switch {
	case len(hits) == 0:
		// Handled, with nothing to offer: this argument is not a path, so
		// falling through to @path completion would only offer wrong answers.
	case len(hits) == 1:
		m.cycle = completeCycle{}
		m.setCompletion(value[:cut] + hits[0] + " ")
	default:
		m.listCandidates(hits)
		if cp := commonPrefix(hits); len(cp) > len(partial) {
			// Extend to the shared prefix; a further Tab starts the cycle at
			// the first candidate rather than listing them again.
			completed := value[:cut] + cp
			m.cycle = completeCycle{base: partial, hits: hits, idx: -1, last: completed}
			m.setCompletion(completed)
			return true
		}
		m.cycle = completeCycle{base: partial, hits: hits}
		m.setCompletion(value[:cut] + hits[0])
	}
	return true
}

// setCompletion replaces the input with a completed line, remembering it so the
// next Tab can tell "cycle on" from "complete this fresh text".
func (m *Model) setCompletion(completed string) {
	m.cycle.last = completed
	m.input.SetValue(completed)
	m.input.CursorEnd()
}

func (m *Model) listCandidates(hits []string) {
	show := hits
	more := ""
	if len(show) > argCandidatesShown {
		more = "  (+" + strconv.Itoa(len(show)-argCandidatesShown) + " more)"
		show = show[:argCandidatesShown]
	}
	m.appendLine(bannerStyle.Render("candidates: " + strings.Join(show, "  ") + more))
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

// completeModeArg offers the modes the /mode picker offers. The legacy modes
// still parse, but they are not choices any more (see SelectableModes).
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
// runningOnly narrows it to the jobs still running.
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
// "all" joins them only when it means more than one, so a lone job is filled
// in on the first Tab.
func completeStopJobArg(m *Model, prev []string, _ string) []string {
	if len(prev) > 0 {
		return nil
	}
	return withAll(m.jobRefs(true))
}

// withAll appends "all" to a list of two or more refs.
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

// completeTrustArg completes /trust's subcommands, and the live approval ids
// after /trust revoke.
func completeTrustArg(m *Model, prev []string, _ string) []string {
	switch {
	case len(prev) == 0:
		return []string{"upgrade", "observe", "off", "revoke"}
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
