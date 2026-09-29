package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Origin names where an effective config value came from. The order below is
// the precedence order the merge honours, lowest to highest: a built-in
// default, then ~/.klaudia/config.toml, then ./.klaudia/config.toml, and
// finally an environment variable resolved at runtime (only the API key
// resolves this way today, via apiKeyEnv).
const (
	OriginDefault = "default"
	OriginHome    = "home"
	OriginProject = "project"
	OriginEnv     = "env"
)

// Setting is one effective config value together with where it came from. It is
// what `klaudia config show [--origin]` prints.
type Setting struct {
	// Key is the dotted config path, e.g. "provider" or "sandbox.mode".
	Key string
	// Value is the display form, already redacted where it names a secret: the
	// resolved API key is never placed here — see Origins.
	Value string
	// Origin is one of the Origin* constants.
	Origin string
}

// Origins introspects the same two files config.Load reads (home then project)
// and reports the effective value and origin of each set field, plus the two
// fields that have a meaningful built-in default even when unset (provider and
// sandbox.mode). It is a parallel path to Load, added rather than folded in so
// Load's behaviour is unchanged: Load merges into one Config and forgets which
// layer set what, which is exactly the information --origin needs to recover.
//
// Origin is decided by replaying the merge precedence per field: a field set in
// the project layer wins over the same field in the home layer, which wins over
// the built-in default. The API key is special-cased for two reasons — its
// value is a secret and must never be printed, and it can resolve from an
// environment variable (apiKeyEnv) rather than a file, which is the one place an
// "env" origin arises. The apiKey line therefore reports only whether a key is
// set and, when it is, whether it came inline from a file or from the named env
// var; the key text itself never appears.
func Origins(cwd string) []Setting {
	home := homeLayer()
	proj := read(ProjectPath(cwd))

	var out []Setting
	// scalar records a string-valued field, choosing project over home; when
	// neither layer set it (both empty) and def is non-empty, the default is
	// reported instead. An empty value with no default is omitted entirely.
	scalar := func(key, homeVal, projVal, def string) {
		switch {
		case projVal != "":
			out = append(out, Setting{key, projVal, OriginProject})
		case homeVal != "":
			out = append(out, Setting{key, homeVal, OriginHome})
		case def != "":
			out = append(out, Setting{key, def, OriginDefault})
		}
	}
	// number records an int field (0 means unset — matching the toml
	// omitempty/merge treatment of these fields).
	number := func(key string, homeVal, projVal int) {
		switch {
		case projVal != 0:
			out = append(out, Setting{key, strconv.Itoa(projVal), OriginProject})
		case homeVal != 0:
			out = append(out, Setting{key, strconv.Itoa(homeVal), OriginHome})
		}
	}
	// boolTrue records a bool field that only ever merges when true (matching
	// merge's `if src.X { dst.X = true }` fields); a false is indistinguishable
	// from unset, so only a true is reported.
	boolTrue := func(key string, homeVal, projVal bool) {
		switch {
		case projVal:
			out = append(out, Setting{key, "true", OriginProject})
		case homeVal:
			out = append(out, Setting{key, "true", OriginHome})
		}
	}
	// boolPtr records a *bool field: set (either layer, project wins) or absent.
	boolPtr := func(key string, homeVal, projVal *bool) {
		switch {
		case projVal != nil:
			out = append(out, Setting{key, strconv.FormatBool(*projVal), OriginProject})
		case homeVal != nil:
			out = append(out, Setting{key, strconv.FormatBool(*homeVal), OriginHome})
		}
	}
	// list records a slice field that accumulates across layers (union): the
	// origin is the highest layer that contributed any element.
	list := func(key string, homeVal, projVal []string) {
		merged := append(append([]string(nil), homeVal...), projVal...)
		if len(merged) == 0 {
			return
		}
		origin := OriginHome
		if len(projVal) > 0 {
			origin = OriginProject
		}
		if len(homeVal) == 0 && len(projVal) > 0 {
			origin = OriginProject
		}
		out = append(out, Setting{key, "[" + strings.Join(merged, ", ") + "]", origin})
	}

	scalar("provider", home.Provider, proj.Provider, ProviderAnthropic)
	scalar("model", home.Model, proj.Model, "")
	scalar("theme", home.Theme, proj.Theme, "")
	scalar("baseURL", home.BaseURL, proj.BaseURL, "")

	// API key: never print the value. Report whether one resolves and its
	// source. An inline apiKey in a file is a file origin; an apiKeyEnv that
	// resolves at runtime is an env origin.
	appendAPIKey(&out, home, proj)
	scalar("apiKeyEnv", home.APIKeyEnv, proj.APIKeyEnv, "")
	appendExtraHeaderEnv(&out, home, proj)

	if home.Temperature != nil || proj.Temperature != nil {
		v := home.Temperature
		origin := OriginHome
		if proj.Temperature != nil {
			v, origin = proj.Temperature, OriginProject
		}
		out = append(out, Setting{"temperature", strconv.FormatFloat(*v, 'g', -1, 64), origin})
	}
	number("contextWindow", home.ContextWindow, proj.ContextWindow)
	number("maxTokens", home.MaxTokens, proj.MaxTokens)

	scalar("sandbox.mode", home.Sandbox.Mode, proj.Sandbox.Mode, SandboxLocal)
	list("sandbox.writeRoots", home.Sandbox.WriteRoots, proj.Sandbox.WriteRoots)
	scalar("sandbox.runtime", home.Sandbox.Runtime, proj.Sandbox.Runtime, "")
	scalar("sandbox.image", home.Sandbox.Image, proj.Sandbox.Image, "")
	boolPtr("sandbox.mountCwd", home.Sandbox.MountCWD, proj.Sandbox.MountCWD)
	boolTrue("sandbox.readOnly", home.Sandbox.ReadOnly, proj.Sandbox.ReadOnly)
	scalar("sandbox.network", home.Sandbox.Network, proj.Sandbox.Network, "")

	scalar("browser.engine", home.Browser.Engine, proj.Browser.Engine, "")
	boolPtr("browser.headless", home.Browser.Headless, proj.Browser.Headless)
	scalar("browser.chromePath", home.Browser.ChromePath, proj.Browser.ChromePath, "")
	scalar("browser.remoteUrl", home.Browser.RemoteURL, proj.Browser.RemoteURL, "")
	scalar("browser.userDataDir", home.Browser.UserDataDir, proj.Browser.UserDataDir, "")
	boolPtr("browser.headedFallback", home.Browser.HeadedFallback, proj.Browser.HeadedFallback)
	scalar("browser.searchEngine", home.Browser.SearchEngine, proj.Browser.SearchEngine, "")

	list("lsp.disabled", home.LSP.Disabled, proj.LSP.Disabled)

	scalar("permissions.mode", home.Permissions.Mode, proj.Permissions.Mode, "")
	list("permissions.allow", home.Permissions.Allow, proj.Permissions.Allow)
	list("permissions.deny", home.Permissions.Deny, proj.Permissions.Deny)

	scalar("trust.mode", home.Trust.Mode, proj.Trust.Mode, "")
	scalar("input.enter", home.Input.Enter, proj.Input.Enter, "")

	return out
}

