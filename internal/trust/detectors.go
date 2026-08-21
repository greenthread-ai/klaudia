package trust

import (
	"strings"

	"github.com/greenthread-ai/klaudia/internal/native/bashparser"
)

// detectorFor returns the classifier for a program, or nil if it is unknown.
func detectorFor(prog string) detector {
	if fn, ok := detectors[prog]; ok {
		return fn
	}
	switch {
	case packageManagers[prog]:
		return detectPackageManager(prog)
	case readOnlyPrograms[prog]:
		return detectReadOnly
	case writePrograms[prog]:
		return detectWrite(prog)
	}
	return nil
}

var detectors map[string]detector

func init() {
	// Assigned in init rather than as a literal: several detectors are
	// recursive through detectorFor, and Go rejects the initialisation cycle.
	detectors = map[string]detector{
		"systemctl": detectService, "service": detectService, "launchctl": detectService,
		"initctl": detectService, "rc-service": detectService, "supervisorctl": detectService,

		"useradd": detectUserAdmin, "usermod": detectUserAdmin, "userdel": detectUserAdmin,
		"adduser": detectUserAdmin, "deluser": detectUserAdmin, "groupadd": detectUserAdmin,
		"groupmod": detectUserAdmin, "groupdel": detectUserAdmin, "passwd": detectUserAdmin,
		"chpasswd": detectUserAdmin, "dscl": detectUserAdmin, "sysadminctl": detectUserAdmin,
		"visudo": detectUserAdmin,

		"ufw": detectFirewall, "firewall-cmd": detectFirewall, "iptables": detectFirewall,
		"ip6tables": detectFirewall, "nft": detectFirewall, "pfctl": detectFirewall,

		"mount": detectMount, "umount": detectMount, "diskutil": detectMount,
		"sshfs": detectMount, "mkfs": detectMount,

		"sysctl": detectSysctl, "modprobe": detectKernel, "insmod": detectKernel,
		"rmmod": detectKernel, "kextload": detectKernel, "nvram": detectKernel,
		"csrutil": detectKernel, "spctl": detectKernel,

		"shutdown": detectPower, "reboot": detectPower, "halt": detectPower,
		"poweroff": detectPower,

		"networksetup": detectNetAdmin, "nmcli": detectNetAdmin, "netplan": detectNetAdmin,
		"resolvectl": detectNetAdmin, "scutil": detectNetAdmin, "route": detectNetAdmin,

		"defaults": detectDefaults,
		"crontab":  detectCrontab,
		"chsh":     detectMachineEnv,

		"sed":  detectSed,
		"perl": detectPerl,

		"npm": detectNodePM, "pnpm": detectNodePM, "yarn": detectNodePM,
		"security":   detectSecurity,
		"gpg":        detectGPG,
		"ssh-keygen": detectSSHKeygen,
	}
}

// --- host state, no path in argv -----------------------------------------

func detectService(c *cmdCtx, args []bashparser.Word) []Effect {
	lits, dropped := literals(args)
	// A --user unit is the login session's, not the machine's, but it still
	// survives logout and starts on next login, so it stays host-zone.
	var verb, unit string
	for _, a := range operands(lits) {
		if verb == "" {
			verb = a
			continue
		}
		if unit == "" {
			unit = a
		}
	}
	if verb == "" || !serviceMutations[verb] {
		return nil // status / list / show — reading, autonomous
	}
	id := strings.TrimSuffix(strings.TrimSuffix(unit, ".service"), ".plist")
	if id == "" {
		id = verb // daemon-reload and friends name no unit
	}
	e := c.effect(KindServiceControl, "service", id, verb+" "+unit, !dropped)
	return []Effect{e}
}

