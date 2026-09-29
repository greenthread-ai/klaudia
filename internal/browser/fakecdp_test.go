package browser

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
)

// fakeCDP is just enough of the Chrome DevTools Protocol, served on loopback,
// for chromedp to attach, navigate, evaluate and read the DOM. It never loads
// anything: "navigating" to a URL selects the canned page the test's pages
// func returns for it, so a search engine URL costs no network and no Chrome.
//
// It speaks both entry points a real Chrome offers: /json/version (what attach
// mode resolves a bare http://host:port through) and a browser websocket per
// id at /devtools/browser/<id>, which a fake chrome executable can announce on
// stderr for launch mode (see fakeChrome).
type fakeCDP struct {
	srv *httptest.Server

	mu       sync.Mutex
	pages    func(browserID, url string) fakePage
	connects map[string]int // browser id -> websocket connections
	navs     []string       // "<browser id> <url>" per Page.navigate
}

type fakePage struct {
	title, html string
	errorText   string // non-empty: Page.navigate reports this load error
}

const defaultFakeHTML = `<html><head><title>Example Domain</title></head><body><h1>Hello</h1><p>from the <b>fake</b> browser</p></body></html>`

func newFakeCDP(t *testing.T) *fakeCDP {
	t.Helper()
	f := &fakeCDP{connects: map[string]int{}}
	f.pages = func(string, string) fakePage {
		return fakePage{title: "Example Domain", html: defaultFakeHTML}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/json/version", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"Browser":              "FakeChrome/1.0",
			"webSocketDebuggerUrl": "ws://" + r.Host + "/devtools/browser/remote",
		})
	})
	mux.HandleFunc("/devtools/browser/", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/devtools/browser/")
		conn, _, _, err := ws.UpgradeHTTP(r, w)
		if err != nil {
			return
		}
		defer conn.Close()
		f.mu.Lock()
		f.connects[id]++
		f.mu.Unlock()
		f.serve(conn, id)
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(func() {
		f.srv.CloseClientConnections()
		f.srv.Close()
	})
	return f
}

// wsURL is the browser websocket a launched fake chrome announces.
func (f *fakeCDP) wsURL(id string) string {
	return "ws" + strings.TrimPrefix(f.srv.URL, "http") + "/devtools/browser/" + id
}

func (f *fakeCDP) setPages(fn func(browserID, url string) fakePage) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pages = fn
}

func (f *fakeCDP) connections(id string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.connects[id]
}

func (f *fakeCDP) navigations() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.navs...)
}

type cdpMessage struct {
	ID        int64           `json:"id,omitempty"`
	SessionID string          `json:"sessionId,omitempty"`
	Method    string          `json:"method,omitempty"`
	Params    json.RawMessage `json:"params,omitempty"`
}

