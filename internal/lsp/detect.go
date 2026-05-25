package lsp

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ServerSpec describes a language server and how to find it.
type ServerSpec struct {
	Language   string   // "go"
	LanguageID string   // LSP languageId, e.g. "go"
	Bin        string   // executable name, e.g. "gopls"
	Args       []string // launch args (most servers default to stdio)
	Exts       []string // file extensions handled, e.g. [".go"]
	// candidates returns extra directories to probe beyond $PATH (the common
	// "installed but not on PATH" case: ~/go/bin, ~/.cargo/bin, …).
	candidates func() []string
}

// builtinServers are the language servers Klaudia knows how to detect. More can
// be added here; acquisition is detection-only (no downloads).
var builtinServers = []ServerSpec{
	{
		Language: "go", LanguageID: "go", Bin: "gopls", Exts: []string{".go"},
		candidates: goBinDirs,
	},
	{
		Language: "rust", LanguageID: "rust", Bin: "rust-analyzer", Exts: []string{".rs"},
		candidates: func() []string { return []string{home(".cargo/bin")} },
	},
	{
		Language: "typescript", LanguageID: "typescript", Bin: "typescript-language-server",
		Args: []string{"--stdio"}, Exts: []string{".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs"},
		candidates: npmBinDirs,
	},
	{
		Language: "python", LanguageID: "python", Bin: "pyright-langserver",
		Args: []string{"--stdio"}, Exts: []string{".py", ".pyi"},
		candidates: npmBinDirs,
	},
	{
		Language: "c", LanguageID: "c", Bin: "clangd", Exts: []string{".c", ".h", ".cc", ".cpp", ".hpp", ".cxx"},
		candidates: func() []string {
			return []string{"/usr/local/opt/llvm/bin", "/opt/homebrew/opt/llvm/bin", "/usr/bin"}
		},
	},
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

// DetectAll returns every builtin server found on this machine, with its path.
// Used by /doctor to report code-intel readiness.
func DetectAll() []struct{ Language, Bin, Path string } {
	var out []struct{ Language, Bin, Path string }
	for _, spec := range builtinServers {
		if p, ok := detect(spec); ok {
			out = append(out, struct{ Language, Bin, Path string }{spec.Language, spec.Bin, p})
		}
	}
	return out
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
