package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/greenthread-ai/klaudia/internal/permission"
	"github.com/greenthread-ai/klaudia/internal/tools"
)

// fakeMCPEnv makes the test binary, when re-executed, serve a stdio MCP server
// instead of running tests. It is set through the server's own config Env, so
// the connect path under test is the same one a real .mcp.json takes.
const fakeMCPEnv = "KLAUDIA_FAKE_MCP"

// init, not TestMain: stderr_test.go owns the package's TestMain, and init
// runs before it. When fakeMCPEnv is set this process is the fake stdio server
// a test spawned, so it serves and exits without running any tests.
func init() {
	if os.Getenv(fakeMCPEnv) == "" {
		return
	}
	if err := newFakeServer("stdio").Run(context.Background(), &mcpsdk.StdioTransport{}); err != nil {
		os.Exit(1)
	}
	os.Exit(0)
}

// newFakeServer builds a server named name with an echo tool that reports
// which server answered, a tool that always fails, and one text resource.
func newFakeServer(name string) *mcpsdk.Server {
	srv := mcpsdk.NewServer(&mcpsdk.Implementation{Name: name, Version: "0.0.1"}, nil)
	mcpsdk.AddTool(srv, &mcpsdk.Tool{Name: "echo", Description: "Echo the message back"},
		func(_ context.Context, _ *mcpsdk.CallToolRequest, in echoIn) (*mcpsdk.CallToolResult, any, error) {
			return &mcpsdk.CallToolResult{Content: []mcpsdk.Content{
				&mcpsdk.TextContent{Text: name + " echo: " + in.Message},
				&mcpsdk.ImageContent{Data: []byte{1, 2, 3}, MIMEType: "image/png"},
				&mcpsdk.TextContent{Text: "second block"},
			}}, nil, nil
		})
	mcpsdk.AddTool(srv, &mcpsdk.Tool{Name: "explode", Description: "Always fails"},
		func(context.Context, *mcpsdk.CallToolRequest, echoIn) (*mcpsdk.CallToolResult, any, error) {
			return nil, nil, errors.New("kaboom")
		})
	srv.AddResource(&mcpsdk.Resource{URI: "mem://readme", Name: "readme", MIMEType: "text/plain"},
		func(_ context.Context, req *mcpsdk.ReadResourceRequest) (*mcpsdk.ReadResourceResult, error) {
			return &mcpsdk.ReadResourceResult{Contents: []*mcpsdk.ResourceContents{
				{URI: req.Params.URI, Text: "hello from " + name},
				{URI: req.Params.URI, Blob: []byte{0}},
				{URI: req.Params.URI, Text: "line two"},
			}}, nil
		})
	return srv
}

func fakeCommandConfig(t *testing.T) ServerConfig {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return ServerConfig{Command: exe, Env: map[string]string{fakeMCPEnv: "1"}}
}

// httpServer serves newFakeServer over the given handler kind on loopback.
func httpServer(t *testing.T, kind string) *httptest.Server {
	t.Helper()
	get := func(*http.Request) *mcpsdk.Server { return newFakeServer(kind) }
	var h http.Handler
	if kind == "sse" {
		h = mcpsdk.NewSSEHandler(get, nil)
	} else {
		h = mcpsdk.NewStreamableHTTPHandler(get, nil)
	}
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)
	return ts
}

func toolNamed(t *testing.T, ts []tools.Tool, name string) tools.Tool {
	t.Helper()
	for _, tl := range ts {
		if tl.Name() == name {
			return tl
		}
	}
	t.Fatalf("no tool %q in %v", name, names(ts))
	return nil
}

func callEcho(t *testing.T, tl tools.Tool, msg string) tools.Result {
	t.Helper()
	raw, _ := json.Marshal(map[string]string{"message": msg})
	res, err := tl.Execute(context.Background(), tools.Context{}, raw)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	return res[0]
}

func testCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// Connect reaches every transport a .mcp.json can name, and a server that
// cannot start costs only itself: its error is reported and it stays listed,
// disconnected, so /mcp can retry it.
func TestConnectReachesEachTransportAndIsolatesFailures(t *testing.T) {
	ctx := testCtx(t)
	cfg := Config{MCPServers: map[string]ServerConfig{
		"stdio":      fakeCommandConfig(t),
		"streamable": {Type: "http", URL: httpServer(t, "streamable").URL},
		"sse":        {Type: "SSE", URL: httpServer(t, "sse").URL},
		"broken":     {Command: "/nonexistent/klaudia-test-mcp"},
	}}
	m, errs := Connect(ctx, cfg)
	defer m.Close()

	if len(errs) != 1 || !strings.Contains(errs[0].Error(), `mcp "broken" connect`) {
		t.Errorf("errs = %v, want exactly the broken server's", errs)
	}
	var got []string
	for _, s := range m.Servers() {
		got = append(got, s.Name)
		if s.Connected() != (s.Name != "broken") {
			t.Errorf("%s Connected = %v", s.Name, s.Connected())
		}
	}
	if strings.Join(got, ",") != "broken,sse,stdio,streamable" {
		t.Errorf("servers = %v, want sorted by name with the placeholder kept", got)
	}

	all := m.Tools(ctx)
	for _, name := range []string{"stdio", "streamable", "sse"} {
		res := callEcho(t, toolNamed(t, all, "mcp__"+name+"__echo"), "ping")
		// Text blocks are joined; the image block in between is dropped.
		if res.IsError || res.Content != name+" echo: ping\nsecond block" {
			t.Errorf("%s echo = %+v", name, res)
		}
	}
}

// A tool whose handler fails comes back as an error result the model can read,
// not as a Go error that would abort the turn.
func TestToolFailureIsAnErrorResult(t *testing.T) {
	m, errs := Connect(testCtx(t), Config{MCPServers: map[string]ServerConfig{
		"s": {URL: httpServer(t, "streamable").URL},
	}})
	defer m.Close()
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	res := callEcho(t, toolNamed(t, m.Tools(context.Background()), "mcp__s__explode"), "x")
	if !res.IsError || !strings.Contains(res.Content, "kaboom") {
		t.Errorf("result = %+v, want an error result carrying the server's message", res)
	}
}

// A peer that goes away mid-session turns calls into error results.
func TestToolCallToADeadPeerIsAnErrorResult(t *testing.T) {
	m, peer := startTestServerWithPeer(t)
	echo := toolNamed(t, m.Tools(context.Background()), "mcp__testsrv__echo")
	if err := peer.Close(); err != nil {
		t.Fatal(err)
	}
	res := callEcho(t, echo, "hi")
	if !res.IsError || !strings.HasPrefix(res.Content, "MCP call failed:") {
		t.Errorf("result = %+v, want an MCP call failed error result", res)
	}
}

// Reconnect relaunches from the recorded config and swaps the session into the
// same *Server, so a tool wrapper handed out before the drop works again.
func TestReconnectRevivesToolsHandedOutEarlier(t *testing.T) {
	m, errs := Connect(testCtx(t), Config{MCPServers: map[string]ServerConfig{
		"stdio": fakeCommandConfig(t),
	}})
	defer m.Close()
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	echo := toolNamed(t, m.Tools(context.Background()), "mcp__stdio__echo")

	if err := m.Disconnect("stdio"); err != nil {
		t.Fatal(err)
	}
	if res := callEcho(t, echo, "a"); !res.IsError || !strings.Contains(res.Content, "disconnected") {
		t.Fatalf("while disconnected: %+v", res)
	}
	// Disconnecting twice is harmless.
	if err := m.Disconnect("stdio"); err != nil {
		t.Errorf("second Disconnect: %v", err)
	}

	if err := m.Reconnect("stdio"); err != nil {
		t.Fatalf("Reconnect: %v", err)
	}
	if res := callEcho(t, echo, "b"); res.IsError || !strings.HasPrefix(res.Content, "stdio echo: b") {
		t.Errorf("after reconnect: %+v", res)
	}
	// Reconnecting a live server replaces its session rather than stacking one.
	before := m.Servers()[0].sess()
	if err := m.Reconnect("stdio"); err != nil {
		t.Fatalf("second Reconnect: %v", err)
	}
	if m.Servers()[0].sess() == before {
		t.Error("Reconnect kept the old session")
	}
}