func detectPackageManager(prog string) detector {
	return func(c *cmdCtx, args []bashparser.Word) []Effect {
		lits, dropped := literals(args)
		ops := operands(lits)
		// brew services start nginx is service control, not a package change.
		if prog == "brew" && len(ops) > 1 && ops[0] == "services" {
			if serviceMutations[ops[1]] {
				unit := ""
				if len(ops) > 2 {
					unit = ops[2]
				}
				return []Effect{c.effect(KindServiceControl, "service", unit, strings.Join(ops, " "), !dropped)}
			}
			return nil
		}
		var verb string
		var pkgs []string
		for _, a := range ops {
			if verb == "" {
				verb = a
				continue
			}
			pkgs = append(pkgs, a)
		}
		// pacman uses flags rather than subcommands.
		if verb == "" {
			for _, a := range lits {
				if packageInstalls[a] || packageRemovals[a] {
					verb = a
				}
			}
		}
		var kind Kind
		switch {
		case packageInstalls[verb]:
			kind = KindPackageInstall
		case packageRemovals[verb]:
			kind = KindPackageRemove
		default:
			return nil // list / search / info — reading
		}
		if len(pkgs) == 0 {
			pkgs = []string{""} // `apt-get upgrade` names nothing
		}
		out := make([]Effect, 0, len(pkgs))
		for _, p := range pkgs {
			out = append(out, c.effect(kind, "package", prog+":"+p, prog+" "+verb+" "+p, !dropped))
		}
		return out
	}
}

// detectNodePM: npm/pnpm/yarn are project work unless installing globally,
// which writes into the toolchain prefix.
func detectNodePM(c *cmdCtx, args []bashparser.Word) []Effect {
	lits, dropped := literals(args)
	if !hasFlag(lits, "-g", "--global", "--location") {
		return nil
	}
	ops := operands(lits)
	verb := ""
	if len(ops) > 0 {
		verb = ops[0]
	}
	if !packageInstalls[verb] && !packageRemovals[verb] && verb != "i" {
		return nil
	}
	kind := KindPackageInstall
	if packageRemovals[verb] {
		kind = KindPackageRemove
	}
	name := ""
	if len(ops) > 1 {
		name = ops[1]
	}
	return []Effect{c.effect(kind, "package", "npm-global:"+name, strings.Join(lits, " "), !dropped)}
}

func detectUserAdmin(c *cmdCtx, args []bashparser.Word) []Effect {
	lits, dropped := literals(args)
	ops := operands(lits)
	who := ""
	if len(ops) > 0 {
		who = ops[len(ops)-1]
	}
	return []Effect{c.effect(KindUserAdmin, "user", who, strings.Join(lits, " "), !dropped)}
}

func detectFirewall(c *cmdCtx, args []bashparser.Word) []Effect {
	lits, dropped := literals(args)
	// Listing rules is reading.
	if len(lits) > 0 {
		switch lits[0] {
		case "-L", "-S", "--list", "list", "status", "--list-all", "-n":
			return nil
		}
	}
	return []Effect{c.effect(KindFirewall, "firewall", "rules", strings.Join(lits, " "), !dropped)}
}

func detectMount(c *cmdCtx, args []bashparser.Word) []Effect {
	lits, dropped := literals(args)
	if len(lits) == 0 {
		return nil // bare `mount` lists
	}
	return []Effect{c.effect(KindMount, "mount", strings.Join(operands(lits), " "), strings.Join(lits, " "), !dropped)}
}

func detectSysctl(c *cmdCtx, args []bashparser.Word) []Effect {
	lits, dropped := literals(args)
	// `sysctl key` reads; `sysctl -w key=v` and `sysctl key=v` write.
	writing := hasFlag(lits, "-w")
	key := ""
	for _, a := range operands(lits) {
		if strings.Contains(a, "=") {
			writing = true
			key = strings.SplitN(a, "=", 2)[0]
		} else if key == "" {
			key = a
		}
	}
	if !writing {
		return nil
	}
	return []Effect{c.effect(KindKernelParam, "sysctl", key, strings.Join(lits, " "), !dropped)}
}

func detectKernel(c *cmdCtx, args []bashparser.Word) []Effect {
	lits, dropped := literals(args)
	return []Effect{c.effect(KindKernelParam, "sysctl", strings.Join(operands(lits), " "), strings.Join(lits, " "), !dropped)}
}

func detectPower(c *cmdCtx, args []bashparser.Word) []Effect {
	lits, _ := literals(args)
	return []Effect{c.effect(KindPower, "host", "this machine", strings.Join(lits, " "), true)}
}

