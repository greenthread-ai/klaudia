package trust

import (
	"os"
	"path/filepath"
	"strings"
)

// Roots is the per-session path context. Build it once with NewRoots: it stats
// the filesystem to canonicalise, which is not something to repeat per command.
type Roots struct {
	Project []string // canonical project roots, longest first
	Home    string   // canonical home directory
	TempDir string   // canonical TMPDIR
}

// NewRoots canonicalises the project roots and the home directory.
//
// A root of "/" is refused. On a CI image where the checkout is at the
// filesystem root, treating everything as project would silently turn the whole
// policy off — a feature that quietly stops applying is worse than one that
// visibly does not exist. The same goes for a root that is itself a system
// directory: adding /etc as a project root must not launder /etc.
func NewRoots(home string, projectRoots ...string) Roots {
	r := Roots{
		Home:    canonical(home),
		TempDir: canonical(os.TempDir()),
	}
	for _, p := range projectRoots {
		c := canonical(p)
		if c == "" || c == "/" || isSystemPrefix(c) {
			continue
		}
		r.Project = append(r.Project, c)
	}
	// Longest first, so a nested extra dir wins over its parent.
	for i := 1; i < len(r.Project); i++ {
		for j := i; j > 0 && len(r.Project[j]) > len(r.Project[j-1]); j-- {
			r.Project[j], r.Project[j-1] = r.Project[j-1], r.Project[j]
		}
	}
	return r
}

// canonical makes a path absolute and resolves symlinks as far as the
// filesystem allows.
//
// A path that does not exist yet is the normal case for a write, so resolution
// walks up to the deepest existing ancestor and rejoins the remainder. Without
// this, writing a *new* file under /etc would fail to resolve and the caller
// would compare an unresolved path against resolved policy prefixes — on macOS
// that means /etc/foo never matches /private/etc and the write looks harmless.
func canonical(p string) string {
	if p == "" {
		return ""
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return filepath.Clean(p)
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		return real
	}
	// Walk up to something that exists, resolve that, put the tail back.
	rest := ""
	cur := abs
	for {
		parent := filepath.Dir(cur)
		if parent == cur {
			return filepath.Clean(abs)
		}
		rest = filepath.Join(filepath.Base(cur), rest)
		if real, err := filepath.EvalSymlinks(parent); err == nil {
			return filepath.Join(real, rest)
		}
		cur = parent
	}
}

// under reports whether path is dir itself or inside it, matching on path
// components so /etc/nginx does not cover /etc/nginx-extra.
func under(path, dir string) bool {
	if dir == "" || path == "" {
		return false
	}
	if path == dir {
		return true
	}
	if !strings.HasSuffix(dir, string(filepath.Separator)) {
		dir += string(filepath.Separator)
	}
	return strings.HasPrefix(path, dir)
}

// hostPrefixes are this machine's operating system. Written unresolved; macOS
// canonicalisation (/etc → /private/etc) is applied at init.
var hostPrefixes = []string{
	"/etc", "/usr", "/opt", "/bin", "/sbin", "/boot", "/srv",
	"/Library", "/System", "/Applications", "/var",
	"/dev", "/proc", "/sys", "/lib", "/lib64", "/run", "/root",
}

// hostExceptions are inside a host prefix but are scratch space, not the OS.
var hostExceptions = []string{
	"/var/tmp", "/var/folders", "/private/var/tmp", "/private/var/folders",
}

// toolCaches are per-user directories that build tooling writes to as a matter
// of course. They live in $HOME but are not the user's configuration, and
// gating them would mean prompting on every `go build`, `npm ci` and `cargo
// build` — the exact friction that gets a protection switched off. Relative to
// home.
var toolCaches = []string{
	".cache", ".npm", ".yarn", ".pnpm-store", ".cargo", ".rustup", ".gradle",
	".m2", ".ivy2", ".nuget", ".stack", ".cabal", ".gem", ".bundle",
	".pub-cache", ".deno", ".bun", ".nvm", ".pyenv", ".rbenv", ".goenv",
	".local/share/virtualenvs", ".ollama",
	"go/pkg/mod", "go/bin",
	"Library/Caches", "Library/pnpm", "Library/Application Support/pnpm",
}

// userHostPaths are per-user files that configure the machine or its login
// environment. They are in $HOME and are emphatically not project work:
// appending to a shell rc is one of the likeliest real modifications an agent
// makes, and it persists into every future shell. Relative to home.
var userHostPaths = []string{
	".zshrc", ".zshenv", ".zprofile", ".zlogin", ".bashrc", ".bash_profile",
	".bash_login", ".profile", ".inputrc", ".config/fish/config.fish",
	".gitconfig", ".config/git/config",
	"Library/LaunchAgents", "Library/LaunchDaemons",
	".config/systemd/user", ".config/autostart",
	".config/environment.d",
}

