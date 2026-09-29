package mcp

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// The test binary doubles as a stdio MCP server when KLAUDIA_TEST_MCP_SERVER
// names a behaviour, so the stderr path is exercised against a real child.
func TestMain(m *testing.M) {
	switch os.Getenv("KLAUDIA_TEST_MCP_SERVER") {
	case "":
		os.Exit(m.Run())
	case "chatty":
		fmt.Fprintln(os.Stderr, "hello from the server")
		fmt.Fprint(os.Stderr, "no newline at the end")
		srv := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "chatty", Version: "0"}, nil)
		_ = srv.Run(context.Background(), &mcpsdk.StdioTransport{})
		os.Exit(0)
	case "broken":
		fmt.Fprintln(os.Stderr, "starting")
		fmt.Fprintln(os.Stderr, "fatal: LOKI_URL is not set")
		os.Exit(3)
	}
}

// syncBuffer is a bytes.Buffer safe for the exec copy goroutine and the test.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func helperServer(t *testing.T, behaviour string) ServerConfig {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return ServerConfig{Command: exe, Env: map[string]string{"KLAUDIA_TEST_MCP_SERVER": behaviour}}
}

func TestStdioServerStderrIsForwardedAndLogged(t *testing.T) {
	var out syncBuffer
	SetStderr(&out)
	defer SetStderr(nil)
	logDir := t.TempDir()
	t.Setenv("KLAUDIA_MCP_STDERR", logDir)

	srv, err := ConnectCommand(context.Background(), "test", helperServer(t, "chatty"))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := srv.sess().Close(); err != nil {
		t.Logf("close: %v", err)
	}

	if got := out.String(); !strings.Contains(got, "mcp[test]: hello from the server\n") {
		t.Fatalf("forwarded stderr = %q, want the prefixed line", got)
	}
	log, err := os.ReadFile(filepath.Join(logDir, "test.log"))
	if err != nil {
		t.Fatalf("per-server log: %v", err)
	}
	if !strings.Contains(string(log), "hello from the server\n") {
		t.Fatalf("log = %q", log)
	}
}

func TestStdioServerConnectFailureCarriesStderrTail(t *testing.T) {
	SetStderr(nil)
	_, err := ConnectCommand(context.Background(), "loki", helperServer(t, "broken"))
	if err == nil {
		t.Fatal("connect to a server that exits at startup succeeded")
	}
	if !strings.Contains(err.Error(), "fatal: LOKI_URL is not set") || !strings.Contains(err.Error(), `mcp "loki" connect`) {
		t.Fatalf("err = %v, want the connect error with the server's stderr tail", err)
	}
}

func TestServerStderrBoundsLinesAndTail(t *testing.T) {
	SetStderr(nil)
	s := newServerStderr("x")
	for i := range stderrTailLines + 5 {
		fmt.Fprintf(s, "line %d\r\n", i)
	}
	tail := strings.Split(s.Tail(), "\n")
	if len(tail) != stderrTailLines || tail[0] != "line 5" || tail[len(tail)-1] != fmt.Sprintf("line %d", stderrTailLines+4) {
		t.Fatalf("tail = %q", tail)
	}
	_, _ = s.Write(bytes.Repeat([]byte("a"), 3*maxStderrLine+10))
	if len(s.partial) > maxStderrLine {
		t.Fatalf("unterminated output buffered %d bytes, want <= %d", len(s.partial), maxStderrLine)
	}
}

func TestServerStderrLogNameIsSanitised(t *testing.T) {
	t.Setenv("KLAUDIA_MCP_STDERR", "/logs")
	if got := newServerStderr("../evil/name").logPath; got != "/logs/.._evil_name.log" {
		t.Fatalf("logPath = %q", got)
	}
}
