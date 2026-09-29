package cli

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

func TestResolvePrintPromptReadsPipedStdinWhenNoPrompt(t *testing.T) {
	got, err := resolvePrintPrompt("", strings.NewReader("what is 2+2?\n"), time.Second, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if got != "what is 2+2?" {
		t.Fatalf("prompt = %q", got)
	}
}

func TestResolvePrintPromptAppendsPipedStdinToPrompt(t *testing.T) {
	got, err := resolvePrintPrompt("review this", strings.NewReader("diff --git a b\n"), time.Second, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if got != "review this\n\ndiff --git a b" {
		t.Fatalf("prompt = %q", got)
	}
}

func TestResolvePrintPromptKeepsPromptWhenStdinEmpty(t *testing.T) {
	got, err := resolvePrintPrompt("hello", strings.NewReader(""), time.Second, io.Discard)
	if err != nil || got != "hello" {
		t.Fatalf("prompt = %q, err = %v", got, err)
	}
}

func TestResolvePrintPromptRefusesEmptyPrompt(t *testing.T) {
	for _, in := range []string{"", "  \n\n"} {
		_, err := resolvePrintPrompt("", strings.NewReader(in), time.Second, io.Discard)
		if !isUsageError(err) {
			t.Fatalf("stdin %q: err = %v, want a usage error", in, err)
		}
	}
}

func TestResolvePrintPromptIgnoresTerminalLikeStdin(t *testing.T) {
	devnull, err := os.Open(os.DevNull)
	if err != nil {
		t.Skip(err)
	}
	defer devnull.Close()
	if got, err := resolvePrintPrompt("hi", devnull, time.Second, io.Discard); err != nil || got != "hi" {
		t.Fatalf("prompt = %q, err = %v", got, err)
	}
	if _, err := resolvePrintPrompt("", devnull, time.Second, io.Discard); !isUsageError(err) {
		t.Fatalf("err = %v, want a usage error", err)
	}
}

// A caller that leaves a stdin pipe open and silent must not hang a run that
// already has its prompt.
func TestResolvePrintPromptDoesNotWaitForeverOnSilentPipe(t *testing.T) {
	pr, pw := io.Pipe()
	defer pw.Close()
	var warn bytes.Buffer
	start := time.Now()
	got, err := resolvePrintPrompt("hello", pr, 50*time.Millisecond, &warn)
	if err != nil || got != "hello" {
		t.Fatalf("prompt = %q, err = %v", got, err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("waited far longer than the limit")
	}
	if !strings.Contains(warn.String(), "No stdin data") {
		t.Fatalf("warning = %q", warn.String())
	}
}

func TestPrintWithNoPromptIsUsageError(t *testing.T) {
	cmd := NewRootCommand()
	cmd.SetArgs([]string{"-p"})
	cmd.SetIn(strings.NewReader(""))
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	if err := cmd.Execute(); !isUsageError(err) {
		t.Fatalf("err = %v, want a usage error", err)
	}
}
