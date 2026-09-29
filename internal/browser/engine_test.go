package browser

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// attachedEngine returns an engine attached to the fake over its http URL, the
// way KLAUDIA_CHROME_REMOTE_URL=http://127.0.0.1:9222 is used for real.
func attachedEngine(t *testing.T, f *fakeCDP) *Engine {
	t.Helper()
	e := NewEngine(context.Background(), Options{Mode: ModeLaunch, RemoteURL: f.srv.URL})
	t.Cleanup(e.Close)
	return e
}

func TestEngineNavigatesAndSnapshotsThePage(t *testing.T) {
	f := newFakeCDP(t)
	e := attachedEngine(t, f)

	if err := e.Navigate("https://example.com/"); err != nil {
		t.Fatalf("Navigate: %v", err)
	}
	snap, err := e.Snapshot(true, true)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if snap.URL != "https://example.com/" || snap.Title != "Example Domain" || snap.HTML != defaultFakeHTML {
		t.Errorf("snapshot = %+v", snap)
	}
	if !strings.Contains(snap.Markdown, "# Hello") || !strings.Contains(snap.Markdown, "**fake**") {
		t.Errorf("markdown = %q", snap.Markdown)
	}

	// Markdown alone is derived from the HTML but doesn't return it.
	snap, err = e.Snapshot(false, true)
	if err != nil {
		t.Fatal(err)
	}
	if snap.HTML != "" || !strings.Contains(snap.Markdown, "# Hello") {
		t.Errorf("markdown-only snapshot = %+v", snap)
	}
	// Neither: just where we are.
	snap, err = e.Snapshot(false, false)
	if err != nil {
		t.Fatal(err)
	}
	if snap.HTML != "" || snap.Markdown != "" || snap.Title != "Example Domain" {
		t.Errorf("bare snapshot = %+v", snap)
	}
}

// The browser is launched lazily and then reused: one connection serves every
// operation until something closes it.
func TestEngineReusesTheBrowserUntilClosed(t *testing.T) {
	f := newFakeCDP(t)
	e := attachedEngine(t, f)

	if f.connections("remote") != 0 {
		t.Fatal("constructing the engine connected to the browser")
	}
	b1, err := e.EnsureBrowser()
	if err != nil {
		t.Fatalf("EnsureBrowser: %v", err)
	}
	b2, err := e.EnsureBrowser()
	if err != nil || b2 != b1 {
		t.Fatalf("second EnsureBrowser = %p, %v; want the same browser %p", b2, err, b1)
	}
	if n := f.connections("remote"); n != 1 {
		t.Errorf("connections = %d, want 1", n)
	}

	// A browser whose context has ended is replaced, not handed out dead.
	b1.Close()
	b3, err := e.EnsureBrowser()
	if err != nil {
		t.Fatalf("EnsureBrowser after the browser died: %v", err)
	}
	if b3 == b1 || b3.Ctx().Err() != nil {
		t.Error("EnsureBrowser returned the dead browser")
	}
	if n := f.connections("remote"); n != 2 {
		t.Errorf("connections = %d, want a reconnect", n)
	}

	e.Close()
	e.Close() // idempotent
	if b3.Ctx().Err() == nil {
		t.Error("Close left the browser running")
	}
}

func TestEngineNavigateErrors(t *testing.T) {
	f := newFakeCDP(t)
	f.setPages(func(_, url string) fakePage {
		return fakePage{errorText: "net::ERR_NAME_NOT_RESOLVED"}
	})
	e := attachedEngine(t, f)

	if err := e.Navigate(""); err == nil || err.Error() != "url required" {
		t.Errorf("empty url err = %v", err)
	}
	if len(f.navigations()) != 0 || f.connections("remote") != 0 {
		t.Error("an empty url reached the browser")
	}
	err := e.Navigate("https://no-such-host.invalid/")
	if err == nil || !strings.Contains(err.Error(), "net::ERR_NAME_NOT_RESOLVED") {
		t.Errorf("load failure err = %v, want the page load error", err)
	}
}

