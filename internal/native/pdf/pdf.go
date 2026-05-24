// Package pdf provides in-process PDF text extraction.
package pdf

import (
	"fmt"

	gopdf "github.com/razvandimescu/gopdf/pdf"
)

// ExtractText extracts text from a PDF file.
func ExtractText(path string) (string, error) {
	doc, err := gopdf.OpenFile(path)
	if err != nil {
		return "", fmt.Errorf("read pdf %s: %w", path, err)
	}

	text, err := doc.Text()
	if err != nil {
		return "", fmt.Errorf("extract pdf text %s: %w", path, err)
	}
	return text, nil
}

// PageCount returns the number of pages in a PDF file.
func PageCount(path string) (int, error) {
	doc, err := gopdf.OpenFile(path)
	if err != nil {
		return 0, fmt.Errorf("read pdf %s: %w", path, err)
	}
	return doc.NumPages(), nil
}
