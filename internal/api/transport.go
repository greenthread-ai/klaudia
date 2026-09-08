package api

import (
	"net/http"
	"time"

	"golang.org/x/net/http2"
)

// The failure this file exists for: a session is left idle — overnight, or
// because the laptop slept — and the next message hangs for six minutes and
// then reports a stall that "auto-retried without success". Reconstructed from
// a transcript: 6h11m between the previous reply and the send that failed.
//
// Nothing was flaky. The Anthropic API is HTTP/2, and Go's automatic HTTP/2
// does no health checking at all by default (http2.Transport.ReadIdleTimeout is
// zero, meaning "never send a keepalive PING"). A multiplexed h2 connection
// that a NAT, VPN or sleep cycle has silently killed therefore stays in the
// pool looking perfectly usable: the request is written into a black hole and
// the read blocks forever. http.Transport.IdleConnTimeout does not save us,
// because its timer does not fire reliably across a sleep, and the request can
// claim the corpse before the cleanup runs.
//
// So the stall watchdog was doing its job — the layer below it was lying about
// the connection. Two changes:
//
//  1. Enable h2 keepalive pings, so a dead connection is *detected* in seconds
//     and the request fails fast onto a fresh one instead of waiting for the
//     120s watchdog and then re-using the same corpse.
//  2. Drop idle connections before a stall retry (see streamRetrying), so even
//     if a ping was not due, the retry dials rather than reusing the pool.

const (
	// h2ReadIdleTimeout is how long a connection may sit unread before the
	// transport sends a PING. Short enough that the first send after a break
	// pays a ping rather than a two-minute stall; long enough to be invisible
	// during an active turn, where data is arriving constantly.
	h2ReadIdleTimeout = 20 * time.Second
	// h2PingTimeout is how long to wait for the PONG before declaring the
	// connection dead. Well inside the stall watchdog, so this path resolves
	// first and produces a retryable network error instead of a stall.
	h2PingTimeout = 10 * time.Second
)

// newHTTPClient builds the HTTP client used for model calls, with HTTP/2
// keepalive pings enabled.
func newHTTPClient() *http.Client {
	t, ok := http.DefaultTransport.(*http.Transport)
	if !ok { // not the stdlib default (test hook, or a future stdlib change)
		return &http.Client{}
	}
	clone := t.Clone()
	// A failure here means no h2 pings, which is exactly today's behaviour —
	// so it is not worth failing a session over, and there is nowhere to report
	// it that would not be terminal noise.
	if h2, err := configureH2(clone); err == nil {
		_ = h2
	}
	return &http.Client{Transport: clone}
}

// configureH2 turns on keepalive pings for a transport's HTTP/2 support and
// returns the h2 transport it configured, so a test can assert the timeouts
// were actually applied rather than trusting that they were.
func configureH2(t *http.Transport) (*http2.Transport, error) {
	h2, err := http2.ConfigureTransports(t)
	if err != nil {
		return nil, err
	}
	h2.ReadIdleTimeout = h2ReadIdleTimeout
	h2.PingTimeout = h2PingTimeout
	return h2, nil
}
