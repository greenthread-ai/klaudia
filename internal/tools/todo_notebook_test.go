package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestTodoWriteStoresAndRenders(t *testing.T) {
	store := &TodoStore{}
	tw, err := NewTodoWrite(store)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(TodoInput{Todos: []TodoItem{
		{Content: "Write code", Status: "in_progress", ActiveForm: "Writing code"},
		{Content: "Run tests", Status: "pending"},
	}})
	res, err := tw.Execute(context.Background(), Context{}, raw)
	if err != nil || res[0].IsError {
		t.Fatalf("execute: err=%v res=%+v", err, res[0])
	}
	if got := store.Items(); len(got) != 2 || got[0].Status != "in_progress" {
		t.Errorf("store = %+v", got)
	}
}

func TestTodoWriteRejectsBadStatus(t *testing.T) {
	tw, _ := NewTodoWrite(&TodoStore{})
	raw, _ := json.Marshal(TodoInput{Todos: []TodoItem{{Content: "x", Status: "bogus"}}})
	if err := tw.ValidateInput(raw); err == nil {
		t.Error("expected invalid status to be rejected")
	}
}

const sampleNotebook = `{
 "cells": [
  {"cell_type": "code", "id": "c1", "metadata": {}, "outputs": [], "execution_count": null, "source": ["print(1)\n"]},
  {"cell_type": "markdown", "id": "c2", "metadata": {}, "source": ["# Title\n"]}
 ],
 "metadata": {},
 "nbformat": 4,
 "nbformat_minor": 5
}`

func writeNotebook(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "nb.ipynb")
	if err := os.WriteFile(p, []byte(sampleNotebook), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func loadCells(t *testing.T, path string) []any {
	t.Helper()
	data, _ := os.ReadFile(path)
	var nb map[string]any
	if err := json.Unmarshal(data, &nb); err != nil {
		t.Fatalf("reload notebook: %v", err)
	}
	cells, _ := nb["cells"].([]any)
	return cells
}

func TestNotebookReplace(t *testing.T) {
	path := writeNotebook(t)
	ne, _ := NewNotebookEdit()
	raw, _ := json.Marshal(NotebookEditInput{NotebookPath: path, CellID: "c1", NewSource: "print(42)\n", EditMode: "replace"})
	res, err := ne.Execute(context.Background(), Context{}, raw)
	if err != nil || res[0].IsError {
		t.Fatalf("replace: err=%v res=%+v", err, res[0])
	}
	cells := loadCells(t, path)
	src := cells[0].(map[string]any)["source"].([]any)
	if src[0].(string) != "print(42)\n" {
		t.Errorf("source = %v", src)
	}
}

func TestNotebookInsertAndDelete(t *testing.T) {
	path := writeNotebook(t)
	ne, _ := NewNotebookEdit()

	// Insert a markdown cell after c1.
	raw, _ := json.Marshal(NotebookEditInput{NotebookPath: path, CellID: "c1", NewSource: "## new\n", CellType: "markdown", EditMode: "insert"})
	if res, err := ne.Execute(context.Background(), Context{}, raw); err != nil || res[0].IsError {
		t.Fatalf("insert: err=%v res=%+v", err, res[0])
	}
	if got := len(loadCells(t, path)); got != 3 {
		t.Fatalf("after insert, cells = %d, want 3", got)
	}

	// Delete c2.
	raw, _ = json.Marshal(NotebookEditInput{NotebookPath: path, CellID: "c2", EditMode: "delete"})
	if res, err := ne.Execute(context.Background(), Context{}, raw); err != nil || res[0].IsError {
		t.Fatalf("delete: err=%v res=%+v", err, res[0])
	}
	if got := len(loadCells(t, path)); got != 2 {
		t.Errorf("after delete, cells = %d, want 2", got)
	}
}

func TestNotebookInsertRequiresType(t *testing.T) {
	ne, _ := NewNotebookEdit()
	raw, _ := json.Marshal(NotebookEditInput{NotebookPath: "/abs/nb.ipynb", NewSource: "x", EditMode: "insert"})
	if err := ne.ValidateInput(raw); err == nil {
		t.Error("expected insert without cell_type to be rejected")
	}
}
