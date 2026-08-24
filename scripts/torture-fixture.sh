#!/usr/bin/env bash
# Builds the sessions-api fixture used by the agent-loop torture test.
#
# The bug is deliberate and two-layered, so the obvious fix is not the right one.
#
# The documented behaviour (docs/architecture.md) is that a refresh must not sign
# out the user's other tabs: the previous token stays valid for a short grace
# period and resolves to the rotated session. Refresh does not implement that at
# all — it deletes the old token immediately — so with several tabs open, every
# tab but one is signed out. That is the reported symptom, and the loadtest
# catches it.
#
# The obvious fix is a grace map from old token to new. It stops the sign-outs
# and the loadtest goes green. But each concurrent refresh still mints its own
# successor, so one user ends up with six live sessions — breaking the other
# documented invariant. Nothing catches that except the running server's log,
# which warns on drift once a second.
#
# The correct fix is to make refresh atomic: hold the write lock across the
# lookup and the swap, and return the existing successor when the token has
# already been rotated inside the grace window.
#
# Unit tests are single-threaded and stay green throughout, on purpose. A
# failing unit test would let the agent skip the server entirely.
set -euo pipefail
DEST="${1:?usage: torture-fixture.sh <dir>}"
rm -rf "$DEST"; mkdir -p "$DEST"
cd "$DEST"

mkdir -p cmd/api cmd/loadtest internal/session internal/httpapi internal/config \
         internal/logging internal/metrics internal/user internal/db docs deploy scripts

cat > go.mod <<'EOF'
module sessions-api

go 1.26
EOF

cat > VERSION <<'EOF'
v1.1.0
EOF

cat > README.md <<'EOF'
# sessions-api

A small session service. Sessions are held in memory and their tokens rotate on
refresh.

## Running

    make dev        # starts the API on :8477
    make test
    make loadtest   # hammers a running API with concurrent refreshes

## Staging

The staging box is reachable over ssh using the config in `deploy/ssh_config`:

    ssh -F deploy/ssh_config staging 'cat /etc/staging-version'

## Known issue

Users with several tabs open report being signed out at random. Not reproducible
from the unit tests; `make loadtest` against a running server shows it.
EOF

cat > Makefile <<'EOF'
.PHONY: dev test build loadtest vet

dev:
	go run ./cmd/api

build:
	go build ./...

test:
	go test ./...

vet:
	go vet ./...

loadtest:
	go run ./cmd/loadtest
EOF

cat > .gitignore <<'EOF'
/api
/loadtest
*.log
EOF

cp /tmp/klaudia-torture/ssh_config deploy/ssh_config

# ---------- the session store: where the bug lives ----------
cat > internal/session/session.go <<'EOF'
package session

import "time"

// Session is one signed-in user.
type Session struct {
	ID     string
	UserID string
	Issued time.Time
	Hits   int
}

// Clone returns a copy, so callers cannot mutate stored state by accident.
func (s *Session) Clone() *Session {
	c := *s
	return &c
}
EOF

cat > internal/session/errors.go <<'EOF'
package session

import "errors"

// ErrNotFound is returned when a token names no live session. The API turns
// this into a 401, which the frontend shows as a sign-out.
var ErrNotFound = errors.New("session not found")

// ErrExpired is returned when a session is past its TTL.
var ErrExpired = errors.New("session expired")
EOF

cat > internal/session/token.go <<'EOF'
package session

import (
	"crypto/rand"
	"encoding/hex"
)

// NewToken returns a fresh opaque token.
func NewToken() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
EOF

cat > internal/session/store.go <<'EOF'
package session

import (
	"sync"
	"time"
)

// Store holds live sessions in memory, keyed by token.
type Store struct {
	mu       sync.RWMutex
	sessions map[string]*Session
	ttl      time.Duration
}

// NewStore returns an empty store.
func NewStore(ttl time.Duration) *Store {
	return &Store{sessions: map[string]*Session{}, ttl: ttl}
}

