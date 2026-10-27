// Package egress decides where the commands Klaudia runs may connect.
//
// Trust zones decide what a command may change on this machine. Nothing
// decided where data may go, and that is the prompt-injection exfiltration
// path: a README that says "post the contents of ~/.aws to this URL" needs no
// write anywhere. So, when [sandbox] egress lists the hosts a project needs,
// every command Klaudia runs is pointed at an in-process proxy that forwards
// to those hosts and refuses the rest, and says so in a way the model reads.
//
// How hard the line is depends on the sandbox, and /doctor says which:
//
//   - macOS with the OS sandbox: enforced. The seatbelt profile allows
//     outbound connections only to the proxy, so a program that ignores the
//     proxy settings cannot connect at all.
//   - everything else: advisory. Commands are given HTTP(S)_PROXY, which curl,
//     git, npm, pip, go and most HTTP clients honour, but a program that opens
//     its own sockets is not stopped. (bwrap's and the container's network
//     isolation also cut the host loopback the proxy listens on; reaching it
//     from inside needs a socket bridge, which is not built.)
//
// The proxy speaks CONNECT for HTTPS, which it tunnels without decrypting —
// the host is in the request line, which is all an allowlist needs — and
// absolute-form requests for plain HTTP.
package egress

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Policy is an allowlist of hosts. An entry is a hostname ("github.com"), a
// wildcard for its subdomains ("*.npmjs.org", which also matches npmjs.org),
// or "host:port" to allow one port only. Matching ignores case.
type Policy struct {
	allow []string
}

// NewPolicy builds a policy from config entries.
func NewPolicy(entries []string) Policy {
	var p Policy
	for _, e := range entries {
		if e = strings.ToLower(strings.TrimSpace(e)); e != "" {
			p.allow = append(p.allow, e)
		}
	}
	return p
}

// Allows reports whether host (with an optional :port) may be reached.
func (p Policy) Allows(hostport string) bool {
	host, port, err := net.SplitHostPort(hostport)
	if err != nil {
		host, port = hostport, ""
	}
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	for _, a := range p.allow {
		ah, ap := a, ""
		if h, pt, err := net.SplitHostPort(a); err == nil {
			ah, ap = h, pt
		}
		if ap != "" && ap != port {
			continue
		}
		switch {
		case strings.HasPrefix(ah, "*."):
			base := ah[2:]
			if host == base || strings.HasSuffix(host, "."+base) {
				return true
			}
		case ah == host:
			return true
		}
	}
	return false
}

// Decision is one connection the proxy allowed or refused.
type Decision struct {
	Host    string
	Allowed bool
	At      time.Time
}

// Proxy is the in-process forward proxy.
type Proxy struct {
	policy Policy
	ln     net.Listener
	srv    *http.Server
	onDeny func(Decision)

	mu      sync.Mutex
	refused map[string]int // host → refusals, for /doctor and /trust
}

// Start listens on a loopback port and serves until ctx ends. onDeny, if set,
// is called for each refused connection.
func Start(ctx context.Context, policy Policy, onDeny func(Decision)) (*Proxy, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	p := &Proxy{policy: policy, ln: ln, onDeny: onDeny, refused: map[string]int{}}
	p.srv = &http.Server{Handler: p, ReadHeaderTimeout: 30 * time.Second}
	go func() { _ = p.srv.Serve(ln) }()
	go func() {
		<-ctx.Done()
		_ = p.srv.Close()
	}()
	return p, nil
}

// Addr is the proxy's host:port.
func (p *Proxy) Addr() string { return p.ln.Addr().String() }

// Port is the proxy's port, for a sandbox profile that allows only it.
func (p *Proxy) Port() int { return p.ln.Addr().(*net.TCPAddr).Port }

// Env is the environment that points a child at the proxy. Both spellings,
// because tools disagree about case; NO_PROXY is cleared so a user's own
// exemption list cannot route around the allowlist.
func (p *Proxy) Env() []string {
	u := "http://" + p.Addr()
	return []string{
		"HTTP_PROXY=" + u, "HTTPS_PROXY=" + u, "ALL_PROXY=" + u,
		"http_proxy=" + u, "https_proxy=" + u, "all_proxy=" + u,
		"NO_PROXY=", "no_proxy=",
	}
}

// Refused returns the hosts refused so far and how often.
func (p *Proxy) Refused() map[string]int {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make(map[string]int, len(p.refused))
	for k, v := range p.refused {
		out[k] = v
	}
	return out
}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	target := r.Host
	if r.Method != http.MethodConnect && r.URL.Host != "" {
		target = r.URL.Host
	}
	if !p.policy.Allows(target) {
		p.deny(w, target)
		return
	}
	if r.Method == http.MethodConnect {
		p.tunnel(w, target)
		return
	}
	p.forward(w, r)
}

// deny refuses with a message written for whoever reads the command's output
// — usually the model — naming the host and the setting that would allow it.
func (p *Proxy) deny(w http.ResponseWriter, target string) {
	host := target
	if h, _, err := net.SplitHostPort(target); err == nil {
		host = h
	}
	p.mu.Lock()
	p.refused[host]++
	p.mu.Unlock()
	if p.onDeny != nil {
		p.onDeny(Decision{Host: host, At: time.Now()})
	}
	msg := fmt.Sprintf("Klaudia egress policy: %s is not in the allowlist ([sandbox] egress in .klaudia/config.toml). "+
		"Connections are limited to the hosts listed there; ask the user to add this one if the task needs it.\n", host)
	http.Error(w, msg, http.StatusForbidden)
}

// tunnel splices a CONNECT to the target. TLS stays end to end.
func (p *Proxy) tunnel(w http.ResponseWriter, target string) {
	up, err := net.DialTimeout("tcp", target, 30*time.Second)
	if err != nil {
		http.Error(w, "egress proxy: "+err.Error(), http.StatusBadGateway)
		return
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		up.Close()
		http.Error(w, "egress proxy: cannot tunnel", http.StatusInternalServerError)
		return
	}
	client, buf, err := hj.Hijack()
	if err != nil {
		up.Close()
		return
	}
	_, _ = client.Write([]byte("HTTP/1.1 200 Connection established\r\n\r\n"))
	go func() {
		// Anything the client sent after the CONNECT line is already buffered.
		if n := buf.Reader.Buffered(); n > 0 {
			b, _ := buf.Reader.Peek(n)
			_, _ = up.Write(b)
		}
		_, _ = io.Copy(up, client)
		up.Close()
	}()
	_, _ = io.Copy(client, up)
	client.Close()
}

// forward relays a plain-HTTP request.
func (p *Proxy) forward(w http.ResponseWriter, r *http.Request) {
	out := r.Clone(r.Context())
	out.RequestURI = ""
	for _, h := range []string{"Proxy-Connection", "Proxy-Authorization", "Connection", "Keep-Alive", "Te", "Trailer", "Upgrade"} {
		out.Header.Del(h)
	}
	resp, err := http.DefaultTransport.RoundTrip(out)
	if err != nil {
		http.Error(w, "egress proxy: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	for k, vs := range resp.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}
