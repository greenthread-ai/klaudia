package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/greenthread/klaudia/internal/memory"
)

func newMemTool(t *testing.T) *Memory {
	t.Helper()
	mt, err := NewMemory(memory.New(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	return mt
}

func TestMemoryAddSearchView(t *testing.T) {
	mt := newMemTool(t)
	ctx := context.Background()

	add, _ := json.Marshal(MemoryInput{Operation: "add", Content: "use doublestar for globbing"})
	if res, _ := mt.Execute(ctx, Context{}, add); res[0].IsError {
		t.Fatalf("add failed: %+v", res[0])
	}

	search, _ := json.Marshal(MemoryInput{Operation: "search", Query: "globbing"})
	res, _ := mt.Execute(ctx, Context{}, search)
	if res[0].IsError || !strings.Contains(res[0].Content, "doublestar") {
		t.Errorf("search = %+v", res[0])
	}

	miss, _ := json.Marshal(MemoryInput{Operation: "search", Query: "nonexistent"})
	res, _ = mt.Execute(ctx, Context{}, miss)
	if !strings.Contains(res[0].Content, "No memories matched") {
		t.Errorf("expected no-match message, got %q", res[0].Content)
	}

	view, _ := json.Marshal(MemoryInput{Operation: "view"})
	res, _ = mt.Execute(ctx, Context{}, view)
	if !strings.Contains(res[0].Content, "doublestar") {
		t.Errorf("view = %q", res[0].Content)
	}
}

func TestMemoryValidate(t *testing.T) {
	mt := newMemTool(t)
	bad, _ := json.Marshal(MemoryInput{Operation: "nope"})
	if mt.ValidateInput(bad) == nil {
		t.Error("expected invalid operation to be rejected")
	}
	noContent, _ := json.Marshal(MemoryInput{Operation: "add"})
	if mt.ValidateInput(noContent) == nil {
		t.Error("expected add without content to be rejected")
	}
}
