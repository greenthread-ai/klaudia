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
	case prog == "pacman":
		return detectPacman
	case flagPackageOps[prog] != nil:
		return detectFlagPackageManager(prog)
	case packageManagers[prog]:
		return detectPackageManager(prog)
	case buildInstallers[prog]:
		return detectBuildInstall(prog)
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
		"pip": detectPip, "pip3": detectPip,
		"python": detectPython, "python3": detectPython,
		"git":        detectGit,
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
		if len(ops) == 0 {
			return nil
		}
		verb, pkgs := ops[0], ops[1:]
		if verb == "update" && indexRefreshers[prog] {
			return nil // refreshes the index; installs nothing
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
		return packageEffects(c, kind, prog, verb, pkgs, dropped)
	}
}

// packageEffects reports one effect per package named, or a single unnamed one
// when the command names none (`apt-get upgrade`, `pacman -Syu`).
func packageEffects(c *cmdCtx, kind Kind, prog, verb string, pkgs []string, dropped bool) []Effect {
	if len(pkgs) == 0 {
		pkgs = []string{""}
	}
	out := make([]Effect, 0, len(pkgs))
	for _, p := range pkgs {
		out = append(out, c.effect(kind, "package", prog+":"+p, strings.TrimSpace(prog+" "+verb+" "+p), !dropped))
	}
	return out
}

// detectFlagPackageManager handles the managers in flagPackageOps: the first
// flag that names an operation decides, and every operand is a package.
func detectFlagPackageManager(prog string) detector {
	ops := flagPackageOps[prog]
	return func(c *cmdCtx, args []bashparser.Word) []Effect {
		lits, dropped := literals(args)
		for _, a := range lits {
			if !strings.HasPrefix(a, "-") || len(a) < 2 {
				continue
			}
			key := a
			if strings.HasPrefix(a, "--") {
				key, _, _ = strings.Cut(a, "=")
			} else {
				key = a[:2] // -ivh → -i
			}
			if kind, ok := ops[key]; ok {
				return packageEffects(c, kind, prog, a, operands(lits), dropped)
			}
		}
		return nil // -q, -l, -L, -s: queries
	}
}

// pacmanLong maps pacman's long options onto the letters of its short grammar.
var pacmanLong = map[string]byte{
	"--sync": 'S', "--remove": 'R', "--upgrade": 'U', "--query": 'Q',
	"--database": 'D', "--files": 'F', "--deptest": 'T',
	"--search": 's', "--info": 'i', "--list": 'l', "--groups": 'g', "--print": 'p',
	"--clean": 'c', "--downloadonly": 'w', "--sysupgrade": 'u', "--refresh": 'y',
}

// detectPacman reads pacman's grammar. The operation is a capital letter (-S
// sync, -R remove, -U install a file, -Q query) and lower-case letters modify
// it, usually in one word: -Syu, -Rns, -Ss. The operands are packages, never a
// verb — which is what the subcommand reading in detectPackageManager got
// wrong: `pacman -S nginx` took "nginx" for the verb and reported nothing.
func detectPacman(c *cmdCtx, args []bashparser.Word) []Effect {
	lits, dropped := literals(args)
	var op byte
	mods := map[byte]bool{}
	note := func(l byte) {
		if l >= 'A' && l <= 'Z' {
			if op == 0 {
				op = l
			}
			return
		}
		mods[l] = true
	}
	var verb string
	for _, a := range lits {
		switch {
		case strings.HasPrefix(a, "--"):
			if l, ok := pacmanLong[a]; ok {
				note(l)
			}
		case strings.HasPrefix(a, "-") && len(a) > 1:
			if verb == "" {
				verb = a
			}
			for i := 1; i < len(a); i++ {
				note(a[i])
			}
		}
	}
	pkgs := operands(lits)
	var kind Kind
	switch op {
	case 'S':
		switch {
		case mods['s'] || mods['i'] || mods['l'] || mods['g'] || mods['p'] || mods['c'] || mods['w']:
			return nil // search, info, list, groups, print, clean cache, download only
		case len(pkgs) == 0 && !mods['u']:
			return nil // -Sy on its own refreshes the index, like `apt-get update`
		}
		kind = KindPackageInstall
	case 'U':
		kind = KindPackageInstall
	case 'R':
		if mods['p'] {
			return nil
		}
		kind = KindPackageRemove
	default:
		return nil // -Q, -F, -T, -V read; -D edits install reasons, not modelled
	}
	if verb == "" {
		verb = "-" + string(op)
	}
	return packageEffects(c, kind, "pacman", verb, pkgs, dropped)
}

// detectPip: `pip install` is project work in a virtualenv and user work with
// --user. Through sudo it writes the system interpreter's site-packages, which
// is this machine. Unprivileged, it is treated as any unmodelled program: we
// cannot tell a venv from a system Python from the command line.
func detectPip(c *cmdCtx, args []bashparser.Word) []Effect {
	if !c.priv {
		return detectUnknown(c, args)
	}
	lits, dropped := literals(args)
	ops := operands(lits)
	if len(ops) == 0 {
		return nil
	}
	var kind Kind
	switch ops[0] {
	case "install":
		kind = KindPackageInstall
	case "uninstall":
		kind = KindPackageRemove
	default:
		return nil // list, show, freeze, download
	}
	return packageEffects(c, kind, "pip", ops[0], ops[1:], dropped)
}

// detectPython sees `python -m pip …` as pip; anything else is unmodelled.
func detectPython(c *cmdCtx, args []bashparser.Word) []Effect {
	for i, w := range args {
		if !w.Literal || !strings.HasPrefix(w.Text, "-") {
			break // the script, or something unreadable: not -m
		}
		if w.Text == "-m" && i+1 < len(args) && args[i+1].Literal {
			if m := args[i+1].Text; m == "pip" || m == "pip3" {
				return detectPip(c, args[i+2:])
			}
			break
		}
	}
	return detectUnknown(c, args)
}

