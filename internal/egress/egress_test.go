package egress

import (
	"context"
	"crypto/tls"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestPolicyAllows(t *testing.T) {
	p := NewPolicy([]string{"github.com", "*.npmjs.org", "example.com:8443"})
	for host, want := range map[string]bool{
		"github.com":          true,
		"GitHub.com:443":      true,
		"api.github.com":      false, // exact entries do not cover subdomains
		"registry.npmjs.org":  true,
		"npmjs.org":           true,
		"evilnpmjs.org":       false,
		"example.com:8443":    true,
		"example.com:443":     false,
		"exfil.attacker.test": false,
	} {
		if got := p.Allows(host); got != want {
			t.Errorf("Allows(%q) = %v, want %v", host, got, want)
		}
	}
}

func client(p *Proxy) *http.Client {
	u, _ := url.Parse("http://" + p.Addr())
	return &http.Client{Transport: &http.Transport{
		Proxy:           http.ProxyURL(u),
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // test server
	}}
}

// Allowed hosts go through, plain and tunnelled; the rest get a 403 that
// names the host and the setting, written for whoever reads the output.
func TestProxyAllowsAndRefuses(t *testing.T) {
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, "plain ok") }))
	defer plain.Close()
	secure := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, "tls ok") }))
	defer secure.Close()
	plainHost := strings.TrimPrefix(plain.URL, "http://")
	secureHost := strings.TrimPrefix(secure.URL, "https://")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var denied []string
	p, err := Start(ctx, NewPolicy([]string{plainHost, secureHost}), func(d Decision) { denied = append(denied, d.Host) })
	if err != nil {
		t.Fatal(err)
	}
	c := client(p)
	for u, want := range map[string]string{plain.URL: "plain ok", secure.URL: "tls ok"} {
		resp, err := c.Get(u)
		if err != nil {
			t.Fatalf("GET %s: %v", u, err)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if string(b) != want {
			t.Errorf("GET %s = %q", u, b)
		}
	}

	resp, err := c.Get("http://exfil.attacker.test/upload")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden || !strings.Contains(string(b), "exfil.attacker.test is not in the allowlist") || !strings.Contains(string(b), "[sandbox] egress") {
		t.Errorf("refusal = %d %q", resp.StatusCode, b)
	}
	if _, err := c.Get("https://exfil.attacker.test/"); err == nil {
		t.Error("a refused CONNECT succeeded")
	}
	if p.Refused()["exfil.attacker.test"] != 2 || len(denied) != 2 {
		t.Errorf("refusals recorded: %v, callbacks %v", p.Refused(), denied)
	}
}
