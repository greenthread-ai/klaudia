package agent

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread/klaudia/internal/api"
	"github.com/greenthread/klaudia/internal/tools"
)

// blockingProvider blocks in StreamTurn until the context is cancelled, then
// returns the context error — modeling an in-flight model call the user
// interrupts with Esc.
type blockingProvider struct{}

func (blockingProvider) StreamTurn(ctx context.Context, _ anthropic.BetaMessageNewParams, _ api.StreamSink) (anthropic.BetaMessage, error) {
	<-ctx.Done()
	return anthropic.BetaMessage{}, ctx.Err()
}

func TestRunReturnsOnContextCancel(t *testing.T) {
	read, _ := tools.NewRead()
	loop := New(blockingProvider{}, tools.NewRegistry(read))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := loop.Run(ctx, Options{Prompt: "hello", Model: "test"}, nil)
		done <- err
	}()

	// Simulate the Esc interrupt shortly after the turn starts.
	time.AfterFunc(20*time.Millisecond, cancel)

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run error = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after context cancel (interrupt is broken)")
	}
}
