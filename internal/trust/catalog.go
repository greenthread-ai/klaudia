package trust

import (
	"strings"

	"github.com/greenthread-ai/klaudia/internal/native/bashparser"
)

// Command identity is the primary signal, not paths.
//
// A path-only classifier misses most of what actually changes a machine:
// `systemctl restart nginx`, `defaults write`, `launchctl load`, `usermod -aG`,
// `ufw allow 80` and `sysctl -w` all reconfigure the host while carrying no
// host path in their arguments at all. So programs are classified by what they
// are, and paths refine the answer.
//
// The tables below are deliberately incomplete and always will be. Coverage is
// documented by the corpus test rather than claimed here.

// cmdCtx is the state a single simple command is classified against.
type cmdCtx struct {
	roots  Roots
	cwd    string // simulated working directory at this point in the line
	cwdOK  bool   // false once a `cd` we could not read has happened
	priv   bool   // reached through sudo/doas/pkexec
	prog   string // the program these arguments belong to, after wrapper stripping
	target Target
}

// detector turns one command's arguments into effects.
type detector func(c *cmdCtx, args []bashparser.Word) []Effect

// base strips a directory from a program name so /usr/bin/systemctl and
// systemctl are the same program.
func base(name string) string {
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		return name[i+1:]
	}
	return name
}

// literals returns the arguments whose text is complete, and whether any were
// dropped for being unreadable.
func literals(args []bashparser.Word) ([]string, bool) {
	out := make([]string, 0, len(args))
	dropped := false
	for _, a := range args {
		if a.Literal {
			out = append(out, a.Text)
			continue
		}
		dropped = true
	}
	return out, dropped
}

// operands returns non-flag arguments.
func operands(args []string) []string {
	out := make([]string, 0, len(args))
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			out = append(out, a)
		}
	}
	return out
}

func hasFlag(args []string, flags ...string) bool {
	for _, a := range args {
		for _, f := range flags {
			if a == f || strings.HasPrefix(a, f+"=") {
				return true
			}
		}
	}
	return false
}

// effect builds one effect, resolving its zone from the target.
func (c *cmdCtx) effect(kind Kind, class, id, evidence string, certain bool) Effect {
	zone := ZoneHost
	switch class {
	case "path":
		zone = c.roots.ClassifyPath(id)
	}
	// A remote target moves everything except credential handling out of this
	// machine's protection: the user asked for work on that box.
	if !c.target.Local && zone != ZoneSensitive {
		zone = ZoneRemote
	}
	if kind == KindCredDisclose || kind == KindCredModify {
		zone = ZoneSensitive
	}
	return Effect{
		Zone: zone, Kind: kind,
		Res:      Resource{Class: class, ID: id},
		Target:   c.target,
		Evidence: evidence,
		Certain:  certain && (c.cwdOK || class != "path"),
	}
}

// pathEffect classifies one path-ish argument.
func (c *cmdCtx) pathEffect(kind Kind, w bashparser.Word, evidence string) (Effect, bool) {
	if !w.Literal {
		// The text is a fragment, not a path. Report that we cannot tell rather
		// than inventing a location — "$HOME/x" reads as "/x", which is a
		// plausible and entirely wrong absolute path.
		return Effect{
			Zone: ZoneProject, Kind: kind,
			Res:      Resource{Class: "path", ID: ""},
			Target:   c.target,
			Evidence: evidence,
			Certain:  false,
		}, true
	}
	resolved := c.roots.Resolve(c.cwd, w.Text)
	if resolved == "" {
		return Effect{}, false
	}
	e := c.effect(kind, "path", resolved, evidence, true)
	// Naming a credential file is only disclosure when the command reads it out
	// or changes it; sensitive.go decides which, this just marks the zone.
	if e.Zone == ZoneSensitive {
		if kind == KindRead {
			e.Kind = KindCredDisclose
		} else if kind.Mutating() {
			e.Kind = KindCredModify
		}
	}
	return e, true
}

// --- program tables -------------------------------------------------------

