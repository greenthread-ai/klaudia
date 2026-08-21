package trust

import (
	"strings"

	"github.com/greenthread-ai/klaudia/internal/native/bashparser"
)

// maxDepth bounds recursion into `sh -c` payloads and remote command strings.
// Exceeding it yields an uncertain effect rather than a hang.
const maxDepth = 3

// maxCommands bounds how many simple commands one line may contribute.
const maxCommands = 64

// ClassifyCommand reads a shell command line and reports what it does.
//
// It is a pure function of the line and the roots: no ledger, no policy, no
// filesystem writes. That is what makes the corpus test meaningful.
func ClassifyCommand(cmd string, roots Roots) Assessment {
	a, err := bashparser.Parse(cmd)
	if err != nil {
		// Failing open here would let anything through by prepending garbage,
		// which is worse than the occasional prompt on an exotic line.
		return Assessment{
			Unparsed: true,
			Reason:   "the command line could not be parsed, so its effects are unknown",
		}
	}
	c := &cmdCtx{roots: roots, cwd: firstNonEmpty(roots.Project...), cwdOK: true, target: LocalTarget()}
	var as Assessment
	classifyAnalysis(a, c, &as, 0)
	as.Targets = distinctTargets(as.Effects)
	return as
}

// ClassifyCommandIn is ClassifyCommand with an explicit starting directory.
func ClassifyCommandIn(cmd, cwd string, roots Roots) Assessment {
	a, err := bashparser.Parse(cmd)
	if err != nil {
		return Assessment{Unparsed: true, Reason: "the command line could not be parsed, so its effects are unknown"}
	}
	c := &cmdCtx{roots: roots, cwd: canonical(cwd), cwdOK: cwd != "", target: LocalTarget()}
	var as Assessment
	classifyAnalysis(a, c, &as, 0)
	as.Targets = distinctTargets(as.Effects)
	return as
}

func classifyAnalysis(a bashparser.Analysis, c *cmdCtx, as *Assessment, depth int) {
	if depth > maxDepth {
		as.Effects = append(as.Effects, Effect{
			Zone: ZoneProject, Kind: KindOpaque, Target: c.target,
			Evidence: "nested shell beyond the depth this reader follows", Certain: false,
		})
		return
	}

	// Redirections write, wherever in the line they appear.
	for _, r := range a.Redirects {
		w := bashparser.Word{Text: r.Target, Literal: r.Literal}
		if e, ok := c.pathEffect(KindWrite, w, "> "+r.Target); ok {
			as.Effects = append(as.Effects, e)
		}
	}

	for _, cmd := range a.Commands {
		if len(as.Effects) > maxCommands {
			return
		}
		classifyOne(cmd, c, as, depth)
	}
}

// shellPayload returns the script hidden inside `sh -c "…"` or `eval`.
//
// bashparser.Analysis.ShellPayloads does this too, but only for a command whose
// *name* is the shell. It is done here instead, after wrapper stripping, so that
// `sudo sh -c 'apt-get install nginx'` is seen — the analysis-level scan sees
// only `sudo` there and the whole script goes unread.
func shellPayload(prog string, args []bashparser.Word) (bashparser.Word, bool) {
	switch prog {
	case "sh", "bash", "zsh", "dash", "ksh":
		for i, a := range args {
			// -c, and combined forms like -lc / -ec that end in c.
			if a.Literal && strings.HasPrefix(a.Text, "-") && strings.HasSuffix(a.Text, "c") && i+1 < len(args) {
				return args[i+1], true
			}
		}
	case "eval":
		// eval concatenates its arguments into one script.
		if len(args) == 0 {
			return bashparser.Word{}, false
		}
		var parts []string
		literal := true
		for _, a := range args {
			parts = append(parts, a.Text)
			literal = literal && a.Literal
		}
		return bashparser.Word{Text: strings.Join(parts, " "), Literal: literal}, true
	}
	return bashparser.Word{}, false
}

