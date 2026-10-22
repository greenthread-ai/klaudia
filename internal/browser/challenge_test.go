package browser

import "testing"

func TestChallengePageNamesWhatIsBlocking(t *testing.T) {
	cases := []struct {
		name string
		snap Snapshot
		want string
	}{{
		name: "cloudflare interstitial",
		snap: Snapshot{
			Title:    "Just a moment...",
			HTML:     `<html><head><title>Just a moment...</title><script src="/cdn-cgi/challenge-platform/h/b/orchestrate/chl_page/v1"></script></head><body></body></html>`,
			Markdown: "Just a moment...",
		},
		want: "Cloudflare challenge",
	}, {
		// nytimes.com, measured: an iframe that renders as 11 bytes of
		// markdown. The evidence is entirely in the markup, which is why the
		// markers are matched against the serialised document.
		name: "datadome iframe that renders as almost nothing",
		snap: Snapshot{
			Title:    "nytimes.com",
			HTML:     `<html><body><iframe src="https://geo.captcha-delivery.com/captcha/?initialCid=xyz"></iframe></body></html>`,
			Markdown: "nytimes.com",
		},
		want: "DataDome captcha",
	}, {
		// reddit.com, measured: the whole challenge is in the title.
		name: "reddit human check",
		snap: Snapshot{
			Title:    "Reddit - Prove your humanity",
			HTML:     `<html><head><title>Reddit - Prove your humanity</title></head><body><p>Press and hold.</p></body></html>`,
			Markdown: "# Prove your humanity\n\nPress and hold.",
		},
		want: "Reddit's human check",
	}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ChallengePage(&tc.snap)
			if !ok {
				t.Fatalf("ChallengePage() did not recognise the interstitial")
			}
			if got != tc.want {
				t.Errorf("ChallengePage() = %q, want %q", got, tc.want)
			}
		})
	}
}

// The expensive mistake is the false positive: escalating to a window, and
// telling the model its content is not real, for a page that is perfectly
// fine. A page about Cloudflare is not a Cloudflare challenge.
func TestChallengePageLeavesRealPagesAlone(t *testing.T) {
	article := Snapshot{
		Title:    "How Cloudflare's challenge platform works",
		HTML:     `<html><head><title>How Cloudflare's challenge platform works</title></head><body><article>` + longText + `<p>The script at /cdn-cgi/challenge-platform/ is what runs the check, and "Just a moment..." is the page it shows while it does. Sites also use captcha-delivery.com and google.com/recaptcha for the same job.</p></article></body></html>`,
		Markdown: longText + "\nThe script at /cdn-cgi/challenge-platform/ is what runs the check.",
	}
	if name, ok := ChallengePage(&article); ok {
		t.Errorf("an article about challenges was taken for one (%s)", name)
	}

	thin := Snapshot{Title: "Status", HTML: `<html><body><p>All systems operational.</p></body></html>`, Markdown: "All systems operational."}
	if name, ok := ChallengePage(&thin); ok {
		t.Errorf("a short ordinary page was taken for a challenge (%s)", name)
	}

	if name, ok := ChallengePage(nil); ok {
		t.Errorf("a missing snapshot was taken for a challenge (%s)", name)
	}
}

// longText is over the thinness limit, so the markers in the test above are
// judged on the page carrying them rather than on their presence.
var longText = func() string {
	s := ""
	for len(s) <= challengeTextLimit {
		s += "Paragraph of an article that is actually about the subject it names. "
	}
	return s
}()

func TestHeadedOptionsOpenAWindowOnThePersistentProfile(t *testing.T) {
	headed := headedOptions(Options{
		Mode:        ModeAttach,
		Headless:    true,
		RemoteURL:   "http://127.0.0.1:9222",
		ChromePath:  "/custom/chrome",
		UserDataDir: "",
	})
	if headed.Headless {
		t.Error("the escalation is a window; Headless must be false")
	}
	if headed.Mode != ModeLaunch {
		t.Error("a window has to be launched, not attached to")
	}
	if headed.RemoteURL != "" {
		t.Error("the remote URL would send us back to the browser we are escalating away from")
	}
	if headed.ChromePath != "/custom/chrome" {
		t.Errorf("ChromePath = %q, want the configured binary", headed.ChromePath)
	}
	// Without a profile the challenge has to be solved again on every fetch;
	// with one, solving it once leaves the cookie that makes the next headless
	// fetch work.
	if headed.UserDataDir != DefaultUserDataDir() {
		t.Errorf("UserDataDir = %q, want the persistent default", headed.UserDataDir)
	}
}

func TestCanEscalate(t *testing.T) {
	cases := []struct {
		name string
		opts Options
		want bool
	}{
		{"headless with the fallback on", Options{Headless: true, HeadedFallback: true}, true},
		{"fallback turned off", Options{Headless: true, HeadedFallback: false}, false},
		{"already headed", Options{Headless: false, HeadedFallback: true}, false},
		{"attached to someone else's chrome", Options{Headless: true, HeadedFallback: true, RemoteURL: "http://host:9222"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := canEscalate(tc.opts); got != tc.want {
				t.Errorf("canEscalate() = %v, want %v", got, tc.want)
			}
		})
	}
}
