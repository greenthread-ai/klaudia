package lsp

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// ServerSpec describes a language server and how to find it.
type ServerSpec struct {
	Language    string   // "go"
	LanguageID  string   // LSP languageId, e.g. "go"
	Bin         string   // executable name, e.g. "gopls"
	Args        []string // launch args (most servers default to stdio)
	Exts        []string // file extensions handled, e.g. [".go"]
	VersionArgs []string // args to print the version (default ["--version"])
	Markers     []string // project-root files implying this language (e.g. go.mod)
	// candidates returns extra directories to probe beyond $PATH (the common
	// "installed but not on PATH" case: ~/go/bin, ~/.cargo/bin, …).
	candidates func() []string
}

// builtinServers are the language servers Klaudia knows how to detect. More can
// be added here; acquisition is detection-only (no downloads).
var builtinServers = []ServerSpec{
	{
		Language: "go", LanguageID: "go", Bin: "gopls", Exts: []string{".go"},
		VersionArgs: []string{"version"}, Markers: []string{"go.mod"},
		candidates: goBinDirs,
	},
	{
		Language: "rust", LanguageID: "rust", Bin: "rust-analyzer", Exts: []string{".rs"},
		VersionArgs: []string{"--version"}, Markers: []string{"Cargo.toml"},
		candidates: func() []string { return []string{home(".cargo/bin")} },
	},
	{
		Language: "typescript", LanguageID: "typescript", Bin: "typescript-language-server",
		Args: []string{"--stdio"}, Exts: []string{".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs"},
		VersionArgs: []string{"--version"}, Markers: []string{"package.json", "tsconfig.json"},
		candidates: npmBinDirs,
	},
	{
		Language: "python", LanguageID: "python", Bin: "pyright-langserver",
		Args: []string{"--stdio"}, Exts: []string{".py", ".pyi"},
		VersionArgs: []string{"--version"}, Markers: []string{"pyproject.toml", "requirements.txt", "setup.py"},
		candidates: npmBinDirs,
	},
	{
		Language: "c", LanguageID: "c", Bin: "clangd", Exts: []string{".c", ".h", ".cc", ".cpp", ".hpp", ".cxx"},
		VersionArgs: []string{"--version"},
		candidates: func() []string {
			return []string{"/usr/local/opt/llvm/bin", "/opt/homebrew/opt/llvm/bin", "/usr/bin"}
		},
	},
}

// ServerInfo is a detected language server with its resolved path and version.
type ServerInfo struct {
	Language string
	Bin      string
	Path     string
	Version  string // best-effort, e.g. "v0.15.2"; "" if unknown
}

var versionRE = regexp.MustCompile(`v?\d+\.\d+(\.\d+)?`)

// serverVersion runs the server's version command and extracts a version token.
func serverVersion(path string, args []string) string {
	if len(args) == 0 {
		args = []string{"--version"}
	}
	out, err := exec.Command(path, args...).CombinedOutput()
	if err != nil && len(out) == 0 {
		return ""
	}
	return versionRE.FindString(string(out))
}

// Survey detects installed servers and, for languages whose project markers are
// present under root but whose server is missing, returns install hints.
func Survey(root string) (found []ServerInfo, missingHints []string) {
	for _, spec := range builtinServers {
		path, ok := detect(spec)
		if ok {
			found = append(found, ServerInfo{
				Language: spec.Language, Bin: spec.Bin, Path: path,
				Version: serverVersion(path, spec.VersionArgs),
			})
			continue
		}
		if root != "" && hasAnyMarker(root, spec.Markers) {
			missingHints = append(missingHints,
				fmt.Sprintf("install %s for %s support", spec.Bin, spec.Language))
		}
	}
	return found, missingHints
}

// hasAnyMarker reports whether any of the marker files exists at root.
func hasAnyMarker(root string, markers []string) bool {
	for _, m := range markers {
		if _, err := os.Stat(filepath.Join(root, m)); err == nil {
			return true
		}
	}
	return false
}

// resolved is a server spec with its located binary path.
type resolved struct {
	spec ServerSpec
	path string
}

// detect finds the server binary on PATH or in its candidate directories.
func detect(spec ServerSpec) (string, bool) {
	if p, err := exec.LookPath(spec.Bin); err == nil {
		return p, true
	}
	if spec.candidates != nil {
		for _, dir := range spec.candidates() {
			if dir == "" {
				continue
			}
			p := filepath.Join(dir, spec.Bin)
			if isExecutable(p) {
				return p, true
			}
		}
	}
	return "", false
}

// specForExt returns the server spec handling a file extension.
func specForExt(ext string) (ServerSpec, bool) {
	ext = strings.ToLower(ext)
	for _, spec := range builtinServers {
		for _, e := range spec.Exts {
			if e == ext {
				return spec, true
			}
		}
	}
	return ServerSpec{}, false
}

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Mode()&0o111 != 0
}

func home(rel string) string {
	h, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(h, rel)
}

// goBinDirs returns where `go install` places binaries (GOBIN, GOPATH/bin, ~/go/bin).
func goBinDirs() []string {
	var dirs []string
	if out, err := exec.Command("go", "env", "GOBIN").Output(); err == nil {
		if d := strings.TrimSpace(string(out)); d != "" {
			dirs = append(dirs, d)
		}
	}
	if out, err := exec.Command("go", "env", "GOPATH").Output(); err == nil {
		if d := strings.TrimSpace(string(out)); d != "" {
			dirs = append(dirs, filepath.Join(d, "bin"))
		}
	}
	return append(dirs, home("go/bin"))
}

// npmBinDirs returns likely global-npm bin directories.
func npmBinDirs() []string {
	var dirs []string
	if out, err := exec.Command("npm", "prefix", "-g").Output(); err == nil {
		if d := strings.TrimSpace(string(out)); d != "" {
			dirs = append(dirs, filepath.Join(d, "bin"))
		}
	}
	return append(dirs, home(".npm-global/bin"), "/usr/local/bin", "/opt/homebrew/bin")
}
