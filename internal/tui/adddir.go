package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// resolveAddDir turns an /add-dir argument into the absolute directory it
// names: ~ is expanded (the shell never saw this text, so nothing else will),
// a relative path is taken from the session's working directory, and the
// result must exist and be a directory. Accepting anything else would put a
// path in the prompt that no tool can read, and the model would find out only
// by failing.
func resolveAddDir(cwd, arg string) (string, error) {
	dir := strings.TrimSpace(arg)
	if dir == "" {
		return "", fmt.Errorf("no path given")
	}
	if dir == "~" || strings.HasPrefix(dir, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("cannot expand ~: %v", err)
		}
		dir = filepath.Join(home, strings.TrimPrefix(dir, "~"))
	}
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(cwd, dir)
	}
	dir = filepath.Clean(dir)
	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("%s does not exist", dir)
		}
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory", dir)
	}
	return dir, nil
}
