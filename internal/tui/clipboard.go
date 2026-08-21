package tui

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/aymanbagabas/go-osc52/v2"
)

// /copy puts text on the system clipboard with OSC 52, which the terminal
// itself executes — so it works over SSH and inside tmux, where a local
// clipboard library would be writing to the wrong machine.
//
// The sequence is emitted through View rather than written to stdout directly.
// termenv.Copy would write to a package-level Output bound to os.Stdout, racing
// the renderer mid-frame; going through the render path keeps it ordered.
//
// Everything /copy offers is taken from a *raw* source — the assistant's
// Markdown, the tool result as the model saw it, the fenced block's body. None
// of it is scraped back out of rendered output, so there are no escapes,
// padding or expanded tabs to undo.

// codeBlock is one fenced block extracted from Markdown source.
type codeBlock struct {
	lang string
	body string
}

// extractFencedBlocks returns the fenced code blocks in md, in order.
func extractFencedBlocks(md string) []codeBlock {
	var out []codeBlock
	var cur []string
	var fence, lang string
	inFence := false

	for _, line := range strings.Split(md, "\n") {
		tok, closing := fenceToken(line)
		switch {
		case !inFence && tok != "":
			inFence, fence = true, tok
			lang = strings.TrimSpace(strings.TrimPrefix(strings.TrimLeft(line, " "), tok))
			cur = nil
		case inFence && tok != "" && closing && tok[0] == fence[0] && len(tok) >= len(fence):
			out = append(out, codeBlock{lang: lang, body: strings.Join(cur, "\n")})
			inFence, fence, lang, cur = false, "", "", nil
		case inFence:
			cur = append(cur, line)
		}
	}
	// An unterminated fence still holds usable code.
	if inFence && len(cur) > 0 {
		out = append(out, codeBlock{lang: lang, body: strings.Join(cur, "\n")})
	}
	return out
}

// clipboardSequence builds the OSC 52 escape for text, adapted for tmux and
// screen, which need it wrapped to pass through to the outer terminal.
func clipboardSequence(text string) string {
	seq := osc52.New(text)
	if strings.HasPrefix(os.Getenv("TERM"), "screen") {
		seq = seq.Screen()
	}
	if os.Getenv("TMUX") != "" {
		seq = seq.Tmux()
	}
	return seq.String()
}

// copyTarget resolves a /copy argument to the text to place on the clipboard.
// The error is user-facing.
func (m *Model) copyTarget(args []string) (string, string, error) {
	what := ""
	if len(args) > 0 {
		what = args[0]
	}
	switch what {
	case "", "answer", "last":
		if strings.TrimSpace(m.lastAssistantText) == "" {
			return "", "", fmt.Errorf("no assistant message yet")
		}
		return m.lastAssistantText, "last answer", nil

	case "code":
		blocks := extractFencedBlocks(m.lastAssistantText)
		if len(blocks) == 0 {
			return "", "", fmt.Errorf("no code block in the last answer")
		}
		idx := len(blocks) - 1 // default: the most recent block
		if len(args) > 1 {
			n, err := strconv.Atoi(args[1])
			if err != nil || n < 1 || n > len(blocks) {
				return "", "", fmt.Errorf("code block %s out of range (1..%d)", args[1], len(blocks))
			}
			idx = n - 1
		}
		label := fmt.Sprintf("code block %d/%d", idx+1, len(blocks))
		if blocks[idx].lang != "" {
			label += " (" + blocks[idx].lang + ")"
		}
		return blocks[idx].body, label, nil

	case "out", "output":
		res, ok := m.results.latest()
		if !ok || strings.TrimSpace(res.content) == "" {
			return "", "", fmt.Errorf("no tool output yet")
		}
		return res.content, fmt.Sprintf("%s output #%d", res.tool, res.seq), nil

	case "all":
		return exportMarkdown(m.history), "whole conversation", nil

	default:
		return "", "", fmt.Errorf("unknown target %q — try: answer | code [N] | out | all", what)
	}
}

// copyToClipboard resolves the target, queues the escape sequence, and returns
// the line to report. OSC 52 gives no acknowledgement, so when it silently does
// nothing the cause is almost always terminal configuration — the confirmation
// names the mechanism so that's diagnosable rather than mysterious.
func (m *Model) copyToClipboard(args []string) string {
	text, label, err := m.copyTarget(args)
	if err != nil {
		return "copy: " + err.Error()
	}
	m.pendingOSC = clipboardSequence(text)

	msg := fmt.Sprintf("Copied %s (%s) to the clipboard via OSC 52.", label, humanBytes(len(text)))
	if os.Getenv("TMUX") != "" {
		msg += " If nothing pastes, tmux needs `set -g set-clipboard on`."
	}
	if len(text) > 64<<10 {
		msg += " Note: some terminals cap OSC 52 payloads around 64–100 KB."
	}
	return msg
}

func humanBytes(n int) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}