func detectNetAdmin(c *cmdCtx, args []bashparser.Word) []Effect {
	lits, dropped := literals(args)
	if len(lits) > 0 {
		switch lits[0] {
		case "show", "get", "list", "-getinfo", "status", "print":
			return nil
		}
	}
	if len(lits) == 0 {
		return nil
	}
	return []Effect{c.effect(KindNetAdmin, "host", "networking", strings.Join(lits, " "), !dropped)}
}

// detectDefaults: macOS user preferences are ordinary; system domains are not.
func detectDefaults(c *cmdCtx, args []bashparser.Word) []Effect {
	lits, dropped := literals(args)
	ops := operands(lits)
	if len(ops) == 0 || ops[0] != "write" {
		return nil // read / read-type
	}
	domain := ""
	if len(ops) > 1 {
		domain = ops[1]
	}
	systemDomain := strings.HasPrefix(domain, "/") || hasFlag(lits, "-currentHost") ||
		strings.HasPrefix(domain, "com.apple.") && hasFlag(lits, "-globalDomain")
	if !systemDomain {
		return nil
	}
	return []Effect{c.effect(KindMachineEnv, "env", domain, strings.Join(lits, " "), !dropped)}
}

func detectCrontab(c *cmdCtx, args []bashparser.Word) []Effect {
	lits, dropped := literals(args)
	if hasFlag(lits, "-l") {
		return nil
	}
	return []Effect{c.effect(KindMachineEnv, "env", "crontab", strings.Join(lits, " "), !dropped)}
}

func detectMachineEnv(c *cmdCtx, args []bashparser.Word) []Effect {
	lits, dropped := literals(args)
	return []Effect{c.effect(KindMachineEnv, "env", strings.Join(operands(lits), " "), strings.Join(lits, " "), !dropped)}
}

// --- reads and writes over paths ------------------------------------------

func detectReadOnly(c *cmdCtx, args []bashparser.Word) []Effect {
	var out []Effect
	exempt := credentialUseExemptions(c.prog, args)
	for i, w := range args {
		if !looksLikePath(w) || exempt[i] {
			continue
		}
		if e, ok := c.pathEffect(KindRead, w, w.Text); ok {
			// Only reads worth reporting: a credential being printed out.
			if e.Zone == ZoneSensitive {
				out = append(out, e)
			}
		}
	}
	return out
}

// detectWrite handles the programs whose destination is an operand rather than
// a redirection. Each needs its own argument grammar.
func detectWrite(prog string) detector {
	return func(c *cmdCtx, args []bashparser.Word) []Effect {
		lits, _ := literals(args)
		var out []Effect

		add := func(w bashparser.Word, kind Kind) {
			if !looksLikePath(w) && w.Literal {
				return
			}
			if e, ok := c.pathEffect(kind, w, prog+" "+w.Text); ok {
				out = append(out, e)
			}
		}

		switch prog {
		case "tee":
			// Every operand is written to.
			for _, w := range args {
				if w.Literal && strings.HasPrefix(w.Text, "-") {
					continue
				}
				add(w, KindWrite)
			}
		case "cp", "mv", "install", "rsync":
			// The last operand is the destination; earlier ones are sources.
			ops := nonFlagWords(args)
			if len(ops) >= 2 {
				add(ops[len(ops)-1], KindWrite)
				// A credential as a *source* going somewhere else is a copy of
				// a secret, which is worth reporting even though the write
				// itself may be innocuous.
				for _, src := range ops[:len(ops)-1] {
					if e, ok := c.pathEffect(KindRead, src, prog+" "+src.Text); ok && e.Zone == ZoneSensitive {
						out = append(out, e)
					}
				}
			} else if len(ops) == 1 {
				add(ops[0], KindWrite)
			}
		case "rm", "shred":
			recursive := hasFlag(lits, "-r", "-R", "-rf", "-fr", "--recursive")
			for _, w := range nonFlagWords(args) {
				kind := KindDelete
				if recursive && c.isBulkTarget(w) {
					kind = KindDestructiveBulk
				}
				add(w, kind)
			}
		case "dd":
			for _, w := range args {
				if w.Literal && strings.HasPrefix(w.Text, "of=") {
					add(bashparser.Word{Text: strings.TrimPrefix(w.Text, "of="), Literal: true}, KindWrite)
				}
			}
		default:
			for _, w := range nonFlagWords(args) {
				add(w, KindWrite)
			}
		}
		return out
	}
}

