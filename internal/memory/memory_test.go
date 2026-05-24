package memory

import (
	"strings"
	"testing"
)

func TestIndexMissingFileReturnsEmpty(t *testing.T) {
	store := New(t.TempDir())

	contents, err := store.Index()
	if err != nil {
		t.Fatalf("Index() error = %v", err)
	}
	if contents != "" {
		t.Fatalf("Index() = %q, want empty string", contents)
	}
}

func TestAddThenIndexContainsTextAndHeader(t *testing.T) {
	store := New(t.TempDir())

	if err := store.Add("remember this"); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	contents, err := store.Index()
	if err != nil {
		t.Fatalf("Index() error = %v", err)
	}
	if !strings.Contains(contents, "# Memory\n\n") {
		t.Fatalf("Index() = %q, want header", contents)
	}
	if !strings.Contains(contents, "remember this") {
		t.Fatalf("Index() = %q, want added text", contents)
	}
}

func TestTwoAddsProduceTwoBulletLines(t *testing.T) {
	store := New(t.TempDir())

	if err := store.Add("first"); err != nil {
		t.Fatalf("Add() first error = %v", err)
	}
	if err := store.Add("second"); err != nil {
		t.Fatalf("Add() second error = %v", err)
	}

	contents, err := store.Index()
	if err != nil {
		t.Fatalf("Index() error = %v", err)
	}

	lines := strings.Split(strings.TrimSpace(contents), "\n")
	bulletLines := 0
	for _, line := range lines {
		if strings.HasPrefix(line, "- ") {
			bulletLines++
		}
	}
	if bulletLines != 2 {
		t.Fatalf("bullet lines = %d, want 2; contents = %q", bulletLines, contents)
	}
}

func TestAddEmptyStringReturnsError(t *testing.T) {
	store := New(t.TempDir())

	if err := store.Add(" \t\n "); err == nil {
		t.Fatal("Add() error = nil, want error")
	}
}