func TestReconnectFailureLeavesTheServerDisconnected(t *testing.T) {
	ts := httpServer(t, "streamable")
	m, errs := Connect(testCtx(t), Config{MCPServers: map[string]ServerConfig{"s": {URL: ts.URL}}})
	defer m.Close()
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	// The endpoint goes away. The client's standalone SSE stream holds a
	// connection open, so drop connections first or Close waits on it.
	ts.CloseClientConnections()
	ts.Close()

	if err := m.Reconnect("s"); err == nil {
		t.Fatal("Reconnect to a closed endpoint succeeded")
	}
	if m.Servers()[0].Connected() {
		t.Error("a failed reconnect left the old session installed")
	}
}

// A server added by hand (as tests and embedders do) has no launch config, so
// there is nothing to reconnect it from; that is an error, not a panic.
func TestReconnectWithoutLaunchConfig(t *testing.T) {
	m, cleanup := startTestServer(t)
	defer cleanup()
	m.ctx = context.Background()
	err := m.Reconnect("testsrv")
	if err == nil || !strings.Contains(err.Error(), "no launch config") {
		t.Errorf("err = %v, want no launch config", err)
	}
	if !m.Servers()[0].Connected() {
		t.Error("a refused reconnect still dropped the live session")
	}
}

func TestResourceToolsListAndRead(t *testing.T) {
	m, errs := Connect(testCtx(t), Config{MCPServers: map[string]ServerConfig{
		"docs":  {URL: httpServer(t, "streamable").URL},
		"ghost": {Command: "/nonexistent/klaudia-test-mcp"},
	}})
	defer m.Close()
	if len(errs) != 1 {
		t.Fatalf("errs = %v", errs)
	}
	rts, err := m.ResourceTools()
	if err != nil {
		t.Fatal(err)
	}
	list, read := rts[0], rts[1]
	if list.Name() != "ListMcpResources" || read.Name() != "ReadMcpResource" {
		t.Fatalf("names = %s, %s", list.Name(), read.Name())
	}
	ctx := context.Background()

	res, err := list.Execute(ctx, tools.Context{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	// The disconnected server contributes nothing and doesn't break the list.
	if res[0].Content != "docs\tmem://readme\treadme" {
		t.Errorf("list = %q", res[0].Content)
	}

	cases := []struct {
		name, input, want string
		isErr             bool
	}{
		{"text contents joined", `{"server":"docs","uri":"mem://readme"}`, "hello from streamable\nline two", false},
		{"unknown server", `{"server":"nope","uri":"mem://readme"}`, `Unknown MCP server "nope"`, true},
		{"disconnected server", `{"server":"ghost","uri":"mem://readme"}`, `MCP server "ghost" is disconnected`, true},
		{"unknown resource", `{"server":"docs","uri":"mem://missing"}`, "Read failed:", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := read.Execute(ctx, tools.Context{}, json.RawMessage(tc.input))
			if err != nil {
				t.Fatal(err)
			}
			if res[0].IsError != tc.isErr || !strings.HasPrefix(res[0].Content, tc.want) {
				t.Errorf("read = %+v, want prefix %q (error=%v)", res[0], tc.want, tc.isErr)
			}
		})
	}
	if _, err := read.Execute(ctx, tools.Context{}, json.RawMessage(`not json`)); err == nil {
		t.Error("malformed input should be a hard error")
	}
}

func TestResourceListWithNothingToShow(t *testing.T) {
	m := &Manager{}
	rts, err := m.ResourceTools()
	if err != nil {
		t.Fatal(err)
	}
	res, _ := rts[0].Execute(context.Background(), tools.Context{}, nil)
	if res[0].Content != "No MCP resources available." {
		t.Errorf("list = %q", res[0].Content)
	}
}

// The resource tools only read, so they never prompt; ReadMcpResource checks
// its input against its schema before it runs.
func TestResourceToolContract(t *testing.T) {
	rts, err := (&Manager{}).ResourceTools()
	if err != nil {
		t.Fatal(err)
	}
	for _, tl := range rts {
		if d, _ := tl.Description(context.Background()); d == "" {
			t.Errorf("%s has no description", tl.Name())
		}
		var sch map[string]any
		if err := json.Unmarshal(tl.InputSchema(), &sch); err != nil || sch["type"] != "object" {
			t.Errorf("%s schema = %s", tl.Name(), tl.InputSchema())
		}
		pctx := permission.Context{Mode: permission.StaticMode(permission.ModeDontAsk)}
		if got := tl.CheckPermissions(pctx, tl.PermissionRequest(nil)).Behavior; got != permission.Allow {
			t.Errorf("%s permission = %s, want allow even in dontAsk", tl.Name(), got)
		}
		if tl.PermissionRequest(nil).Specifier != "" {
			t.Errorf("%s has a specifier", tl.Name())
		}
	}
	list, read := rts[0], rts[1]
	if err := list.ValidateInput(json.RawMessage(`{}`)); err != nil {
		t.Errorf("list rejected {}: %v", err)
	}
	if err := read.ValidateInput(json.RawMessage(`{"server":"s","uri":"u"}`)); err != nil {
		t.Errorf("read rejected a valid input: %v", err)
	}
	if err := read.ValidateInput(json.RawMessage(`{"server":7}`)); err == nil {
		t.Error("read accepted a non-string server")
	}
}

func TestMCPToolContract(t *testing.T) {
	m, cleanup := startTestServer(t)
	defer cleanup()
	echo := toolNamed(t, m.Tools(context.Background()), "mcp__testsrv__echo")

	if d, _ := echo.Description(context.Background()); d != "Echo the message back" {
		t.Errorf("description = %q, want the server's", d)
	}
	if err := echo.ValidateInput(json.RawMessage(`{"message":"x"}`)); err != nil {
		t.Errorf("valid input rejected: %v", err)
	}
	if err := echo.ValidateInput(json.RawMessage(`{"message":`)); err == nil {
		t.Error("malformed JSON accepted")
	}
	// Approvals key on the qualified name.
	if got := echo.PermissionRequest(nil).Specifier; got != "mcp__testsrv__echo" {
		t.Errorf("specifier = %q", got)
	}
	plan := permission.Context{Mode: permission.StaticMode(permission.ModePlan)}
	if got := echo.CheckPermissions(plan, permission.PermissionRequest{}).Behavior; got != permission.Deny {
		t.Errorf("plan-mode decision = %s, want deny", got)
	}
}

// Watch is the production entry point: the default debounce, the same paths
// LoadConfig reads.
func TestWatchReportsAConfigWrite(t *testing.T) {
	isolateConfigRoot(t)
	dir := t.TempDir()
	changed := make(chan struct{}, 4)
	stop, err := Watch(dir, func() { changed <- struct{}{} })
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	if err := os.WriteFile(dir+"/.mcp.json", []byte(`{"mcpServers":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	waitForChange(t, changed, "a new project .mcp.json")
}

// With none of the config directories present there is nothing to watch; that
// is a no-op watcher, not an error.
func TestWatchWithNoWatchableDirectories(t *testing.T) {
	t.Setenv("KLAUDIA_CONFIG_DIR", t.TempDir()+"/absent")
	stop, err := Watch(t.TempDir()+"/also-absent", func() { t.Error("onChange called") })
	if err != nil {
		t.Fatal(err)
	}
	stop()
	stop()
}
