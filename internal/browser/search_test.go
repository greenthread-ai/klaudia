package browser

import (
	"errors"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

func TestIsProfileInUseError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"singleton lock", errors.New("Failed to create SingletonLock: File exists"), true},
		{"process singleton", errors.New("Failed to create a ProcessSingleton for your profile directory"), true},
		{"chrome user data in use", errors.New("user data directory is already in use"), true},
		{"other launch error", errors.New("chrome executable not found"), false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isProfileInUseError(tc.err); got != tc.want {
				t.Fatalf("isProfileInUseError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestShouldUseHeadedFallback(t *testing.T) {
	cases := []struct {
		name   string
		engine string
		html   string
		want   bool
	}{
		{"ddg challenge", "ddg", `<div id="anomaly-modal">Unfortunately, bots use DuckDuckGo too.</div>`, true},
		{"google challenge", "google", `<form action="/sorry/index"><div class="g-recaptcha"></div></form>`, true},
		{"ddg empty provider page", "ddg", `<html><title>DuckDuckGo</title><form action="https://duckduckgo.com/html/"></form><div class="result__body"></div></html>`, true},
		{"google empty provider page", "google", `<html><title>Google Search</title><form><input name="q"></form><div id="search"></div></html>`, true},
		{"ddg explicit no results", "ddg", `<html><title>DuckDuckGo</title><div>No results found for xyz</div></html>`, false},
		{"google explicit no results", "google", `<html><title>Google Search</title><p>Your search did not match any documents.</p></html>`, false},
		{"non provider", "ddg", `<html><title>Example</title></html>`, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldUseHeadedFallback(tc.engine, tc.html); got != tc.want {
				t.Fatalf("shouldUseHeadedFallback() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestIsGoogleInternalURL(t *testing.T) {
	for _, internal := range []string{
		"https://www.google.com/search?q=x",
		"https://accounts.google.com/signin",
		"https://policies.google.com/privacy",
		"https://www.gstatic.com/og/x.png",
	} {
		if !isGoogleInternalURL(internal) {
			t.Errorf("%q should be treated as Google-internal", internal)
		}
	}
	for _, ext := range []string{"https://example.com/a", "https://go.dev/doc"} {
		if isGoogleInternalURL(ext) {
			t.Errorf("%q is an external result, not internal", ext)
		}
	}
}

func TestParseGoogleResultsDropsInternalLinks(t *testing.T) {
	// A synthetic SERP: one real result plus Google's own nav/account links.
	html := `
	<a href="https://www.google.com/search?q=more"><h3>More Google</h3></a>
	<a href="https://accounts.google.com/signin"><h3>Sign in</h3></a>
	<div class="g">
	  <a href="https://example.com/go-tutorial"><h3>Go Tutorial</h3></a>
	  <div class="VwiC3b">Learn the Go programming language.</div>
	</div>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatal(err)
	}
	got := parseGoogleResults(doc)
	if len(got) != 1 {
		t.Fatalf("got %d results, want 1 (internal links dropped): %+v", len(got), got)
	}
	if got[0].URL != "https://example.com/go-tutorial" || got[0].Title != "Go Tutorial" {
		t.Errorf("unexpected result: %+v", got[0])
	}
	if !strings.Contains(got[0].Snippet, "Go programming language") {
		t.Errorf("snippet not captured: %q", got[0].Snippet)
	}
}

func TestSearchURLEscaping(t *testing.T) {
	got, err := searchURL("ddg", "hello world & friends")
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://duckduckgo.com/html/?q=hello+world+%26+friends" {
		t.Errorf("ddg url = %q", got)
	}
	if _, err := searchURL("bing", "x"); err == nil {
		t.Error("unsupported engine should error")
	}
}

func TestNormalizeDDGURL(t *testing.T) {
	// DDG wraps the real URL in a uddg redirect param.
	in := "//duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com%2Fa%3Fb%3Dc&rut=x"
	if got := normalizeDDGURL(in); got != "https://example.com/a?b=c" {
		t.Errorf("normalizeDDGURL = %q", got)
	}
	// A plain absolute URL passes through.
	if got := normalizeDDGURL("https://example.org/x"); got != "https://example.org/x" {
		t.Errorf("passthrough = %q", got)
	}
}

func TestIsHTTPURL(t *testing.T) {
	for _, ok := range []string{"http://a.com", "https://a.com/x", " https://a.com "} {
		if !isHTTPURL(ok) {
			t.Errorf("%q should be valid http(s)", ok)
		}
	}
	for _, bad := range []string{"file:///etc/passwd", "javascript:alert(1)", "chrome://settings", "data:text/html,x", "ftp://a.com", "/relative", ""} {
		if isHTTPURL(bad) {
			t.Errorf("%q should be rejected", bad)
		}
	}
}

func TestFilterResultsDropsNonHTTPAndAppliesDomains(t *testing.T) {
	in := []SearchResult{
		{Title: "ok", URL: "https://good.com/a"},
		{Title: "evil", URL: "file:///etc/passwd"},     // dropped: not http(s)
		{Title: "js", URL: "javascript:alert(1)"},      // dropped: not http(s)
		{Title: "dup", URL: "https://good.com/a"},      // dropped: duplicate
		{Title: "blocked", URL: "https://spam.com/x"},  // dropped: blocked domain
		{Title: "sub", URL: "https://docs.good.com/y"}, // kept: subdomain of allowed
	}
	out := filterResults(in, []string{"good.com"}, []string{"spam.com"}, 10)
	if len(out) != 2 {
		t.Fatalf("got %d results, want 2: %+v", len(out), out)
	}
	if out[0].URL != "https://good.com/a" || out[1].URL != "https://docs.good.com/y" {
		t.Errorf("unexpected results: %+v", out)
	}
}

func TestFilterResultsRespectsMax(t *testing.T) {
	in := []SearchResult{
		{URL: "https://a.com/1"}, {URL: "https://b.com/2"}, {URL: "https://c.com/3"},
	}
	if out := filterResults(in, nil, nil, 2); len(out) != 2 {
		t.Errorf("max not respected: got %d", len(out))
	}
}

func TestDomainMatchesSet(t *testing.T) {
	set := domainSet([]string{"www.Example.com", " example.org "})
	if !domainMatchesSet("example.com", set) || !domainMatchesSet("sub.example.com", set) {
		t.Error("expected example.com and its subdomain to match")
	}
	if domainMatchesSet("notexample.com", set) {
		t.Error("notexample.com should not match example.com")
	}
	if domainMatchesSet("anything.com", map[string]bool{}) {
		t.Error("empty set matches nothing")
	}
}
