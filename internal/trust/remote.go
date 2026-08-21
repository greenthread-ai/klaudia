package trust

import (
	"strings"

	"github.com/greenthread-ai/klaudia/internal/native/bashparser"
)

// Remote work is governed by the task, not by this machine's protection.
//
// `ssh staging sudo systemctl restart nginx` is the job the user asked for; the
// same line without the `ssh` prefix is a change to the machine they are typing
// on. Everything here exists to tell those two apart, which is why the remote
// check runs before any other look at the arguments.

// remoteTarget reports whether a command sends work somewhere else, returning
// the target and the arguments that remain after the target has been consumed.
func remoteTarget(prog string, args []bashparser.Word) (Target, []bashparser.Word, bool) {
	switch prog {
	case "ssh", "mosh", "autossh":
		return sshTarget(prog, args)
	case "scp", "sftp":
		return copyTarget(prog, args)
	case "rsync":
		return rsyncTarget(args)
	case "docker", "podman", "nerdctl":
		return containerTarget(prog, args)
	case "kubectl", "oc":
		return kubectlTarget(prog, args)
	case "vagrant":
		return Target{Host: "vagrant", Via: "vagrant", Label: "vagrant"}, args, true
	case "ansible", "ansible-playbook":
		return ansibleTarget(args)
	}
	return Target{}, nil, false
}

// sshFlagsWithValue are ssh's options that consume the following word. Getting
// this wrong means mistaking an option value for the destination host, which
// would silently move a local command into the remote zone.
var sshFlagsWithValue = map[string]bool{
	"-b": true, "-c": true, "-D": true, "-E": true, "-e": true, "-F": true,
	"-I": true, "-i": true, "-J": true, "-L": true, "-l": true, "-m": true,
	"-O": true, "-o": true, "-p": true, "-Q": true, "-R": true, "-S": true,
	"-W": true, "-w": true,
}

func sshTarget(prog string, args []bashparser.Word) (Target, []bashparser.Word, bool) {
	i := 0
	for i < len(args) {
		w := args[i]
		if !w.Literal {
			// The destination might be this word. We cannot tell, so treat the
			// whole thing as remote-but-unknown rather than fall through to
			// local classification.
			return Target{Via: prog, Host: "", Label: prog + " (destination not statically known)"}, nil, true
		}
		t := w.Text
		if strings.HasPrefix(t, "-") && len(t) > 1 {
			if sshFlagsWithValue[t] && i+1 < len(args) {
				i += 2
				continue
			}
			i++
			continue
		}
		return Target{Via: prog, Host: t, Label: prog + " " + t}, args[i+1:], true
	}
	// `ssh -O exit host` with no destination reached, or bare `ssh`. Not remote
	// work we can attribute; nothing local happens either.
	return Target{Via: prog, Label: prog}, nil, true
}

// copyTarget handles scp/sftp, where the remote side is a host:path operand.
func copyTarget(prog string, args []bashparser.Word) (Target, []bashparser.Word, bool) {
	for _, w := range args {
		if !w.Literal || strings.HasPrefix(w.Text, "-") {
			continue
		}
		if h, ok := splitHostPath(w.Text); ok {
			return Target{Via: prog, Host: h, Label: prog + " " + w.Text}, nil, true
		}
	}
	// Purely local scp is just a copy; let the write detector see it.
	return Target{}, nil, false
}

// rsyncTarget is copyTarget plus the local-destination case: `rsync remote:/x
// /etc/` writes to this machine and must not be laundered by the remote side.
func rsyncTarget(args []bashparser.Word) (Target, []bashparser.Word, bool) {
	ops := nonFlagWords(args)
	if len(ops) < 2 {
		return Target{}, nil, false
	}
	dest := ops[len(ops)-1]
	if dest.Literal {
		if h, ok := splitHostPath(dest.Text); ok {
			return Target{Via: "rsync", Host: h, Label: "rsync " + dest.Text}, nil, true
		}
	}
	// Destination is local: sources may be remote, but the effect lands here.
	return Target{}, nil, false
}

// splitHostPath recognises the host:path form, rejecting Windows-style drive
// letters and anything that is plainly a local path.
func splitHostPath(s string) (string, bool) {
	if strings.HasPrefix(s, "/") || strings.HasPrefix(s, ".") || strings.HasPrefix(s, "~") {
		return "", false
	}
	i := strings.IndexByte(s, ':')
	if i <= 0 {
		return "", false
	}
	host := s[:i]
	if strings.Contains(host, "/") || len(host) == 1 {
		return "", false
	}
	return host, true
}

