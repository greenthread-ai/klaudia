package browser

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

type SearchOptions struct {
	Engine         string
	Query          string
	AllowedDomains []string
	BlockedDomains []string
	MaxResults     int
}

type SearchResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet,omitempty"`
}

func (e *Engine) Search(ctx context.Context, opts SearchOptions) ([]SearchResult, error) {
	if strings.TrimSpace(opts.Query) == "" {
		return nil, fmt.Errorf("query required")
	}
	engine := strings.ToLower(strings.TrimSpace(opts.Engine))
	if engine == "" {
		engine = strings.ToLower(strings.TrimSpace(e.browserOpts.SearchEngine))
	}
	if engine == "" {
		engine = strings.ToLower(getenv("KLAUDIA_WEB_SEARCH_ENGINE"))
	}
	if engine == "" {
		engine = "ddg"
	}
	maxResults := opts.MaxResults
	if maxResults <= 0 {
		maxResults = 8
	}
	if maxResults > 20 {
		maxResults = 20
	}

	searchURL, err := searchURL(engine, opts.Query)
	if err != nil {
		return nil, err
	}
	results, err := e.searchOnce(engine, searchURL)
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		snap, snapErr := e.Snapshot(true, false)
		if snapErr == nil && isSearchChallengePage(engine, snap.HTML) && e.browserOpts.HeadedFallback && e.browserOpts.RemoteURL == "" {
			results, err = e.searchWithHeadedFallback(ctx, engine, searchURL)
			if err != nil {
				return nil, err
			}
		}
	}
	return filterResults(results, opts.AllowedDomains, opts.BlockedDomains, maxResults), nil
}

func (e *Engine) searchOnce(engine, searchURL string) ([]SearchResult, error) {
	if err := e.Navigate(searchURL); err != nil {
		return nil, err
	}
	snap, err := e.Snapshot(true, false)
	if err != nil {
		return nil, err
	}
	return parseResults(engine, snap.HTML)
}

func (e *Engine) searchWithHeadedFallback(ctx context.Context, engine, searchURL string) ([]SearchResult, error) {
	oldOpts := e.Options()
	headedOpts := oldOpts
	headedOpts.Headless = false
	headedOpts.Mode = ModeLaunch
	headedOpts.RemoteURL = ""
	if headedOpts.UserDataDir == "" {
		headedOpts.UserDataDir = DefaultUserDataDir()
	}
	if err := e.Relaunch(headedOpts); err != nil {
		return nil, err
	}
	defer func() {
		if oldOpts.Headless {
			_ = e.Relaunch(oldOpts)
		}
	}()

	if err := e.Navigate(searchURL); err != nil {
		return nil, err
	}
	deadline := time.Now().Add(2 * time.Minute)
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		snap, err := e.Snapshot(true, false)
		if err != nil {
			return nil, err
		}
		if !isSearchChallengePage(engine, snap.HTML) {
			results, err := parseResults(engine, snap.HTML)
			if err != nil {
				return nil, err
			}
			if len(results) > 0 {
				return results, nil
			}
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("search challenge detected; opened headed Chrome with profile %q, but no results appeared within 2 minutes", headedOpts.UserDataDir)
		}
		time.Sleep(2 * time.Second)
	}
}

func searchURL(engine, query string) (string, error) {
	q := url.QueryEscape(query)
	switch engine {
	case "ddg", "duckduckgo", "duckduckgo-html":
		return "https://duckduckgo.com/html/?q=" + q, nil
	case "google":
		return "https://www.google.com/search?q=" + q + "&hl=en", nil
	default:
		return "", fmt.Errorf("unsupported search engine %q", engine)
	}
}

func parseResults(engine, html string) ([]SearchResult, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil, err
	}
	switch engine {
	case "ddg", "duckduckgo", "duckduckgo-html":
		return parseDDGResults(doc), nil
	case "google":
		return parseGoogleResults(doc), nil
	default:
		return nil, fmt.Errorf("unsupported search engine %q", engine)
	}
}

func isSearchChallengePage(engine, html string) bool {
	lower := strings.ToLower(html)
	switch engine {
	case "ddg", "duckduckgo", "duckduckgo-html":
		return strings.Contains(lower, "anomaly-modal") ||
			strings.Contains(lower, "unfortunately, bots use duckduckgo too") ||
			strings.Contains(lower, "duckduckgo.com/anomaly.js")
	case "google":
		return strings.Contains(lower, "/sorry/index") ||
			strings.Contains(lower, "our systems have detected unusual traffic") ||
			strings.Contains(lower, "g-recaptcha")
	default:
		return false
	}
}

func parseDDGResults(doc *goquery.Document) []SearchResult {
	var out []SearchResult
	doc.Find(".result").Each(func(_ int, s *goquery.Selection) {
		link := s.Find("a.result__a").First()
		title := cleanText(link.Text())
		href, _ := link.Attr("href")
		snippet := cleanText(s.Find(".result__snippet").First().Text())
		if title == "" || href == "" {
			return
		}
		out = append(out, SearchResult{Title: title, URL: normalizeDDGURL(href), Snippet: snippet})
	})
	return out
}

func parseGoogleResults(doc *goquery.Document) []SearchResult {
	var out []SearchResult
	doc.Find("a").Each(func(_ int, s *goquery.Selection) {
		href, _ := s.Attr("href")
		if !strings.HasPrefix(href, "http") {
			return
		}
		title := cleanText(s.Find("h3").First().Text())
		if title == "" {
			return
		}
		snippet := cleanText(s.Parent().Parent().Text())
		out = append(out, SearchResult{Title: title, URL: href, Snippet: snippet})
	})
	return dedupeResults(out)
}

func normalizeDDGURL(href string) string {
	if strings.HasPrefix(href, "//") {
		href = "https:" + href
	}
	u, err := url.Parse(href)
	if err != nil {
		return href
	}
	if v := u.Query().Get("uddg"); v != "" {
		if decoded, err := url.QueryUnescape(v); err == nil {
			return decoded
		}
		return v
	}
	return href
}

func filterResults(results []SearchResult, allowed, blocked []string, max int) []SearchResult {
	allowedSet := domainSet(allowed)
	blockedSet := domainSet(blocked)
	out := make([]SearchResult, 0, min(max, len(results)))
	seen := map[string]bool{}
	for _, r := range results {
		if seen[r.URL] {
			continue
		}
		seen[r.URL] = true
		domain := resultDomain(r.URL)
		if len(allowedSet) > 0 && !domainMatchesSet(domain, allowedSet) {
			continue
		}
		if domainMatchesSet(domain, blockedSet) {
			continue
		}
		out = append(out, r)
		if len(out) >= max {
			break
		}
	}
	return out
}

func dedupeResults(results []SearchResult) []SearchResult {
	out := make([]SearchResult, 0, len(results))
	seen := map[string]bool{}
	for _, r := range results {
		if seen[r.URL] {
			continue
		}
		seen[r.URL] = true
		out = append(out, r)
	}
	return out
}

func domainSet(domains []string) map[string]bool {
	out := make(map[string]bool, len(domains))
	for _, d := range domains {
		d = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(d)), "www.")
		if d != "" {
			out[d] = true
		}
	}
	return out
}

func domainMatchesSet(domain string, set map[string]bool) bool {
	if len(set) == 0 || domain == "" {
		return false
	}
	for d := range set {
		if domain == d || strings.HasSuffix(domain, "."+d) {
			return true
		}
	}
	return false
}

func resultDomain(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
}

func cleanText(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
