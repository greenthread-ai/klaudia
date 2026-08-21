// Package trust decides which zone an action falls into: work the agent may do
// on its own, and changes to this machine that need the user's agreement first.
//
// # What this is, and what it is not
//
// This is a guardrail against well-intentioned mistakes. It reads command lines
// and tool inputs; it does not observe syscalls. A command that computes its
// own target, runs a script the agent wrote earlier, or triggers a package
// postinstall hook will do whatever it does without this package noticing, and
// that is accepted rather than defended against.
//
// The security boundary is internal/sandbox, which is real kernel enforcement
// and is off by default. Nothing in this package should be described to a user
// in language that implies more than the above — see the copy test in
// internal/tui.
//
// # Shape of the decision
//
// Classification produces Effects, not verdicts. An Effect says "this writes
// /etc/nginx/nginx.conf" or "this restarts the service nginx"; deciding whether
// that needs asking is the caller's job, because it depends on policy and on
// which approvals are already live. Keeping the two apart is what lets the
// classifier be tested as a pure function over command lines.
package trust

import "strings"

// Zone is where an action lands. Ordered least to most protected so that
// combining effects is a max().
type Zone int

const (
	// ZoneTask is agent bookkeeping — todos, memory, skills. No side effects
	// outside Klaudia itself.
	ZoneTask Zone = iota
	// ZoneProject is work inside the project and its build caches: reading,
	// editing, building, testing, git, dev servers. Autonomous, including the
	// destructive parts — `rm -rf ./dist` and `git reset --hard` are ordinary
	// project work and prompting for them would defeat the point.
	ZoneProject
	// ZoneNetwork is fetching things. Autonomous.
	ZoneNetwork
	// ZoneRemote is an effect that lands on another machine or in a container.
	// Autonomous: governed by the task the user asked for, not by this
	// machine's protection. `ssh staging sudo systemctl restart nginx` is the
	// job; the same line without `ssh` is not.
	ZoneRemote
	// ZoneHost is a change to this machine's operating system.
	ZoneHost
	// ZoneSensitive is local credential material. Reading one to use it is
	// invisible to us and therefore free; naming one on a command line so its
	// contents are printed, copied or altered is not.
	ZoneSensitive
)

func (z Zone) String() string {
	switch z {
	case ZoneTask:
		return "task"
	case ZoneProject:
		return "project"
	case ZoneNetwork:
		return "network"
	case ZoneRemote:
		return "remote"
	case ZoneHost:
		return "host"
	case ZoneSensitive:
		return "sensitive"
	}
	return "unknown"
}

// Protected reports whether a zone needs the user's agreement before acting.
func (z Zone) Protected() bool { return z == ZoneHost || z == ZoneSensitive }

// Kind is what an effect does. Split finely enough that a grant to install a
// package cannot authorise removing one.
type Kind string

const (
	KindRead           Kind = "read"
	KindWrite          Kind = "write"
	KindDelete         Kind = "delete"
	KindExec           Kind = "exec"
	KindServiceControl Kind = "service-control"
	KindPackageInstall Kind = "package-install"
	KindPackageRemove  Kind = "package-remove"
	KindUserAdmin      Kind = "user-admin"
	KindNetAdmin       Kind = "net-admin"
	KindFirewall       Kind = "firewall"
	KindMount          Kind = "mount"
	KindKernelParam    Kind = "kernel-param"
	KindMachineEnv     Kind = "machine-env"
	KindPower          Kind = "power"
	KindCredDisclose   Kind = "cred-disclose"
	KindCredModify     Kind = "cred-modify"
	// KindDestructiveBulk is a recursive delete aimed somewhere that would ruin
	// someone's day regardless of zone — /, a top-level directory, $HOME, or a
	// project root itself.
	KindDestructiveBulk Kind = "destructive-bulk"
	// KindOpaque is a script this reader could not follow: assembled at runtime,
	// unparseable, or nested deeper than it recurses. It counts as mutating, so
	// it asks. An unreadable script could do anything, and saying "I could not
	// read this" is more useful than silently reporting that it does nothing.
	KindOpaque Kind = "opaque"
)

// Mutating reports whether a kind changes anything. Reads are cheap to allow
// and expensive to prompt for: `cat /etc/os-release` must never ask, or the
// feature gets switched off within a day.
func (k Kind) Mutating() bool { return k != KindRead && k != KindExec }

