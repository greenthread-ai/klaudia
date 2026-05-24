package browser

import "testing"

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
		{Title: "evil", URL: "file:///etc/passwd"},      // dropped: not http(s)
		{Title: "js", URL: "javascript:alert(1)"},        // dropped: not http(s)
		{Title: "dup", URL: "https://good.com/a"},         // dropped: duplicate
		{Title: "blocked", URL: "https://spam.com/x"},     // dropped: blocked domain
		{Title: "sub", URL: "https://docs.good.com/y"},    // kept: subdomain of allowed
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
