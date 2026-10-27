package sandbox

import (
	"context"
	"fmt"
	"os"
	"strings"
)

// Seatbelt runs commands through macOS sandbox-exec.
type Seatbelt struct {
	WriteRoots []string
	Network    string
}

func NewSeatbelt(writeRoots []string, network string) *Seatbelt {
	return &Seatbelt{WriteRoots: writeRoots, Network: network}
}

func (s *Seatbelt) Name() string { return "sandbox-exec" }

func (s *Seatbelt) Argv(req Request) (string, []string) {
	return "sandbox-exec", []string{"-p", s.profile(req), shellPath, "-c", req.Command}
}

func (s *Seatbelt) Run(ctx context.Context, req Request) (Response, error) {
	name, args := s.Argv(req)
	return runArgv(ctx, req, name, args)
}

func (s *Seatbelt) profile(req Request) string {
	var b strings.Builder
	b.WriteString("(version 1)\n")
	b.WriteString("(allow default)\n")
	b.WriteString("(deny file-write*)\n")
	b.WriteString("(allow file-write*\n")
	for _, root := range s.writeRoots(req) {
		fmt.Fprintf(&b, "  (subpath %q)\n", root)
	}
	b.WriteString("  (literal \"/dev/null\") (literal \"/dev/stdout\") (literal \"/dev/stderr\") (literal \"/dev/zero\")\n")
	b.WriteString("  (regex #\"^/dev/tty\"))\n")
	switch {
	case s.Network == "none":
		b.WriteString("(deny network*)\n")
	case egressPort > 0:
		// Egress is enforced here: outbound connections reach the proxy and
		// nothing else, so a program that ignores HTTPS_PROXY cannot connect
		// around the allowlist. Unix sockets stay open (ssh-agent, docker).
		b.WriteString("(deny network-outbound)\n")
		fmt.Fprintf(&b, "(allow network-outbound (remote ip \"localhost:%d\"))\n", egressPort)
		b.WriteString("(allow network-outbound (remote unix-socket))\n")
	}
	return b.String()
}

func (s *Seatbelt) writeRoots(req Request) []string {
	roots := make([]string, 0, 4+len(s.WriteRoots))
	if req.WorkingDir != "" {
		roots = append(roots, req.WorkingDir)
	}
	roots = append(roots, os.TempDir(), "/private/tmp", "/private/var/folders")
	roots = append(roots, s.WriteRoots...)
	return resolveRoots(roots)
}
