package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread-ai/klaudia/internal/memory"
)

func recapUser(s string) anthropic.BetaMessageParam {
	return anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock(s))
}

func recapAssistant(s string) anthropic.BetaMessageParam {
	return anthropic.BetaMessageParam{Role: anthropic.BetaMessageParamRoleAssistant,
		Content: []anthropic.BetaContentBlockParamUnion{anthropic.NewBetaTextBlock(s)}}
}

func recapTool(name string) anthropic.BetaMessageParam {
	return anthropic.BetaMessageParam{Role: anthropic.BetaMessageParamRoleAssistant,
		Content: []anthropic.BetaContentBlockParamUnion{{OfToolUse: &anthropic.BetaToolUseBlockParam{ID: "t1", Name: name, Input: map[string]any{}}}}}
}

func recapToolResult() anthropic.BetaMessageParam {
	return anthropic.NewBetaUserMessage(anthropic.NewBetaToolResultBlock("t1", "TOOL-OUTPUT", false))
}

func started(history []anthropic.BetaMessageParam) string {
	m := New(context.Background(), nil, history, &Session{Memory: memory.Disabled()})
	return m.transcript.String()
}

func TestResumedSessionRecapsTheConversation(t *testing.T) {
	out := started([]anthropic.BetaMessageParam{
		recapUser("what is on the disk?"), recapTool("Glob"), recapToolResult(),
		recapAssistant("Two files: a.go and b.go."),
	})
	for _, want := range []string{"what is on the disk?", "⏺ Glob", "Two files: a.go and b.go.", "── resumed ──"} {
		if !strings.Contains(out, want) {
			t.Errorf("recap is missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "TOOL-OUTPUT") {
		t.Errorf("tool results are not recapped:\n%s", out)
	}
}

func TestFreshSessionHasNoRecap(t *testing.T) {
	if out := started(nil); strings.Contains(out, "resumed") {
		t.Errorf("a fresh session printed a recap:\n%s", out)
	}
}

func TestRecapShowsTheTailFromAPrompt(t *testing.T) {
	var h []anthropic.BetaMessageParam
	for i := 0; i < 10; i++ {
		h = append(h, recapUser(fmt.Sprintf("question %d", i)), recapTool("Read"), recapToolResult(), recapAssistant(fmt.Sprintf("answer %d", i)))
	}
	out := started(h)
	if strings.Contains(out, "question 0") || !strings.Contains(out, "question 9") {
		t.Errorf("recap should cover only the latest messages:\n%s", out)
	}
	if !strings.Contains(out, "earlier message(s)") {
		t.Errorf("recap should say what it left out:\n%s", out)
	}
	// It opens on a prompt, never on an orphaned tool call or answer.
	idx := strings.Index(out, "earlier message(s)")
	rest := out[idx:]
	if first := strings.Index(rest, "› "); first < 0 || strings.Index(rest, "answer") < first {
		t.Errorf("recap should begin at a prompt:\n%s", rest)
	}
}

func TestSummaryResumeSaysSo(t *testing.T) {
	out := started([]anthropic.BetaMessageParam{recapUser(summarySeed + "\n\nwe fixed the build")})
	if !strings.Contains(out, "compacted summary") || strings.Contains(out, "we fixed the build") {
		t.Errorf("a summary resume should say so, not print the summary as a prompt:\n%s", out)
	}
}

func TestClipLongBlocks(t *testing.T) {
	long := strings.Repeat("line\n", resumeRecapLines+5)
	if got := clip(long); strings.Count(got, "\n") != resumeRecapLines || !strings.HasSuffix(got, "…") {
		t.Errorf("clip = %q", got)
	}
}