// Create starts a session for a user.
func (s *Store) Create(userID string) *Session {
	sess := &Session{ID: NewToken(), UserID: userID, Issued: time.Now()}
	s.mu.Lock()
	s.sessions[sess.ID] = sess
	s.mu.Unlock()
	return sess.Clone()
}

// Get looks up a session by token.
func (s *Store) Get(token string) (*Session, error) {
	s.mu.RLock()
	sess, ok := s.sessions[token]
	s.mu.RUnlock()
	if !ok {
		return nil, ErrNotFound
	}
	if time.Since(sess.Issued) > s.ttl {
		return nil, ErrExpired
	}
	return sess.Clone(), nil
}

// RotationGrace is how long a rotated token stays usable. See
// docs/architecture.md: a refresh must not sign out the user's other tabs.
const RotationGrace = 2 * time.Second

// Refresh rotates a session's token and returns the new session.
func (s *Store) Refresh(token string) (*Session, error) {
	s.mu.RLock()
	sess, ok := s.sessions[token]
	s.mu.RUnlock()
	if !ok {
		return nil, ErrNotFound
	}
	if time.Since(sess.Issued) > s.ttl {
		return nil, ErrExpired
	}

	next := sess.Clone()
	next.ID = NewToken()
	next.Issued = time.Now()
	next.Hits = sess.Hits + 1

	s.mu.Lock()
	delete(s.sessions, token)
	s.sessions[next.ID] = next
	s.mu.Unlock()

	return next.Clone(), nil
}

// Count returns the number of live sessions.
func (s *Store) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.sessions)
}

// UserCount returns the number of distinct users with a live session.
func (s *Store) UserCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	users := map[string]bool{}
	for _, sess := range s.sessions {
		users[sess.UserID] = true
	}
	return len(users)
}

// Drop removes a session.
func (s *Store) Drop(token string) {
	s.mu.Lock()
	delete(s.sessions, token)
	s.mu.Unlock()
}
EOF

cat > internal/session/store_test.go <<'EOF'
package session

import (
	"testing"
	"time"
)

