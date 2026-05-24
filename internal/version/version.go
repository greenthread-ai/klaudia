// Package version holds build/version metadata for the Klaudia Go binary.
//
// The version string intentionally matches the JS reference (08-entry.js)
// so the differential test harness sees identical `--version` output.
package version

// Version is reported by `klaudia --version`. Mirrors the JS bundle's
// VERSION constant ("2.1.66-klaudia").
const Version = "2.1.66-klaudia"

// Name is the human-facing product name appended after the version.
const Name = "Klaudia"