// classifyScript re-parses an inline script and folds its effects in.
func classifyScript(w bashparser.Word, c *cmdCtx, as *Assessment, depth int) {
	if !w.Literal {
		as.Effects = append(as.Effects, Effect{
			Zone: ZoneProject, Kind: KindOpaque, Target: c.target,
			Evidence: "inline shell script assembled at runtime", Certain: false,
		})
		return
	}
	inner, err := bashparser.Parse(w.Text)
	if err != nil {
		as.Effects = append(as.Effects, Effect{
			Zone: ZoneProject, Kind: KindOpaque, Target: c.target,
			Evidence: "inline shell script that could not be parsed", Certain: false,
		})
		return
	}
	classifyAnalysis(inner, c, as, depth+1)
}

// classifyOne handles a single simple command after wrapper stripping.
func classifyOne(cmd bashparser.Command, c *cmdCtx, as *Assessment, depth int) {
	name, args, priv, ok := unwrap(cmd)
	if !ok {
		// The program name itself was an expansion ("$SUDO apt-get install").
		// We cannot know what runs the line, but the words that follow often
		// still say what it does, so the first readable one is tried as the
		// program. detectorFor only matches programs we know, which keeps the
		// false-positive cost of the guess low, and everything it produces is
		// marked uncertain.
		as.Effects = append(as.Effects, Effect{
			Zone: ZoneProject, Kind: KindExec, Target: c.target,
			Evidence: "program name is not statically known", Certain: false,
		})
		if len(args) > 0 && args[0].Literal {
			before := len(as.Effects)
			classifyOne(bashparser.Command{
				Name: args[0].Text, NameWord: args[0], ArgWords: args[1:],
			}, c, as, depth)
			for i := before; i < len(as.Effects); i++ {
				as.Effects[i].Certain = false
			}
		}
		return
	}

	sub := *c
	sub.priv = c.priv || priv
	prog := base(name)
	sub.prog = prog

	// An inline script is the whole command; nothing else in argv matters.
	if payload, isShell := shellPayload(prog, args); isShell {
		classifyScript(payload, &sub, as, depth)
		return
	}

	// Remote first: a remote target rewrites the zone of everything the command
	// does, so it must be decided before anything else looks at the arguments.
	if rt, remoteArgs, isRemote := remoteTarget(prog, args); isRemote {
		sub.target = rt
		as.Targets = append(as.Targets, rt)
		classifyRemote(prog, remoteArgs, &sub, as, depth)
		return
	}

	// `cd` moves the simulated working directory for later commands on the line.
	if prog == "cd" || prog == "pushd" {
		lits, _ := literals(args)
		ops := operands(lits)
		if len(ops) == 1 && len(args) == 1 && args[0].Literal {
			c.cwd = c.roots.Resolve(c.cwd, ops[0])
		} else {
			// `cd $DIR`, `cd -`, `popd`: every later relative path is a guess.
			c.cwdOK = false
		}
		return
	}

	if fn := detectorFor(prog); fn != nil {
		as.Effects = append(as.Effects, fn(&sub, args)...)
		return
	}

	// Unknown program. Its path-shaped arguments still matter: something we do
	// not recognise, pointed at /etc, is exactly the case worth asking about.
	// Marked uncertain because we do not know whether it reads or writes.
	exempt := credentialUseExemptions(prog, args)
	for i, w := range args {
		if !looksLikePath(w) || exempt[i] {
			continue
		}
		e, ok := sub.pathEffect(KindWrite, w, prog+" "+w.Text)
		if !ok {
			continue
		}
		if e.Zone.Protected() {
			e.Certain = false
			as.Effects = append(as.Effects, e)
		}
	}
}

