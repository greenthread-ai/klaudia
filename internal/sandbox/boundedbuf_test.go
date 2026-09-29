package sandbox

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"testing"
)

func TestHeadTailBuffer(t *testing.T) {
	b := newHeadTailBuffer(4)
	// Written in uneven pieces: "abcd" head, "wxyz" last four, 6 dropped.
	for _, p := range []string{"ab", "cdEFGHIJ", "wx", "yz"} {
		b.Write([]byte(p))
	}
	got := b.String()
	if !strings.HasPrefix(got, "abcd") || !strings.HasSuffix(got, "wxyz") || !strings.Contains(got, "[6 bytes") {
		t.Errorf("String = %q, want head abcd, 6 dropped, tail wxyz", got)
	}

	small := newHeadTailBuffer(4)
	small.Write([]byte("hi there"))
	if small.String() != "hi there" {
		t.Errorf("under the limit: %q, want the output unchanged", small.String())
	}
}

// A command that prints far more than the cap: memory stays bounded and the
// start and end of its output survive.
func TestRunBoundsOutput(t *testing.T) {
	if _, err := exec.LookPath("head"); err != nil {
		t.Skip("no head")
	}
	resp, err := NewLocal().Run(context.Background(), Request{
		Command: "echo START; head -c 40000000 /dev/zero | tr '\\0' x; echo; echo END",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Stdout) > 2*streamKeep+200 {
		t.Errorf("stdout held %d bytes, want at most ~%d", len(resp.Stdout), 2*streamKeep)
	}
	if !strings.HasPrefix(resp.Stdout, "START") || !strings.HasSuffix(strings.TrimSpace(resp.Stdout), "END") {
		t.Errorf("start or end of the output lost: %q … %q", resp.Stdout[:10], resp.Stdout[len(resp.Stdout)-10:])
	}
	if !bytes.Contains([]byte(resp.Stdout), []byte("dropped while the command ran")) {
		t.Error("no note of the dropped middle")
	}
}