// detectBuildInstall: `sudo make install`, `sudo ninja -C build install`,
// `sudo cmake --install build`. See buildInstallers for why sudo is required.
func detectBuildInstall(prog string) detector {
	return func(c *cmdCtx, args []bashparser.Word) []Effect {
		if !c.priv {
			return detectUnknown(c, args)
		}
		lits, dropped := literals(args)
		target := ""
		if prog == "cmake" {
			for i, a := range lits {
				if a == "--install" || (a == "--target" || a == "-t") && i+1 < len(lits) && lits[i+1] == "install" {
					target = "install"
				}
			}
		} else {
			for _, a := range operands(lits) {
				if a == "install" || a == "uninstall" {
					target = a
				}
			}
		}
		if target == "" {
			return detectUnknown(c, args)
		}
		kind := KindPackageInstall
		if target == "uninstall" {
			kind = KindPackageRemove
		}
		return []Effect{c.effect(kind, "package", prog+" "+target, strings.Join(append([]string{prog}, lits...), " "), !dropped)}
	}
}

// gitGlobalValueFlags are git's own options that take a separate value, which
// has to be skipped to find the subcommand.
var gitGlobalValueFlags = map[string]bool{
	"-C": true, "-c": true, "--git-dir": true, "--work-tree": true,
	"--namespace": true, "--config-env": true, "--exec-path": true,
}

// detectGit: only `git config` aimed outside the repository changes anything
// this package cares about. --global writes ~/.gitconfig, which docs/trust.md
// counts as host (it configures every repository on the machine), and --system
// writes /etc/gitconfig. Repository config is project work. Every other git
// subcommand is treated as an unmodelled program, which is what it was before
// git had a detector.
func detectGit(c *cmdCtx, args []bashparser.Word) []Effect {
	i := 0
	for i < len(args) {
		w := args[i]
		if !w.Literal {
			return detectUnknown(c, args)
		}
		if !strings.HasPrefix(w.Text, "-") {
			break
		}
		if gitGlobalValueFlags[w.Text] {
			i++
		}
		i++
	}
	if i >= len(args) || args[i].Text != "config" {
		return detectUnknown(c, args)
	}
	return gitConfig(c, args[i+1:])
}

// gitConfigValueFlags take a separate value in `git config`.
var gitConfigValueFlags = map[string]bool{
	"-f": true, "--file": true, "--blob": true, "--type": true,
	"--default": true, "--comment": true, "--value": true,
}

func gitConfig(c *cmdCtx, args []bashparser.Word) []Effect {
	var file *bashparser.Word
	write, read := false, false
	var ops []bashparser.Word
	for i := 0; i < len(args); i++ {
		w := args[i]
		if !w.Literal || !strings.HasPrefix(w.Text, "-") {
			ops = append(ops, w)
			continue
		}
		flag, val, hasVal := strings.Cut(w.Text, "=")
		switch flag {
		case "--global":
			file = &bashparser.Word{Text: "~/.gitconfig", Literal: true}
		case "--system":
			file = &bashparser.Word{Text: "/etc/gitconfig", Literal: true}
		case "-f", "--file":
			if hasVal {
				file = &bashparser.Word{Text: val, Literal: true}
			} else if i+1 < len(args) {
				file = &args[i+1]
			}
		case "--get", "--get-all", "--get-regexp", "--get-urlmatch", "--get-color",
			"--get-colorbool", "-l", "--list":
			read = true
		case "--unset", "--unset-all", "--add", "--replace-all", "--rename-section",
			"--remove-section", "-e", "--edit":
			write = true
		}
		if gitConfigValueFlags[flag] && !hasVal {
			i++ // the value is not an operand
		}
	}
	// git 2.46 subcommands: `git config set --global k v`, `git config get k`.
	if len(ops) > 0 && ops[0].Literal {
		switch ops[0].Text {
		case "get", "list":
			read = true
		case "set", "unset", "rename-section", "remove-section", "edit":
			write = true
		}
		if read || write {
			ops = ops[1:]
		}
	}
	// The classic form: one operand reads a key, two set it.
	if !read && !write && len(ops) >= 2 {
		write = true
	}
	if !write || read || file == nil {
		return nil
	}
	if e, ok := c.pathEffect(KindWrite, *file, "git config "+file.Text); ok {
		return []Effect{e}
	}
	return nil
}

// detectNodePM: npm/pnpm/yarn are project work unless installing globally,
// which writes into the toolchain prefix.
func detectNodePM(c *cmdCtx, args []bashparser.Word) []Effect {
	lits, dropped := literals(args)
	global := hasFlag(lits, "-g", "--global", "--location")
	ops := operands(lits)
	// Yarn classic spells it as a subcommand: `yarn global add typescript`.
	if c.prog == "yarn" && len(ops) > 0 && ops[0] == "global" {
		global = true
		ops = ops[1:]
	}
	if !global {
		return nil
	}
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
		case "ln":
			// Only the link is written. The target is merely named, often
			// through $(pwd), and reading it as a write made
			// `ln -s $(pwd)/tool ~/.local/bin/tool` ask about a path it never
			// touches.
			ops := nonFlagWords(args)
			switch {
			case len(ops) >= 2:
				add(ops[len(ops)-1], KindWrite)
			case len(ops) == 1:
				// `ln -s /opt/x/tool` links ./tool in the working directory.
				add(bashparser.Word{Text: "./", Literal: true}, KindWrite)
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
