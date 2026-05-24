package tools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestReadPNGImage(t *testing.T) {
	data, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+M8AAAMBAQDJ/pLvAAAAAElFTkSuQmCC")
	if err != nil {
		t.Fatalf("decode png: %v", err)
	}
	path := filepath.Join(t.TempDir(), "test.png")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	r := mustRead(t)
	raw, _ := json.Marshal(ReadInput{FilePath: path})
	res, err := r.Execute(context.Background(), Context{}, raw)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res[0].IsError {
		t.Fatalf("unexpected error result: %+v", res[0])
	}
	if len(res[0].Images) != 1 {
		t.Fatalf("len(Images) = %d, want 1", len(res[0].Images))
	}
	if res[0].Images[0].MediaType != "image/png" {
		t.Errorf("MediaType = %q, want image/png", res[0].Images[0].MediaType)
	}
	if res[0].Images[0].Base64 == "" {
		t.Error("Base64 is empty")
	}
}

func TestReadJPGImage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.jpg")
	if err := os.WriteFile(path, []byte{0xff, 0xd8, 0xff}, 0o644); err != nil {
		t.Fatal(err)
	}

	r := mustRead(t)
	raw, _ := json.Marshal(ReadInput{FilePath: path})
	res, err := r.Execute(context.Background(), Context{}, raw)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res[0].IsError {
		t.Fatalf("unexpected error result: %+v", res[0])
	}
	if len(res[0].Images) != 1 {
		t.Fatalf("len(Images) = %d, want 1", len(res[0].Images))
	}
	if res[0].Images[0].MediaType != "image/jpeg" {
		t.Errorf("MediaType = %q, want image/jpeg", res[0].Images[0].MediaType)
	}
}
