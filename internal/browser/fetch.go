package browser

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"time"
)

// FetchOptions is one page fetch: where to go and what to bring back.
type FetchOptions struct {
	URL             string
	IncludeHTML     bool
	IncludeMarkdown bool
	// Notify, if set, is told what the fetch is doing when it stops being a
	// fetch and starts asking something of the user. Opening a window on
	// someone's desktop without saying why is the part that would be rude.
	Notify func(line string)
}

// FetchResult is a rendered page plus what the fetch had to say about it.
type FetchResult struct {
	*Snapshot
	// Challenge names the anti-bot interstitial the site served instead of the
	// page, and is empty when it served the page. It is carried here rather
	// than re-derived by the caller because the evidence for it is the markup,
	// which the caller usually asked not to receive.
	Challenge string
}

const (
	// challengeWait is how long the window stays open for a human to clear the
	// challenge. Two minutes is the search fallback's existing budget: long
	// enough to read a puzzle and solve it, short enough that a fetch left
	// running by an unattended agent eventually gives up.
	challengeWait = 2 * time.Minute
	// challengePollEvery is how often the open window is re-read. The user is
	// clicking, not typing into a form field we are racing.
	challengePollEvery = 2 * time.Second
)

// escalateOptions is headedOptions, indirected so the tests can exercise the
// whole escalation — the decision, the relaunch, the polling, the restore —
// with a replacement browser that is still headless, and so put no window on
// the screen of whoever runs `go test`. The one flag they fake is the one
// headedOptions is separately tested for.
var escalateOptions = headedOptions

// Fetch renders a page and returns it, escalating to a visible Chrome window if
// the site served an anti-bot interstitial instead of the page.
//
// The order matters and is the whole design:
//
//  1. Fetch headless, as always. Nothing about the common case changes — no
//     window, no prompt, no extra launch.
//  2. Only if what came back is a challenge (ChallengePage, deliberately
//     narrow) does anything else happen.
//  3. Escalate once, to a headed browser on the persistent profile, and say so.
//     The window is the only way a human can clear it.
//  4. Close that window and go back to headless, on every path out including a
//     cancelled context.
//
// Step 4 is not a tidiness nicety: the headed browser holds the shared profile
// directory, so a window left open is a lock left held, and the next headless
// launch falls back to a throwaway profile that has none of the cookies the
// user just earned by solving the challenge.
func (e *Engine) Fetch(ctx context.Context, opts FetchOptions) (*FetchResult, error) {
	if opts.URL == "" {
		return nil, fmt.Errorf("url required")
	}
	snap, err := e.fetchOnce(opts.URL)
	if err != nil {
		return nil, err
	}
	name, challenged := ChallengePage(snap)
	if !challenged {
		return result(snap, "", opts), nil
	}
	browserOpts := e.Options()
	if !canEscalate(browserOpts) {
		return result(snap, name, opts), nil
	}

	notify(opts.Notify, fmt.Sprintf("%s is serving %s, not the page — opening a Chrome window so you can clear it (up to %s); the fetch finishes on its own once you have",
		hostOf(opts.URL), name, challengeWait))

	cleared, err := e.refetchUnder(ctx, escalateOptions(browserOpts), opts.URL)
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return nil, err
	case err != nil:
		// The window is what failed, not the fetch. Returning the challenge
		// page is still the truth about what the site served, and the result
		// says which it is.
		notify(opts.Notify, fmt.Sprintf("couldn't open a Chrome window for the challenge: %v", err))
		return result(snap, name, opts), nil
	}
	stillName, still := ChallengePage(cleared)
	if still {
		notify(opts.Notify, fmt.Sprintf("%s is still showing the challenge; returning what it served", hostOf(opts.URL)))
	} else {
		stillName = ""
	}
	return result(cleared, stillName, opts), nil
}

func (e *Engine) fetchOnce(url string) (*Snapshot, error) {
	if err := e.Navigate(url); err != nil {
		return nil, err
	}
	// Always both: the challenge markers live in the markup (an iframe src that
	// renders as nothing), the thinness test lives in the rendered text, and
	// trimSnapshot drops whatever the caller did not ask for.
	return e.Snapshot(true, true)
}

// refetchUnder swaps the browser for one launched with opts, fetches url again,
// and polls until the challenge is gone or the budget runs out — then puts the
// previous browser back whatever happened.
//
// The restore closes the replacement rather than relaunching the old one: the
// engine creates a browser on first use, so the window shuts the moment it is
// no longer needed instead of at the next fetch.
func (e *Engine) refetchUnder(ctx context.Context, opts Options, url string) (*Snapshot, error) {
	previous := e.Options()
	if err := e.Relaunch(opts); err != nil {
		e.Reset(previous)
		return nil, err
	}
	defer e.Reset(previous)

	deadline := time.Now().Add(challengeWait)
	for {
		snap, err := e.fetchOnce(url)
		if err != nil {
			return nil, err
		}
		if _, challenged := ChallengePage(snap); !challenged {
			return snap, nil
		}
		if time.Now().After(deadline) {
			return snap, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(challengePollEvery):
		}
	}
}

// result trims the snapshot to what the caller asked for and attaches the
// verdict. Fetch gathers both representations because the challenge test needs
// both; that is an internal detail and must not change what a caller receives.
func result(snap *Snapshot, challenge string, opts FetchOptions) *FetchResult {
	if snap == nil {
		return nil
	}
	out := *snap
	if !opts.IncludeHTML {
		out.HTML = ""
	}
	if !opts.IncludeMarkdown {
		out.Markdown = ""
	}
	return &FetchResult{Snapshot: &out, Challenge: challenge}
}

func notify(fn func(string), line string) {
	if fn != nil {
		fn(line)
	}
}

func hostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return raw
	}
	return u.Host
}
