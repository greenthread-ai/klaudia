package trust

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Grants are what an approval buys.
//
// The point of the whole design is that the user agrees to an *operation*, not
// to a command. "Install nginx and configure it as a development proxy" is one
// decision; the package install, the config directory, the write, the validate
// and the restart that carry it out are not five more. So an approval mints a
// grant describing the operation's reach, and every later effect is checked
// against it.
//
// Three rules hold this together and are enforced here rather than trusted to
// callers:
//
//   - Grants live in memory for the session and are never written to disk. A
//     persisted grant is a permission the user no longer remembers giving.
//   - No wildcards. A grant names paths, services and packages; it never names
//     a pattern. Widening is done by this package, from a real path, with
//     bounds — not by whatever string the model supplied.
//   - Kinds are not fungible. Approving an install does not approve a removal,
//     and approving a write does not approve a recursive delete.
type Grant struct {
	ID      string
	Summary string
	Reason  string
	Scope   Scope
	Granted time.Time
	revoked bool
}

// Scope is the reach of one grant. Every field is a list of exact names; there
// is no pattern syntax by construction.
type Scope struct {
	Paths    []string // canonical directory or file prefixes
	Services []string // unit names, without .service/.plist
	Packages []string // package names, without the manager prefix
	Kinds    []Kind   // the kinds this grant authorises
}

// Request is what the model declares before doing anything to this machine.
type Request struct {
	Summary  string
	Reason   string
	Paths    []string
	Services []string
	Packages []string
	Kinds    []Kind // optional; inferred from the other fields when empty
}

// Ledger holds the session's live grants.
type Ledger struct {
	mu     sync.Mutex
	grants []*Grant
	seq    int
	roots  Roots
}

// NewLedger returns an empty ledger bound to the session's roots, which it needs
// to canonicalise the paths a request names.
func NewLedger(roots Roots) *Ledger { return &Ledger{roots: roots} }

// ErrWildcard is returned when a request tries to name a pattern instead of a
// path. Widening is this package's job precisely so that "grant me /etc/**"
// never becomes expressible.
type ErrWildcard struct{ Value string }

func (e ErrWildcard) Error() string {
	return fmt.Sprintf("%q is a pattern, not a path: grants name exact paths and are widened by Klaudia, not by the requester", e.Value)
}

// ErrTooBroad is returned when a request names a whole system directory.
type ErrTooBroad struct{ Value string }

func (e ErrTooBroad) Error() string {
	return fmt.Sprintf("%q covers a whole system directory, which is more than any single operation needs", e.Value)
}

