// Package browser provides a lazily-launched headless-Chrome engine (driven over
// the Chrome DevTools Protocol) plus web search, used by the BrowserFetch,
// BrowserSearch, BrowserNavigate, and BrowserSnapshot tools. Nothing spawns until a
// web tool actually runs, and the browser is closed at session end.
package browser

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/chromedp/cdproto/cdp"
	cdpdom "github.com/chromedp/cdproto/dom"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

const (
	defaultNavigateTimeout = 30 * time.Second
	defaultActionTimeout   = 15 * time.Second
)

type Engine struct {
	parentCtx   context.Context
	browserOpts Options

	mu      sync.Mutex
	browser *Browser
}

type Snapshot struct {
	URL      string `json:"url"`
	Title    string `json:"title"`
	HTML     string `json:"html,omitempty"`
	Markdown string `json:"markdown,omitempty"`
}

func NewEngine(parent context.Context, opts Options) *Engine {
	return &Engine{parentCtx: parent, browserOpts: opts}
}

func DefaultEngine(parent context.Context) *Engine {
	return NewEngine(parent, Options{
		Mode:           ModeLaunch,
		Headless:       envBool("KLAUDIA_BROWSER_HEADLESS", true),
		ChromePath:     getenv("KLAUDIA_CHROME_PATH"),
		UserDataDir:    firstNonEmpty(getenv("KLAUDIA_CHROME_USER_DATA_DIR"), DefaultUserDataDir()),
		RemoteURL:      getenv("KLAUDIA_CHROME_REMOTE_URL"),
		SearchEngine:   getenv("KLAUDIA_WEB_SEARCH_ENGINE"),
		HeadedFallback: envBool("KLAUDIA_BROWSER_HEADED_FALLBACK", true),
	})
}

func (e *Engine) EnsureBrowser() (*Browser, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.browser != nil {
		if e.browser.Ctx().Err() == nil {
			return e.browser, nil
		}
		e.browser.Close()
		e.browser = nil
	}
	opts := e.browserOpts
	if opts.RemoteURL != "" {
		opts.Mode = ModeAttach
	}
	b, err := New(e.parentCtx, opts)
	if err != nil {
		return nil, err
	}
	e.browser = b
	return b, nil
}

func (e *Engine) Close() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.closeLocked()
}

func (e *Engine) closeLocked() {
	if e.browser != nil {
		e.browser.Close()
		e.browser = nil
	}
}

func (e *Engine) Relaunch(opts Options) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.closeLocked()
	e.browserOpts = opts
	if opts.RemoteURL != "" {
		opts.Mode = ModeAttach
	}
	b, err := New(e.parentCtx, opts)
	if err != nil {
		return err
	}
	e.browser = b
	return nil
}

// Reset closes the current browser and adopts opts for the next one, which is
// created lazily on first use. It is Relaunch without the launch: the way back
// from a headed escalation, where the window has to close now and the headless
// replacement is not needed until something is fetched.
func (e *Engine) Reset(opts Options) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.closeLocked()
	e.browserOpts = opts
}

func (e *Engine) Options() Options {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.browserOpts
}

func (e *Engine) RunWithTimeout(timeout time.Duration, fn func(context.Context) error) error {
	b, err := e.EnsureBrowser()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(b.Ctx(), timeout)
	defer cancel()
	if err := fn(ctx); err != nil {
		if errors.Is(err, context.DeadlineExceeded) || ctx.Err() == context.DeadlineExceeded {
			return b.Diagnostics(fmt.Errorf("browser operation exceeded %s timeout: %w", timeout, err))
		}
		return b.Diagnostics(err)
	}
	return nil
}

func (e *Engine) Navigate(url string) error {
	if url == "" {
		return fmt.Errorf("url required")
	}
	return e.RunWithTimeout(defaultNavigateTimeout, func(ctx context.Context) error {
		if err := chromedp.Do(ctx, commitNavigation(url)); err != nil {
			return err
		}
		return waitStable(ctx)
	})
}

// commitNavigation starts the navigation and returns as soon as Chrome accepts
// it, which is what chromedp.Navigate does before it additionally waits for the
// load event. We do not want that wait.
//
// The load event means every subresource finished, including third-party ads
// and trackers that are free never to finish. Measured cold against Chrome 152:
// theguardian.com had its full text 650ms after the navigation committed and
// had still not fired load 60 seconds later, so a page that was readable almost
// immediately failed on the 30s timeout; arstechnica.com fired load at 10.8s
// with its text complete at 1.25s. Waiting for load is not a stricter version
// of waiting for content — on these sites it is a wait for something unrelated
// that may never happen.
//
// Page.navigate still reports the failures that matter: a host that does not
// resolve, or a refused connection, comes back as ErrorText, which is what
// chromedp's own navigate action turns into a LoadError.
func commitNavigation(url string) chromedp.Action[chromedp.Void] {
	return chromedp.Func(func(ctx context.Context, t *chromedp.Target) error {
		res, err := cdp.Call(ctx, t, page.Navigate, page.NavigateParams{URL: url})
		if err != nil {
			return err
		}
		if res.ErrorText != "" {
			return &chromedp.LoadError{ErrorText: res.ErrorText}
		}
		return nil
	})
}

