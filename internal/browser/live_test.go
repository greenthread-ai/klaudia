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

// chromeEngine starts an engine against a local server, or skips.
func chromeEngine(t *testing.T, html string) (*Engine, string) {
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

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(html))
	}))
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
