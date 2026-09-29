package browser

import (
	"context"
	"path/filepath"
	"testing"
)

func clearBrowserEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"KLAUDIA_BROWSER_HEADLESS", "KLAUDIA_CHROME_PATH", "KLAUDIA_CHROME_USER_DATA_DIR",
		"KLAUDIA_CHROME_REMOTE_URL", "KLAUDIA_WEB_SEARCH_ENGINE", "KLAUDIA_BROWSER_HEADED_FALLBACK",
	} {
		t.Setenv(k, "")
	}
}

func TestDefaultEngineDefaults(t *testing.T) {
	clearBrowserEnv(t)
	cfg := t.TempDir()
	t.Setenv("KLAUDIA_CONFIG_DIR", cfg)

	got := DefaultEngine(context.Background()).Options()
	want := Options{
		Mode: ModeLaunch, Headless: true, HeadedFallback: true,
		UserDataDir: filepath.Join(cfg, "browser", "chrome-profile"),
	}
	if got != want {
		t.Errorf("Options = %+v\nwant %+v", got, want)
	}
}

func TestDefaultEngineReadsTheEnvironment(t *testing.T) {
	clearBrowserEnv(t)
	t.Setenv("KLAUDIA_BROWSER_HEADLESS", " off ")
	t.Setenv("KLAUDIA_BROWSER_HEADED_FALLBACK", "NO")
	t.Setenv("KLAUDIA_CHROME_PATH", " /opt/chrome ")
	t.Setenv("KLAUDIA_CHROME_USER_DATA_DIR", "/tmp/profile")
	t.Setenv("KLAUDIA_CHROME_REMOTE_URL", "http://127.0.0.1:9222")
	t.Setenv("KLAUDIA_WEB_SEARCH_ENGINE", "google")

	got := DefaultEngine(context.Background()).Options()
	want := Options{
		Mode: ModeLaunch, Headless: false, HeadedFallback: false, ChromePath: "/opt/chrome",
		UserDataDir: "/tmp/profile", RemoteURL: "http://127.0.0.1:9222", SearchEngine: "google",
	}
	if got != want {
		t.Errorf("Options = %+v\nwant %+v", got, want)
	}
}

func TestEnvBool(t *testing.T) {
	cases := []struct {
		val       string
		def, want bool
	}{
		{"", true, true},
		{"", false, false},
		{"1", false, true},
		{"TRUE", false, true},
		{"yes", false, true},
		{"on", false, true},
		{"0", true, false},
		{"false", true, false},
		{"No", true, false},
		{"off", true, false},
		// Anything unrecognised keeps the default rather than guessing.
		{"maybe", true, true},
		{"maybe", false, false},
	}
	for _, tc := range cases {
		t.Setenv("KLAUDIA_TEST_BOOL", tc.val)
		if got := envBool("KLAUDIA_TEST_BOOL", tc.def); got != tc.want {
			t.Errorf("envBool(%q, %v) = %v, want %v", tc.val, tc.def, got, tc.want)
		}
	}
}

// Without KLAUDIA_CONFIG_DIR the profile lives under ~/.klaudia.
func TestDefaultUserDataDirFallsBackToHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("KLAUDIA_CONFIG_DIR", "")
	if got, want := DefaultUserDataDir(), filepath.Join(home, ".klaudia", "browser", "chrome-profile"); got != want {
		t.Errorf("DefaultUserDataDir = %q, want %q", got, want)
	}
	t.Setenv("HOME", "")
	if got := DefaultUserDataDir(); got != "" {
		t.Errorf("DefaultUserDataDir with no home = %q, want empty", got)
	}
}

func TestParseResultsRejectsUnknownEngine(t *testing.T) {
	if _, err := parseResults("bing", "<html></html>"); err == nil {
		t.Error("parseResults accepted an unsupported engine")
	}
	for _, engine := range []string{"bing", ""} {
		if isSearchChallengePage(engine, ddgChallengeHTML) || isEmptySearchProviderPage(engine, ddgResultsHTML) || isNoResultsPage(engine, "no results found") {
			t.Errorf("engine %q matched a provider-specific page", engine)
		}
	}
}
