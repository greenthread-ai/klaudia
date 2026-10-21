package browser

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// A live Chrome test, because everything else in this package tests the parts
// around the browser and none of the parts that talk to it. The chromedp bump
// from v0.16 to v0.21 was a whole-API rewrite (Action[T], one action per Run,
// Location/Title returning values, the remote allocator moving packages); all
// of it compiled, and compiling is not evidence that a navigation still ends
// with the page's HTML in hand.
//
// It serves its own page, so it needs Chrome but not the network.

// chromeEngine starts an engine against a local server returning one page.
func chromeEngine(t *testing.T, html string) (*Engine, string) {
	t.Helper()
	return chromeServing(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(html))
	}))
}

// chromeServing starts an engine against a server the caller controls, or skips.
func chromeServing(t *testing.T, h http.Handler) (*Engine, string) {
	t.Helper()
	if testing.Short() {
		t.Skip("live browser test")
	}
	path := getenv("KLAUDIA_CHROME_PATH")
	if path == "" {
		path = defaultChromePath()
	}
	if path == "" {
		t.Skip("no chrome path for this platform")
	}
	if _, err := os.Stat(path); err != nil {
		t.Skipf("chrome not installed at %s", path)
	}

	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	// A temporary profile: the shared one may be locked by a real session.
	e := NewEngine(context.Background(), Options{
		Mode:        ModeLaunch,
		Headless:    true,
		ChromePath:  path,
		UserDataDir: t.TempDir(),
	})
	t.Cleanup(e.Close)
	return e, srv.URL
}

const livePage = `<!doctype html>
<html><head><title>Live page</title></head>
<body><h1>Heading</h1><p>Body text that markdown should keep.</p></body></html>`

func TestLiveNavigateAndSnapshot(t *testing.T) {
	e, url := chromeEngine(t, livePage)

	if err := e.Navigate(url); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	snap, err := e.Snapshot(true, true)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	if snap.Title != "Live page" {
		t.Errorf("title = %q, want %q", snap.Title, "Live page")
	}
	if !strings.HasPrefix(snap.URL, url) {
		t.Errorf("url = %q, want it to start with %q", snap.URL, url)
	}
	// The root node, not the <html> element: the doctype is part of what the
	// snapshot is supposed to return.
	if !strings.Contains(strings.ToLower(snap.HTML), "<!doctype html>") {
		t.Errorf("html lost the doctype, so it is not the document root:\n%s", snap.HTML)
	}
	if !strings.Contains(snap.HTML, "<h1>Heading</h1>") {
		t.Errorf("html missing the body:\n%s", snap.HTML)
	}
	if !strings.Contains(snap.Markdown, "Heading") || !strings.Contains(snap.Markdown, "markdown should keep") {
		t.Errorf("markdown missing the page text:\n%s", snap.Markdown)
	}
}

// includeHTML=false must still produce a title and a URL. The two are fetched
// by separate round trips now that one Run carries one action, so a wrong
// short-circuit would return an empty title rather than failing.
func TestLiveSnapshotWithoutHTML(t *testing.T) {
	e, url := chromeEngine(t, livePage)
	if err := e.Navigate(url); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	snap, err := e.Snapshot(false, false)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if snap.Title == "" || snap.URL == "" {
		t.Errorf("title = %q, url = %q; both should be set", snap.Title, snap.URL)
	}
	if snap.HTML != "" || snap.Markdown != "" {
		t.Errorf("asked for neither, got html=%d markdown=%d bytes", len(snap.HTML), len(snap.Markdown))
	}
}

// waitStable is what makes a client-rendered page snapshot-able; it polls with
// two evaluations that each became their own Run in v0.21.
//
// 400ms, deliberately: waitStable returns once the rendered text has held the
// same length for two consecutive 250ms polls, so it tolerates a page that is
// briefly empty but gives up on one that stays empty for longer than that. A
// slower script here would fail against the real behaviour rather than the
// port.
func TestLiveWaitsForScriptRenderedContent(t *testing.T) {
	e, url := chromeEngine(t, `<!doctype html><html><head><title>SPA</title></head><body>
<div id="app"></div>
<script>setTimeout(() => { document.getElementById("app").textContent = ["rendered", "late"].join(" "); }, 400);</script>
</body></html>`)

	if err := e.Navigate(url); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	snap, err := e.Snapshot(true, false)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	// Built from parts in the script, so the only way the string can appear in
	// the serialised DOM is if it was executed — matching the script source
	// would pass without waiting for anything.
	if !strings.Contains(snap.HTML, `<div id="app">rendered late</div>`) {
		t.Errorf("snapshot taken before the page settled:\n%s", snap.HTML)
	}
}

// A per-action timeout must fail that action and leave the browser usable.
// chromedp ties the browser's life to the context of the first Run, so a
// deadline applied in the wrong place kills the session instead of the call.
func TestLiveTimeoutDoesNotKillTheBrowser(t *testing.T) {
	e, url := chromeEngine(t, livePage)
	if err := e.Navigate(url); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	err := e.RunWithTimeout(time.Millisecond, func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	})
	if err == nil {
		t.Fatal("expected the deadline to be reported")
	}
	if !strings.Contains(err.Error(), "timeout") {
		t.Errorf("error should name the timeout: %v", err)
	}
	if _, err := e.Snapshot(false, false); err != nil {
		t.Fatalf("browser unusable after a per-action timeout: %v", err)
	}
}

