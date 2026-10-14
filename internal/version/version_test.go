package version

import (
	"strings"
	"testing"
)

func TestDescribeCoversBothBuildPaths(t *testing.T) {
	cases := []struct {
		name               string
		rev, when, mainVer string
		dirty              bool
		want               string
	}{{
		name: "local build reads VCS settings",
		rev:  "a32e343be9691f0c4e2f", when: "2026-10-14T09:37:43Z", mainVer: "(devel)",
		want: "a32e343 (2026-10-14)",
	}, {
		name: "uncommitted changes are flagged",
		rev:  "a32e343be9691f0c4e2f", when: "2026-10-14T09:37:43Z", mainVer: "(devel)", dirty: true,
		want: "a32e343-dirty (2026-10-14)",
	}, {
		// `go install github.com/...@main` builds from the module cache: no
		// VCS settings at all, and the commit lives in the pseudo-version.
		// Normalised to the same 7 characters as the local path, so a binary
		// from `go install` and one from `go build` compare at sight.
		name:    "module install falls back to the pseudo-version",
		mainVer: "v0.0.0-20261014093743-a32e343be969",
		want:    "a32e343 (2026-10-14)",
	}, {
		name:    "tagged release has no commit to report",
		mainVer: "v1.2.3",
		want:    "",
	}, {
		name: "nothing embedded",
		want: "",
	}}

	for _, tc := range cases {
		if got := describe(tc.rev, tc.when, tc.mainVer, tc.dirty); got != tc.want {
			t.Errorf("%s: describe = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestFullAlwaysCarriesTheConstant(t *testing.T) {
	// The constant mirrors the retired JS bundle and the parity harness; the
	// build suffix is additive, never a replacement.
	if !strings.HasPrefix(Full(), Version+" ("+Name+")") {
		t.Errorf("Full() = %q, must start with the version and name", Full())
	}
}