// containerTarget: most docker work lands inside a container, which is remote
// for our purposes. The exceptions are the subcommands that reach back out to
// this machine — bind mounts, volume paths, `cp` out of a container — and
// `docker -H`, which points the whole thing at another daemon entirely.
func containerTarget(prog string, args []bashparser.Word) (Target, []bashparser.Word, bool) {
	lits, _ := literals(args)
	ops := operands(lits)
	if len(ops) == 0 {
		return Target{}, nil, false
	}
	host := "container"
	for i, w := range args {
		if !w.Literal {
			continue
		}
		if (w.Text == "-H" || w.Text == "--host") && i+1 < len(args) && args[i+1].Literal {
			host = args[i+1].Text
		} else if strings.HasPrefix(w.Text, "--host=") {
			host = strings.TrimPrefix(w.Text, "--host=")
		}
	}
	switch ops[0] {
	case "run", "exec", "start", "stop", "restart", "rm", "build", "compose",
		"pull", "push", "logs", "ps", "images", "kill", "create", "commit":
		label := prog + " " + ops[0]
		if len(ops) > 1 {
			label += " " + ops[1]
		}
		return Target{Via: prog, Host: host, Label: label}, nil, true
	case "cp":
		// `docker cp api:/etc/nginx.conf /etc/nginx.conf` writes here.
		return Target{}, nil, false
	}
	return Target{Via: prog, Host: host, Label: prog + " " + ops[0]}, nil, true
}

func kubectlTarget(prog string, args []bashparser.Word) (Target, []bashparser.Word, bool) {
	lits, _ := literals(args)
	ops := operands(lits)
	ctx := "cluster"
	for i, w := range args {
		if w.Literal && w.Text == "--context" && i+1 < len(args) && args[i+1].Literal {
			ctx = args[i+1].Text
		} else if w.Literal && strings.HasPrefix(w.Text, "--context=") {
			ctx = strings.TrimPrefix(w.Text, "--context=")
		}
	}
	verb := ""
	if len(ops) > 0 {
		verb = ops[0]
	}
	return Target{Via: prog, Host: ctx, Label: prog + " " + verb}, nil, true
}

func ansibleTarget(args []bashparser.Word) (Target, []bashparser.Word, bool) {
	lits, _ := literals(args)
	ops := operands(lits)
	host := "inventory"
	if len(ops) > 0 {
		host = ops[0]
	}
	// `ansible localhost -c local` really does change this machine, and the
	// classifier should not pretend otherwise.
	if host == "localhost" || host == "127.0.0.1" {
		return Target{}, nil, false
	}
	return Target{Via: "ansible", Host: host, Label: "ansible " + host}, nil, true
}

// classifyRemote records what a remote command does, at remote zone.
//
// The payload is deliberately not run through the local path classifier: a
// remote `/etc/nginx/nginx.conf` is that host's file, and resolving it against
// this machine's roots would be meaningless. What we do keep is the *shape* of
// the change, so the transcript and any future remote policy can see that a
// service was restarted on staging rather than "something happened over ssh".
func classifyRemote(prog string, args []bashparser.Word, c *cmdCtx, as *Assessment, depth int) {
	label := c.target.Label
	if label == "" {
		label = c.target.Host
	}

	if len(args) == 0 {
		// An interactive session or a plain connection. Nothing to describe.
		as.Effects = append(as.Effects, Effect{
			Zone: ZoneRemote, Kind: KindExec, Target: c.target,
			Res:      Resource{Class: "host", ID: c.target.Host},
			Evidence: label, Certain: c.target.Host != "",
		})
		return
	}

	// ssh joins its command words with spaces and hands them to a remote shell,
	// so the remote side is one script however it was quoted locally.
	lits, dropped := literals(args)
	script := strings.Join(lits, " ")

	if depth < maxDepth {
		if inner, err := bashparser.Parse(script); err == nil {
			remote := &cmdCtx{roots: c.roots, cwd: "", cwdOK: false, priv: c.priv, target: c.target}
			before := len(as.Effects)
			classifyAnalysis(inner, remote, as, depth+1)
			// Everything the nested pass produced belongs to the remote host.
			for i := before; i < len(as.Effects); i++ {
				as.Effects[i].Zone = remoteZone(as.Effects[i].Zone)
				as.Effects[i].Target = c.target
			}
			if len(as.Effects) > before {
				return
			}
		}
	}

	as.Effects = append(as.Effects, Effect{
		Zone: ZoneRemote, Kind: KindExec, Target: c.target,
		Res:      Resource{Class: "host", ID: c.target.Host},
		Evidence: label + ": " + script, Certain: !dropped && c.target.Host != "",
	})
}

// remoteZone moves a locally-computed zone onto the remote host. Credentials
// are the one thing that does not move: `ssh host cat ~/.ssh/id_rsa` pulls a
// secret back into the transcript here, wherever the file lives.
func remoteZone(z Zone) Zone {
	if z == ZoneSensitive {
		return ZoneSensitive
	}
	return ZoneRemote
}
