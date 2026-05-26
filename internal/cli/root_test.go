package cli

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread/klaudia/internal/session"
)

func TestResolveResumeIDAutoResumesMostRecentSessionForCWD(t *testing.T) {
	t.Setenv("KLAUDIA_CONFIG_DIR", t.TempDir())
	cwd := "/work/proj"
	otherCWD := "/work/other"

	if w, err := session.NewWriter(otherCWD, "other-session"); err != nil {
		t.Fatal(err)
	} else if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if w, err := session.NewWriter(cwd, "project-session"); err != nil {
		t.Fatal(err)
	} else if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	got, err := resolveResumeID(cwd, options{})
	if err != nil {
		t.Fatal(err)
	}
	if got != "project-session" {
		t.Fatalf("resumeID = %q, want project-session", got)
	}
}

func TestResolveResumeIDStartsNewWhenNoSessionExists(t *testing.T) {
	t.Setenv("KLAUDIA_CONFIG_DIR", t.TempDir())

	got, err := resolveResumeID("/work/proj", options{})
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Fatalf("resumeID = %q, want empty", got)
	}
}

func TestResolveResumeIDNewSessionSkipsAutoResume(t *testing.T) {
	t.Setenv("KLAUDIA_CONFIG_DIR", t.TempDir())
	cwd := "/work/proj"
	w, err := session.NewWriter(cwd, "project-session")
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	got, err := resolveResumeID(cwd, options{newSession: true})
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Fatalf("resumeID = %q, want empty", got)
	}
}

func TestResolveResumeIDExplicitResumeWinsOverAutoResume(t *testing.T) {
	t.Setenv("KLAUDIA_CONFIG_DIR", t.TempDir())
	cwd := "/work/proj"
	w, err := session.NewWriter(cwd, "project-session")
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	got, err := resolveResumeID(cwd, options{resume: "explicit-session"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "explicit-session" {
		t.Fatalf("resumeID = %q, want explicit-session", got)
	}
}

func TestResolveResumeIDNewSessionConflictsWithExplicitResume(t *testing.T) {
	_, err := resolveResumeID("/work/proj", options{newSession: true, resume: "old"})
	if err == nil || !strings.Contains(err.Error(), "--new-session cannot be combined") {
		t.Fatalf("err = %v, want --new-session conflict", err)
	}
}
func TestCompactAndPersistWritesSummaryOnSuccess(t *testing.T) {
	called := false
	_, summary, err := compactAndPersist(context.Background(), nil, func(context.Context, []anthropic.BetaMessageParam) ([]anthropic.BetaMessageParam, string, error) {
		return []anthropic.BetaMessageParam{anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("summary"))}, "summary", nil
	}, func(got string) {
		called = true
		if got != "summary" {
			t.Errorf("summary = %q, want summary", got)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if summary != "summary" {
		t.Errorf("summary = %q, want summary", summary)
	}
	if !called {
		t.Fatal("onSummary was not called")
	}
}

func TestCompactAndPersistSkipsSummaryOnError(t *testing.T) {
	boom := errors.New("boom")
	_, _, err := compactAndPersist(context.Background(), nil, func(context.Context, []anthropic.BetaMessageParam) ([]anthropic.BetaMessageParam, string, error) {
		return nil, "summary", boom
	}, func(string) {
		t.Fatal("onSummary should not be called")
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}
}
