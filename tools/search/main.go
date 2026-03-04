// klaudia search — ripgrep-compatible search tool
//
// Drop-in replacement for ripgrep as used by Klaudia.
// Supports the subset of rg flags that the app actually uses:
//
// Grep mode:  search <pattern> [path] [-l] [-c] [-n] [-i] [-e pattern]
//
//	[-A n] [-B n] [-C n] [-U] [--multiline-dotall]
//	[--glob pattern] [--type type] [--hidden] [--max-columns n]
//
// Glob mode:  search --files [--glob pattern] [--sort=modified]
//
//	[--no-ignore] [--hidden] [--max-depth n]
//
// Exit codes: 0 = matches found, 1 = no matches, 2 = error (ripgrep compat)
package main

import (
	"bufio"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
)

type config struct {
	// Mode
	filesOnly bool // --files (glob mode)

	// Pattern
	pattern    string
	ignoreCase bool // -i

	// Output
	filesWithMatches bool // -l
	countMode        bool // -c
	lineNumbers      bool // -n
	maxColumns       int  // --max-columns

	// Context
	beforeContext int // -B
	afterContext  int // -A

	// Multiline
	multiline       bool // -U
	multilineDotall bool // --multiline-dotall

	// Filters
	globs    []string // --glob
	fileType string   // --type
	hidden   bool     // --hidden
	noIgnore bool     // --no-ignore
	maxDepth int      // --max-depth

	// Sorting
	sortModified bool // --sort=modified

	// Search path
	path string
}

func main() {
	cfg, err := parseArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(2)
	}

	if cfg.filesOnly {
		code := runFilesMode(cfg)
		os.Exit(code)
	}

	code := runGrepMode(cfg)
	os.Exit(code)
}

func parseArgs(args []string) (*config, error) {
	cfg := &config{
		lineNumbers: false,
		maxColumns:  0,
		maxDepth:    -1,
		path:        ".",
	}

	i := 0
	patternSet := false
	for i < len(args) {
		arg := args[i]
		switch {
		case arg == "--files":
			cfg.filesOnly = true
		case arg == "-l" || arg == "--files-with-matches":
			cfg.filesWithMatches = true
		case arg == "-c" || arg == "--count":
			cfg.countMode = true
		case arg == "-n" || arg == "--line-number":
			cfg.lineNumbers = true
		case arg == "-i" || arg == "--ignore-case":
			cfg.ignoreCase = true
		case arg == "-U" || arg == "--multiline":
			cfg.multiline = true
		case arg == "--multiline-dotall":
			cfg.multilineDotall = true
		case arg == "--hidden":
			cfg.hidden = true
		case arg == "--no-ignore":
			cfg.noIgnore = true
		case arg == "--no-config":
			// ignore, compat only
		case arg == "--sort=modified":
			cfg.sortModified = true
		case strings.HasPrefix(arg, "--sort="):
			// ignore other sort modes for now
		case arg == "--glob" || arg == "-g":
			i++
			if i >= len(args) {
				return nil, fmt.Errorf("--glob requires a value")
			}
			cfg.globs = append(cfg.globs, args[i])
		case arg == "--type":
			i++
			if i >= len(args) {
				return nil, fmt.Errorf("--type requires a value")
			}
			cfg.fileType = args[i]
		case arg == "--max-columns":
			i++
			if i >= len(args) {
				return nil, fmt.Errorf("--max-columns requires a value")
			}
			fmt.Sscanf(args[i], "%d", &cfg.maxColumns)
		case arg == "--max-depth":
			i++
			if i >= len(args) {
				return nil, fmt.Errorf("--max-depth requires a value")
			}
			fmt.Sscanf(args[i], "%d", &cfg.maxDepth)
		case arg == "-j":
			i++ // skip thread count, we handle concurrency differently
		case arg == "-e":
			i++
			if i >= len(args) {
				return nil, fmt.Errorf("-e requires a pattern")
			}
			cfg.pattern = args[i]
			patternSet = true
		case arg == "-A":
			i++
			if i >= len(args) {
				return nil, fmt.Errorf("-A requires a value")
			}
			fmt.Sscanf(args[i], "%d", &cfg.afterContext)
		case arg == "-B":
			i++
			if i >= len(args) {
				return nil, fmt.Errorf("-B requires a value")
			}
			fmt.Sscanf(args[i], "%d", &cfg.beforeContext)
		case arg == "-C":
			i++
			if i >= len(args) {
				return nil, fmt.Errorf("-C requires a value")
			}
			var c int
			fmt.Sscanf(args[i], "%d", &c)
			cfg.beforeContext = c
			cfg.afterContext = c
		case strings.HasPrefix(arg, "-"):
			// Unknown flag — skip silently for compat
		default:
			if !patternSet && !cfg.filesOnly {
				cfg.pattern = arg
				patternSet = true
			} else {
				cfg.path = arg
			}
		}
		i++
	}

	if !cfg.filesOnly && !patternSet {
		return nil, fmt.Errorf("no search pattern provided")
	}

	return cfg, nil
}

