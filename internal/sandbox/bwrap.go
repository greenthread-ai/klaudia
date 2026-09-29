package sandbox

import (
	"context"
	"os"
	"path/filepath"
)

// Bwrap runs commands through bubblewrap.
type Bwrap struct {
	WriteRoots []string
	Network    string
	// Hide lists files and directories the command must not read — the
	// user's credentials (see trust.CredentialPaths). `--ro-bind / /` made
	// all of them readable inside the sandbox. Keep lists paths inside them
	// to leave visible. Paths that do not exist are skipped.
	Hide, Keep []string
}

func NewBwrap(writeRoots []string, network string) *Bwrap {
	return &Bwrap{WriteRoots: writeRoots, Network: network}
}

func (b *Bwrap) Name() string { return "bwrap" }

func (b *Bwrap) Argv(req Request) (string, []string) {
	return "bwrap", b.buildArgs(req)
}

func (b *Bwrap) Run(ctx context.Context, req Request) (Response, error) {
	name, args := b.Argv(req)
	return runArgv(ctx, req, name, args)
}

func (b *Bwrap) buildArgs(req Request) []string {
	args := []string{"--ro-bind", "/", "/", "--dev", "/dev", "--proc", "/proc", "--tmpfs", "/tmp"}
	for _, root := range b.writeRoots(req) {
		args = append(args, "--bind", root, root)
	}
	// After the write roots, so a credential directory inside one (a write
	// root of $HOME) is still hidden.
	args = append(args, hideArgs(b.Hide, b.Keep)...)
	if b.Network == "none" {
		args = append(args, "--unshare-net")
	}
	args = append(args, "--", shellPath, "-c", req.Command)
	return args
}

// hideArgs covers each existing directory in hide with an empty tmpfs and
// each existing file with /dev/null, then binds the keep paths back in from
// the host (bwrap reads bind sources outside the new namespace, so a file
// under a hidden directory is still reachable as a source).
func hideArgs(hide, keep []string) []string {
	var args []string
	for _, p := range existing(hide) {
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			args = append(args, "--tmpfs", p)
		} else {
			args = append(args, "--ro-bind", "/dev/null", p)
		}
	}
	for _, p := range existing(keep) {
		args = append(args, "--ro-bind", p, p)
	}
	return args
}

// existing resolves each path's symlinks and drops those that do not exist.
func existing(paths []string) []string {
	var out []string
	for _, p := range paths {
		if real, err := filepath.EvalSymlinks(p); err == nil {
			out = append(out, real)
		}
	}
	return out
}

func (b *Bwrap) writeRoots(req Request) []string {
	roots := make([]string, 0, 1+len(b.WriteRoots))
	if req.WorkingDir != "" {
		roots = append(roots, req.WorkingDir)
	}
	roots = append(roots, b.WriteRoots...)
	return resolveRoots(roots)
}