// credentialPaths hold secrets. Relative to home unless absolute.
var credentialPaths = []string{
	".ssh", ".gnupg", ".aws", ".azure", ".config/gcloud", ".config/op",
	".kube/config", ".docker/config.json", ".netrc", ".npmrc", ".pypirc",
	".git-credentials", ".password-store", ".config/gh/hosts.yml",
	"Library/Keychains",
	"/Library/Keychains", "/etc/ssh",
}

// credentialExceptions are inside a credential directory but hold no secret.
// known_hosts in particular is touched constantly by ordinary ssh use.
var credentialExceptions = []string{
	".ssh/known_hosts", ".ssh/known_hosts.old", ".ssh/config",
}

// credentialSuffixes name key material wherever it lives.
var credentialSuffixes = []string{".pem", ".p12", ".pfx", ".key", ".keystore", ".jks"}

func init() {
	// macOS puts the real /etc, /var and /tmp under /private. Policy prefixes
	// must be in the same shape as the paths they are compared against, which
	// canonical() has already resolved.
	for _, p := range []string{"/etc", "/var", "/tmp"} {
		if real, err := filepath.EvalSymlinks(p); err == nil && real != p {
			hostPrefixes = append(hostPrefixes, real)
		}
	}
}

func isSystemPrefix(p string) bool {
	for _, h := range hostPrefixes {
		if p == h {
			return true
		}
	}
	return false
}

// ClassifyPath places an already-canonical path in a zone.
//
// Order matters and is deliberate:
//
//  1. Project roots win over everything. A project at /opt/app or
//     /usr/local/src/foo is ordinary work, and losing that would make the
//     feature unusable for anyone whose code does not live under $HOME.
//  2. Credentials next, because ~/.ssh is inside home and must not be caught
//     by the home-is-fine rule.
//  3. Tool caches, so builds stay silent.
//  4. Per-user host configuration — shell rcs, LaunchAgents.
//  5. System prefixes.
//  6. Anything else, including the rest of $HOME, is the user's own data and
//     is not the operating system. A deliberate decision, not an omission:
//     this protects the machine, not the home directory.
func (r Roots) ClassifyPath(path string) Zone {
	if path == "" {
		return ZoneProject
	}
	for _, root := range r.Project {
		if under(path, root) {
			return ZoneProject
		}
	}
	if r.isCredential(path) {
		return ZoneSensitive
	}
	if r.Home != "" {
		for _, c := range toolCaches {
			if under(path, filepath.Join(r.Home, c)) {
				return ZoneProject
			}
		}
		for _, h := range userHostPaths {
			if under(path, filepath.Join(r.Home, h)) {
				return ZoneHost
			}
		}
	}
	if r.TempDir != "" && under(path, r.TempDir) {
		return ZoneProject
	}
	for _, e := range hostExceptions {
		if under(path, e) {
			return ZoneProject
		}
	}
	for _, h := range hostPrefixes {
		if under(path, h) {
			return ZoneHost
		}
	}
	return ZoneProject
}

func (r Roots) isCredential(path string) bool {
	for _, e := range credentialExceptions {
		if r.Home != "" && path == filepath.Join(r.Home, e) {
			return false
		}
	}
	for _, s := range credentialSuffixes {
		if strings.HasSuffix(path, s) {
			return true
		}
	}
	for _, c := range credentialPaths {
		full := c
		if !filepath.IsAbs(c) {
			if r.Home == "" {
				continue
			}
			full = filepath.Join(r.Home, c)
		}
		if under(path, canonicalPolicy(full)) {
			return true
		}
	}
	return false
}

// canonicalPolicy resolves a policy path without the deepest-ancestor fallback:
// a policy entry that does not exist on this machine simply never matches.
func canonicalPolicy(p string) string {
	if real, err := filepath.EvalSymlinks(p); err == nil {
		return real
	}
	return filepath.Clean(p)
}

// Resolve turns a path as written on a command line into a canonical absolute
// path, relative to cwd, expanding a leading ~.
func (r Roots) Resolve(cwd, p string) string {
	if p == "" {
		return ""
	}
	if p == "~" || strings.HasPrefix(p, "~/") {
		if r.Home == "" {
			return ""
		}
		p = filepath.Join(r.Home, strings.TrimPrefix(strings.TrimPrefix(p, "~"), "/"))
	}
	if !filepath.IsAbs(p) {
		if cwd == "" {
			return ""
		}
		p = filepath.Join(cwd, p)
	}
	return canonical(p)
}
