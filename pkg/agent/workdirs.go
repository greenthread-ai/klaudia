package agent

import (
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
)

// Additional working directories (/add-dir) are told in the conversation, not
// the system prompt, for the reason mode.go gives: on a preserved-thinking
// model a changed system prompt invalidates every earlier thinking block, and
// /add-dir used to rewrite it mid-session. The note rides in the next prompt
// message and names only directories the conversation has not been told, so
// each request is the last one with messages appended.
//
// Being in the history also makes the note the record: a resumed session
// reads its directories back from it (AnnouncedDirs), where before they were
// lost, and the system prompt silently changed back. After a compaction or
// /clear the notes are gone with the rest, and the next prompt tells the
// model again.

// dirsHeader opens the note; each directory follows on a "- " line.
const dirsHeader = "# Additional working directories\n" +
	"The user has made these directories available in addition to the working directory:"

// tellDirs adds the note to msg, a prompt message about to be sent and
// recorded, for each of dirs the conversation has not been told.
func tellDirs(history []anthropic.BetaMessageParam, msg *anthropic.BetaMessageParam, dirs []string) {
	if len(dirs) == 0 {
		return
	}
	told := map[string]bool{}
	for _, d := range AnnouncedDirs(history) {
		told[d] = true
	}
	var b strings.Builder
	for _, d := range dirs {
		if !told[d] {
			told[d] = true
			b.WriteString("\n- " + d)
		}
	}
	if b.Len() > 0 {
		msg.Content = append(msg.Content, anthropic.NewBetaTextBlock(dirsHeader+b.String()))
	}
}

// AnnouncedDirs is every directory the conversation has been told about, in
// the order it was told.
func AnnouncedDirs(history []anthropic.BetaMessageParam) []string {
	var out []string
	for _, m := range history {
		if m.Role != anthropic.BetaMessageParamRoleUser {
			continue
		}
		for _, b := range m.Content {
			if b.OfText == nil {
				continue
			}
			rest, ok := strings.CutPrefix(b.OfText.Text, dirsHeader)
			if !ok {
				continue
			}
			for _, line := range strings.Split(rest, "\n") {
				if d, ok := strings.CutPrefix(line, "- "); ok && d != "" {
					out = append(out, d)
				}
			}
		}
	}
	return out
}

// IsContextNote reports whether text is a block the loop added to a user
// message to tell the model about its setting (the permission mode, extra
// directories) rather than something the user wrote.
func IsContextNote(text string) bool {
	text = strings.TrimSpace(text)
	return strings.HasPrefix(text, dirsHeader) || strings.HasPrefix(text, "# Permission mode: ")
}