// unwrap strips wrapper programs to reach the command that actually runs,
// reporting whether privilege was raised on the way.
func unwrap(cmd bashparser.Command) (name string, args []bashparser.Word, priv bool, ok bool) {
	name, args = cmd.Name, cmd.ArgWords
	if !cmd.NameWord.Literal {
		return "", cmd.ArgWords, false, false
	}
	for i := 0; i < 8; i++ { // bounded: `sudo env nice timeout …` nests, but not forever
		prog := base(name)
		skip, isWrapper := wrappers[prog]
		if !isWrapper {
			return name, args, priv, true
		}
		if privilegedWrappers[prog] {
			priv = true
		}
		rest := args
		if skip < 0 {
			rest = skipWrapperFlags(prog, args)
		} else if skip < len(rest) {
			rest = rest[skip:]
		}
		if len(rest) == 0 {
			// A wrapper with nothing after it (`sudo -l`, bare `env`).
			return name, args, priv, true
		}
		if !rest[0].Literal {
			return "", rest, priv, false
		}
		name, args = rest[0].Text, rest[1:]
	}
	return name, args, priv, true
}

// skipWrapperFlags drops a wrapper's own options so the next word is the
// wrapped program. Each wrapper needs its own grammar for which flags take a
// value; guessing produces confident wrong answers.
func skipWrapperFlags(prog string, args []bashparser.Word) []bashparser.Word {
	takesValue := map[string]map[string]bool{
		"sudo":    {"-u": true, "-g": true, "-p": true, "-C": true, "-h": true, "-U": true, "-T": true, "-R": true},
		"doas":    {"-u": true, "-C": true},
		"su":      {"-c": true, "-s": true, "-l": true},
		"runuser": {"-u": true, "-c": true, "-s": true},
		"env":     {"-u": true, "-C": true, "--chdir": true, "--unset": true},
		"nice":    {"-n": true, "--adjustment": true},
		"ionice":  {"-c": true, "-n": true, "-p": true},
		"stdbuf":  {"-i": true, "-o": true, "-e": true},
		"timeout": {"-s": true, "--signal": true, "-k": true, "--kill-after": true},
		"watch":   {"-n": true, "-d": true},
		"xargs":   {"-I": true, "-n": true, "-P": true, "-d": true, "-s": true, "-a": true},
	}[prog]

	i := 0
	for i < len(args) {
		a := args[i]
		if !a.Literal {
			return args[i:]
		}
		t := a.Text
		switch {
		case t == "--":
			return args[i+1:]
		case strings.HasPrefix(t, "-") && len(t) > 1:
			if takesValue[t] && i+1 < len(args) {
				i += 2
				continue
			}
			i++
		case prog == "env" && strings.Contains(t, "="):
			// env VAR=value cmd
			i++
		case prog == "timeout" && isDuration(t):
			i++
		default:
			return args[i:]
		}
	}
	return nil
}

func isDuration(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if (r < '0' || r > '9') && r != '.' && r != 's' && r != 'm' && r != 'h' && r != 'd' {
			return false
		}
	}
	return s[0] >= '0' && s[0] <= '9'
}

// looksLikePath is a cheap filter for arguments worth resolving. Deliberately
// conservative: a bare word like "install" is not a path, but anything with a
// separator or a leading ~ might be.
func looksLikePath(w bashparser.Word) bool {
	if !w.Literal {
		return false
	}
	t := w.Text
	if t == "" || strings.HasPrefix(t, "-") {
		return false
	}
	return strings.HasPrefix(t, "/") || strings.HasPrefix(t, "~") ||
		strings.HasPrefix(t, "./") || strings.HasPrefix(t, "../") || strings.Contains(t, "/")
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

func distinctTargets(effects []Effect) []Target {
	seen := map[string]bool{}
	var out []Target
	for _, e := range effects {
		key := e.Target.Host + "|" + e.Target.Via
		if e.Target.Local {
			key = "local"
		}
		if !seen[key] {
			seen[key] = true
			out = append(out, e.Target)
		}
	}
	return out
}