// htmlPage is a handler for one page plus whatever extra routes a test needs.
func htmlPage(body string, routes map[string]http.HandlerFunc) http.Handler {
	mux := http.NewServeMux()
	for path, h := range routes {
		mux.HandleFunc(path, h)
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(body))
	})
	return mux
}

// The load event means every subresource finished, and an ad or a tracker is
// free never to finish. arstechnica.com fired load 10.8s after its text was
// complete and theguardian.com had not fired it 60s later, so a fetch of a page
// that was readable in under a second failed on the 30s navigate timeout. Here
// the never-finishing subresource is explicit.
func TestLiveNavigateDoesNotWaitForTheLoadEvent(t *testing.T) {
	hang := make(chan struct{})
	t.Cleanup(func() { close(hang) })
	e, url := chromeServing(t, htmlPage(
		`<!doctype html><html><head><title>Slow subresource</title></head>
<body><p>Readable immediately.</p><img src="/hang"></body></html>`,
		map[string]http.HandlerFunc{
			"/hang": func(w http.ResponseWriter, r *http.Request) {
				select {
				case <-hang:
				case <-r.Context().Done():
				}
			},
		}))

	start := time.Now()
	if err := e.Navigate(url); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("navigate took %s waiting for a subresource that never arrives", elapsed.Round(time.Millisecond))
	}
	snap, err := e.Snapshot(true, false)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if !strings.Contains(snap.HTML, "Readable immediately.") {
		t.Errorf("returned early but without the content:\n%s", snap.HTML)
	}
}

// stackoverflow.com/questions reports readyState "complete" with no body text
// 700ms in, then goes back to "loading" and renders at 2.2s. Under the old rule
// — two consecutive polls agreeing — that captured Cloudflare's "Just a
// moment..." page: a plausible-looking 16-byte answer from a site that was not
// slow and was not down.
func TestLiveEmptyBodyIsNotSettled(t *testing.T) {
	e, url := chromeServing(t, htmlPage(`<!doctype html><html><head><title>Late</title></head>
<body></body>
<script>setTimeout(() => { document.body.textContent = ["the", "real", "page"].join(" "); }, 1200);</script>
</html>`, nil))

	if err := e.Navigate(url); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	snap, err := e.Snapshot(true, false)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if !strings.Contains(snap.HTML, "<body>the real page</body>") {
		t.Errorf("settled on the empty document:\n%s", snap.HTML)
	}
}

// ...but a page that genuinely has no text must not cost the whole budget.
func TestLiveTrulyEmptyPageReturnsOnTheGrace(t *testing.T) {
	e, url := chromeServing(t, htmlPage(
		`<!doctype html><html><head><title>Nothing</title></head><body></body></html>`, nil))

	start := time.Now()
	if err := e.Navigate(url); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	elapsed := time.Since(start)
	if elapsed < emptyTextGrace {
		t.Errorf("returned in %s, before the grace — nothing waited for late content", elapsed.Round(time.Millisecond))
	}
	if elapsed > stabilizeTimeout {
		t.Errorf("an empty page cost %s, the whole settle budget", elapsed.Round(time.Millisecond))
	}
}

// An interstitial that redirects to the real page is the shape Cloudflare and
// DataDome use, and the shape bbc.co.uk/news uses to reach bbc.com/news. While
// the first document has nothing to read we are still polling, so the
// navigation is seen and the answer is the page the site meant to serve.
//
// Note what is not claimed: a page that has settled and only then redirects is
// returned as it was. waitStable has no signal that distinguishes it from one
// that is finished, and pausing every fetch to find out is not worth it.
func TestLiveRedirectWhileWaitingYieldsTheSecondDocument(t *testing.T) {
	e, url := chromeServing(t, htmlPage(`<!doctype html><html><head><title>Interstitial</title></head>
<body></body>
<script>setTimeout(() => { location.href = "/second"; }, 700);</script></html>`,
		map[string]http.HandlerFunc{
			"/second": func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				_, _ = w.Write([]byte(`<!doctype html><html><head><title>Second</title></head>
<body><p>second document, the one that should be returned</p></body></html>`))
			},
		}))

	if err := e.Navigate(url); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	snap, err := e.Snapshot(true, false)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if snap.Title != "Second" {
		t.Errorf("title = %q, want the document the page redirected to", snap.Title)
	}
	if !strings.Contains(snap.HTML, "second document") {
		t.Errorf("returned the interstitial:\n%s", snap.HTML)
	}
	if !strings.HasSuffix(snap.URL, "/second") {
		t.Errorf("url = %q, want the redirect target", snap.URL)
	}
}

// Dropping chromedp.Navigate drops its load-event wait, not its error
// reporting: Page.navigate still reports a host that does not resolve.
func TestLiveUnreachableHostStillFails(t *testing.T) {
	e, _ := chromeEngine(t, livePage)
	err := e.Navigate("http://no-such-host.invalid/")
	if err == nil {
		t.Fatal("navigating to an unresolvable host succeeded")
	}
	if !strings.Contains(err.Error(), "ERR_NAME_NOT_RESOLVED") {
		t.Errorf("error should name the failure, got %v", err)
	}
}
