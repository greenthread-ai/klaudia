package tui

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/greenthread-ai/klaudia/internal/memory"
)

// handleMemoryCommand dispatches /memory subcommands to the same Store ops
// the LLM-facing Memory tool uses. Splitting it out of the main slash
// switch keeps the handler readable as the surface grew past add/view.
//
// Subcommands:
//
//	/memory                     view the index (default)
//	/memory add <note>          append a session bullet
//	/memory recent [duration]   newest first; default window 7d
//	/memory stale  [duration]   oldest first; default threshold 30d
//	/memory tag <tag>           filter by frontmatter tag
//	/memory promote <name>      copy detail-note body to KNOWLEDGE.md
//	/memory supersede <old> <new>   mark old as superseded by new
func (m *Model) handleMemoryCommand(args []string) string {
	if len(args) == 0 {
		idx, _ := m.sess.Memory.Index()
		if strings.TrimSpace(idx) == "" {
			return bannerStyle.Render("No memory yet. /memory add <note> to save one. /memory recent / stale to audit aged context.")
		}
		return bannerStyle.Render(strings.TrimSpace(idx))
	}
	switch strings.ToLower(args[0]) {
	case "add":
		note := strings.TrimSpace(strings.TrimPrefix(strings.Join(args, " "), args[0]))
		if err := m.sess.Memory.Add(note); err != nil {
			if errors.Is(err, memory.ErrDisabled) {
				return errStyle.Render("memory is not available in this run")
			}
			return errStyle.Render("memory: " + err.Error())
		}
		return bannerStyle.Render("Saved to memory.")
	case "recent":
		window := 7 * 24 * time.Hour
		if len(args) > 1 {
			d, err := parseUserDuration(args[1])
			if err != nil {
				return errStyle.Render("/memory recent: " + err.Error())
			}
			window = d
		}
		entries, err := m.sess.Memory.Recent(window)
		if err != nil {
			return errStyle.Render("/memory recent: " + err.Error())
		}
		return bannerStyle.Render(renderEntriesList(entries, "recent", window))
	case "stale":
		threshold := 30 * 24 * time.Hour
		if len(args) > 1 {
			d, err := parseUserDuration(args[1])
			if err != nil {
				return errStyle.Render("/memory stale: " + err.Error())
			}
			threshold = d
		}
		entries, err := m.sess.Memory.Stale(threshold)
		if err != nil {
			return errStyle.Render("/memory stale: " + err.Error())
		}
		return bannerStyle.Render(renderEntriesList(entries, "stale", threshold))
	case "tag":
		if len(args) < 2 {
			return errStyle.Render("/memory tag <name> — provide a tag to filter by")
		}
		entries, err := m.sess.Memory.ByTag(args[1])
		if err != nil {
			return errStyle.Render("/memory tag: " + err.Error())
		}
		return bannerStyle.Render(renderEntriesList(entries, "tag:"+args[1], 0))
	case "promote":
		if len(args) < 2 {
			return errStyle.Render("/memory promote <name> — provide a note name (without .md)")
		}
		if err := m.sess.Memory.Promote(args[1]); err != nil {
			return errStyle.Render("/memory promote: " + err.Error())
		}
		return bannerStyle.Render("Promoted " + args[1] + " to KNOWLEDGE.md; source marked superseded.")
	case "supersede":
		if len(args) < 3 {
			return errStyle.Render("/memory supersede <old> <new> — provide both note names")
		}
		if err := m.sess.Memory.Supersede(args[1], args[2]); err != nil {
			return errStyle.Render("/memory supersede: " + err.Error())
		}
		return bannerStyle.Render("Marked " + args[1] + " as superseded by " + args[2] + ".")
	}
	return errStyle.Render("/memory: unknown subcommand " + args[0] +
		" (try: add, recent, stale, tag, promote, supersede)")
}

// parseUserDuration mirrors the tool's parseMemoryDuration so /memory recent
// 7d and {operation:"recent", within:"7d"} accept the same input. Kept
// duplicate-but-tiny rather than importing across the tools/tui boundary.
func parseUserDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if strings.HasSuffix(s, "d") {
		var days int
		if _, err := fmt.Sscanf(strings.TrimSuffix(s, "d"), "%d", &days); err != nil {
			return 0, fmt.Errorf("invalid duration %q (try 7d, 24h, 1h30m)", s)
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}
	return time.ParseDuration(s)
}

// renderEntriesList formats the Recent/Stale/ByTag result for display.
// Mirrors the shape used by the tool's formatEntries — a bullet list with
// a humanized "Nd ago" annotation — so users see the same data whether
// they typed /memory recent or the agent reached for the same op via the
// Memory tool.
func renderEntriesList(entries []memory.Entry, label string, window time.Duration) string {
	if len(entries) == 0 {
		switch {
		case window > 0 && label == "recent":
			return "No memory notes touched within " + humanizeDur(window) + "."
		case window > 0 && label == "stale":
			return "No memory notes older than " + humanizeDur(window) + "."
		default:
			return "No memory notes matched " + label + "."
		}
	}
	var b strings.Builder
	b.WriteString(label)
	if window > 0 {
		b.WriteString(" (window: " + humanizeDur(window) + ")")
	}
	b.WriteByte('\n')
	now := time.Now()
	for _, e := range entries {
		b.WriteString("  · ")
		b.WriteString(e.Name)
		if e.Title != "" {
			b.WriteString(" — " + e.Title)
		}
		b.WriteString(" (" + humanizeDur(now.Sub(e.Updated)) + " ago)\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func humanizeDur(d time.Duration) string {
	switch {
	case d >= 24*time.Hour:
		return fmt.Sprintf("%dd", int(d/(24*time.Hour)))
	case d >= time.Hour:
		return fmt.Sprintf("%dh", int(d/time.Hour))
	case d >= time.Minute:
		return fmt.Sprintf("%dm", int(d/time.Minute))
	default:
		return fmt.Sprintf("%ds", int(d/time.Second))
	}
}
