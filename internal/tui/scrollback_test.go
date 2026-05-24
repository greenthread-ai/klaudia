package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestPageKeysScrollConversation(t *testing.T) {
	m := newScrollableTestModel(t)
	m.vp.GotoBottom()
	bottom := m.vp.YOffset
	if bottom == 0 {
		t.Fatal("expected scrollable content")
	}

	model, cmd := m.onKey(tea.KeyMsg{Type: tea.KeyPgUp})
	if cmd != nil {
		t.Fatal("page up returned unexpected command")
	}
	m = model.(*Model)
	if m.vp.YOffset >= bottom {
		t.Fatalf("page up offset = %d, want less than %d", m.vp.YOffset, bottom)
	}

	model, cmd = m.onKey(tea.KeyMsg{Type: tea.KeyPgDown})
	if cmd != nil {
		t.Fatal("page down returned unexpected command")
	}
	m = model.(*Model)
	if m.vp.YOffset != bottom {
		t.Fatalf("page down offset = %d, want %d", m.vp.YOffset, bottom)
	}
}

func TestMouseWheelScrollsConversation(t *testing.T) {
	m := newScrollableTestModel(t)
	m.vp.GotoBottom()
	bottom := m.vp.YOffset
	if bottom == 0 {
		t.Fatal("expected scrollable content")
	}

	model, cmd := m.onMouse(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelUp})
	if cmd != nil {
		t.Fatal("wheel up returned unexpected command")
	}
	m = model.(*Model)
	if m.vp.YOffset >= bottom {
		t.Fatalf("wheel up offset = %d, want less than %d", m.vp.YOffset, bottom)
	}

	model, cmd = m.onMouse(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown})
	if cmd != nil {
		t.Fatal("wheel down returned unexpected command")
	}
	m = model.(*Model)
	if m.vp.YOffset != bottom {
		t.Fatalf("wheel down offset = %d, want %d", m.vp.YOffset, bottom)
	}
}

func TestSyncViewportPreservesScrollbackPosition(t *testing.T) {
	m := newScrollableTestModel(t)
	m.vp.GotoBottom()
	m.vp.PageUp()
	offset := m.vp.YOffset
	if offset == 0 {
		t.Fatal("expected page up to move within scrollback")
	}

	m.appendLine("new output while reading history")
	if m.vp.YOffset != offset {
		t.Fatalf("offset after append = %d, want %d", m.vp.YOffset, offset)
	}
}

func TestSyncViewportFollowsWhenAtBottom(t *testing.T) {
	m := newScrollableTestModel(t)
	m.vp.GotoBottom()

	m.appendLine("new output at bottom")
	if !m.vp.AtBottom() {
		t.Fatalf("expected viewport to stay at bottom, offset=%d", m.vp.YOffset)
	}
}

func newScrollableTestModel(t *testing.T) *Model {
	t.Helper()
	m := newTestModel()
	m.resize(40, 8)
	var b strings.Builder
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&b, "line %02d\n", i)
	}
	m.appendLine(strings.TrimRight(b.String(), "\n"))
	return m
}
