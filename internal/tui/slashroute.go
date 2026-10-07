package tui

import (
	"sort"
	"strings"
)

// slashAliases are the names handleSlash accepts that /help does not list.
// They count as commands when deciding whether a "/…" line is a command, but
// are never offered as a did-you-mean suggestion.
var slashAliases = []string{"/?", "/exit", "/allow", "/deny"}

// isSlashCommand reports whether name (with its leading "/") is a built-in
// command, an alias of one, or a loaded skill.
func (m *Model) isSlashCommand(name string) bool {
	for _, c := range commandList {
		if c.name == name {
			return true
		}
	}
	for _, a := range slashAliases {
		if a == name {
			return true
		}
	}
	_, ok := m.lookupSkill(strings.TrimPrefix(name, "/"))
	return ok
}

// slashAsPrompt decides whether a submitted line that starts with "/" is really
// a prompt. It returns the text to send and true when it is; false means the
// line goes to handleSlash as before.
//
// A path is the common case: "/etc/nginx/nginx.conf fails to parse" used to be
// answered with "Unknown command". The rule, in order:
//
//   - "//…" is an explicit escape: the line is sent with one slash removed.
//   - A first word that names a command or skill is a command, whatever follows.
//   - Otherwise a first word with a second "/" (a path), or one followed by more
//     text, is a prompt.
//   - A lone unknown "/word" is still a command, so handleSlash can say it is
//     unknown and suggest the nearest one — a typo'd "/modle" should not be
//     sent to the model as a one-word prompt.
func (m *Model) slashAsPrompt(in inputText) (inputText, bool) {
	if !strings.HasPrefix(in.Display, "/") {
		return in, false
	}
	if strings.HasPrefix(in.Display, "//") {
		return inputText{
			Display: in.Display[1:],
			Prompt:  strings.TrimPrefix(in.Prompt, "/"),
		}, true
	}
	fields := strings.Fields(in.Display)
	first := fields[0]
	if m.isSlashCommand(first) {
		return in, false
	}
	if strings.Count(first, "/") > 1 || len(fields) > 1 {
		return in, true
	}
	return in, false
}

// unknownSlashMessage is the error for a /word that is neither a command nor a
// skill, with the nearest commands when any is close enough to be a typo.
func (m *Model) unknownSlashMessage(cmd string) string {
	msg := "Unknown command " + cmd + "."
	if near := m.nearestSlashCommands(cmd); len(near) > 0 {
		msg += " Did you mean " + strings.Join(near, " or ") + "?"
	}
	return msg + " Try /help, or start with // to send it as a message."
}

// nearestSlashCommands returns the listed commands and skills closest to cmd by
// edit distance — at most three, in alphabetical order — or nil when nothing
// is within two edits. Ties are all returned: "/modle" is one edit from both
// /mode and /model, and picking one would be a guess.
func (m *Model) nearestSlashCommands(cmd string) []string {
	var names []string
	for _, c := range commandList {
		if strings.HasPrefix(c.name, "/") {
			names = append(names, c.name)
		}
	}
	if m.sess != nil {
		for _, sk := range m.sess.Skills {
			names = append(names, "/"+sk.Name)
		}
	}
	sort.Strings(names)
	var best []string
	bestDist := 3
	for _, n := range names {
		switch d := editDistance(cmd, n); {
		case d < bestDist:
			best, bestDist = []string{n}, d
		case d == bestDist:
			best = append(best, n)
		}
	}
	if len(best) > 3 {
		best = best[:3]
	}
	return best
}

// editDistance is the optimal-string-alignment distance between a and b, by
// rune: Levenshtein plus adjacent transpositions, the commonest typing slip.
func editDistance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	d := make([][]int, len(ra)+1)
	for i := range d {
		d[i] = make([]int, len(rb)+1)
		d[i][0] = i
	}
	for j := range d[0] {
		d[0][j] = j
	}
	for i := 1; i <= len(ra); i++ {
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			d[i][j] = min(d[i-1][j]+1, d[i][j-1]+1, d[i-1][j-1]+cost)
			if i > 1 && j > 1 && ra[i-1] == rb[j-2] && ra[i-2] == rb[j-1] {
				d[i][j] = min(d[i][j], d[i-2][j-2]+1)
			}
		}
	}
	return d[len(ra)][len(rb)]
}
