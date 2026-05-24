package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	gopdf "github.com/razvandimescu/gopdf/pdf"
)

// TestReadPDF generates a PDF and confirms Read returns its extracted text.
func TestReadPDF(t *testing.T) {
	c := gopdf.NewCreator()
	pb := c.NewPage(595, 842)
	pb.DrawText(72, 760, "Klaudia read-pdf alpha")
	pb.DrawText(72, 740, "gamma delta line")
	data, err := c.Build()
	if err != nil {
		t.Fatalf("build pdf: %v", err)
	}
	path := filepath.Join(t.TempDir(), "doc.pdf")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	r := mustRead(t)
	raw, _ := json.Marshal(ReadInput{FilePath: path})
	res, err := r.Execute(context.Background(), Context{}, raw)
	if err != nil || res[0].IsError {
		t.Fatalf("read pdf: err=%v res=%+v", err, res[0])
	}
	if !strings.Contains(res[0].Content, "alpha") || !strings.Contains(res[0].Content, "gamma") {
		t.Errorf("extracted text missing content: %q", res[0].Content)
	}
	if !strings.Contains(res[0].Content, "[PDF: 1 page") {
		t.Errorf("expected page-count header, got %q", res[0].Content)
	}
}
