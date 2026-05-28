package tools

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/greenthread-ai/klaudia/internal/memory"
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

func TestMemoryAddProjectScope(t *testing.T) {
	cwd := t.TempDir()
	mt, err := NewMemoryForProject(memory.New(t.TempDir()), cwd)
	if err != nil {
		t.Fatal(err)
	}

	add, _ := json.Marshal(MemoryInput{Operation: "add", Scope: "project", Content: "prefer table tests"})
	res, _ := mt.Execute(context.Background(), Context{}, add)
	if res[0].IsError {
		t.Fatalf("project add failed: %+v", res[0])
	}
	data, err := os.ReadFile(memory.KnowledgePath(cwd))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "prefer table tests") {
		t.Errorf("KNOWLEDGE.md = %q", data)
	}
}

func TestMemoryValidate(t *testing.T) {
	mt := newMemTool(t)
	validAdd, _ := json.Marshal(MemoryInput{Operation: "add", Content: "remember this"})
	if err := mt.ValidateInput(validAdd); err != nil {
		t.Fatalf("valid add rejected: %v", err)
	}
	validSearch, _ := json.Marshal(MemoryInput{Operation: "search", Query: "remember"})
	if err := mt.ValidateInput(validSearch); err != nil {
		t.Fatalf("valid search rejected: %v", err)
	}
	validView, _ := json.Marshal(MemoryInput{Operation: "view"})
	if err := mt.ValidateInput(validView); err != nil {
		t.Fatalf("valid view rejected: %v", err)
	}
	bad, _ := json.Marshal(MemoryInput{Operation: "nope"})
	if mt.ValidateInput(bad) == nil {
		t.Error("expected invalid operation to be rejected")
	}
	noContent, _ := json.Marshal(MemoryInput{Operation: "add"})
	if mt.ValidateInput(noContent) == nil {
		t.Error("expected add without content to be rejected")
	}
}