func TestRunWithTimeoutNamesTheTimeout(t *testing.T) {
	f := newFakeCDP(t)
	e := attachedEngine(t, f)

	err := e.RunWithTimeout(50*time.Millisecond, func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	})
	if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "exceeded 50ms timeout") {
		t.Errorf("err = %v, want a named timeout wrapping DeadlineExceeded", err)
	}

	boom := errors.New("boom")
	if err := e.RunWithTimeout(time.Second, func(context.Context) error { return boom }); !errors.Is(err, boom) {
		t.Errorf("err = %v, want the operation's own error", err)
	}
}

func TestAttachFailures(t *testing.T) {
	if _, err := New(context.Background(), Options{Mode: ModeAttach}); err == nil || !strings.Contains(err.Error(), "requires remote URL") {
		t.Errorf("attach without URL err = %v", err)
	}
	if _, err := New(context.Background(), Options{Mode: Mode(42)}); err == nil {
		t.Error("unknown mode accepted")
	}

	// Something is listening, but it is not Chrome.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	e := NewEngine(ctx, Options{RemoteURL: "http://127.0.0.1:1"})
	defer e.Close()
	_, err := e.EnsureBrowser()
	if err == nil || !strings.Contains(err.Error(), "attach chrome at http://127.0.0.1:1") {
		t.Errorf("attach to a dead port err = %v", err)
	}
}

// Relaunch swaps the options and the browser in one step; a remote URL in the
// new options means attach, whatever the mode says.
func TestRelaunchSwitchesBrowsers(t *testing.T) {
	a, b := newFakeCDP(t), newFakeCDP(t)
	e := attachedEngine(t, a)
	if _, err := e.EnsureBrowser(); err != nil {
		t.Fatal(err)
	}
	opts := Options{Mode: ModeLaunch, RemoteURL: b.srv.URL, SearchEngine: "google"}
	if err := e.Relaunch(opts); err != nil {
		t.Fatalf("Relaunch: %v", err)
	}
	if got := e.Options(); got != opts {
		t.Errorf("Options = %+v, want %+v", got, opts)
	}
	if err := e.Navigate("https://example.com/"); err != nil {
		t.Fatal(err)
	}
	if len(a.navigations()) != 0 || len(b.navigations()) != 1 {
		t.Errorf("navigations a=%v b=%v; want the relaunched browser used", a.navigations(), b.navigations())
	}

	if err := e.Relaunch(Options{Mode: ModeAttach}); err == nil {
		t.Error("Relaunch with an impossible config succeeded")
	}
}