// File type to extensions mapping (subset matching ripgrep)
var typeExtensions = map[string][]string{
	"js":     {".js", ".mjs", ".cjs", ".jsx"},
	"ts":     {".ts", ".mts", ".cts", ".tsx"},
	"py":     {".py", ".pyi"},
	"go":     {".go"},
	"rust":   {".rs"},
	"java":   {".java"},
	"c":      {".c", ".h"},
	"cpp":    {".cpp", ".cc", ".cxx", ".hpp", ".hh", ".hxx", ".h"},
	"css":    {".css"},
	"html":   {".html", ".htm"},
	"json":   {".json"},
	"yaml":   {".yaml", ".yml"},
	"xml":    {".xml"},
	"md":     {".md", ".markdown"},
	"sh":     {".sh", ".bash", ".zsh"},
	"ruby":   {".rb"},
	"php":    {".php"},
	"swift":  {".swift"},
	"kotlin": {".kt", ".kts"},
	"scala":  {".scala"},
	"r":      {".r", ".R"},
	"lua":    {".lua"},
	"sql":    {".sql"},
	"toml":   {".toml"},
}

func shouldSkipDir(name string, hidden bool) bool {
	if name == ".git" || name == "node_modules" || name == "__pycache__" ||
		name == ".svn" || name == ".hg" || name == "vendor" {
		return true
	}
	if !hidden && strings.HasPrefix(name, ".") {
		return true
	}
	return false
}

func matchesGlobs(path string, globs []string) bool {
	if len(globs) == 0 {
		return true
	}

	hasPositive := false
	excluded := false
	included := false

	for _, g := range globs {
		if strings.HasPrefix(g, "!") {
			// Negative glob — exclude
			neg := g[1:]
			if globMatch(path, neg) {
				excluded = true
			}
		} else {
			hasPositive = true
			if globMatch(path, g) {
				included = true
			}
		}
	}

	if excluded {
		return false
	}
	if hasPositive {
		return included
	}
	return true
}

func globMatch(path, pattern string) bool {
	// Try matching against full path and basename
	matched, _ := filepath.Match(pattern, filepath.Base(path))
	if matched {
		return true
	}
	matched, _ = filepath.Match(pattern, path)
	if matched {
		return true
	}
	// Handle ** patterns by converting to a simple contains check
	if strings.Contains(pattern, "**") {
		simple := strings.ReplaceAll(pattern, "**/", "")
		simple = strings.ReplaceAll(simple, "/**", "")
		matched, _ = filepath.Match(simple, filepath.Base(path))
		if matched {
			return true
		}
		// Check if path contains the directory pattern
		if strings.Contains(path, strings.Trim(simple, "*")) {
			return true
		}
	}
	return false
}

func matchesType(path string, fileType string) bool {
	if fileType == "" {
		return true
	}
	exts, ok := typeExtensions[fileType]
	if !ok {
		return true // unknown type, don't filter
	}
	ext := strings.ToLower(filepath.Ext(path))
	return slices.Contains(exts, ext)
}

type fileInfo struct {
	path    string
	modTime int64
}