// Mint validates a request, widens it to the shape of the operation, and
// records the grant. The caller is responsible for having asked the user first;
// this function does not prompt.
func (l *Ledger) Mint(req Request) (*Grant, error) {
	scope, err := l.scopeFor(req)
	if err != nil {
		return nil, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.seq++
	g := &Grant{
		ID:      fmt.Sprintf("g%d", l.seq),
		Summary: strings.TrimSpace(req.Summary),
		Reason:  strings.TrimSpace(req.Reason),
		Scope:   scope,
		Granted: time.Now(),
	}
	l.grants = append(l.grants, g)
	return g, nil
}

func (l *Ledger) scopeFor(req Request) (Scope, error) {
	var s Scope
	for _, p := range req.Paths {
		if strings.ContainsAny(p, "*?[") {
			return Scope{}, ErrWildcard{Value: p}
		}
		full := l.roots.Resolve(firstNonEmpty(l.roots.Project...), p)
		if full == "" {
			continue
		}
		w := widenPath(full)
		if w == "" {
			return Scope{}, ErrTooBroad{Value: p}
		}
		s.Paths = appendUnique(s.Paths, w)
	}
	for _, svc := range req.Services {
		if strings.ContainsAny(svc, "*?") {
			return Scope{}, ErrWildcard{Value: svc}
		}
		s.Services = appendUnique(s.Services, normaliseService(svc))
	}
	for _, pkg := range req.Packages {
		if strings.ContainsAny(pkg, "*?") {
			return Scope{}, ErrWildcard{Value: pkg}
		}
		s.Packages = appendUnique(s.Packages, normalisePackage(pkg))
	}
	s.Kinds = inferKinds(req)
	return s, nil
}

// inferKinds turns a request into the set of kinds it authorises.
//
// The defaults are the least surprising reading of what was asked for: naming a
// path authorises writing and deleting *within* it, naming a service authorises
// controlling it, naming a package authorises installing it. Removal and bulk
// deletion are never inferred — those have to be asked for explicitly, because
// "install nginx" should not quietly cover "uninstall postgres".
func inferKinds(req Request) []Kind {
	if len(req.Kinds) > 0 {
		out := make([]Kind, 0, len(req.Kinds))
		for _, k := range req.Kinds {
			if k == KindDestructiveBulk || k == KindOpaque {
				continue // never grantable in advance
			}
			out = appendUniqueKind(out, k)
		}
		return out
	}
	var out []Kind
	if len(req.Paths) > 0 {
		out = append(out, KindRead, KindWrite, KindDelete)
	}
	if len(req.Services) > 0 {
		out = append(out, KindServiceControl)
	}
	if len(req.Packages) > 0 {
		out = append(out, KindPackageInstall)
	}
	return out
}

// widenPath turns an approved path into the prefix a grant covers.
//
// Approving /etc/nginx/nginx.conf has to cover /etc/nginx, or the second step
// of the approved operation — writing conf.d/proxy.conf, say — drifts and
// prompts again, which is the fatigue this whole design exists to remove.
//
// The widening stops well short of anything dangerous. A path whose parent is a
// system root (/etc, /usr, /Library) is granted exactly, not widened: approving
// /etc/hosts must not hand over /etc. Returns "" when the path itself is a
// system root or the filesystem root, which is never grantable.
func widenPath(p string) string {
	p = filepath.Clean(p)
	if p == "/" || p == "." {
		return ""
	}
	if isSystemPrefix(p) || depth(p) <= 1 {
		return ""
	}
	parent := filepath.Dir(p)
	if parent == "/" || isSystemPrefix(parent) || depth(parent) <= 1 {
		return p
	}
	// A path that already looks like a directory is its own prefix; a file gets
	// its directory. We cannot stat: the whole point of a grant is often to
	// create something that does not exist yet, so this goes on the shape of the
	// name. An extensionless leaf (/usr/local/bin/tool) is therefore granted
	// exactly rather than widened — narrower than ideal, but wrong in the
	// harmless direction.
	if filepath.Ext(p) == "" {
		return p
	}
	return parent
}

// depth counts path components below the root, so /etc is 1 and /etc/nginx is 2.
func depth(p string) int {
	t := strings.Trim(filepath.ToSlash(p), "/")
	if t == "" {
		return 0
	}
	return strings.Count(t, "/") + 1
}

func normaliseService(s string) string {
	s = strings.TrimSpace(s)
	for _, suffix := range []string{".service", ".plist", ".socket", ".timer"} {
		s = strings.TrimSuffix(s, suffix)
	}
	return s
}

// normalisePackage drops any manager prefix, so a grant for "nginx" matches
// whether the effect came from apt-get, brew or dnf. The manager is not the
// thing the user agreed to; the package is.
func normalisePackage(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndexByte(s, ':'); i >= 0 {
		s = s[i+1:]
	}
	return s
}

// Cover splits effects into those a live grant authorises and those it does not.
//
// Only protected effects are checked: project work was never gated, and running
// it past the ledger would mean a grant could somehow *narrow* what is allowed.
func (l *Ledger) Cover(effects []Effect) (covered, drift []Effect) {
	l.mu.Lock()
	live := make([]*Grant, 0, len(l.grants))
	for _, g := range l.grants {
		if !g.revoked {
			live = append(live, g)
		}
	}
	l.mu.Unlock()

	for _, e := range effects {
		if coveredBy(live, e) {
			covered = append(covered, e)
			continue
		}
		drift = append(drift, e)
	}
	return covered, drift
}

func coveredBy(grants []*Grant, e Effect) bool {
	// Two things are never covered in advance, however broad the grant: a
	// recursive delete of a root, and a script we could not read. Both are
	// cases where the user cannot have known what they were agreeing to.
	if e.Kind == KindDestructiveBulk || e.Kind == KindOpaque {
		return false
	}
	if !e.Certain {
		// An effect we could not pin down cannot be matched against a scope
		// honestly. Saying "covered" here would mean a grant for /etc/nginx
		// silently authorising `> "$TARGET"`.
		return false
	}
	for _, g := range grants {
		if g.Scope.allows(e) {
			return true
		}
	}
	return false
}

func (s Scope) allows(e Effect) bool {
	if !hasKind(s.Kinds, e.Kind) {
		return false
	}
	switch e.Res.Class {
	case "path":
		for _, p := range s.Paths {
			if !under(e.Res.ID, p) {
				continue
			}
			// A grant lets Klaudia change what is *inside* the scope, not
			// remove the scope itself. Approving "configure
			// /etc/nginx/nginx.conf" widens to /etc/nginx so the rest of the
			// operation can proceed; it must not thereby authorise
			// `rm -rf /etc/nginx`.
			if e.Kind == KindDelete && e.Res.ID == p {
				continue
			}
			return true
		}
	case "service":
		for _, svc := range s.Services {
			if svc == normaliseService(e.Res.ID) {
				return true
			}
		}
	case "package":
		for _, pkg := range s.Packages {
			if pkg == normalisePackage(e.Res.ID) {
				return true
			}
		}
	}
	// Everything else — users, firewall, mounts, kernel parameters, power,
	// credentials — has no scope vocabulary and so is never pre-granted. Each
	// one is asked for at the point it happens.
	return false
}

// Describe renders a grant for /trust.
func (g *Grant) Describe() string {
	var parts []string
	if len(g.Scope.Paths) > 0 {
		parts = append(parts, "paths "+strings.Join(g.Scope.Paths, ", "))
	}
	if len(g.Scope.Services) > 0 {
		parts = append(parts, "services "+strings.Join(g.Scope.Services, ", "))
	}
	if len(g.Scope.Packages) > 0 {
		parts = append(parts, "packages "+strings.Join(g.Scope.Packages, ", "))
	}
	if len(parts) == 0 {
		return g.Summary
	}
	return g.Summary + " — " + strings.Join(parts, "; ")
}

// Revoked reports whether this grant has been withdrawn.
func (g *Grant) Revoked() bool { return g.revoked }

// List returns the live grants, oldest first.
func (l *Ledger) List() []*Grant {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]*Grant, 0, len(l.grants))
	for _, g := range l.grants {
		if !g.revoked {
			out = append(out, g)
		}
	}
	return out
}

