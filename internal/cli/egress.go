package cli

import (
	"context"
	"fmt"
	"runtime"
	"sort"
	"strings"

	"github.com/greenthread-ai/klaudia/internal/config"
	"github.com/greenthread-ai/klaudia/internal/egress"
	"github.com/greenthread-ai/klaudia/internal/sandbox"
)

// startEgress starts the egress proxy when [sandbox] egress lists hosts, and
// points every command at it. Nil when there is no allowlist, or when the
// sandbox cannot reach a proxy on the host's loopback (a container, or bwrap
// with network = "none"): pointing commands at an unreachable proxy would
// break every download rather than limit them, so that is warned about
// instead.
func startEgress(ctx context.Context, sb config.Sandbox, exec sandbox.Executor, warn func(string)) *egress.Proxy {
	if len(sb.Egress) == 0 {
		return nil
	}
	if reach := egressReach(sb, exec); reach == "unreachable" {
		warn("[sandbox] egress is not applied: this sandbox cannot reach a proxy on the host (container, or network = \"none\")")
		return nil
	}
	p, err := egress.Start(ctx, egress.NewPolicy(sb.Egress), nil)
	if err != nil {
		warn("[sandbox] egress: could not start the proxy: " + err.Error())
		return nil
	}
	sandbox.SetEgress(p.Env(), p.Port())
	return p
}

// egressReach says how hard the egress line is with this sandbox:
// "enforced" (the OS sandbox only lets commands reach the proxy), "advisory"
// (commands are pointed at it, and a program that opens its own sockets is
// not stopped), or "unreachable".
func egressReach(sb config.Sandbox, exec sandbox.Executor) string {
	switch exec.(type) {
	case *sandbox.Container:
		return "unreachable"
	case *sandbox.Bwrap:
		if sb.Network == "none" {
			return "unreachable"
		}
		return "advisory"
	case *sandbox.Seatbelt:
		if sb.Network == "none" {
			return "unreachable" // the profile denies all network, the proxy included
		}
		if runtime.GOOS == "darwin" {
			return "enforced"
		}
	}
	return "advisory"
}

// egressDoctor is /doctor's egress row.
func egressDoctor(sb config.Sandbox, exec sandbox.Executor, p *egress.Proxy) string {
	if len(sb.Egress) == 0 {
		return ""
	}
	reach := egressReach(sb, exec)
	var b strings.Builder
	fmt.Fprintf(&b, "%d allowed (%s) — ", len(sb.Egress), strings.Join(sb.Egress, ", "))
	switch reach {
	case "enforced":
		b.WriteString("enforced by the OS sandbox")
	case "advisory":
		b.WriteString("advisory: commands use the proxy, but one that opens its own sockets is not stopped (mode = \"os\" on macOS enforces it)")
	default:
		b.WriteString("not applied: this sandbox cannot reach the proxy")
	}
	if p != nil {
		if refused := p.Refused(); len(refused) > 0 {
			hosts := make([]string, 0, len(refused))
			for h, n := range refused {
				hosts = append(hosts, fmt.Sprintf("%s ×%d", h, n))
			}
			sort.Strings(hosts)
			b.WriteString("; refused so far: " + strings.Join(hosts, ", "))
		}
	}
	return b.String()
}
