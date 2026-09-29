package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/greenthread-ai/klaudia/internal/memory"
)

func TestMemoryRemove(t *testing.T) {
	dir := t.TempDir()
	mt, err := NewMemory(memory.New(dir))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	run := func(in MemoryInput) Result {
		t.Helper()
		raw, _ := json.Marshal(in)
		if err := mt.ValidateInput(raw); err != nil {
			t.Fatalf("ValidateInput(%s) = %v", raw, err)
		}
		res, err := mt.Execute(ctx, Context{}, raw)
		if err != nil {
			t.Fatal(err)
		}
		return res[0]
	}

	run(MemoryInput{Operation: "add", Content: "deploy with make release"})
	run(MemoryInput{Operation: "add", Content: "deploy needs the vpn"})

	// Ambiguous: nothing removed, both candidates listed.
	res := run(MemoryInput{Operation: "remove", Query: "deploy"})
	if !res.IsError || !strings.Contains(res.Content, "make release") || !strings.Contains(res.Content, "the vpn") {
		t.Errorf("ambiguous remove = %+v", res)
	}

	res = run(MemoryInput{Operation: "remove", Query: "deploy vpn"})
	if res.IsError || !strings.Contains(res.Content, "deploy needs the vpn") {
		t.Errorf("remove = %+v", res)
	}
	if view := run(MemoryInput{Operation: "view"}); strings.Contains(view.Content, "vpn") || !strings.Contains(view.Content, "make release") {
		t.Errorf("view after remove = %q", view.Content)
	}

	if res := run(MemoryInput{Operation: "remove", Query: "nonexistent"}); !res.IsError || !strings.Contains(res.Content, "Nothing removed") {
		t.Errorf("remove of a missing note = %+v", res)
	}

	// A detail note by name.
	if err := os.MkdirAll(filepath.Join(dir, "memory"), 0o755); err != nil {
		t.Fatal(err)
	}
	note := filepath.Join(dir, "memory", "tools.md")
	if err := os.WriteFile(note, []byte("# Tools\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if res := run(MemoryInput{Operation: "remove", Name: "tools"}); res.IsError {
		t.Errorf("remove by name = %+v", res)
	}
	if _, err := os.Stat(note); !os.IsNotExist(err) {
		t.Errorf("detail note still exists: %v", err)
	}
	if res := run(MemoryInput{Operation: "remove", Name: "tools"}); !res.IsError || !strings.Contains(res.Content, "no detail note") {
		t.Errorf("remove of a missing detail note = %+v", res)
	}
}

func TestMemoryRemoveValidate(t *testing.T) {
	mt := newMemTool(t)
	for _, in := range []MemoryInput{
		{Operation: "remove"},
		{Operation: "remove", Query: "x", Name: "y"},
		{Operation: "remove", Query: "   "},
	} {
		raw, _ := json.Marshal(in)
		if err := mt.ValidateInput(raw); err == nil {
			t.Errorf("ValidateInput(%s) = nil, want an error", raw)
		}
	}
}
