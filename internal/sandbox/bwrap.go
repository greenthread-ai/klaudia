package sandbox

import "context"

// Bwrap runs commands through bubblewrap.
type Bwrap struct {
	WriteRoots []string
	Network    string
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
	if b.Network == "none" {
		args = append(args, "--unshare-net")
	}
	args = append(args, "--", shellPath, "-c", req.Command)
	return args
}

func (b *Bwrap) writeRoots(req Request) []string {
	roots := make([]string, 0, 1+len(b.WriteRoots))
	if req.WorkingDir != "" {
		roots = append(roots, req.WorkingDir)
	}
	roots = append(roots, b.WriteRoots...)
	return roots
}
