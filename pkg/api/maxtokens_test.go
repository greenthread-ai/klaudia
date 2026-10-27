package api

import "testing"

func TestMaxOutputTokens(t *testing.T) {
	tests := []struct {
		name  string
		model string
		want  int
	}{
		// The 5.5 generation. Absent from this table until 2026-10, which is
		// how an Opus 5.5 session ran at the 8192 floor — a 15x under-request
		// that truncated an ordinary large Write mid-arguments.
		{name: "opus 5.5 gets its real 128k cap", model: "claude-opus-5-5", want: 128000},
		{name: "sonnet 5.5", model: "claude-sonnet-5-5", want: 128000},
		{name: "haiku 5.5 is 1M-context, so 128k out — not the 4.5 haiku's 64k", model: "claude-haiku-5-5", want: 128000},
		{name: "fable 5.1", model: "claude-fable-5-1", want: 128000},

		// Aliases resolve before the lookup, so `--model opus` must not land on
		// the floor either.
		{name: "the opus alias resolves to a known model", model: "opus", want: 128000},
		{name: "the haiku alias now points at a 1M model", model: "haiku", want: 128000},
		{name: "empty model means DefaultModel", model: "", want: 128000},

		// 1M-context models → 128k output.
		{name: "opus 5", model: "claude-opus-5", want: 128000},
		{name: "opus 4.8", model: "claude-opus-4-8", want: 128000},
		{name: "sonnet 5", model: "claude-sonnet-5", want: 128000},
		{name: "fable 5", model: "claude-fable-5", want: 128000},
		{name: "sonnet 4.6", model: "claude-sonnet-4-6", want: 128000},

		// 200k-context models → 64k.
		{name: "haiku 4.5", model: "claude-haiku-4-5", want: 64000},
		{name: "opus 4.5 snapshot", model: "claude-opus-4-5-20251101", want: 64000},
		{name: "sonnet 4.5 snapshot", model: "claude-sonnet-4-5-20250929", want: 64000},

		// Unknown / OpenAI-compatible models fall back to the safe floor. This
		// stays 8192 deliberately: an unknown id may be an old snapshot with a
		// genuinely low cap, and an over-cap max_tokens is a 400 (see
		// modelMaxOutputTokens). The live figure from ListModels is what
		// actually keeps a new Claude model off the floor.
		{name: "an unknown openai-compatible model takes the floor", model: "gpt-oss-120b", want: DefaultMaxOutputTokens},
		{name: "an unreleased claude snapshot takes the floor too", model: "claude-opus-9-9", want: DefaultMaxOutputTokens},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := MaxOutputTokens(tc.model); got != tc.want {
				t.Errorf("MaxOutputTokens(%q) = %d, want %d", tc.model, got, tc.want)
			}
		})
	}
}

func TestModelTablesAgreeWithEachOther(t *testing.T) {
	// The three tables are documented as "keep in sync" and nothing checked it,
	// so the 5.5 generation went missing from all three at once. Every model
	// with a context window must have an output cap and an effort ceiling, and
	// the 1M/128k, 200k/64k pairing must hold — that pairing is the rule used
	// to fill the table, so a typo in one column is caught here rather than on
	// the wire.
	for model, window := range modelContextWindows {
		out, ok := modelMaxOutputTokens[model]
		if !ok {
			t.Errorf("%s has a context window but no output cap", model)
			continue
		}
		if _, ok := modelMaxEffort[model]; !ok {
			t.Errorf("%s has a context window but no effort ceiling", model)
		}
		switch window {
		case 1_000_000:
			if out != 128000 {
				t.Errorf("%s is 1M-context, so its output cap should be 128000, got %d", model, out)
			}
		case 200_000:
			if out != 64000 {
				t.Errorf("%s is 200k-context, so its output cap should be 64000, got %d", model, out)
			}
		default:
			t.Errorf("%s has an unexpected context window %d — add the matching output cap rule", model, window)
		}
	}
	for model := range modelMaxOutputTokens {
		if _, ok := modelContextWindows[model]; !ok {
			t.Errorf("%s has an output cap but no context window", model)
		}
	}
	// Every alias and the default must resolve into the tables, or the short
	// names users actually type land on the unknown-model floor.
	for alias := range modelAliases {
		if _, ok := modelMaxOutputTokens[string(ResolveModel(alias))]; !ok {
			t.Errorf("alias %q resolves to %q, which is not in modelMaxOutputTokens", alias, ResolveModel(alias))
		}
	}
	if _, ok := modelMaxOutputTokens[DefaultModel]; !ok {
		t.Errorf("DefaultModel %q is not in modelMaxOutputTokens", DefaultModel)
	}
}
