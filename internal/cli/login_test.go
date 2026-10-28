package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// --login stores a piped key 0600 under the config root, and refuses an
// empty one.
func TestLoginStoresAPipedKey(t *testing.T) {
	root := t.TempDir()
	t.Setenv("KLAUDIA_CONFIG_DIR", root)
	cmd := &cobra.Command{}
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetIn(strings.NewReader("sk-ant-piped\n"))
	if err := login(cmd); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "credentials.json")
	b, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(b), `"apiKey": "sk-ant-piped"`) {
		t.Fatalf("stored %s, %v", b, err)
	}
	if st, _ := os.Stat(path); st.Mode().Perm() != 0o600 {
		t.Errorf("mode %v, want 0600", st.Mode().Perm())
	}
	if strings.Contains(out.String(), "sk-ant-piped") {
		t.Errorf("the key was echoed: %q", out.String())
	}
	cmd.SetIn(strings.NewReader(""))
	if err := login(cmd); err == nil {
		t.Error("an empty stdin stored something")
	}
}