func collectFiles(cfg *config) ([]string, error) {
	var files []fileInfo
	root := cfg.path

	// Handle single file input
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return []string{root}, nil
	}

	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip errors silently
		}

		if d.IsDir() {
			name := d.Name()
			if path != root && shouldSkipDir(name, cfg.hidden) {
				return filepath.SkipDir
			}
			if cfg.maxDepth >= 0 {
				rel, _ := filepath.Rel(root, path)
				depth := 0
				if rel != "." {
					depth = strings.Count(rel, string(os.PathSeparator)) + 1
				}
				if depth > cfg.maxDepth {
					return filepath.SkipDir
				}
			}
			return nil
		}

		if !cfg.hidden && strings.HasPrefix(d.Name(), ".") {
			return nil
		}

		rel, _ := filepath.Rel(root, path)
		if !matchesGlobs(rel, cfg.globs) {
			return nil
		}

		if !matchesType(path, cfg.fileType) {
			return nil
		}

		if cfg.sortModified {
			finfo, err := d.Info()
			if err == nil {
				files = append(files, fileInfo{path: rel, modTime: finfo.ModTime().UnixMilli()})
			}
		} else {
			files = append(files, fileInfo{path: rel})
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	if cfg.sortModified {
		sort.Slice(files, func(i, j int) bool {
			return files[i].modTime > files[j].modTime // newest first
		})
	}

	result := make([]string, len(files))
	for i, f := range files {
		result[i] = f.path
	}
	return result, nil
}

func runFilesMode(cfg *config) int {
	files, err := collectFiles(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 2
	}

	if len(files) == 0 {
		return 1
	}

	w := bufio.NewWriter(os.Stdout)
	for _, f := range files {
		// Output paths relative to CWD, prefixed with search dir (ripgrep compat)
		if cfg.path != "." && !filepath.IsAbs(f) {
			fmt.Fprintln(w, filepath.Join(cfg.path, f))
		} else {
			fmt.Fprintln(w, f)
		}
	}
	w.Flush()
	return 0
}

func compilePattern(cfg *config) (*regexp.Regexp, error) {
	pattern := cfg.pattern
	flags := ""
	if cfg.ignoreCase {
		flags += "(?i)"
	}
	if cfg.multiline && cfg.multilineDotall {
		flags += "(?s)"
	}
	return regexp.Compile(flags + pattern)
}

func isBinary(data []byte) bool {
	for _, b := range data {
		if b == 0 {
			return true
		}
		if len(data) > 512 {
			break
		}
	}
	return false
}

func runGrepMode(cfg *config) int {
	re, err := compilePattern(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: invalid pattern: %v\n", err)
		return 2
	}

	files, err := collectFiles(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 2
	}

	w := bufio.NewWriter(os.Stdout)
	defer w.Flush()

	anyMatch := false

	for _, file := range files {
		fullPath := file
		displayPath := file
		if file != cfg.path && !filepath.IsAbs(file) {
			fullPath = filepath.Join(cfg.path, file)
			displayPath = fullPath
		}
		data, err := os.ReadFile(fullPath)
		if err != nil {
			continue
		}

		if isBinary(data) {
			continue
		}

		lines := strings.Split(string(data), "\n")

		if cfg.filesWithMatches {
			if slices.ContainsFunc(lines, re.MatchString) {
				fmt.Fprintln(w, displayPath)
				anyMatch = true
			}
			continue
		}

		if cfg.countMode {
			count := 0
			for _, line := range lines {
				if re.MatchString(line) {
					count++
				}
			}
			if count > 0 {
				fmt.Fprintf(w, "%s:%d\n", displayPath, count)
				anyMatch = true
			}
			continue
		}

		// Content mode — output matching lines with optional context
		matchIndices := []int{}
		for i, line := range lines {
			if re.MatchString(line) {
				matchIndices = append(matchIndices, i)
			}
		}

		if len(matchIndices) == 0 {
			continue
		}
		anyMatch = true

		printed := make(map[int]bool)
		for _, idx := range matchIndices {
			start := max(idx-cfg.beforeContext, 0)
			end := idx + cfg.afterContext
			if end >= len(lines) {
				end = len(lines) - 1
			}

			for i := start; i <= end; i++ {
				if printed[i] {
					continue
				}
				printed[i] = true

				line := lines[i]
				if cfg.maxColumns > 0 && len(line) > cfg.maxColumns {
					line = line[:cfg.maxColumns] + " [truncated]"
				}

				if cfg.lineNumbers {
					fmt.Fprintf(w, "%s:%d:%s\n", displayPath, i+1, line)
				} else {
					fmt.Fprintf(w, "%s:%s\n", displayPath, line)
				}
			}
		}
	}

	if !anyMatch {
		return 1
	}
	return 0
}
