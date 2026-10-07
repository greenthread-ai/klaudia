// Package version holds build/version metadata for the Klaudia Go binary.
//
// There are two different things called "version" here, and they answer
// different questions:
//
//   - Version ("2.1.66-klaudia") is the compatibility string. It names the
//     Claude Code release the port tracks, and it is what goes on the wire and
//     into files other tools read: the transcript "version" field, the MCP
//     clientInfo, the first line of --version. It changes only when the
//     reference does.
//   - Get() is which build this binary is: the module version, the commit it
//     was built from, whether the tree had uncommitted changes, and when that
//     commit was made. That is what a bug report needs, and what tells a stale
//     installed binary from a fresh one.
//
// Build facts come from runtime/debug.ReadBuildInfo, which `go build` and
// `go install` fill from the checkout's VCS state. `go test` and `go run`
// binaries carry no VCS facts, and neither does a build from a source tarball
// or one with -buildvcs=false; release builds can stamp the facts explicitly:
//
//	go build -ldflags "-X github.com/greenthread-ai/klaudia/internal/version.release=v1.2.3 \
//	  -X github.com/greenthread-ai/klaudia/internal/version.commit=$(git rev-parse HEAD)" ./cmd/klaudia
//
// A stamped value wins over the one inferred from build info.
package version

import (
	"regexp"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
)

// Reference is the Claude Code release this port tracks.
const Reference = "2.1.66"

// Version is the compatibility version string: the first line of
// `klaudia --version`, the transcript "version" field and the MCP clientInfo
// version. Kept stable for the tools that read those; see Get for the build.
const Version = Reference + "-klaudia"

// Name is the human-facing product name appended after the version.
const Name = "Klaudia"

// Overridable at link time with -ldflags -X (see the package comment). They are
// variables rather than constants because -X can only set a string variable.
var (
	release string // e.g. "v1.2.3"
	commit  string // full or abbreviated commit hash
)

// Info is what is known about the build of the running binary. Every field
// may be empty: a test binary, `go run`, or a -buildvcs=false build knows
// little more than the Go version.
type Info struct {
	Module    string // main module version: "v1.2.3", a pseudo-version, or ""
	Release   string // a release name, stamped or read from a tagged module version
	Revision  string // commit hash
	Modified  bool   // the working tree had uncommitted changes at build time
	Time      string // commit time, RFC 3339, as the VCS reported it
	GoVersion string // toolchain that built the binary
}

var get = sync.OnceValue(func() Info {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		bi = nil
	}
	return fromBuildInfo(bi, release, commit)
})

// Get reports the running binary's build. Computed once.
func Get() Info { return get() }

// pseudoVersion matches the tail of a Go pseudo-version
// (v0.0.0-20260929101010-abcdef123456), which names a commit, not a release.
var pseudoVersion = regexp.MustCompile(`\d{14}-[0-9a-f]{12}(\+dirty)?$`)

// fromBuildInfo is Get without the process-wide state, so it can be tested
// against build info that a test binary would never have.
func fromBuildInfo(bi *debug.BuildInfo, stampedRelease, stampedCommit string) Info {
	info := Info{GoVersion: runtime.Version()}
	if bi != nil {
		if bi.GoVersion != "" {
			info.GoVersion = bi.GoVersion
		}
		if v := bi.Main.Version; v != "" && v != "(devel)" {
			info.Module = v
			if !pseudoVersion.MatchString(v) {
				info.Release = v
			}
		}
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.revision":
				info.Revision = s.Value
			case "vcs.modified":
				info.Modified = s.Value == "true"
			case "vcs.time":
				info.Time = s.Value
			}
		}
	}
	if stampedRelease != "" {
		info.Release = stampedRelease
	}
	if stampedCommit != "" {
		info.Revision = stampedCommit
	}
	return info
}

// shortRev abbreviates a commit hash to the 12 characters a Go pseudo-version uses.
func shortRev(rev string) string {
	if len(rev) > 12 {
		return rev[:12]
	}
	return rev
}

// Short names the build in a few characters, for the startup banner: the
// release when there is one, else the commit (with "+dirty" when the tree had
// uncommitted changes), else "dev".
func (i Info) Short() string {
	switch {
	case i.Release != "":
		return i.Release
	case i.Revision != "":
		s := shortRev(i.Revision)
		if i.Modified {
			s += "+dirty"
		}
		return s
	default:
		return "dev"
	}
}

// Summary is Short plus whatever else is known, on one line, for /doctor and
// --version: "abcdef123456+dirty (committed 2026-09-29T10:10:10Z, go1.26.8)".
func (i Info) Summary() string {
	var extra []string
	if i.Release != "" && i.Revision != "" {
		rev := "commit " + shortRev(i.Revision)
		if i.Modified {
			rev += "+dirty"
		}
		extra = append(extra, rev)
	}
	if i.Revision == "" {
		extra = append(extra, "no commit recorded")
	}
	if i.Time != "" {
		extra = append(extra, "committed "+i.Time)
	}
	if i.GoVersion != "" {
		extra = append(extra, i.GoVersion)
	}
	if len(extra) == 0 {
		return i.Short()
	}
	return i.Short() + " (" + strings.Join(extra, ", ") + ")"
}

// Line is the full `klaudia --version` output (without the trailing newline).
// The first line is the compatibility string, unchanged, so anything that
// parses it keeps working; the build follows on its own line.
func Line() string {
	return Version + " (" + Name + ")\nbuild " + Get().Summary()
}