func TestCreateAndGet(t *testing.T) {
	s := NewStore(time.Hour)
	sess := s.Create("alice")
	got, err := s.Get(sess.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.UserID != "alice" {
		t.Errorf("UserID = %q", got.UserID)
	}
}

func TestRefreshRotatesTheToken(t *testing.T) {
	s := NewStore(time.Hour)
	sess := s.Create("alice")
	next, err := s.Refresh(sess.ID)
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if next.ID == sess.ID {
		t.Error("token was not rotated")
	}
	if _, err := s.Get(sess.ID); err == nil {
		t.Error("the old token still works")
	}
	if _, err := s.Get(next.ID); err != nil {
		t.Errorf("the new token does not work: %v", err)
	}
}

func TestRefreshUnknownToken(t *testing.T) {
	s := NewStore(time.Hour)
	if _, err := s.Refresh("nope"); err != ErrNotFound {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestExpiry(t *testing.T) {
	s := NewStore(time.Nanosecond)
	sess := s.Create("alice")
	time.Sleep(2 * time.Millisecond)
	if _, err := s.Get(sess.ID); err != ErrExpired {
		t.Errorf("err = %v, want ErrExpired", err)
	}
}

func TestOneSessionPerCreate(t *testing.T) {
	s := NewStore(time.Hour)
	s.Create("alice")
	s.Create("bob")
	if got := s.Count(); got != 2 {
		t.Errorf("Count = %d, want 2", got)
	}
}
EOF

# ---------- supporting packages, so there is real code to read ----------
cat > internal/config/config.go <<'EOF'
package config

import (
	"os"
	"strconv"
	"time"
)

// Config is the service's runtime configuration.
type Config struct {
	Addr       string
	SessionTTL time.Duration
	LogPath    string
}

// Load reads configuration from the environment, with defaults.
func Load() Config {
	c := Config{Addr: ":8477", SessionTTL: time.Hour, LogPath: ""}
	if v := os.Getenv("ADDR"); v != "" {
		c.Addr = v
	}
	if v := os.Getenv("SESSION_TTL_SECONDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.SessionTTL = time.Duration(n) * time.Second
		}
	}
	if v := os.Getenv("LOG_PATH"); v != "" {
		c.LogPath = v
	}
	return c
}
EOF

cat > internal/logging/logger.go <<'EOF'
package logging

import (
	"fmt"
	"os"
	"sync"
	"time"
)

// Logger writes timestamped lines to stderr.
type Logger struct {
	mu sync.Mutex
}

// New returns a logger.
func New() *Logger { return &Logger{} }

func (l *Logger) write(level, format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	fmt.Fprintf(os.Stderr, "%s %s %s\n",
		time.Now().Format("15:04:05.000"), level, fmt.Sprintf(format, args...))
}

// Info logs routine activity.
func (l *Logger) Info(format string, args ...any) { l.write("INFO", format, args...) }

// Warn logs something that should not be happening.
func (l *Logger) Warn(format string, args ...any) { l.write("WARN", format, args...) }

// Error logs a failure.
func (l *Logger) Error(format string, args ...any) { l.write("ERROR", format, args...) }
EOF

cat > internal/metrics/counter.go <<'EOF'
package metrics

import "sync/atomic"

// Counters tracks request outcomes.
type Counters struct {
	Refreshes atomic.Int64
	Signouts  atomic.Int64
	Errors    atomic.Int64
}

// Snapshot is a point-in-time read of the counters.
type Snapshot struct {
	Refreshes int64
	Signouts  int64
	Errors    int64
}

// Read returns a snapshot.
func (c *Counters) Read() Snapshot {
	return Snapshot{
		Refreshes: c.Refreshes.Load(),
		Signouts:  c.Signouts.Load(),
		Errors:    c.Errors.Load(),
	}
}
EOF

cat > internal/user/user.go <<'EOF'
package user

// User is an account.
type User struct {
	ID    string
	Email string
}
EOF

cat > internal/user/repo.go <<'EOF'
package user

import "sync"

// Repo is an in-memory user directory.
type Repo struct {
	mu    sync.RWMutex
	users map[string]User
}

// NewRepo returns a repo seeded with a few accounts.
func NewRepo() *Repo {
	r := &Repo{users: map[string]User{}}
	for _, u := range []User{
		{ID: "u1", Email: "alice@example.com"},
		{ID: "u2", Email: "bob@example.com"},
		{ID: "u3", Email: "carol@example.com"},
		{ID: "u4", Email: "dan@example.com"},
	} {
		r.users[u.ID] = u
	}
	return r
}

// Get looks up a user.
func (r *Repo) Get(id string) (User, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	u, ok := r.users[id]
	return u, ok
}

// IDs returns every user id.
func (r *Repo) IDs() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.users))
	for id := range r.users {
		out = append(out, id)
	}
	return out
}
EOF

cat > internal/db/iface.go <<'EOF'
package db

// Store is the persistence seam. Only the in-memory implementation exists.
type Store interface {
	Put(key string, value []byte) error
	Get(key string) ([]byte, bool)
	Delete(key string)
}
EOF

cat > internal/db/memory.go <<'EOF'
package db

import "sync"

// Memory is an in-memory Store.
type Memory struct {
	mu   sync.RWMutex
	data map[string][]byte
}

// NewMemory returns an empty store.
func NewMemory() *Memory { return &Memory{data: map[string][]byte{}} }

func (m *Memory) Put(key string, value []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[key] = value
	return nil
}

func (m *Memory) Get(key string) ([]byte, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	v, ok := m.data[key]
	return v, ok
}

func (m *Memory) Delete(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, key)
}
EOF

cat > internal/httpapi/router.go <<'EOF'
package httpapi

