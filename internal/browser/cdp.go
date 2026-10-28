package browser

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/remote"
)

type Mode int

const (
	ModeLaunch Mode = iota
	ModeAttach
)

type Options struct {
	Mode           Mode
	Headless       bool
	ChromePath     string
	UserDataDir    string
	RemoteURL      string
	SearchEngine   string
	HeadedFallback bool
}

type Browser struct {
	allocCancel context.CancelFunc
	ctx         context.Context
	cancel      context.CancelFunc
	plog        *protocolLog
}

func defaultChromePath() string {
	if runtime.GOOS == "darwin" {
		return "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
	}
	return ""
}

func DefaultUserDataDir() string {
	base := os.Getenv("KLAUDIA_CONFIG_DIR")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		base = filepath.Join(home, ".klaudia")
	}
	return filepath.Join(base, "browser", "chrome-profile")
}

func New(parent context.Context, opts Options) (*Browser, error) {
	switch opts.Mode {
	case ModeLaunch:
		return newLaunched(parent, opts)
	case ModeAttach:
		return newAttached(parent, opts)
	default:
		return nil, fmt.Errorf("unknown browser mode")
	}
}

func newLaunched(parent context.Context, opts Options) (*Browser, error) {
	b, err := newLaunchedWithOptions(parent, opts)
	if err == nil || opts.UserDataDir == "" || !isProfileInUseError(err) {
		return b, err
	}

	fallback := opts
	fallback.UserDataDir = ""
	b, fallbackErr := newLaunchedWithOptions(parent, fallback)
	if fallbackErr != nil {
		return nil, fmt.Errorf("%w; retry with temporary chrome profile also failed: %w", err, fallbackErr)
	}
	return b, nil
}

func newLaunchedWithOptions(parent context.Context, opts Options) (*Browser, error) {
	flags := append([]chromedp.ExecAllocatorOption{}, chromedp.DefaultExecAllocatorOptions[:]...)
	flags = append(flags, chromedp.Flag("disable-blink-features", "AutomationControlled"))
	if !opts.Headless {
		// VisibleWindow, not Flag("headless", false): it also drops
		// hide-scrollbars, mute-audio and enable-automation, and maximises the
		// window. The only reason we ever run headed is the search challenge
		// the user has to solve by hand, and that is a window they have to see,
		// scroll, and not be flagged as automation in.
		flags = append(flags, chromedp.VisibleWindow)
	}
	if opts.UserDataDir != "" {
		if err := os.MkdirAll(opts.UserDataDir, 0o700); err != nil {
			return nil, fmt.Errorf("create chrome profile dir: %w", err)
		}
		flags = append(flags, chromedp.UserDataDir(opts.UserDataDir))
	}
	chromePath := opts.ChromePath
	if chromePath == "" {
		chromePath = defaultChromePath()
	}
	if chromePath != "" {
		flags = append(flags, chromedp.ExecPath(chromePath))
	}

	plog := newProtocolLog()
	allocCtx, allocCancel := chromedp.NewExecAllocator(parent, flags...)
	ctx, cancel := chromedp.NewContext(allocCtx, plog.options()...)
	// Do with no steps still allocates the browser and opens the target, which
	// is the point: a launch that fails has to fail here, not on first use.
	// This context carries no timeout by design — chromedp ties the browser's
	// lifetime to the context of the first run, so a deadline here would be a
	// deadline on the whole session (see RunWithTimeout for per-action limits).
	// The start is bounded from outside instead (startWithin).
	if err := startWithin(ctx, launchTimeout, func() { cancel(); allocCancel() }); err != nil {
		cancel()
		allocCancel()
		return nil, plog.annotate(fmt.Errorf("launch chrome: %w", err))
	}
	return &Browser{allocCancel: allocCancel, ctx: ctx, cancel: cancel, plog: plog}, nil
}

func isProfileInUseError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "singletonlock") ||
		strings.Contains(msg, "processsingleton") ||
		strings.Contains(msg, "profile directory") ||
		strings.Contains(msg, "profile appears to be in use") ||
		strings.Contains(msg, "user data directory is already in use")
}

func newAttached(parent context.Context, opts Options) (*Browser, error) {
	if opts.RemoteURL == "" {
		return nil, fmt.Errorf("attach mode requires remote URL")
	}
	plog := newProtocolLog()
	allocCtx, allocCancel := remote.NewAllocator(parent, opts.RemoteURL)
	ctx, cancel := chromedp.NewContext(allocCtx, plog.options()...)
	if err := startWithin(ctx, launchTimeout, func() { cancel(); allocCancel() }); err != nil {
		cancel()
		allocCancel()
		return nil, plog.annotate(fmt.Errorf("attach chrome at %s: %w", opts.RemoteURL, err))
	}
	return &Browser{allocCancel: allocCancel, ctx: ctx, cancel: cancel, plog: plog}, nil
}

func (b *Browser) Ctx() context.Context { return b.ctx }

// Diagnostics adds anything chromedp reported about this browser — minus the
// unmodelled-event noise, see log.go — to a failed operation's error.
func (b *Browser) Diagnostics(err error) error {
	if b.plog == nil {
		return err
	}
	return b.plog.annotate(err)
}

func (b *Browser) Close() {
	if b.cancel != nil {
		b.cancel()
	}
	if b.allocCancel != nil {
		b.allocCancel()
	}
}

// launchTimeout bounds starting or attaching to Chrome. A Chrome that never
// answered on its DevTools pipe (seen in a test run: chromedp sat in
// connectPipe until the test binary's 10-minute timeout) left the web tool
// call, and the turn, waiting for good. Generous, since a first launch can
// set up a profile; a var so a test can shorten it.
var launchTimeout = 30 * time.Second

// startWithin runs chromedp.Do on ctx, giving up after timeout. The deadline
// cannot go on ctx itself (chromedp ties the browser's whole lifetime to the
// context of its first run), so it is enforced from outside: on expiry abort
// cancels the contexts, which kills a half-started Chrome, and the start is
// reported as timed out.
func startWithin(ctx context.Context, timeout time.Duration, abort func()) error {
	done := make(chan error, 1)
	go func() { done <- chromedp.Do(ctx) }()
	t := time.NewTimer(timeout)
	defer t.Stop()
	select {
	case err := <-done:
		return err
	case <-t.C:
		abort()
		<-done // chromedp returns once its context is cancelled
		return fmt.Errorf("no answer from Chrome within %s", timeout)
	}
}
