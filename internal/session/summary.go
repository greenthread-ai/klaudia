package session

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SummaryPath returns the summary file path for a session, stored alongside
// the transcript in the per-project session directory.
func SummaryPath(cwd, sessionID string) string {
	return filepath.Join(Dir(cwd), sessionID+".summary.md")
}

func legacySummaryPath(cwd, sessionID string) string {
	return filepath.Join(legacyDir(cwd), sessionID+".summary.md")
}

func localLegacySummaryPath(cwd, sessionID string) string {
	return filepath.Join(cwd, ".klaudia", "sessions", sessionID+".summary.md")
}

// WriteSummary persists a compaction summary for a session, stamped with the
// time and (when available) the current git commit. Last write wins.
func WriteSummary(cwd, sessionID, summary, gitCommit string) error {
	if err := os.MkdirAll(filepath.Dir(SummaryPath(cwd, sessionID)), 0o755); err != nil {
		return err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# Session summary %s\n\n", sessionID)
	fmt.Fprintf(&b, "_Compacted %s", Now())
	if gitCommit != "" {
		fmt.Fprintf(&b, " at %s", gitCommit)
	}
	b.WriteString("_\n\n")
	b.WriteString(strings.TrimSpace(summary))
	b.WriteString("\n")
	return os.WriteFile(SummaryPath(cwd, sessionID), []byte(b.String()), 0o644)
}

// ReadSummary returns the persisted summary body for a session, or ("", false)
// if none exists. The stamped header line is stripped so the body can seed a
// resumed conversation directly.
func ReadSummary(cwd, sessionID string) (string, bool) {
	body, _, ok := ReadSummaryStamped(cwd, sessionID)
	return body, ok
}

// ReadSummaryStamped is ReadSummary plus the "_Compacted <ts>_" stamp, which
// is where a transcript without boundary lines is cut on resume. at is "" for
// a file with no stamp.
func ReadSummaryStamped(cwd, sessionID string) (body, at string, ok bool) {
	data, err := os.ReadFile(SummaryPath(cwd, sessionID))
	if err != nil {
		data, err = os.ReadFile(legacySummaryPath(cwd, sessionID))
		if err != nil {
			data, err = os.ReadFile(localLegacySummaryPath(cwd, sessionID))
			if err != nil {
				return "", "", false
			}
		}
	}
	body = stripSummaryHeader(string(data))
	if strings.TrimSpace(body) == "" {
		return "", "", false
	}
	return body, summaryStamp(string(data)), true
}

// summaryStamp extracts the timestamp from WriteSummary's header line,
// "_Compacted <ts>[ at <commit>]_".
func summaryStamp(s string) string {
	for _, ln := range strings.Split(s, "\n") {
		t := strings.TrimSpace(ln)
		if !strings.HasPrefix(t, "_Compacted ") {
			continue
		}
		f := strings.Fields(strings.Trim(strings.TrimPrefix(t, "_Compacted "), "_"))
		if len(f) > 0 {
			return f[0]
		}
	}
	return ""
}

// stripSummaryHeader removes the leading "# Session summary …" + "_Compacted …_"
// lines, returning just the summary body.
func stripSummaryHeader(s string) string {
	lines := strings.Split(s, "\n")
	var out []string
	skipping := true
	for _, ln := range lines {
		if skipping {
			t := strings.TrimSpace(ln)
			if t == "" || strings.HasPrefix(t, "# Session summary") || strings.HasPrefix(t, "_Compacted") {
				continue
			}
			skipping = false
		}
		out = append(out, ln)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}