// Launch mode runs the configured executable. A profile held by another Chrome
// is retried once with a throwaway profile rather than failing the tool call.
func TestLaunchRetriesWithATemporaryProfileWhenTheProfileIsBusy(t *testing.T) {
	f := newFakeCDP(t)
	busy := filepath.Join(t.TempDir(), "profile")
	b, err := New(context.Background(), Options{
		Mode: ModeLaunch, Headless: true, ChromePath: fakeChrome(t, f, busy), UserDataDir: busy,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer b.Close()
	if n := f.connections("headless"); n != 1 {
		t.Errorf("headless connections = %d, want 1", n)
	}
	if _, err := os.Stat(busy); err != nil {
		t.Errorf("the configured profile dir was not created: %v", err)
	}
}

// When Chrome's output is lost, the busy-profile message goes with it. The
// retry must not depend on it: a silent failure with a configured profile is
// retried with a temporary one too.
func TestLaunchRetriesWhenChromeOutputIsLost(t *testing.T) {
	f := newFakeCDP(t)
	busy := filepath.Join(t.TempDir(), "profile")
	b, err := New(context.Background(), Options{
		Mode: ModeLaunch, Headless: true, ChromePath: fakeSilentChrome(t, f, busy), UserDataDir: busy,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer b.Close()
	if n := f.connections("headless"); n != 1 {
		t.Errorf("headless connections = %d, want 1 (from the retry)", n)
	}
}

// A silent failure says so, rather than "chrome failed to start:" and nothing.
func TestSilentLaunchFailureIsExplained(t *testing.T) {
	f := newFakeCDP(t)
	busy := filepath.Join(t.TempDir(), "profile")
	chrome := fakeSilentChrome(t, f, busy)
	f.srv.Close() // the retry fails too
	_, err := New(context.Background(), Options{Mode: ModeLaunch, ChromePath: chrome, UserDataDir: busy})
	if err == nil || !strings.Contains(err.Error(), "without any output") {
		t.Errorf("err = %v, want it to say Chrome printed nothing", err)
	}
}

func TestLaunchFailureIsReported(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "no-chrome")
	_, err := New(context.Background(), Options{Mode: ModeLaunch, Headless: true, ChromePath: missing})
	if err == nil || !strings.Contains(err.Error(), "launch chrome") {
		t.Fatalf("err = %v, want a launch error", err)
	}

	// A busy profile whose retry also fails reports both failures.
	f := newFakeCDP(t)
	busy := filepath.Join(t.TempDir(), "profile")
	chrome := fakeChrome(t, f, busy)
	f.srv.Close() // announced websocket now refuses connections
	_, err = New(context.Background(), Options{Mode: ModeLaunch, ChromePath: chrome, UserDataDir: busy})
	if err == nil || !strings.Contains(err.Error(), "ProcessSingleton") || !strings.Contains(err.Error(), "retry with temporary chrome profile also failed") {
		t.Errorf("err = %v, want the profile error and the failed retry", err)
	}

	// A profile path that cannot be created is reported as such.
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = New(context.Background(), Options{Mode: ModeLaunch, ChromePath: missing, UserDataDir: filepath.Join(file, "sub")})
	if err == nil || !strings.Contains(err.Error(), "create chrome profile dir") {
		t.Errorf("err = %v, want a profile dir error", err)
	}
}

const ddgResultsHTML = `<html><body>
<div class="result"><a class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fgo.dev%2Fdoc%2F&rut=x">Go docs</a>
  <a class="result__snippet">The Go   programming language.</a></div>
<div class="result"><a class="result__a" href="https://pkg.go.dev/fmt">fmt package</a></div>
<div class="result"><a class="result__a" href="https://spam.example/">Spam</a></div>
<div class="result"><a class="result__a" href="">No link</a></div>
</body></html>`

const ddgChallengeHTML = `<html><body><div id="anomaly-modal">Unfortunately, bots use DuckDuckGo too.</div></body></html>`

func TestSearchParsesAndFiltersResults(t *testing.T) {
	f := newFakeCDP(t)
	f.setPages(func(_, url string) fakePage { return fakePage{title: "DuckDuckGo", html: ddgResultsHTML} })
	e := attachedEngine(t, f)

	got, err := e.Search(context.Background(), SearchOptions{
		Query: "golang docs", BlockedDomains: []string{"spam.example"},
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	want := []SearchResult{
		{Title: "Go docs", URL: "https://go.dev/doc/", Snippet: "The Go programming language."},
		{Title: "fmt package", URL: "https://pkg.go.dev/fmt"},
	}
	if !slices.Equal(got, want) {
		t.Errorf("results = %+v\nwant %+v", got, want)
	}
	// The default engine is DuckDuckGo's HTML endpoint, and the query is escaped.
	if navs := f.navigations(); len(navs) != 1 || navs[0] != "remote https://duckduckgo.com/html/?q=golang+docs" {
		t.Errorf("navigations = %q", navs)
	}
}

func TestSearchEngineSelection(t *testing.T) {
	f := newFakeCDP(t)
	f.setPages(func(string, string) fakePage { return fakePage{html: "<html></html>"} })

	// Engine options, then the env var, then the default.
	e := NewEngine(context.Background(), Options{RemoteURL: f.srv.URL, SearchEngine: "Google"})
	t.Cleanup(e.Close)
	t.Setenv("KLAUDIA_WEB_SEARCH_ENGINE", "ddg")
	if _, err := e.Search(context.Background(), SearchOptions{Query: "q", MaxResults: 100}); err != nil {
		t.Fatal(err)
	}
	e2 := attachedEngine(t, f)
	if _, err := e2.Search(context.Background(), SearchOptions{Query: "q"}); err != nil {
		t.Fatal(err)
	}
	navs := f.navigations()
	if len(navs) != 2 || !strings.Contains(navs[0], "www.google.com/search?q=q") || !strings.Contains(navs[1], "duckduckgo.com") {
		t.Errorf("navigations = %q", navs)
	}

	if _, err := e.Search(context.Background(), SearchOptions{Query: "  "}); err == nil || err.Error() != "query required" {
		t.Errorf("blank query err = %v", err)
	}
	if _, err := e.Search(context.Background(), SearchOptions{Query: "q", Engine: "bing"}); err == nil || !strings.Contains(err.Error(), `unsupported search engine "bing"`) {
		t.Errorf("unknown engine err = %v", err)
	}
	if len(f.navigations()) != 2 {
		t.Error("an invalid search reached the browser")
	}
}

// Attached to someone else's Chrome there is no headed window to open, so a
// challenge page yields no results rather than a relaunch.
func TestSearchChallengeWhileAttachedReturnsNothing(t *testing.T) {
	f := newFakeCDP(t)
	f.setPages(func(string, string) fakePage { return fakePage{title: "DuckDuckGo", html: ddgChallengeHTML} })
	e := NewEngine(context.Background(), Options{RemoteURL: f.srv.URL, HeadedFallback: true})
	t.Cleanup(e.Close)

	got, err := e.Search(context.Background(), SearchOptions{Query: "q"})
	if err != nil || len(got) != 0 {
		t.Errorf("Search = %+v, %v; want no results and no error", got, err)
	}
	if len(f.navigations()) != 1 {
		t.Errorf("navigations = %q, want no retry", f.navigations())
	}
}

// Headless Chrome is the one search engines challenge. When that happens the
// engine reopens the search in a visible Chrome with the persistent profile
// (where a person can clear the challenge once), takes the results from there,
// and goes back to headless afterwards.
func TestSearchFallsBackToHeadedChromeOnAChallenge(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("KLAUDIA_CONFIG_DIR", cfg)
	f := newFakeCDP(t)
	f.setPages(func(id, url string) fakePage {
		if id == "headless" {
			return fakePage{title: "DuckDuckGo", html: ddgChallengeHTML}
		}
		return fakePage{title: "DuckDuckGo", html: ddgResultsHTML}
	})
	e := NewEngine(context.Background(), Options{
		Mode: ModeLaunch, Headless: true, HeadedFallback: true, ChromePath: fakeChrome(t, f, ""),
	})
	t.Cleanup(e.Close)

	got, err := e.Search(context.Background(), SearchOptions{Query: "q", AllowedDomains: []string{"go.dev"}})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 2 || got[0].URL != "https://go.dev/doc/" || got[1].URL != "https://pkg.go.dev/fmt" {
		t.Errorf("results = %+v, want the go.dev results from the headed browser", got)
	}
	if n := f.connections("headed"); n != 1 {
		t.Errorf("headed launches = %d, want 1", n)
	}
	if n := f.connections("headless"); n != 2 {
		t.Errorf("headless launches = %d, want the original and the restore", n)
	}
	// The headed browser used the persistent profile, not a throwaway one.
	if _, err := os.Stat(DefaultUserDataDir()); err != nil {
		t.Errorf("persistent profile not used: %v", err)
	}
	if opts := e.Options(); !opts.Headless || opts.UserDataDir != "" {
		t.Errorf("options after fallback = %+v, want the original headless options", opts)
	}
}
