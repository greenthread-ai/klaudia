package cli

import (
	"context"
	"errors"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
)

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
