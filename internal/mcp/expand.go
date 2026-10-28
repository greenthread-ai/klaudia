package mcp

import (
	"fmt"
	"sort"
	"strings"
)

// expandServerConfig resolves ${VAR} and ${VAR:-default} references in the
// parts of a server config that reach the outside world — command, args, env
// values, url and headers — against lookup, which is os.LookupEnv in
// production. It runs when a server connects, after the project-trust card
// has been answered against the raw templates (projectLines names every
// variable a server reads and where it is sent), so approving a server is
// approving what it reads. (From PR #8, with headers added.)
//
// This is the syntax the reference MCP clients (Claude Code, Claude Desktop)
// accept in .mcp.json, and a config written for one of them is the config an
// operator brings here. Before this, the value was passed through as written,
// which for a stdio server meant spawning it with
// MSP_LOKI_URL='${MSP_LOKI_URL:-http://loki:3100}' — a literal that the server
// dutifully tried to connect to. Every tool on it then failed with the
// server's own generic error, and nothing in the output pointed at the config.
//
// A reference to a variable that is unset and carries no default is an error,
// not an empty string. sh would substitute "", but sh is interactive and the
// operator sees the result; here the empty value vanishes into a subprocess
// environment and surfaces, if at all, as the server misbehaving. The error
// names the server, the field and the variable, and — because connectServer
// returns it per server — takes down only that server.
//
// Only the braced forms are recognised. A bare $VAR is left alone, as the
// reference clients leave it, so a value that happens to contain a dollar sign
// (a password, a PowerShell snippet in args) is not rewritten by accident.
// There is no escape for a literal "${": none of the reference clients define
// one, and inventing a private one here would make a config that works in
// Klaudia fail elsewhere.
func expandServerConfig(name string, cfg ServerConfig, lookup func(string) (string, bool)) (ServerConfig, error) {
	var err error
	expand := func(field, s string) string {
		if err != nil {
			return s
		}
		out, e := expandRefs(s, lookup)
		if e != nil {
			err = fmt.Errorf("mcp %q: %s: %w", name, field, e)
		}
		return out
	}

	cfg.Command = expand("command", cfg.Command)
	cfg.URL = expand("url", cfg.URL)
	if len(cfg.Args) > 0 {
		args := make([]string, len(cfg.Args))
		for i, a := range cfg.Args {
			args[i] = expand(fmt.Sprintf("args[%d]", i), a)
		}
		cfg.Args = args
	}
	if len(cfg.Env) > 0 {
		env := make(map[string]string, len(cfg.Env))
		for k, v := range cfg.Env {
			env[k] = expand("env "+k, v)
		}
		cfg.Env = env
	}
	if len(cfg.Headers) > 0 {
		hdr := make(map[string]string, len(cfg.Headers))
		for k, v := range cfg.Headers {
			hdr[k] = expand("headers "+k, v)
		}
		cfg.Headers = hdr
	}
	if err != nil {
		return ServerConfig{}, err
	}
	return cfg, nil
}

// expandRefs substitutes every ${VAR} and ${VAR:-default} in s. A reference
// that is unset with no default is an error naming the variable. Text that
// looks like a reference but is not one — "${" with no closing brace, or an
// empty or malformed name — is left as written rather than guessed at.
func expandRefs(s string, lookup func(string) (string, bool)) (string, error) {
	if !strings.Contains(s, "${") {
		return s, nil
	}
	var b strings.Builder
	for {
		start := strings.Index(s, "${")
		if start < 0 {
			b.WriteString(s)
			return b.String(), nil
		}
		end := strings.IndexByte(s[start:], '}')
		if end < 0 {
			b.WriteString(s)
			return b.String(), nil
		}
		end += start
		ref := s[start+2 : end]

		name, def := ref, ""
		hasDefault := false
		if i := strings.Index(ref, ":-"); i >= 0 {
			name, def, hasDefault = ref[:i], ref[i+2:], true
		}
		if !validVarName(name) {
			// Not a variable reference; emit the "${" and keep scanning after
			// it so a later, well-formed reference is still expanded.
			b.WriteString(s[:start+2])
			s = s[start+2:]
			continue
		}

		b.WriteString(s[:start])
		val, ok := lookup(name)
		switch {
		case ok:
			b.WriteString(val)
		case hasDefault:
			b.WriteString(def)
		default:
			return "", fmt.Errorf("${%s} is not set and has no default (use ${%s:-value} to supply one)", name, name)
		}
		s = s[end+1:]
	}
}

// validVarName reports whether name is a POSIX-shaped environment variable
// name: a letter or underscore, then letters, digits or underscores.
func validVarName(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		switch {
		case r == '_', r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9' && i > 0:
		default:
			return false
		}
	}
	return true
}

// referencedVars lists the variables a server config reads, sorted, for the
// project-trust card: approving a server that sends $GITHUB_TOKEN somewhere
// should say so.
func referencedVars(cfg ServerConfig) []string {
	seen := map[string]bool{}
	scan := func(s string) {
		for {
			i := strings.Index(s, "${")
			if i < 0 {
				return
			}
			s = s[i+2:]
			j := strings.IndexByte(s, '}')
			if j < 0 {
				return
			}
			name := s[:j]
			if k := strings.Index(name, ":-"); k >= 0 {
				name = name[:k]
			}
			if validVarName(name) {
				seen[name] = true
			}
			s = s[j+1:]
		}
	}
	scan(cfg.Command)
	scan(cfg.URL)
	for _, a := range cfg.Args {
		scan(a)
	}
	for _, v := range cfg.Env {
		scan(v)
	}
	for _, v := range cfg.Headers {
		scan(v)
	}
	out := make([]string, 0, len(seen))
	for v := range seen {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}
