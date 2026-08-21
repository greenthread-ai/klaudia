package tui

import (
	"fmt"
	"strings"
	"testing"
)

func buildSession(m *Model, turns int) {
	for i := 0; i < turns; i++ {
		m.appendLine(fmt.Sprintf("› user question number %d about the codebase", i))
		m.appendLine(strings.Repeat(fmt.Sprintf("assistant paragraph %d with a fair amount of text. ", i), 8))
		m.appendLine("⚙ Bash go test ./...")
		m.appendLine("  ✓ Bash · 1.2s: ok  github.com/example/pkg  0.5s")
	}
	m.out.drainText()
}

// The regression this whole migration exists to prevent: the cost of one
// streamed token must not grow with the length of the session.
func benchStream(b *testing.B, turns int) {
	m := newTestModel()
	m.resize(120, 40)
	buildSession(m, turns)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.appendText("token ")
	}
}

func BenchmarkStreamDelta_10turns(b *testing.B)  { benchStream(b, 10) }
func BenchmarkStreamDelta_50turns(b *testing.B)  { benchStream(b, 50) }
func BenchmarkStreamDelta_200turns(b *testing.B) { benchStream(b, 200) }

func BenchmarkAppendLine_50turns(b *testing.B) {
	m := newTestModel()
	m.resize(120, 40)
	buildSession(m, 50)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.appendLine("  ✓ Bash · 0.1s: done")
	}
}
