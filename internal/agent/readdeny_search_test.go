package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/greenthread-ai/klaudia/internal/permission"
	"github.com/greenthread-ai/klaudia/internal/tools"
)

// Read(~/secrets/**) denied Grep and Glob *of* ~/secrets, but a search
// started at ~ walked straight into it. (Dot-directories such as ~/.ssh were
// already skipped by the walk; a denied directory without a leading dot was
// not.)
func TestSearchAboveDeniedDirSkipsIt(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	os.MkdirAll(filepath.Join(home, "secrets"), 0o700)
	os.WriteFile(filepath.Join(home, "secrets", "api-key.txt"), []byte("TOKEN=abc123"), 0o600)
	os.WriteFile(filepath.Join(home, "notes.txt"), []byte("TOKEN rotation is monthly"), 0o644)

	deny, err := permission.ParseRules([]string{"Read(~/secrets/**)"})
	if err != nil {
		t.Fatal(err)
	}
	tctx := tools.Context{WorkingDir: home, Hidden: readDenied(deny)}

	grep, _ := tools.NewGrep()
	raw, _ := json.Marshal(map[string]any{"pattern": "TOKEN", "path": home, "output_mode": "content"})
	res, err := grep.Execute(context.Background(), tctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(res[0].Content, "abc123") {
		t.Errorf("grep above ~/secrets returned its content:\n%s", res[0].Content)
	}
	if !strings.Contains(res[0].Content, "notes.txt") || !strings.Contains(res[0].Content, "not searched") {
		t.Errorf("want the readable file and a note about the skipped path:\n%s", res[0].Content)
	}

	glob, _ := tools.NewGlob()
	raw, _ = json.Marshal(map[string]any{"pattern": "**/*.txt", "path": home})
	res, err = glob.Execute(context.Background(), tctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(res[0].Content, "api-key.txt") {
		t.Errorf("glob above ~/secrets listed its files:\n%s", res[0].Content)
	}
}