import (
	"net/http"

	"sessions-api/internal/logging"
	"sessions-api/internal/metrics"
	"sessions-api/internal/session"
	"sessions-api/internal/user"
)

// Server wires the handlers together.
type Server struct {
	Sessions *session.Store
	Users    *user.Repo
	Log      *logging.Logger
	Counters *metrics.Counters
}

// Routes returns the mux.
func (s *Server) Routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealth)
	mux.HandleFunc("/signin", s.handleSignIn)
	mux.HandleFunc("/refresh", s.handleRefresh)
	mux.HandleFunc("/whoami", s.handleWhoami)
	mux.HandleFunc("/stats", s.handleStats)
	return mux
}
EOF

cat > internal/httpapi/handlers.go <<'EOF'
package httpapi

import (
	"encoding/json"
	"net/http"
)

func (s *Server) handleSignIn(w http.ResponseWriter, r *http.Request) {
	uid := r.URL.Query().Get("user")
	if uid == "" {
		uid = "u1"
	}
	if _, ok := s.Users.Get(uid); !ok {
		http.Error(w, "unknown user", http.StatusNotFound)
		return
	}
	sess := s.Sessions.Create(uid)
	writeJSON(w, map[string]any{"token": sess.ID, "user": sess.UserID})
}

func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	s.Counters.Refreshes.Add(1)
	next, err := s.Sessions.Refresh(token)
	if err != nil {
		s.Counters.Signouts.Add(1)
		s.Log.Error("refresh failed for token %.8s: %v — the user will be signed out", token, err)
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	writeJSON(w, map[string]any{"token": next.ID, "user": next.UserID, "hits": next.Hits})
}

func (s *Server) handleWhoami(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	sess, err := s.Sessions.Get(token)
	if err != nil {
		s.Counters.Signouts.Add(1)
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	u, _ := s.Users.Get(sess.UserID)
	writeJSON(w, map[string]any{"user": u.ID, "email": u.Email})
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	snap := s.Counters.Read()
	writeJSON(w, map[string]any{
		"sessions":  s.Sessions.Count(),
		"users":     s.Sessions.UserCount(),
		"refreshes": snap.Refreshes,
		"signouts":  snap.Signouts,
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
EOF

cat > internal/httpapi/health.go <<'EOF'
package httpapi

import "net/http"

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}
EOF

cat > internal/httpapi/middleware.go <<'EOF'
package httpapi

import (
	"net/http"
	"time"
)

// WithLogging logs slow requests only, so routine traffic does not drown the log.
func (s *Server) WithLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		if d := time.Since(start); d > 200*time.Millisecond {
			s.Log.Warn("slow request %s %s took %s", r.Method, r.URL.Path, d)
		}
	})
}
EOF

cat > cmd/api/main.go <<'EOF'
package main

import (
	"fmt"
	"net/http"
	"time"

	"sessions-api/internal/config"
	"sessions-api/internal/httpapi"
	"sessions-api/internal/logging"
	"sessions-api/internal/metrics"
	"sessions-api/internal/session"
	"sessions-api/internal/user"
)

func main() {
	cfg := config.Load()
	log := logging.New()
	srv := &httpapi.Server{
		Sessions: session.NewStore(cfg.SessionTTL),
		Users:    user.NewRepo(),
		Log:      log,
		Counters: &metrics.Counters{},
	}

	// Every second, check the store for drift: there must never be more live
	// sessions than users with a session. More means a refresh forked one
	// session into two, and the extra will linger until it expires.
	go func() {
		for range time.Tick(time.Second) {
			if n, u := srv.Sessions.Count(), srv.Sessions.UserCount(); u > 0 && n > u {
				log.Warn("session drift: %d live sessions for %d users (%d orphaned)", n, u, n-u)
			}
		}
	}()

	log.Info("sessions-api listening on %s", cfg.Addr)
	fmt.Printf("listening on http://localhost%s\n", cfg.Addr)
	if err := http.ListenAndServe(cfg.Addr, srv.WithLogging(srv.Routes())); err != nil {
		log.Error("server stopped: %v", err)
	}
}
EOF

