package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/textproto"
	"os/exec"
	"strconv"
	"sync"
	"time"
)

// Client is a minimal LSP client speaking JSON-RPC 2.0 over a server's stdio
// with Content-Length framing. It tracks diagnostics pushed by the server.
type Client struct {
	cmd    *exec.Cmd
	cancel context.CancelFunc
	stdin  io.WriteCloser

	wmu sync.Mutex // serialises framed writes
	seq int

	mu      sync.Mutex
	pending map[int]chan rpcResponse   // id -> response waiter
	diags   map[string][]Diagnostic    // uri -> latest diagnostics
	diagCh  map[string][]chan struct{} // uri -> waiters for the next publish
	closed  bool
}

type rpcResponse struct {
	result json.RawMessage
	err    *rpcError
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type wireMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int            `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// NewClient spawns the server binary and starts the read loop. parent bounds
// the server's lifetime (cancelling it, or Close, terminates the process).
func NewClient(parent context.Context, bin string, args ...string) (*Client, error) {
	ctx, cancel := context.WithCancel(parent)
	cmd := exec.CommandContext(ctx, bin, args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start %s: %w", bin, err)
	}
	c := &Client{
		cmd: cmd, cancel: cancel, stdin: stdin,
		pending: map[int]chan rpcResponse{},
		diags:   map[string][]Diagnostic{},
		diagCh:  map[string][]chan struct{}{},
	}
	go c.readLoop(stdout)
	return c, nil
}

// readLoop reads framed messages until EOF and dispatches them.
func (c *Client) readLoop(r io.Reader) {
	br := bufio.NewReader(r)
	tp := textproto.NewReader(br)
	for {
		hdr, err := tp.ReadMIMEHeader()
		if err != nil {
			c.failAll(err)
			return
		}
		n, err := strconv.Atoi(hdr.Get("Content-Length"))
		if err != nil || n <= 0 {
			continue
		}
		body := make([]byte, n)
		if _, err := io.ReadFull(br, body); err != nil {
			c.failAll(err)
			return
		}
		var msg wireMessage
		if json.Unmarshal(body, &msg) != nil {
			continue
		}
		c.dispatch(msg)
	}
}

func (c *Client) dispatch(msg wireMessage) {
	switch {
	case msg.ID != nil && (msg.Result != nil || msg.Error != nil):
		// Response to one of our requests.
		c.mu.Lock()
		ch := c.pending[*msg.ID]
		delete(c.pending, *msg.ID)
		c.mu.Unlock()
		if ch != nil {
			ch <- rpcResponse{result: msg.Result, err: msg.Error}
		}
	case msg.Method == "textDocument/publishDiagnostics":
		var p struct {
			URI         string       `json:"uri"`
			Diagnostics []Diagnostic `json:"diagnostics"`
		}
		if json.Unmarshal(msg.Params, &p) == nil {
			c.mu.Lock()
			c.diags[p.URI] = p.Diagnostics
			waiters := c.diagCh[p.URI]
			delete(c.diagCh, p.URI)
			c.mu.Unlock()
			for _, w := range waiters {
				close(w)
			}
		}
	case msg.ID != nil:
		// Server-to-client request we don't implement: reply with null so the
		// server isn't left waiting (e.g. workspace/configuration).
		_ = c.reply(*msg.ID)
	}
}

// failAll runs when the read loop ends — the server exited or its stdout broke.
// Pending requests fail with the cause, and Diagnostics waiters are released so
// they return at once instead of sitting out diagnosticsWait on a dead server.
func (c *Client) failAll(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	for id, ch := range c.pending {
		ch <- rpcResponse{err: &rpcError{Message: err.Error()}}
		delete(c.pending, id)
	}
	for uri, waiters := range c.diagCh {
		for _, w := range waiters {
			close(w)
		}
		delete(c.diagCh, uri)
	}
}

// Alive reports whether the server is still connected. It turns false once the
// read loop has ended, which is how the pool notices a server that crashed.
func (c *Client) Alive() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return !c.closed
}

// call sends a request and waits for the response (bounded by ctx).
func (c *Client) call(ctx context.Context, method string, params any, result any) error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return fmt.Errorf("lsp client closed")
	}
	c.seq++
	id := c.seq
	ch := make(chan rpcResponse, 1)
	c.pending[id] = ch
	c.mu.Unlock()

	if err := c.write(wireMessage{JSONRPC: "2.0", ID: &id, Method: method, Params: mustJSON(params)}); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return ctx.Err()
	case resp := <-ch:
		if resp.err != nil {
			return fmt.Errorf("lsp %s: %s", method, resp.err.Message)
		}
		if result != nil && len(resp.result) > 0 {
			return json.Unmarshal(resp.result, result)
		}
		return nil
	}
}

// notify sends a notification (no response expected).
func (c *Client) notify(method string, params any) error {
	return c.write(wireMessage{JSONRPC: "2.0", Method: method, Params: mustJSON(params)})
}

func (c *Client) reply(id int) error {
	return c.write(wireMessage{JSONRPC: "2.0", ID: &id, Result: json.RawMessage("null")})
}

// write frames and sends one message.
func (c *Client) write(msg wireMessage) error {
	body, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	c.wmu.Lock()
	defer c.wmu.Unlock()
	if _, err := fmt.Fprintf(c.stdin, "Content-Length: %d\r\n\r\n", len(body)); err != nil {
		return err
	}
	_, err = c.stdin.Write(body)
	return err
}

func mustJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

// closeGrace is how long Close gives a server to exit on its own after the
// exit notification before killing it.
const closeGrace = 2 * time.Second

// Close shuts the server down gracefully, then kills the process if it hasn't
// exited within closeGrace. Killing it straight after sending exit — as Close
// used to — meant a server that hadn't yet read the notification never got to
// exit cleanly.
func (c *Client) Close() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = c.call(ctx, "shutdown", nil, nil)
	_ = c.notify("exit", nil)
	_ = c.stdin.Close()

	exited := make(chan struct{})
	go func() {
		_ = c.cmd.Wait()
		close(exited)
	}()
	select {
	case <-exited:
	case <-time.After(closeGrace):
		c.cancel()
		<-exited
	}
	c.cancel()
}
