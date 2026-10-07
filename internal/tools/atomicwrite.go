package tools

import (
	"bytes"
	"os"
	"path/filepath"
)

// writeFileAtomic replaces path's contents with data so that the file is
// always either the old version or the new one.
//
// os.WriteFile truncates the file and then writes it: a crash, a full disk or
// a killed process in between left a truncated or empty file where the
// model's edit was meant to go. Here the data goes to a temporary file in the
// same directory, is synced, and is renamed over the original. The original's
// permissions are kept, and a symlink is written through to its target rather
// than replaced by a regular file.
//
// A file that does not exist yet has nothing to lose, so it is created
// directly with defaultMode, which leaves the umask to narrow it as before.
func writeFileAtomic(path string, data []byte, defaultMode os.FileMode) error {
	target := path
	if real, err := filepath.EvalSymlinks(path); err == nil {
		target = real
	}
	st, err := os.Stat(target)
	if err != nil {
		return os.WriteFile(target, data, defaultMode)
	}
	mode := st.Mode().Perm()
	tmp, err := os.CreateTemp(filepath.Dir(target), "."+filepath.Base(target)+".klaudia-*")
	if err != nil {
		return err
	}
	done := false
	defer func() {
		if !done {
			tmp.Close()
			os.Remove(tmp.Name())
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), target); err != nil {
		return err
	}
	done = true
	return nil
}

// matchLineEndings converts content's LF line endings to CRLF when the file
// it replaces uses CRLF and content has none of its own. Write sent whatever
// the model produced, which is LF, so overwriting a Windows-style file turned
// every line ending in it into a diff. Edit already keeps them.
func matchLineEndings(path string, content []byte) []byte {
	if bytes.Contains(content, []byte("\r\n")) || !bytes.Contains(content, []byte("\n")) {
		return content
	}
	old, err := os.ReadFile(path)
	if err != nil {
		return content
	}
	first := bytes.IndexByte(old, '\n')
	if first <= 0 || old[first-1] != '\r' {
		return content
	}
	return bytes.ReplaceAll(content, []byte("\n"), []byte("\r\n"))
}
