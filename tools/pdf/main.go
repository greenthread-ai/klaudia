// klaudia pdf — PDF info and page rendering tool
//
// Pure Go (no CGO) replacement for pdfinfo + pdftoppm as used by Klaudia.
//
// Modes:
//   klaudia-pdf info <file>              Print page count (like pdfinfo)
//   klaudia-pdf render <file> <outdir>   Extract page images (like pdftoppm)
//     [-f first] [-l last]
//   klaudia-pdf text <file>              Extract text content
//     [-f first] [-l last]
//   klaudia-pdf -v                       Print version
//
// Exit codes: 0 = success, 1 = error
package main

import (
	"fmt"
	"os"
	"strconv"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "Usage: klaudia-pdf <info|render|text|-v> [args...]")
		os.Exit(1)
	}

	switch os.Args[1] {
	case "-v", "--version":
		fmt.Println("klaudia-pdf 0.1.0")
		os.Exit(0)
	case "info":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "Usage: klaudia-pdf info <file.pdf>")
			os.Exit(1)
		}
		os.Exit(runInfo(os.Args[2]))
	case "render":
		if len(os.Args) < 4 {
			fmt.Fprintln(os.Stderr, "Usage: klaudia-pdf render <file.pdf> <output-prefix> [-f first] [-l last]")
			os.Exit(1)
		}
		os.Exit(runRender(os.Args[2], os.Args[3], os.Args[4:]))
	case "text":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "Usage: klaudia-pdf text <file.pdf> [-f first] [-l last]")
			os.Exit(1)
		}
		os.Exit(runText(os.Args[2], os.Args[3:]))
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", os.Args[1])
		os.Exit(1)
	}
}

func runInfo(path string) int {
	conf := model.NewDefaultConfiguration()
	conf.ValidationMode = model.ValidationRelaxed

	ctx, err := api.ReadContextFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening PDF: %v\n", err)
		return 1
	}

	if err := api.OptimizeContext(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Error processing PDF: %v\n", err)
		return 1
	}

	// Output in pdfinfo format so Klaudia's parser works unchanged
	fmt.Printf("Pages:          %d\n", ctx.PageCount)
	return 0
}

func parsePageRange(args []string) (int, int) {
	first := 1
	last := -1

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-f":
			i++
			if i < len(args) {
				first, _ = strconv.Atoi(args[i])
			}
		case "-l":
			i++
			if i < len(args) {
				last, _ = strconv.Atoi(args[i])
			}
		case "-jpeg", "-r":
			i++ // skip value for -r
		}
	}

	return first, last
}

func runRender(pdfPath, outputPrefix string, args []string) int {
	first, last := parsePageRange(args)

	conf := model.NewDefaultConfiguration()
	conf.ValidationMode = model.ValidationRelaxed

	// Build page selection string
	pageStr := ""
	if first > 0 && last > 0 {
		pageStr = fmt.Sprintf("%d-%d", first, last)
	} else if first > 0 {
		pageStr = fmt.Sprintf("%d-", first)
	}

	var selectedPages []string
	if pageStr != "" {
		selectedPages = []string{pageStr}
	}

	// Extract images from the PDF pages to the output directory
	err := api.ExtractImagesFile(pdfPath, outputPrefix, selectedPages, conf)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error extracting images: %v\n", err)
		// Fall back to text extraction hint
		fmt.Fprintln(os.Stderr, "Note: Pure Go PDF rendering extracts embedded images. For full page rasterization, install poppler-utils.")
		return 1
	}

	return 0
}

func runText(pdfPath string, args []string) int {
	first, last := parsePageRange(args)

	conf := model.NewDefaultConfiguration()
	conf.ValidationMode = model.ValidationRelaxed

	// Build page selection string
	pageStr := ""
	if first > 0 && last > 0 {
		pageStr = fmt.Sprintf("%d-%d", first, last)
	} else if first > 0 {
		pageStr = fmt.Sprintf("%d-", first)
	}

	var selectedPages []string
	if pageStr != "" {
		selectedPages = []string{pageStr}
	}

	outDir, err := os.MkdirTemp("", "klaudia-pdf-text-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating temp dir: %v\n", err)
		return 1
	}
	defer os.RemoveAll(outDir)

	err = api.ExtractContentFile(pdfPath, outDir, selectedPages, conf)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error extracting text: %v\n", err)
		return 1
	}

	// Read and print all extracted content files
	entries, _ := os.ReadDir(outDir)
	for _, e := range entries {
		data, err := os.ReadFile(outDir + "/" + e.Name())
		if err == nil {
			fmt.Print(string(data))
		}
	}
	return 0
}
