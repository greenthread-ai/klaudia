// Package version holds build/version metadata for the Klaudia Go binary.
//
// The version string intentionally matches the JS reference (08-entry.js)
// so the differential test harness sees identical `--version` output.
package version

import (
	"runtime/debug"
	"strings"
)

// Version is reported by `klaudia --version`. Mirrors the JS bundle's
// VERSION constant ("2.1.66-klaudia").
const Version = "2.1.66-klaudia"

// Name is the human-facing product name appended after the version.
const Name = "Klaudia"

// Build returns the commit this binary was built from, with a date, or "" when
// the toolchain embedded nothing (`go run`, or a build from a tarball).
//
// Version alone cannot answer the only question anyone asks of `--version`:
// "does this binary have the fix?". It is a constant mirroring a retired
// JavaScript bundle, so it has not moved in the lifetime of the Go port, while
// `go install` reports a pseudo-version like
// v0.0.0-20261014093743-a32e343be969 that pins the commit exactly. Two numbers,
// one real. This surfaces the real one — debugging a stale binary otherwise
// means comparing file mtimes against git log, which is how more than one
// session here has been spent.
func Build() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}

	var rev, when string
	dirty := false
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.time":
			when = s.Value
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}

	return describe(rev, when, info.Main.Version, dirty)
}

// describe is the pure half of Build, so both paths can be tested without a
// toolchain that embeds the metadata being tested.
func describe(rev, when, mainVersion string, dirty bool) string {
	// `go install module@version` builds from the module cache, where there is
	// no VCS to read; the pseudo-version carries the commit instead, as its
	// last hyphen-separated field.
	if rev == "" && mainVersion != "" && mainVersion != "(devel)" {
		if i := strings.LastIndex(mainVersion, "-"); i >= 0 {
			rev = mainVersion[i+1:]
		}
		if t := pseudoTimestamp(mainVersion); t != "" {
			when = t
		}
	}
	if rev == "" {
		return ""
	}

	out := shortRev(rev)
	if dirty {
		out += "-dirty" // built from a working tree with uncommitted changes
	}
	if when != "" {
		if len(when) > 10 {
			when = when[:10]
		}
		out += " (" + when + ")"
	}
	return out
}

// pseudoTimestamp pulls the yyyymmdd out of v0.0.0-20261014093743-a32e343be969
// and formats it as a date.
func pseudoTimestamp(v string) string {
	parts := strings.Split(v, "-")
	if len(parts) < 3 {
		return ""
	}
	ts := parts[len(parts)-2]
	if len(ts) < 8 {
		return ""
	}
	return ts[0:4] + "-" + ts[4:6] + "-" + ts[6:8]
}

func shortRev(rev string) string {
	if len(rev) > 7 {
		return rev[:7]
	}
	return rev
}

// Full is the complete `--version` line: the constant, the product name, and
// the build when one is known.
func Full() string {
	out := Version + " (" + Name + ")"
	if b := Build(); b != "" {
		out += " " + b
	}
	return out
}