// Revoke withdraws one grant. Revocation is immediate: the next effect checked
// against the ledger will drift.
func (l *Ledger) Revoke(id string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, g := range l.grants {
		if g.ID == id && !g.revoked {
			g.revoked = true
			return true
		}
	}
	return false
}

// RevokeAll withdraws everything, for `/trust revoke all` and for the end of a
// turn the user interrupted.
func (l *Ledger) RevokeAll() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for _, g := range l.grants {
		if !g.revoked {
			g.revoked = true
			n++
		}
	}
	return n
}

func hasKind(ks []Kind, k Kind) bool {
	for _, x := range ks {
		if x == k {
			return true
		}
	}
	return false
}

func appendUnique(ss []string, s string) []string {
	if s == "" {
		return ss
	}
	for _, x := range ss {
		if x == s {
			return ss
		}
	}
	return append(ss, s)
}

func appendUniqueKind(ks []Kind, k Kind) []Kind {
	for _, x := range ks {
		if x == k {
			return ks
		}
	}
	return append(ks, k)
}

// MintFromEffects records a grant covering exactly what was just approved.
//
// This is the path taken when the user agrees to a change at the moment it is
// attempted — the scope-drift card, and any frontend with no declaration tool
// wired up. Approving one effect still widens to the operation, so the rest of
// it proceeds; what it cannot do is invent reach the effects did not show.
//
// Effects that are not grantable in advance — a recursive delete of a root, an
// unreadable script, an effect we could not pin down — are silently left out.
// The caller has allowed this one call; it has not signed up for the next one.
func (l *Ledger) MintFromEffects(summary, reason string, effects []Effect) (*Grant, error) {
	var req Request
	req.Summary = summary
	req.Reason = reason
	for _, e := range effects {
		if !e.Certain || e.Kind == KindDestructiveBulk || e.Kind == KindOpaque {
			continue
		}
		switch e.Res.Class {
		case "path":
			req.Paths = appendUnique(req.Paths, e.Res.ID)
		case "service":
			req.Services = appendUnique(req.Services, e.Res.ID)
		case "package":
			req.Packages = appendUnique(req.Packages, e.Res.ID)
		default:
			continue // no scope vocabulary: asked for again next time
		}
		req.Kinds = appendUniqueKind(req.Kinds, e.Kind)
	}
	if len(req.Paths) == 0 && len(req.Services) == 0 && len(req.Packages) == 0 {
		// Nothing durable to record. The call proceeds on the caller's say-so
		// and the next one asks again, which is the right outcome for a bulk
		// delete or an unreadable line.
		return nil, nil
	}
	// A path we would refuse to grant outright (a whole system directory) is
	// dropped rather than failing the whole approval: the user said yes to this
	// call, and the only question is how much of it to remember.
	kept := req.Paths[:0]
	for _, p := range req.Paths {
		if widenPath(p) != "" {
			kept = append(kept, p)
		}
	}
	req.Paths = kept
	if len(req.Paths) == 0 && len(req.Services) == 0 && len(req.Packages) == 0 {
		return nil, nil
	}
	return l.Mint(req)
}