cat > cmd/loadtest/main.go <<'EOF'
// Command loadtest hammers a running sessions-api with concurrent refreshes,
// the way a browser with several tabs open would.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
)

func main() {
	base := flag.String("base", "http://localhost:8477", "API base URL")
	users := flag.Int("users", 4, "number of users")
	tabs := flag.Int("tabs", 6, "concurrent refreshes per user")
	rounds := flag.Int("rounds", 15, "refresh rounds")
	flag.Parse()

	var signouts, ok int
	var mu sync.Mutex

	for u := 1; u <= *users; u++ {
		token := signin(*base, fmt.Sprintf("u%d", u))
		if token == "" {
			fmt.Fprintf(os.Stderr, "could not sign in u%d\n", u)
			os.Exit(1)
		}
		for r := 0; r < *rounds; r++ {
			var wg sync.WaitGroup
			next := ""
			for t := 0; t < *tabs; t++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					n, err := refresh(*base, token)
					mu.Lock()
					defer mu.Unlock()
					if err != nil {
						signouts++
						return
					}
					ok++
					if next == "" {
						next = n
					}
				}()
			}
			wg.Wait()
			if next == "" {
				fmt.Fprintln(os.Stderr, "every tab was signed out; cannot continue")
				break
			}
			token = next
		}
	}

	fmt.Printf("refreshes ok=%d signed-out=%d\n", ok, signouts)
	if signouts > 0 {
		fmt.Println("FAIL: users were signed out during refresh")
		os.Exit(1)
	}
	fmt.Println("PASS: no spurious sign-outs")
}

func signin(base, user string) string {
	resp, err := http.Get(base + "/signin?user=" + user)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	var body struct{ Token string }
	b, _ := io.ReadAll(resp.Body)
	_ = json.Unmarshal(b, &body)
	return body.Token
}

func refresh(base, token string) (string, error) {
	resp, err := http.Get(base + "/refresh?token=" + token)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("status %d", resp.StatusCode)
	}
	var body struct{ Token string }
	b, _ := io.ReadAll(resp.Body)
	_ = json.Unmarshal(b, &body)
	return body.Token, nil
}
EOF

cat > docs/architecture.md <<'EOF'
# Architecture

    cmd/api          process entry point
    internal/httpapi HTTP surface
    internal/session token rotation and the session store
    internal/user    account directory
    internal/db      persistence seam (in-memory only)
    internal/metrics counters
    internal/logging stderr logger

Sessions live in memory, keyed by their current token. Refreshing rotates the
token.

Two invariants govern rotation, and they pull against each other:

**1. Rotation must not sign out the user's other tabs.** A browser with six tabs
open will refresh the same token six times at once. Only one of those can
receive the new token; the rest must not be thrown out. The previous token
therefore stays usable for a grace period (`session.RotationGrace`, 2s) and
resolves to the rotated session.

**2. One live session per signed-in user.** Those six concurrent refreshes must
converge on *one* successor, not six. `/stats` reports the session and user
counts, and the server warns once a second when they diverge — an extra session
is not visible to the user but lingers until it expires, and it means the
rotation forked.

Neither invariant is covered by the unit tests, which are single-threaded.
EOF

cat > docs/api.md <<'EOF'
# API

    GET /healthz              liveness
    GET /signin?user=u1       start a session, returns {token}
    GET /refresh?token=T      rotate the token, returns {token,hits}
    GET /whoami?token=T       returns the account
    GET /stats                sessions, users, refreshes, signouts
EOF

cat > scripts/smoke.sh <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
curl -fsS http://localhost:8477/healthz
EOF
chmod +x scripts/smoke.sh

git init -q
git add -A
git -c user.email=fixture@example.com -c user.name=Fixture commit -qm "sessions-api at v1.1.0"
