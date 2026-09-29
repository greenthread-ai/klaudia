package tui

import (
	"strings"
	"testing"
)

func TestEffortCommandSetsAndClears(t *testing.T) {
	m := newTestModel()
	m.resize(100, 40)

	m.handleSlash("/effort xhigh")
	if m.sess.Effort != "xhigh" {
		t.Fatalf("effort = %q, want xhigh", m.sess.Effort)
	}
	m.handleSlash("/effort default")
	if m.sess.Effort != "" {
		t.Fatalf("effort = %q, want cleared", m.sess.Effort)
	}
}

func TestEffortCommandRejectsUnknownLevel(t *testing.T) {
	m := newTestModel()
	m.resize(100, 40)
	m.sess.Effort = "high"

	m.handleSlash("/effort extreme")
	if m.sess.Effort != "high" {
		t.Errorf("a bad level changed the setting to %q", m.sess.Effort)
	}
	if !strings.Contains(visibleText(m.transcript.String()), "unknown effort") {
		t.Error("a bad level must be reported")
	}
}

func TestEffortCommandReportsCurrentLevel(t *testing.T) {
	m := newTestModel()
	m.resize(100, 40)

	m.handleSlash("/effort")
	if !strings.Contains(visibleText(m.transcript.String()), "Effort: default") {
		t.Errorf("transcript = %q", visibleText(m.transcript.String()))
	}
}
