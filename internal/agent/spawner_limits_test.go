package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread-ai/klaudia/pkg/api"
	"github.com/greenthread-ai/klaudia/internal/tools"
)

// paramProvider records the request the loop built, so a test can assert on
// what actually went on the wire rather than on an intermediate field.
type paramProvider struct {
	got anthropic.BetaMessageNewParams
}

func (p *paramProvider) StreamTurn(_ context.Context, params anthropic.BetaMessageNewParams, _ api.StreamSink) (anthropic.BetaMessage, error) {
	p.got = params
	return anthropic.BetaMessage{StopReason: "end_turn"}, nil
}

// A sub-agent runs the parent's model, so re-deriving the output cap from the
// offline table can only lose information: a figure the provider reported, or a
// maxTokens override from the config, is thrown away and the child drops to
// whatever the table says — the 8192 floor for a model the table does not know.
// A sub-agent writing a large file is exactly where 8192 truncates mid-call,
// which is the failure this whole change came from. The parent's resolved
// sizing has to reach the child.
func TestSpawnedChildInheritsResolvedLimits(t *testing.T) {
	// A model id no table knows, so the fallback path is the 8192 floor and the
	// two cases are far apart enough to be unambiguous. Not a real Claude id on
	// purpose: every one of those is in the table, and adding the next
	// generation would silently turn this test into a no-op.
	const unlistedModel = "unlisted-test-model"
	if got := api.MaxOutputTokens(unlistedModel); got != api.DefaultMaxOutputTokens {
		t.Fatalf("fixture no longer unlisted: MaxOutputTokens(%q) = %d, want the %d floor",
			unlistedModel, got, api.DefaultMaxOutputTokens)
	}

	tests := []struct {
		name   string
		limits api.ModelLimits
		want   int64
	}{
		{
			name:   "the parent's resolved cap reaches the child's request",
			limits: api.ModelLimits{MaxOutputTokens: 128000, ContextWindow: 1_000_000},
			want:   128000,
		},
		{
			name:   "a config override is inherited too, even below the floor",
			limits: api.ModelLimits{MaxOutputTokens: 4096, ContextWindow: 200_000},
			want:   4096,
		},
		{
			// A Spawner wired without WithLimits must still behave: the zero
			// value means "nothing resolved", not "cap of zero".
			name: "unset limits fall back to the table lookup",
			want: api.DefaultMaxOutputTokens,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := &paramProvider{}
			read, err := tools.NewRead()
			if err != nil {
				t.Fatal(err)
			}
			s := newTestSpawner(provider, tools.NewRegistry(read), unlistedModel,
				bypassPerm(), nil, 1).WithWorkingDir(t.TempDir())
			if tt.limits != (api.ModelLimits{}) {
				s = s.WithLimits(tt.limits)
			}
			if _, err := s.Spawn(context.Background(), "Explore", "look around", nil); err != nil {
				t.Fatalf("Spawn: %v", err)
			}
			if provider.got.MaxTokens != tt.want {
				t.Errorf("child requested max_tokens=%d, want %d", provider.got.MaxTokens, tt.want)
			}
		})
	}
}

// A child's system prompt is its role followed by the host's shared context,
// built for the child's own working directory.
func TestSpawnedChildGetsSharedContext(t *testing.T) {
	provider := &paramProvider{}
	read, err := tools.NewRead()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	var gotDir string
	s := newTestSpawner(provider, tools.NewRegistry(read), "unlisted-test-model", bypassPerm(), nil, 1).
		WithWorkingDir(dir).
		WithContext(func(d string) string { gotDir = d; return "\n\nSHARED CONTEXT" })
	if _, err := s.Spawn(context.Background(), "Explore", "look around", nil); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	var system string
	for _, b := range provider.got.System {
		system += b.Text
	}
	if !strings.HasPrefix(system, "You are a read-only exploration agent") || !strings.Contains(system, "SHARED CONTEXT") {
		t.Errorf("child system prompt = %q, want the role then the shared context", system)
	}
	if gotDir != dir {
		t.Errorf("context built for %q, want the child's dir %q", gotDir, dir)
	}
}