const (
	stabilizeTimeout   = 10 * time.Second
	stabilizePollEvery = 250 * time.Millisecond
	// How long a parsed document may show no text at all before we accept that
	// it has none. Some pages really are text-free; most that look that way are
	// a moment away from rendering.
	emptyTextGrace = 3 * time.Second
)

// pageState is one poll. The three facts are fetched in a single round trip,
// because fetching them separately lets them describe different instants.
type pageState struct {
	Ready string `json:"ready"`
	Text  int    `json:"text"`
	Href  string `json:"href"`
}

const pageStateExpr = `({ready: document.readyState,` +
	` text: ((document.body && document.body.innerText) || "").length,` +
	` href: document.location.href})`

// waitStable waits for the page to be worth reading: parsed, non-empty, and
// unchanged across two consecutive polls. It is best-effort and bounded — a
// poll error or the deadline proceeds with whatever has rendered, because a
// partial page beats a failed fetch.
//
// Each rule is here because a real site needs it (measured cold on Chrome 152,
// times from the navigation committing):
//
//   - "interactive" counts, not only "complete". theguardian.com stays
//     interactive indefinitely, so requiring "complete" meant never settling
//     and returning the page at the deadline instead, ten seconds after it was
//     ready.
//
//   - Empty text never settles. stackoverflow.com/questions reports readyState
//     "complete" with no body text at all 700ms in, then returns to "loading"
//     and renders at 2.2s. Two polls of agreeing emptiness is exactly what the
//     old rule accepted, so it captured a blank page from a site that was not
//     slow.
//
//   - A changed URL restarts the count, so two polls that describe different
//     documents are never counted as agreement. bbc.co.uk/news client-side
//     redirects to bbc.com/news, and the two render nearly the same length of
//     text; without this, the pair either side of the swap looks like a settled
//     page and the snapshot that follows races the replacement.
//
// What it cannot do is wait for a redirect that has not happened yet: a page
// that settles and only then navigates is returned as it was, because there is
// no signal to distinguish it from one that is simply finished. That is a
// bounded wrongness — the content is real, just from the pre-redirect URL —
// and the alternative is a pause on every fetch for a case that is rare.
func waitStable(ctx context.Context) error {
	deadline := time.Now().Add(stabilizeTimeout)
	var last pageState
	stable, first := 0, true
	emptySince := time.Now()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		st, err := chromedp.Run(ctx, chromedp.Evaluate[pageState](pageStateExpr))
		if err != nil {
			return nil // best-effort; don't fail navigation on a poll hiccup
		}
		switch {
		case st.Ready == "loading" || !first && st.Href != last.Href:
			// Still being parsed, or a different document than the poll before:
			// nothing seen so far describes the page we are going to return.
			stable, emptySince = 0, time.Now()
		case st.Text == 0:
			stable = 0
			if time.Since(emptySince) >= emptyTextGrace {
				return nil // it really has no text
			}
		case !first && st.Text == last.Text:
			if stable++; stable >= 2 {
				return nil // settled
			}
		default:
			stable, emptySince = 0, time.Now()
		}
		last, first = st, false

		if time.Now().After(deadline) {
			return nil // bounded: proceed with whatever has rendered
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(stabilizePollEvery):
		}
	}
}

func (e *Engine) Snapshot(includeHTML, includeMarkdown bool) (*Snapshot, error) {
	var snap Snapshot
	err := e.RunWithTimeout(defaultActionTimeout, func(ctx context.Context) error {
		var err error
		if snap.URL, err = chromedp.Run(ctx, chromedp.Location()); err != nil {
			return err
		}
		if snap.Title, err = chromedp.Run(ctx, chromedp.Title()); err != nil {
			return err
		}
		if !includeHTML && !includeMarkdown {
			return nil
		}
		snap.HTML, err = chromedp.Run(ctx, documentHTML())
		return err
	})
	if err != nil {
		return nil, err
	}
	if includeMarkdown {
		md, err := HTMLToMarkdown(snap.HTML)
		if err != nil {
			return nil, err
		}
		snap.Markdown = md
	}
	if !includeHTML {
		snap.HTML = ""
	}
	return &snap, nil
}

// documentHTML is the serialised document, root node included. chromedp's own
// OuterHTML takes a selector and would give us the <html> element; the root is
// what carries the doctype, and the markdown converter is fed the whole thing.
func documentHTML() chromedp.Action[string] {
	return func(ctx context.Context, t *chromedp.Target) (string, error) {
		doc, err := cdp.Call(ctx, t, cdpdom.GetDocument, cdpdom.GetDocumentParams{})
		if err != nil {
			return "", err
		}
		if doc.Root == nil {
			return "", fmt.Errorf("page has no document root")
		}
		res, err := cdp.Call(ctx, t, cdpdom.GetOuterHTML, cdpdom.GetOuterHTMLParams{NodeID: doc.Root.NodeID})
		if err != nil {
			return "", err
		}
		return res.OuterHTML, nil
	}
}
