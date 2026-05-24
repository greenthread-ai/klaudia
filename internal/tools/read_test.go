package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mustRead(t *testing.T) *Read {
	t.Helper()
	r, err := NewRead()
	if err != nil {
		t.Fatalf("NewRead: %v", err)
	}
	return r
}

func TestReadCatNFormat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("alpha\nbeta\ngamma\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := mustRead(t)
	raw, _ := json.Marshal(ReadInput{FilePath: path})
	res, err := r.Execute(context.Background(), Context{}, raw)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	want := "     1\talpha\n     2\tbeta\n     3\tgamma\n"
	if res[0].Content != want {
		t.Errorf("content =\n%q\nwant\n%q", res[0].Content, want)
	}
}

func TestReadOffsetLimit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("a\nb\nc\nd\ne\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := mustRead(t)
	raw, _ := json.Marshal(ReadInput{FilePath: path, Offset: 2, Limit: 2})
	res, _ := r.Execute(context.Background(), Context{}, raw)
	want := "     2\tb\n     3\tc\n"
	if res[0].Content != want {
		t.Errorf("content = %q, want %q", res[0].Content, want)
	}
}

func TestReadValidateRejectsRelativePath(t *testing.T) {
	r := mustRead(t)
	raw, _ := json.Marshal(ReadInput{FilePath: "relative/path.txt"})
	if err := r.ValidateInput(raw); err == nil {
		t.Error("expected relative path to be rejected")
	}
}

func TestReadMissingFileIsErrorResult(t *testing.T) {
	r := mustRead(t)
	raw, _ := json.Marshal(ReadInput{FilePath: "/nonexistent/klaudia/x.txt"})
	res, err := r.Execute(context.Background(), Context{}, raw)
	if err != nil {
		t.Fatalf("Execute returned hard error: %v", err)
	}
	if !res[0].IsError || !strings.Contains(res[0].Content, "does not exist") {
		t.Errorf("expected file-not-found error result, got %+v", res[0])
	}
}

func TestReadDirectoryIsCleanError(t *testing.T) {
	r := mustRead(t)
	dir := t.TempDir()
	raw, _ := json.Marshal(ReadInput{FilePath: dir})
	res, err := r.Execute(context.Background(), Context{}, raw)
	if err != nil {
		t.Fatalf("Execute returned hard error: %v", err)
	}
	if !res[0].IsError || !strings.Contains(res[0].Content, "is a directory") {
		t.Errorf("expected a clean 'is a directory' error, got %+v", res[0])
	}
}

func TestRegistryLookup(t *testing.T) {
	r := mustRead(t)
	reg := NewRegistry(r)
	if _, ok := reg.Lookup("Read"); !ok {
		t.Error("Read not found in registry")
	}
	if _, ok := reg.Lookup("Nope"); ok {
		t.Error("unexpected tool found")
	}
}
