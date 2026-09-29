package version

import (
	"runtime/debug"
	"strings"
	"testing"
)

// These tests build their own debug.BuildInfo: a `go test` binary carries no
// VCS settings, so nothing here may depend on what Get() returns.

func vcs(rev, modified, time string) []debug.BuildSetting {
	return []debug.BuildSetting{
		{Key: "-compiler", Value: "gc"},
		{Key: "vcs", Value: "git"},
		{Key: "vcs.revision", Value: rev},
		{Key: "vcs.time", Value: time},
		{Key: "vcs.modified", Value: modified},
	}
}

const rev = "abcdef1234567890abcdef1234567890abcdef12"

func TestFromBuildInfoCheckoutBuild(t *testing.T) {
	bi := &debug.BuildInfo{
		GoVersion: "go1.26.8",
		Main:      debug.Module{Path: "github.com/greenthread-ai/klaudia", Version: "v0.0.0-20260929101010-abcdef123456+dirty"},
		Settings:  vcs(rev, "true", "2026-09-29T10:10:10Z"),
	}
	info := fromBuildInfo(bi, "", "")
	if info.Release != "" {
		t.Errorf("a pseudo-version is not a release, got Release=%q", info.Release)
	}
	if info.Revision != rev || !info.Modified || info.Time != "2026-09-29T10:10:10Z" {
		t.Errorf("vcs facts not read: %+v", info)
	}
	if got := info.Short(); got != "abcdef123456+dirty" {
		t.Errorf("Short = %q", got)
	}
	want := "abcdef123456+dirty (committed 2026-09-29T10:10:10Z, go1.26.8)"
	if got := info.Summary(); got != want {
		t.Errorf("Summary = %q, want %q", got, want)
	}
}

func TestFromBuildInfoTaggedRelease(t *testing.T) {
	bi := &debug.BuildInfo{
		GoVersion: "go1.26.8",
		Main:      debug.Module{Version: "v1.2.3"},
		Settings:  vcs(rev, "false", "2026-09-29T10:10:10Z"),
	}
	info := fromBuildInfo(bi, "", "")
	if got := info.Short(); got != "v1.2.3" {
		t.Errorf("Short = %q", got)
	}
	if got := info.Summary(); !strings.Contains(got, "commit abcdef123456,") {
		t.Errorf("a release summary should still name the commit: %q", got)
	}
}

func TestFromBuildInfoStampedOverrides(t *testing.T) {
	bi := &debug.BuildInfo{
		Main:     debug.Module{Version: "v0.0.0-20260929101010-abcdef123456"},
		Settings: vcs(rev, "false", ""),
	}
	info := fromBuildInfo(bi, "v2.0.0", "0123456789ab")
	if info.Release != "v2.0.0" || info.Revision != "0123456789ab" {
		t.Errorf("stamped values should win: %+v", info)
	}
	if got := info.Short(); got != "v2.0.0" {
		t.Errorf("Short = %q", got)
	}
}

func TestFromBuildInfoNothingKnown(t *testing.T) {
	// go run / go test / -buildvcs=false: "(devel)" and no vcs settings.
	info := fromBuildInfo(&debug.BuildInfo{GoVersion: "go1.26.8", Main: debug.Module{Version: "(devel)"}}, "", "")
	if info.Module != "" || info.Release != "" || info.Revision != "" {
		t.Errorf("nothing should be inferred: %+v", info)
	}
	if got := info.Short(); got != "dev" {
		t.Errorf("Short = %q", got)
	}
	if got := info.Summary(); got != "dev (no commit recorded, go1.26.8)" {
		t.Errorf("Summary = %q", got)
	}
	// No build info at all still yields the toolchain.
	if got := fromBuildInfo(nil, "", ""); got.GoVersion == "" || got.Short() != "dev" {
		t.Errorf("nil build info: %+v", got)
	}
}

func TestLineKeepsCompatibilityFirstLine(t *testing.T) {
	first, rest, ok := strings.Cut(Line(), "\n")
	if first != "2.1.66-klaudia (Klaudia)" {
		t.Errorf("first line = %q; tools parse it, keep it unchanged", first)
	}
	if !ok || !strings.HasPrefix(rest, "build ") {
		t.Errorf("second line should carry the build: %q", rest)
	}
}