func (f *fakeCDP) serve(conn interface {
	Read([]byte) (int, error)
	Write([]byte) (int, error)
}, id string) {
	const session = "S1"
	cur := fakePage{}
	url := "about:blank"
	textPolls := 0

	send := func(v any) bool {
		b, _ := json.Marshal(v)
		return wsutil.WriteServerText(conn, b) == nil
	}
	for {
		data, err := wsutil.ReadClientText(conn)
		if err != nil {
			return
		}
		var msg cdpMessage
		if json.Unmarshal(data, &msg) != nil {
			return
		}
		reply := func(result any) bool {
			return send(map[string]any{"id": msg.ID, "sessionId": msg.SessionID, "result": result})
		}
		event := func(method string, params any) bool {
			return send(map[string]any{"method": method, "sessionId": session, "params": params})
		}
		value := func(v any) map[string]any {
			typ := "string"
			if _, ok := v.(int); ok {
				typ = "number"
			}
			return map[string]any{"result": map[string]any{"type": typ, "value": v}}
		}

		var ok bool
		switch msg.Method {
		case "Target.setDiscoverTargets":
			ok = reply(map[string]any{})
			if ok && msg.SessionID == "" {
				ok = send(map[string]any{"method": "Target.targetCreated", "params": map[string]any{
					"targetInfo": map[string]any{
						"targetId": "T1", "type": "page", "title": "", "url": "about:blank",
						"attached": false, "canAccessOpener": false,
					},
				}})
			}
		case "Target.createTarget":
			// Attach mode opens its own tab rather than waiting for one.
			ok = reply(map[string]any{"targetId": "T1"})
		case "Target.attachToTarget":
			ok = reply(map[string]any{"sessionId": session})
		case "Runtime.evaluate":
			var p struct {
				Expression string `json:"expression"`
			}
			_ = json.Unmarshal(msg.Params, &p)
			switch {
			case p.Expression == "self":
				ok = reply(map[string]any{"result": map[string]any{"type": "object", "className": "Window"}})
			case p.Expression == "document.readyState":
				ok = reply(value("complete"))
			case strings.Contains(p.Expression, "innerText"):
				// The first poll after a navigation sees an empty body, as a
				// client-rendered page would; after that the text is stable.
				textPolls++
				n := len(cur.html)
				if textPolls == 1 {
					n = 0
				}
				ok = reply(value(n))
			case p.Expression == "document.location.toString()":
				ok = reply(value(url))
			case p.Expression == "document.title":
				ok = reply(value(cur.title))
			default:
				ok = reply(map[string]any{"result": map[string]any{"type": "undefined"}})
			}
		case "Page.navigate":
			var p struct {
				URL string `json:"url"`
			}
			_ = json.Unmarshal(msg.Params, &p)
			f.mu.Lock()
			f.navs = append(f.navs, id+" "+p.URL)
			page := f.pages(id, p.URL)
			f.mu.Unlock()
			if page.errorText != "" {
				ok = reply(map[string]any{"frameId": "F1", "loaderId": "L1", "errorText": page.errorText})
				break
			}
			url, cur, textPolls = p.URL, page, 0
			ok = reply(map[string]any{"frameId": "F1", "loaderId": "L1"}) &&
				event("Page.frameNavigated", map[string]any{
					"type": "Navigation",
					"frame": map[string]any{
						"id": "F1", "loaderId": "L1", "url": url, "domainAndRegistry": "",
						"securityOrigin": "", "mimeType": "text/html", "secureContextType": "Secure",
						"crossOriginIsolatedContextType": "NotIsolated", "gatedAPIFeatures": []string{},
					},
				}) &&
				event("Page.lifecycleEvent", map[string]any{"frameId": "F1", "loaderId": "L1", "name": "init", "timestamp": 1}) &&
				event("Page.loadEventFired", map[string]any{"timestamp": 2})
		case "DOM.getDocument":
			ok = reply(map[string]any{"root": map[string]any{
				"nodeId": 1, "backendNodeId": 1, "nodeType": 9, "nodeName": "#document",
				"localName": "", "nodeValue": "",
			}})
		case "DOM.getOuterHTML":
			ok = reply(map[string]any{"outerHTML": cur.html})
		default:
			ok = reply(map[string]any{})
		}
		if !ok {
			return
		}
	}
}

// fakeChrome installs an executable standing in for Chrome in launch mode. It
// announces a browser websocket on the fake CDP server the way Chrome does on
// stderr — "headless" or "headed" by whether it was passed --headless, so a
// test can serve the two differently — and then waits to be killed.
//
// If its --user-data-dir is busyProfile it instead fails the way Chrome does
// when another instance holds the profile. It lingers a second before exiting:
// chromedp calls cmd.Wait concurrently with reading the output pipe, and Wait
// closes the pipe, so a process that exits the instant it has written can have
// its output discarded unread (chromedp allocate.go, readOutput). Real Chrome
// takes longer than that to give up; an instant exit lost the ProcessSingleton
// line under GOMAXPROCS=1 and made these tests flaky.
func fakeChrome(t *testing.T, f *fakeCDP, busyProfile string) string {
	t.Helper()
	return writeFakeChrome(t, f, busyProfile,
		`echo "Failed to create a ProcessSingleton for your profile directory." >&2; sleep 1; exit 21`)
}

// fakeSilentChrome is fakeChrome whose busy-profile failure prints nothing —
// what klaudia sees when chromedp loses Chrome's output, reproduced
// deterministically instead of by racing it.
func fakeSilentChrome(t *testing.T, f *fakeCDP, busyProfile string) string {
	t.Helper()
	return writeFakeChrome(t, f, busyProfile, `exit 21`)
}

func writeFakeChrome(t *testing.T, f *fakeCDP, busyProfile, onBusy string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake-chrome")
	script := fmt.Sprintf(`#!/bin/sh
id=headed
for a in "$@"; do
  case "$a" in
    --headless*) id=headless ;;
    --user-data-dir=%[1]s) %[3]s ;;
  esac
done
echo "DevTools listening on %[2]s$id" >&2
exec sleep 300
`, busyProfile, f.wsURL(""), onBusy)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}