// homeLayer reads the home config layer alone (~/.klaudia/config.toml),
// returning a zero Config when there is no home directory or file.
func homeLayer() Config {
	home, err := os.UserHomeDir()
	if err != nil {
		return Config{}
	}
	return read(filepath.Join(home, ".klaudia", "config.toml"))
}

// appendAPIKey reports the API key's presence and source without ever emitting
// the key itself. Precedence mirrors ResolveAPIKey (inline apiKey wins over
// apiKeyEnv) and the merge (project over home).
func appendAPIKey(out *[]Setting, home, proj Config) {
	// Inline key set in a file → redacted, file origin.
	switch {
	case proj.APIKey != "":
		*out = append(*out, Setting{"apiKey", "(set inline, redacted)", OriginProject})
		return
	case home.APIKey != "":
		*out = append(*out, Setting{"apiKey", "(set inline, redacted)", OriginHome})
		return
	}
	// Otherwise, if apiKeyEnv names a variable, the key resolves from the
	// environment when that variable is non-empty — an env origin.
	envName := proj.APIKeyEnv
	if envName == "" {
		envName = home.APIKeyEnv
	}
	if envName != "" {
		if strings.TrimSpace(os.Getenv(envName)) != "" {
			*out = append(*out, Setting{"apiKey", fmt.Sprintf("(set from $%s, redacted)", envName), OriginEnv})
		} else {
			*out = append(*out, Setting{"apiKey", fmt.Sprintf("(unset: $%s is empty)", envName), OriginEnv})
		}
	}
}

// appendExtraHeaderEnv reports extraHeadersEnv as header→$VAR name pairs (never
// values); it merges per key, project over home, matching merge.
func appendExtraHeaderEnv(out *[]Setting, home, proj Config) {
	merged := map[string]string{}
	for h, v := range home.ExtraHeadersEnv {
		merged[h] = v
	}
	fromProj := map[string]bool{}
	for h, v := range proj.ExtraHeadersEnv {
		merged[h] = v
		fromProj[h] = true
	}
	if len(merged) == 0 {
		return
	}
	headers := make([]string, 0, len(merged))
	for h := range merged {
		headers = append(headers, h)
	}
	sort.Strings(headers)
	for _, h := range headers {
		origin := OriginHome
		if fromProj[h] {
			origin = OriginProject
		}
		*out = append(*out, Setting{"extraHeadersEnv." + h, "$" + merged[h], origin})
	}
}
