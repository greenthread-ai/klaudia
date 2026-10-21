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
		if err := chromedp.Do(ctx, chromedp.Navigate(url)); err != nil {
			return err
		}
		// chromedp.Navigate returns on the load event, before JS-rendered (SPA)
		// content appears. Wait for the DOM to settle so snapshots capture it.
		return waitStable(ctx)
	})
}

const (
	stabilizeTimeout   = 6 * time.Second
	stabilizePollEvery = 250 * time.Millisecond
)

// waitStable waits until the document is "complete" and its rendered text has
// stopped growing across two consecutive polls (so client-rendered content is
// present), bounded by stabilizeTimeout. It is best-effort: a poll error or the
// timeout simply proceeds rather than failing the navigation.
func waitStable(ctx context.Context) error {
	deadline := time.Now().Add(stabilizeTimeout)
	lastLen, stable := -1, 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		ready, err := chromedp.Run(ctx, chromedp.Evaluate[string](`document.readyState`))
		if err != nil {
			return nil // best-effort; don't fail navigation on a poll hiccup
		}
		textLen, err := chromedp.Run(ctx, chromedp.Evaluate[int](`((document.body&&document.body.innerText)||"").length`))
		if err != nil {
			return nil
		}
		if ready == "complete" {
			if textLen == lastLen {
				if stable++; stable >= 2 {
					return nil // settled
				}
			} else {
				stable = 0
			}
			lastLen = textLen
		}
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