// Resource identifies what an effect lands on.
type Resource struct {
	Class string // "path" | "service" | "package" | "user" | "firewall" | "mount" | "sysctl" | "env" | "cred" | "host"
	ID    string // canonical: cleaned absolute path, unit name without .service, package name
}

func (r Resource) String() string { return r.Class + ":" + r.ID }

// Target is where an effect lands. Local is the machine Klaudia runs on.
type Target struct {
	Local bool
	Host  string // "staging", "deploy@prod", "container:api"
	Via   string // "ssh" | "scp" | "rsync" | "docker" | "kubectl"
	Label string // display form, verbatim from the command line
}

// LocalTarget is this machine.
func LocalTarget() Target { return Target{Local: true} }

// Effect is one thing a tool call does.
type Effect struct {
	Zone     Zone
	Kind     Kind
	Res      Resource
	Target   Target
	Evidence string // the argv fragment or path that produced this, for the UI
	// Certain is false when the effect was derived from something the source
	// does not fully determine — an unexpanded variable, an unresolvable
	// working directory, a recursion limit. It is the single funnel for every
	// "I cannot read this" case, so policy needs one branch rather than a
	// dozen. An uncertain mutating effect must never be auto-approved, and an
	// uncertain read is not worth a prompt.
	Certain bool
}

// Assessment is everything a tool call was found to do.
type Assessment struct {
	Effects []Effect
	Targets []Target
	// Unparsed means the command line could not be read at all. Treated as
	// needing agreement: failing open here would make the guardrail defeatable
	// by prepending garbage, which is worse than a rare prompt.
	Unparsed bool
	Reason   string
}

// Zone returns the most protected zone in the assessment.
func (a Assessment) Zone() Zone {
	z := ZoneTask
	for _, e := range a.Effects {
		if e.Zone > z {
			z = e.Zone
		}
	}
	return z
}

// NeedsAgreement reports whether anything here should be put to the user, and
// returns the effects responsible.
func (a Assessment) NeedsAgreement() ([]Effect, bool) {
	var out []Effect
	for _, e := range a.Effects {
		switch {
		case e.Zone.Protected():
			out = append(out, e)
		case e.Kind == KindDestructiveBulk:
			// Zone-independent: a recursive delete aimed at a root is a
			// catastrophe whether or not the path is "project work".
			out = append(out, e)
		case !e.Certain && e.Kind.Mutating():
			// We could not tell what it changes. Say so rather than guess.
			out = append(out, e)
		}
	}
	return out, len(out) > 0 || a.Unparsed
}

// Summary renders the effects as a short human list, newest concern first.
func (a Assessment) Summary() string {
	if a.Unparsed {
		return "could not read this command line"
	}
	seen := map[string]bool{}
	var parts []string
	for _, e := range a.Effects {
		if !e.Zone.Protected() && e.Certain && e.Kind != KindDestructiveBulk {
			continue
		}
		line := e.Describe()
		if !seen[line] {
			seen[line] = true
			parts = append(parts, line)
		}
	}
	return strings.Join(parts, ", ")
}

// Describe renders one effect as a phrase a person can read.
func (e Effect) Describe() string {
	if e.Kind == KindOpaque || e.Res.ID == "" {
		if !e.Certain {
			switch e.Res.Class {
			case "path":
				return "writes a path it does not determine statically"
			default:
				return "does something this reader could not determine"
			}
		}
	}
	verb := map[Kind]string{
		KindRead: "reads", KindWrite: "writes", KindDelete: "deletes",
		KindExec: "runs", KindServiceControl: "controls service",
		KindPackageInstall: "installs package", KindPackageRemove: "removes package",
		KindUserAdmin: "changes user", KindNetAdmin: "changes networking",
		KindFirewall: "changes firewall", KindMount: "changes mounts",
		KindKernelParam: "sets kernel parameter", KindMachineEnv: "changes machine environment",
		KindPower: "powers off or reboots", KindCredDisclose: "discloses credential",
		KindCredModify: "modifies credential", KindDestructiveBulk: "recursively deletes",
		KindOpaque: "runs a script this reader could not follow",
	}[e.Kind]
	if verb == "" {
		verb = string(e.Kind)
	}
	out := verb + " " + e.Res.ID
	if !e.Certain {
		// The effect was found but not confirmed — an unexpanded variable, an
		// unresolvable working directory. Naming it is more useful than
		// collapsing it to "something", but the hedge has to be visible.
		out += " (probably)"
	}
	return out
}
