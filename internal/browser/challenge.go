package browser

import "strings"

// A bot challenge is the one failure a better headless fetch cannot fix. The
// page loads, settles, and returns successfully — it is simply not the page
// that was asked for. Measured on Chrome 152 against the current navigation
// code: reddit.com serves "Reddit - Prove your humanity" and nytimes.com serves
// a DataDome captcha iframe worth 11 bytes of markdown, and both did so
// identically before and after the waitStable rewrite. No amount of waiting
// turns either into the article.
//
// So the escalation is to a window a human can use, which is what the search
// path already does for DuckDuckGo's and Google's own challenges.

// challengeTextLimit is how much rendered text a page may have and still be
// taken for a challenge. Interstitials are thin by construction — Cloudflare's
// is 16 bytes, DataDome's 11 — while a real page that merely *mentions*
// Cloudflare or recaptcha (a blog post about them, this repo's own docs) is
// not. The marker alone is not enough; a page with an article on it is an
// article.
const challengeTextLimit = 2000

// challengeMarkers are the strings each vendor's interstitial carries, named so
// the user can be told which wall they are being asked to climb. Matched
// case-insensitively against the serialised document, which is where an iframe
// src lives even when nothing of it renders.
var challengeMarkers = []struct {
	name   string
	needle string
}{
	{"a Cloudflare challenge", "cdn-cgi/challenge-platform"},
	{"a Cloudflare challenge", "just a moment..."},
	{"a Cloudflare challenge", "checking your browser before accessing"},
	{"a Cloudflare challenge", "enable javascript and cookies to continue"},
	{"a DataDome captcha", "captcha-delivery.com"},
	{"a DataDome captcha", "datadome"},
	{"Reddit's human check", "prove your humanity"},
	{"Google's unusual-traffic check", "our systems have detected unusual traffic"},
	{"a reCAPTCHA", "google.com/recaptcha"},
	{"an hCaptcha", "hcaptcha.com/captcha"},
	{"a human-verification page", "verify you are human"},
	{"a human-verification page", "are you a robot"},
}

// ChallengePage reports whether a snapshot is an anti-bot interstitial rather
// than the page that was asked for, and names the challenge if so.
func ChallengePage(snap *Snapshot) (string, bool) {
	if snap == nil {
		return "", false
	}
	if challengeTextLen(snap) > challengeTextLimit {
		return "", false
	}
	haystack := strings.ToLower(snap.HTML + "\n" + snap.Title + "\n" + snap.Markdown)
	for _, m := range challengeMarkers {
		if strings.Contains(haystack, m.needle) {
			return m.name, true
		}
	}
	return "", false
}

// challengeTextLen measures the readable page, not the markup: a challenge
// carries kilobytes of script and stylesheet around a single sentence, so HTML
// length says nothing. Markdown is the rendered text when we have it; a
// snapshot taken without it falls back to the title alone, which keeps a
// marker-free page from being judged thin on no evidence.
func challengeTextLen(snap *Snapshot) int {
	if snap.Markdown != "" {
		return len(strings.TrimSpace(snap.Markdown))
	}
	return len(strings.TrimSpace(snap.Title))
}

// headedOptions is the escalation: the same browser, visible, launched rather
// than attached, with a profile that survives the session. The profile is the
// point — a challenge solved by hand leaves a cookie behind, so the next
// headless fetch of that site can succeed on its own.
func headedOptions(opts Options) Options {
	headed := opts
	headed.Headless = false
	headed.Mode = ModeLaunch
	headed.RemoteURL = ""
	if headed.UserDataDir == "" {
		headed.UserDataDir = DefaultUserDataDir()
	}
	return headed
}

// canEscalate reports whether opening a window is something this engine may do.
//
// A browser that is already headed has nowhere to escalate to; one attached to
// a remote Chrome (KLAUDIA_CHROME_REMOTE_URL) is not ours to replace, and the
// window would open on whichever machine that Chrome is running on rather than
// in front of the person who could solve the challenge.
func canEscalate(opts Options) bool {
	return opts.HeadedFallback && opts.Headless && opts.RemoteURL == ""
}
