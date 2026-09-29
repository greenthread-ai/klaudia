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

// SummaryPathFor returns the summary file that sits beside a transcript file.
func SummaryPathFor(transcriptPath string) string {
	return strings.TrimSuffix(transcriptPath, ".jsonl") + ".summary.md"
}

// WriteSummary persists a compaction summary for a session, stamped with the
// time and (when available) the current git commit. Last write wins.
func WriteSummary(cwd, sessionID, summary, gitCommit string) error {
	return WriteSummaryAt(SummaryPath(cwd, sessionID), sessionID, summary, gitCommit)
}

// WriteSummaryAt is WriteSummary to an explicit summary file (SummaryPathFor
// a located transcript).
func WriteSummaryAt(path, sessionID, summary, gitCommit string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
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
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

// ReadSummary returns the persisted summary body for a session, or ("", false)
// if none exists. The stamped header line is stripped so the body can seed a
// resumed conversation directly.
func ReadSummary(cwd, sessionID string) (string, bool) {
	var data []byte
	err := os.ErrNotExist
	// A session located outside cwd's project dir keeps its summary beside
	// its transcript; read that one first.
	if tp, ok := Locate(cwd, sessionID); ok {
		data, err = os.ReadFile(SummaryPathFor(tp))
	}
	if err != nil {
		data, err = os.ReadFile(SummaryPath(cwd, sessionID))
	}
	if err != nil {
		data, err = os.ReadFile(legacySummaryPath(cwd, sessionID))
		if err != nil {
			data, err = os.ReadFile(localLegacySummaryPath(cwd, sessionID))
			if err != nil {
				return "", false
			}
		}
	}
	body := stripSummaryHeader(string(data))
	if strings.TrimSpace(body) == "" {
		return "", false
	}
	return body, true
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
