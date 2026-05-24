package pdf

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	gopdf "github.com/razvandimescu/gopdf/pdf"
)

func TestExtractTextAndPageCount(t *testing.T) {
	path := writeTestPDF(t)

	text, err := ExtractText(path)
	if err != nil {
		t.Fatalf("ExtractText() error = %v", err)
	}
	for _, want := range []string{"alpha", "beta"} {
		if !strings.Contains(text, want) {
			t.Fatalf("ExtractText() = %q, want containing %q", text, want)
		}
	}

	pages, err := PageCount(path)
	if err != nil {
		t.Fatalf("PageCount() error = %v", err)
	}
	if pages != 1 {
		t.Fatalf("PageCount() = %d, want 1", pages)
	}
}

func TestExtractTextNonExistentPath(t *testing.T) {
	_, err := ExtractText(filepath.Join(t.TempDir(), "missing.pdf"))
	if err == nil {
		t.Fatal("ExtractText() error = nil, want error")
	}
}

func writeTestPDF(t *testing.T) string {
	t.Helper()

	c := gopdf.NewCreator()
	pb := c.NewPage(595, 842)
	pb.DrawText(72, 760, "Klaudia pdf test alpha")
	pb.DrawText(72, 740, "second line beta")
	data, err := c.Build()
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	path := filepath.Join(t.TempDir(), "sample.pdf")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return path
}