// isBulkTarget reports whether a recursive delete is aimed somewhere that would
// be a catastrophe regardless of zone: the filesystem root, a top-level
// directory, home itself, or a project root.
func (c *cmdCtx) isBulkTarget(w bashparser.Word) bool {
	if !w.Literal {
		return true // `rm -rf "$DIR"` with an unknown DIR is exactly the classic accident
	}
	p := c.roots.Resolve(c.cwd, w.Text)
	if p == "" || p == "/" {
		return true
	}
	if p == c.roots.Home {
		return true
	}
	for _, root := range c.roots.Project {
		if p == root {
			return true
		}
	}
	// Depth 1 from the root: /usr, /etc, /Users…
	return strings.Count(strings.TrimSuffix(p, "/"), "/") <= 1
}

func nonFlagWords(args []bashparser.Word) []bashparser.Word {
	out := make([]bashparser.Word, 0, len(args))
	for _, w := range args {
		if w.Literal && strings.HasPrefix(w.Text, "-") {
			continue
		}
		out = append(out, w)
	}
	return out
}

// detectSed: only -i edits in place.
func detectSed(c *cmdCtx, args []bashparser.Word) []Effect {
	lits, _ := literals(args)
	if !hasFlag(lits, "-i", "--in-place") && !hasAnyPrefix(lits, "-i") {
		return detectReadOnly(c, args)
	}
	var out []Effect
	for _, w := range nonFlagWords(args) {
		if !looksLikePath(w) {
			continue
		}
		if e, ok := c.pathEffect(KindWrite, w, "sed -i "+w.Text); ok {
			out = append(out, e)
		}
	}
	return out
}

func detectPerl(c *cmdCtx, args []bashparser.Word) []Effect {
	lits, _ := literals(args)
	if !hasAnyPrefix(lits, "-i") {
		return nil
	}
	return detectWrite("perl")(c, args)
}

func hasAnyPrefix(args []string, prefix string) bool {
	for _, a := range args {
		if strings.HasPrefix(a, prefix) {
			return true
		}
	}
	return false
}

// --- credentials ----------------------------------------------------------

func detectSecurity(c *cmdCtx, args []bashparser.Word) []Effect {
	lits, _ := literals(args)
	ops := operands(lits)
	if len(ops) == 0 {
		return nil
	}
	switch ops[0] {
	case "find-generic-password", "find-internet-password", "dump-keychain":
		if hasFlag(lits, "-w") || ops[0] == "dump-keychain" {
			return []Effect{c.effect(KindCredDisclose, "cred", "keychain", strings.Join(lits, " "), true)}
		}
	case "add-generic-password", "add-internet-password", "delete-generic-password",
		"import", "set-keychain-password", "unlock-keychain":
		return []Effect{c.effect(KindCredModify, "cred", "keychain", strings.Join(lits, " "), true)}
	}
	return nil
}

func detectGPG(c *cmdCtx, args []bashparser.Word) []Effect {
	lits, _ := literals(args)
	switch {
	case hasFlag(lits, "--export-secret-keys", "--export-secret-subkeys"):
		return []Effect{c.effect(KindCredDisclose, "cred", "gpg secret keys", strings.Join(lits, " "), true)}
	case hasFlag(lits, "--delete-secret-keys", "--import"):
		return []Effect{c.effect(KindCredModify, "cred", "gpg keyring", strings.Join(lits, " "), true)}
	}
	return nil
}

func detectSSHKeygen(c *cmdCtx, args []bashparser.Word) []Effect {
	// -f names the key file. Generating a *new* key is fine; overwriting an
	// existing one destroys access, and we cannot tell which from the line
	// alone, so report a credential modification and let the user judge.
	for i, w := range args {
		if w.Literal && w.Text == "-f" && i+1 < len(args) {
			if e, ok := c.pathEffect(KindWrite, args[i+1], "ssh-keygen -f "+args[i+1].Text); ok && e.Zone == ZoneSensitive {
				return []Effect{e}
			}
		}
	}
	return nil
}
