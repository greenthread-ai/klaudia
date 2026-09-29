package lsp

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// isolateToolchain points PATH at an empty directory and HOME at a scratch one,
// so detection sees only the fake servers a test installs — never a gopls or
// rust-analyzer the developer happens to have.
func isolateToolchain(t *testing.T) (pathDir, home string) {
	t.Helper()
	pathDir, home = t.TempDir(), t.TempDir()
	t.Setenv("PATH", pathDir)
	t.Setenv("HOME", home)
	t.Setenv("GOBIN", "")
	return pathDir, home
}

// fakeExe installs a shell script at dir/name that prints out and exits code.
func fakeExe(t *testing.T, dir, name, out string, code int) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, name)
	script := "#!/bin/sh\n"
	if out != "" {
		script += "echo '" + out + "'\n"
	}
	script += "exit " + string(rune('0'+code)) + "\n"
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func findInfo(found []ServerInfo, lang string) (ServerInfo, bool) {
	for _, f := range found {
		if f.Language == lang {
			return f, true
		}
	}
	return ServerInfo{}, false
}

func TestSurveyFindsServersOnPathWithTheirVersion(t *testing.T) {
	pathDir, _ := isolateToolchain(t)
	gopls := fakeExe(t, pathDir, "gopls", "golang.org/x/tools/gopls v0.15.2", 0)

	found, _ := Survey("")
	info, ok := findInfo(found, "go")
	if !ok {
		t.Fatalf("gopls on PATH not found; found = %+v", found)
	}
	if info.Path != gopls || info.Bin != "gopls" || info.Version != "v0.15.2" {
		t.Errorf("go server = %+v", info)
	}
}

// The common "installed but not on PATH" case: cargo puts rust-analyzer in
// ~/.cargo/bin, which a login shell may not have added yet.
func TestSurveyFindsServersInToolchainDirsOffPath(t *testing.T) {
	_, home := isolateToolchain(t)
	ra := fakeExe(t, filepath.Join(home, ".cargo", "bin"), "rust-analyzer", "rust-analyzer 1.79.0 (129f3b99 2024-06-10)", 0)
	gopls := fakeExe(t, filepath.Join(home, "go", "bin"), "gopls", "", 1)

	found, _ := Survey("")
	if info, ok := findInfo(found, "rust"); !ok || info.Path != ra || info.Version != "1.79.0" {
		t.Errorf("rust server = %+v, %v; want %s at 1.79.0", info, ok, ra)
	}
	// A server whose version command fails silently is still usable; its
	// version is just unknown.
	if info, ok := findInfo(found, "go"); !ok || info.Path != gopls || info.Version != "" {
		t.Errorf("go server = %+v, %v; want %s with no version", info, ok, gopls)
	}
}

func TestSurveyHintsOnlyForLanguagesTheProjectUses(t *testing.T) {
	isolateToolchain(t)
	root := t.TempDir()
	write(t, filepath.Join(root, "Cargo.toml"), "[package]\n")

	_, hints := Survey(root)
	if !slices.Contains(hints, "install rust-analyzer for rust support") {
		t.Errorf("hints = %q, want one for rust-analyzer", hints)
	}
	for _, h := range hints {
		if strings.Contains(h, "gopls") {
			t.Errorf("hinted gopls for a project with no go.mod: %q", hints)
		}
	}
	if _, hints := Survey(""); len(hints) != 0 {
		t.Errorf("no root should give no hints, got %q", hints)
	}
}

func TestDetectIgnoresNonExecutablesAndDirectories(t *testing.T) {
	isolateToolchain(t)
	dir := t.TempDir()
	write(t, filepath.Join(dir, "plainfile"), "not a program")
	if err := os.Mkdir(filepath.Join(dir, "adir"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, bin := range []string{"plainfile", "adir"} {
		spec := ServerSpec{Bin: bin, candidates: func() []string { return []string{"", dir} }}
		if p, ok := detect(spec); ok {
			t.Errorf("detect(%s) = %q, want not found", bin, p)
		}
	}
}

func TestSpecForExtIsCaseInsensitive(t *testing.T) {
	for ext, lang := range map[string]string{".GO": "go", ".tsx": "typescript", ".pyi": "python", ".hpp": "c"} {
		spec, ok := specForExt(ext)
		if !ok || spec.Language != lang {
			t.Errorf("specForExt(%q) = %q, %v; want %q", ext, spec.Language, ok, lang)
		}
	}
	if _, ok := specForExt(".txt"); ok {
		t.Error(".txt should have no server")
	}
}

func TestPoolExplainsWhyNoServerIsAvailable(t *testing.T) {
	isolateToolchain(t)
	root := t.TempDir()
	ctx := context.Background()

	cases := []struct {
		name, file, want string
		pool             *Pool
	}{
		{"unknown extension", "notes.txt", `no language server configured for ".txt" files`, NewPool(nil, root, nil, nil)},
		{"language disabled", "main.go", `no language server configured for ".go" files`, NewPool(ctx, root, []string{"go"}, nil)},
		{"not installed", "lib.rs", `no rust language server found (install "rust-analyzer"`, NewPool(ctx, root, nil, nil)},
		{"override not installed", "main.go", `no go language server found (install "my-gopls"`,
			NewPool(ctx, root, nil, map[string]ServerSpec{"go": {Bin: "my-gopls"}})},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer tc.pool.Close()
			path := filepath.Join(root, tc.file)
			if _, err := tc.pool.Diagnostics(ctx, path); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("Diagnostics err = %v, want %q", err, tc.want)
			}
			if _, err := tc.pool.Definition(ctx, path, Position{}); err == nil {
				t.Error("Definition succeeded without a server")
			}
			if _, err := tc.pool.References(ctx, path, Position{}); err == nil {
				t.Error("References succeeded without a server")
			}
		})
	}
}

// An override replaces the command but keeps the language's identity: the
// builtin languageId and extensions still apply unless the override sets them.
func TestPoolOverrideKeepsTheBuiltinLanguageID(t *testing.T) {
	isolateToolchain(t)
	bin := fakeExe(t, t.TempDir(), "custom-ts", "", 0)
	p := NewPool(context.Background(), t.TempDir(), nil, map[string]ServerSpec{
		"typescript": {Bin: bin, Args: []string{"--stdio"}},
	})
	spec, got, ok := p.serverFor("app.jsx")
	if !ok || got != bin || spec.LanguageID != "typescript" || spec.Language != "typescript" ||
		!slices.Contains(spec.Exts, ".jsx") || !slices.Equal(spec.Args, []string{"--stdio"}) {
		t.Errorf("serverFor = %+v, %q, %v", spec, got, ok)
	}
	p2 := NewPool(context.Background(), t.TempDir(), nil, map[string]ServerSpec{
		"typescript": {Bin: bin, LanguageID: "typescriptreact"},
	})
	if spec, _, _ := p2.serverFor("app.tsx"); spec.LanguageID != "typescriptreact" {
		t.Errorf("override LanguageID = %q, want typescriptreact", spec.LanguageID)
	}
}
