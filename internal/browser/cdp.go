package browser

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/chromedp/chromedp"
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
	flags = append(flags,
		chromedp.Flag("headless", opts.Headless),
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
	)
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
	if err := chromedp.Run(ctx); err != nil {
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
	allocCtx, allocCancel := chromedp.NewRemoteAllocator(parent, opts.RemoteURL)
	ctx, cancel := chromedp.NewContext(allocCtx, plog.options()...)
	if err := chromedp.Run(ctx); err != nil {
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