// wrappers run another command. The value is how many leading arguments to skip
// before the wrapped command begins; -1 means "skip flags, and assignments for
// env".
var wrappers = map[string]int{
	"sudo": -1, "doas": -1, "pkexec": -1, "runuser": -1, "su": -1,
	"env": -1, "nohup": 0, "setsid": 0, "nice": -1, "ionice": -1,
	"stdbuf": -1, "command": 0, "builtin": 0, "exec": 0, "time": 0,
	"timeout": -1, "watch": -1, "xargs": -1, "proxychains": 0,
}

// privileged wrappers are the ones that raise privilege. Reaching a command
// through one is recorded for display, but is NOT itself the trigger:
// `sudo -u deploy ./scripts/deploy.sh` inside the project is project work, and
// treating every sudo as a host change is how a protection gets switched off.
var privilegedWrappers = map[string]bool{
	"sudo": true, "doas": true, "pkexec": true, "runuser": true, "su": true,
}

// readOnlyPrograms inspect without changing. Host paths passed to these are
// reads, which must never prompt — `cat /etc/os-release` and
// `systemctl status nginx` are constant during ordinary debugging.
var readOnlyPrograms = map[string]bool{
	"cat": true, "bat": true, "less": true, "more": true, "head": true, "tail": true,
	"ls": true, "stat": true, "file": true, "wc": true, "du": true, "df": true,
	"grep": true, "rg": true, "ag": true, "ack": true, "diff": true, "cmp": true,
	"ps": true, "top": true, "htop": true, "which": true, "whereis": true, "type": true,
	"printenv": true, "uname": true, "hostname": true, "id": true, "whoami": true,
	"groups": true, "date": true, "echo": true, "printf": true, "readlink": true,
	"realpath": true, "dirname": true, "basename": true, "sort": true, "uniq": true,
	"cut": true, "tr": true, "jq": true, "yq": true, "xxd": true, "od": true,
	"strings": true, "md5sum": true, "shasum": true, "sha256sum": true, "true": true,
	"false": true, "sleep": true, "pwd": true, "uptime": true, "lsof": true, "netstat": true,
	"dig": true, "nslookup": true, "ping": true, "traceroute": true, "host": true,
}

// serviceManagers control system services. The mutating subcommands are listed
// so that `systemctl status` stays autonomous.
var serviceMutations = map[string]bool{
	"start": true, "stop": true, "restart": true, "reload": true, "try-restart": true,
	"reload-or-restart": true, "enable": true, "disable": true, "mask": true,
	"unmask": true, "edit": true, "set-property": true, "daemon-reload": true,
	"isolate": true, "load": true, "unload": true, "bootstrap": true, "bootout": true,
	"kickstart": true, "setenv": true, "remove": true,
}

// packageManagers and the subcommands that install or remove.
var packageInstalls = map[string]bool{
	"install": true, "add": true, "reinstall": true, "upgrade": true, "update": true,
	"tap": true, "-S": true, "-U": true,
}
var packageRemovals = map[string]bool{
	"remove": true, "uninstall": true, "purge": true, "autoremove": true,
	"erase": true, "delete": true, "untap": true, "-R": true, "-Rs": true,
}

var packageManagers = map[string]bool{
	"apt": true, "apt-get": true, "aptitude": true, "dpkg": true, "dnf": true,
	"yum": true, "rpm": true, "zypper": true, "pacman": true, "apk": true,
	"snap": true, "flatpak": true, "port": true, "brew": true, "nix-env": true,
	"softwareupdate": true, "mas": true, "emerge": true,
}

// writeDestinations maps a program to a function returning the argument
// positions it writes to. Each needs the program's own grammar; there is no
// shortcut, and getting one wrong is a silent miss.
var writePrograms = map[string]bool{
	"tee": true, "cp": true, "mv": true, "rm": true, "ln": true, "install": true,
	"mkdir": true, "rmdir": true, "chmod": true, "chown": true, "chgrp": true,
	"truncate": true, "touch": true, "patch": true, "dd": true, "shred": true,
	"rsync": true,
}
