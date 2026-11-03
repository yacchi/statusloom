// Package webconfig implements the local configuration web server ("the
// configurator"): a short-lived HTTP server, bound to 127.0.0.1 only, that
// serves a React frontend (embedded as static assets) and a JSON API for
// viewing, editing, and previewing statusloom configuration.
//
// See statusloom-local-development-plan.md sections 14-16 for the design
// this package implements.
package webconfig

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"sync"
	"time"
)

// defaultIdleTimeout is used when Options.IdleTimeout is zero.
const defaultIdleTimeout = 30 * time.Minute

// shutdownGrace bounds how long Serve waits for in-flight requests to
// finish once a shutdown has been triggered.
const shutdownGrace = 5 * time.Second

// Options configures Serve.
type Options struct {
	// Port to bind on 127.0.0.1. Zero means a random free port.
	Port int
	// OpenBrowser, when true, attempts to open the configurator URL in
	// the user's default browser. Failure to do so is silently ignored.
	OpenBrowser bool
	// Stdout receives the single startup line announcing the
	// configurator URL. Required for the URL to be discoverable; if nil,
	// the line is discarded.
	Stdout io.Writer
	// IdleTimeout is how long the server waits without an /api/* request
	// before shutting down. Zero means 30 minutes.
	IdleTimeout time.Duration
}

// Serve runs the configurator server until shutdown (via POST
// /api/shutdown, the idle timeout, or ctx cancellation). It returns nil on
// clean shutdown, or an error if the server could not be started.
func Serve(ctx context.Context, opts Options) error {
	if opts.IdleTimeout <= 0 {
		opts.IdleTimeout = defaultIdleTimeout
	}
	stdout := opts.Stdout
	if stdout == nil {
		stdout = io.Discard
	}

	token, err := generateToken()
	if err != nil {
		return fmt.Errorf("webconfig: generating token: %w", err)
	}

	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", opts.Port))
	if err != nil {
		return fmt.Errorf("webconfig: listen: %w", err)
	}

	port := ln.Addr().(*net.TCPAddr).Port

	s := newServer(token, port, opts.IdleTimeout)

	// Best-effort: remove legacy random monitor temp dirs left under the OS
	// temp dir by older statusloom versions, so they don't accumulate. The
	// current monitor workspace is a stable, reused dir under the cache dir and
	// is intentionally never swept or removed.
	sweepStaleMonitorDirs(os.TempDir(), time.Now())

	// Kill any live embedded-terminal processes (which also releases the
	// monitor-workspace lock each holds), regardless of which shutdown path
	// Serve returns through. The stable monitor workspace dir itself persists.
	defer s.closeAllTerminals()

	unused := newUnusedConns()
	httpServer := &http.Server{Handler: s.routes(), ConnState: unused.track}

	url := fmt.Sprintf("http://127.0.0.1:%d/#token=%s", port, token)
	fmt.Fprintf(stdout, "Statusloom configurator: %s\n", url)

	if opts.OpenBrowser {
		go openBrowser(url)
	}

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- httpServer.Serve(ln)
	}()

	select {
	case <-s.done:
	case <-ctx.Done():
	case err := <-serveErr:
		// The listener died on its own (e.g. accept error); nothing left
		// to shut down.
		if err != nil && err != http.ErrServerClosed {
			return err
		}
		return nil
	}

	s.stopIdleTimer()

	// Close connections that have not sent a request yet before draining:
	// Shutdown would otherwise wait out shutdownGrace for them (see
	// unusedConns).
	unused.closeAll()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		_ = httpServer.Close()
	}

	if err := <-serveErr; err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

// unusedConns tracks connections that have been accepted but have not sent
// their first request yet (http.StateNew). Such a connection has no work in
// flight, but net/http's Server.Shutdown treats it as active for 5 seconds
// (golang/go#22682), so a client that opens a socket and sends nothing (a
// browser preconnect, or a client transport's unused dial) would otherwise
// hold Shutdown for its whole grace period. Serve closes these explicitly
// before draining.
type unusedConns struct {
	mu     sync.Mutex
	conns  map[net.Conn]struct{}
	closed bool
}

func newUnusedConns() *unusedConns {
	return &unusedConns{conns: make(map[net.Conn]struct{})}
}

// track is an http.Server.ConnState hook.
func (u *unusedConns) track(c net.Conn, st http.ConnState) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if st != http.StateNew {
		delete(u.conns, c)
		return
	}
	if u.closed {
		_ = c.Close()
		return
	}
	u.conns[c] = struct{}{}
}

// closeAll closes every connection still waiting for its first request, and
// closes any connection accepted from now on as soon as it appears (the
// listener is closed by Shutdown afterwards).
func (u *unusedConns) closeAll() {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.closed = true
	for c := range u.conns {
		_ = c.Close()
	}
	clear(u.conns)
}

// server holds the configurator's runtime state: the bearer token, the
// bound port (needed for Origin validation), and the idle-shutdown timer.
type server struct {
	token       string
	port        int
	idleTimeout time.Duration

	mu           sync.Mutex
	idleTimer    *time.Timer
	shutdownOnce sync.Once
	done         chan struct{}

	// cleanupOnce guards the one-shot ~/.claude.json janitor
	// (cleanupDeadMonitorEntries), run lazily the first time a monitor
	// workspace is provisioned.
	cleanupOnce sync.Once

	// liveMu guards the live-preview state below: the set of connected
	// WebSocket subscribers and the live embedded-terminal sessions.
	liveMu      sync.Mutex
	subscribers map[*liveSubscriber]struct{}
	terminals   map[string]*terminalSession

	// spawnMu serializes embedded-terminal session creation so two rapid
	// requests cannot both take over and spawn a claude concurrently.
	spawnMu sync.Mutex
}

func newServer(token string, port int, idleTimeout time.Duration) *server {
	s := &server{
		token:       token,
		port:        port,
		idleTimeout: idleTimeout,
		done:        make(chan struct{}),
	}
	s.idleTimer = time.AfterFunc(idleTimeout, s.triggerShutdown)
	return s
}

// touch resets the idle timer; called on every /api/* request.
func (s *server) touch() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.idleTimer != nil {
		s.idleTimer.Reset(s.idleTimeout)
	}
}

// stopIdleTimer stops the idle timer so it cannot fire after shutdown has
// already begun for another reason.
func (s *server) stopIdleTimer() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.idleTimer != nil {
		s.idleTimer.Stop()
	}
}

// triggerShutdown requests a graceful shutdown; safe to call multiple
// times (from the idle timer and from the /api/shutdown handler).
func (s *server) triggerShutdown() {
	s.shutdownOnce.Do(func() {
		close(s.done)
	})
}

// generateToken returns 16 random bytes from crypto/rand, hex-encoded
// (lowercase, 32 chars).
func generateToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
