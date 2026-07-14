package webconfig

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/coder/websocket"

	"github.com/yacchi/statusloom/internal/adapters/claude"
	"github.com/yacchi/statusloom/internal/cache"
)

// staleMonitorDirTTL bounds how old an orphaned monitor temp dir must be
// before sweepStaleMonitorDirs will remove it. Graceful shutdown already
// reclaims these immediately via cleanupTmpDirs; this sweep only exists to
// catch directories leaked by a crash or SIGKILL (which skip that path).
// The threshold is deliberately long so that a still-open, long-running
// monitor workspace from another (or the same) instance is never mistaken
// for garbage just because a new configurator process happens to start
// while it's in use.
const staleMonitorDirTTL = 24 * time.Hour

// sweepStaleMonitorDirs best-effort removes monitor temp directories left
// behind under tmpRoot by a previous, uncleanly-terminated configurator
// process. Every step (Glob, Stat, RemoveAll) is best-effort: errors are
// ignored and the sweep simply continues, since a leaked directory here is
// a minor annoyance, not a correctness problem.
func sweepStaleMonitorDirs(tmpRoot string, now time.Time) {
	matches, err := filepath.Glob(filepath.Join(tmpRoot, "statusloom-monitor-*"))
	if err != nil {
		return
	}
	for _, path := range matches {
		info, err := os.Stat(path)
		if err != nil || !info.IsDir() {
			continue
		}
		if now.Sub(info.ModTime()) > staleMonitorDirTTL {
			_ = os.RemoveAll(path)
		}
	}
}

// liveSubscriberBuffer bounds how many pending broadcast messages a single
// WebSocket subscriber may queue before the server starts dropping updates
// for that (slow) subscriber. Live updates are advisory "something changed"
// notifications, so dropping an intermediate one is harmless - the client
// re-fetches the latest snapshot on the next message it does receive.
const liveSubscriberBuffer = 16

// wsWriteTimeout bounds a single WebSocket write, so a stuck client cannot
// wedge a broadcast goroutine indefinitely.
const wsWriteTimeout = 5 * time.Second

// liveSubscriber is one connected WebSocket client. ch carries JSON-encoded
// liveUpdate messages from broadcast to the connection's write loop.
type liveSubscriber struct {
	ch chan []byte
}

// liveUpdate is the small text-JSON message broadcast to every WebSocket
// subscriber when a monitored session posts fresh data. It carries only the
// session id and observation time; the client re-fetches the full snapshot
// (GET /api/sessions, POST /api/dsl/preview) as needed.
type liveUpdate struct {
	Type       string `json:"type"`
	SessionID  string `json:"sessionId"`
	ObservedAt string `json:"observedAt"` // RFC3339
}

// addSubscriber registers sub to receive future broadcasts.
func (s *server) addSubscriber(sub *liveSubscriber) {
	s.liveMu.Lock()
	defer s.liveMu.Unlock()
	if s.subscribers == nil {
		s.subscribers = make(map[*liveSubscriber]struct{})
	}
	s.subscribers[sub] = struct{}{}
}

// removeSubscriber unregisters sub (idempotent).
func (s *server) removeSubscriber(sub *liveSubscriber) {
	s.liveMu.Lock()
	defer s.liveMu.Unlock()
	delete(s.subscribers, sub)
}

// broadcast delivers msg to every subscriber without blocking: a subscriber
// whose buffer is full simply misses this message (see liveSubscriberBuffer).
func (s *server) broadcast(msg []byte) {
	s.liveMu.Lock()
	defer s.liveMu.Unlock()
	for sub := range s.subscribers {
		select {
		case sub.ch <- msg:
		default:
		}
	}
}

// recordTmpDir remembers dir so it can be removed on shutdown.
func (s *server) recordTmpDir(dir string) {
	s.liveMu.Lock()
	defer s.liveMu.Unlock()
	s.tmpDirs = append(s.tmpDirs, dir)
}

// cleanupTmpDirs best-effort removes every temp directory created for a
// monitor session. Called from Serve's shutdown path.
func (s *server) cleanupTmpDirs() {
	s.liveMu.Lock()
	dirs := s.tmpDirs
	s.tmpDirs = nil
	s.liveMu.Unlock()
	for _, d := range dirs {
		_ = os.RemoveAll(d)
	}
}

// handleLive handles POST /api/live: the per-render payload forwarded by
// `statusloom monitor`. It decodes the payload into a snapshot (400 on
// failure), persists it to the session snapshot cache (so the UI's real-data
// preview sees it authoritatively), and broadcasts a live-update to every
// connected WebSocket subscriber. Bearer auth is applied by withSecurity
// because the path is under /api/.
func (s *server) handleLive(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxConfigBodyBytes)
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	snap, err := claude.New().Decode(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	now := time.Now()
	// Store authoritatively (monitor also stores, but the server storing here
	// keeps the cache warm even if a future emitter does not). A snapshot with
	// no session id cannot be keyed, so it is only broadcast, not stored.
	if snap.Session.ID != "" {
		_ = cache.StoreSnapshot(snap.Session.ID, snap, now)
	}

	msg, err := json.Marshal(liveUpdate{
		Type:       "live-update",
		SessionID:  snap.Session.ID,
		ObservedAt: now.Format(time.RFC3339),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.broadcast(msg)

	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleLiveWS handles GET /ws/live, the WebSocket channel that streams
// live-update notifications to the browser. Host and Origin are validated by
// withSecurity (the /ws/ path is not under /api/, so bearer auth is NOT
// applied there); this handler instead authenticates via the ?token= query
// parameter using a constant-time comparison, rejecting with 401 before the
// upgrade. Origin verification is left entirely to withSecurity's validOrigin
// (hence InsecureSkipVerify below), avoiding a second, differently-configured
// origin check inside coder/websocket.
func (s *server) handleLiveWS(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if subtle.ConstantTimeCompare([]byte(token), []byte(s.token)) != 1 {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	// Connecting counts as activity; keep the server alive while watched.
	s.touch()

	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		// Origin is already validated by withSecurity (validOrigin: scheme
		// http, host 127.0.0.1/localhost, port == server port). Skipping
		// coder/websocket's own check avoids a redundant, differently-scoped
		// verification.
		InsecureSkipVerify: true,
	})
	if err != nil {
		return
	}
	defer c.CloseNow()

	ctx := r.Context()

	sub := &liveSubscriber{ch: make(chan []byte, liveSubscriberBuffer)}
	s.addSubscriber(sub)
	defer s.removeSubscriber(sub)

	// Reader goroutine: drains incoming frames (processing pings/close) and
	// resets the idle timer on any client activity. It exits when the
	// connection closes or ctx is cancelled, which unblocks the write loop
	// below via ctx / a failed write.
	go func() {
		for {
			if _, _, err := c.Read(ctx); err != nil {
				return
			}
			s.touch()
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-sub.ch:
			writeCtx, cancel := context.WithTimeout(ctx, wsWriteTimeout)
			err := c.Write(writeCtx, websocket.MessageText, msg)
			cancel()
			if err != nil {
				return
			}
		}
	}
}

// liveSessionResponse is the body of POST /api/live/session.
type liveSessionResponse struct {
	LaunchCommand string `json:"launchCommand"`
	TmpDir        string `json:"tmpDir"`
}

// provisionMonitorDir creates a throwaway working directory set up as a
// statusline-customization workspace:
//
//   - .claude/settings.local.json whose statusLine runs `statusloom monitor
//     --draft`, so a session here renders the web configurator's unsaved
//     draft and emits to /api/live (live preview flows with no extra wiring);
//   - CLAUDE.md explaining the draft edit loop and the config schema;
//   - sample.json, a representative payload for `statusloom claude < sample.json`;
//   - a git repo with one initial commit (best-effort), so the statusline's
//     git widgets have something to show.
//
// The dir is recorded for removal on shutdown (even if a later step fails, so
// nothing is leaked) and its path is returned. Shared by the live-session
// endpoint (which hands the dir to the user) and the embedded-terminal
// endpoint (which spawns claude in it).
func (s *server) provisionMonitorDir() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	exe, err = filepath.Abs(exe)
	if err != nil {
		return "", err
	}

	tmp, err := os.MkdirTemp("", "statusloom-monitor-*")
	if err != nil {
		return "", err
	}
	// Record the dir even on later failure so shutdown still cleans it up.
	s.recordTmpDir(tmp)

	claudeDir := filepath.Join(tmp, ".claude")
	if err := os.MkdirAll(claudeDir, 0o700); err != nil {
		return tmp, err
	}

	// --draft: the workspace session renders the shared draft, so edits made
	// via `statusloom draft push` / the web UI preview live here.
	command := fmt.Sprintf(
		"%s monitor --emit-url http://127.0.0.1:%d/api/live --token %s --draft",
		exe, s.port, s.token,
	)
	settings := map[string]any{
		"statusLine": map[string]any{
			"type":    "command",
			"command": command,
		},
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return tmp, err
	}
	data = append(data, '\n')
	if err := os.WriteFile(filepath.Join(claudeDir, "settings.local.json"), data, 0o600); err != nil {
		return tmp, err
	}

	if err := os.WriteFile(filepath.Join(tmp, "CLAUDE.md"), []byte(workspaceClaudeMD), 0o600); err != nil {
		return tmp, err
	}
	if err := os.WriteFile(filepath.Join(tmp, "sample.json"), []byte(workspaceSampleJSON), 0o600); err != nil {
		return tmp, err
	}

	// Best-effort git init + initial commit. Failure (git absent, sandboxed,
	// etc.) is ignored: the workspace still works, the git widgets just stay
	// empty.
	gitInitWorkspace(tmp)

	return tmp, nil
}

// handleLiveSession handles POST /api/live/session: it provisions a throwaway
// working directory (see provisionMonitorDir) and tells the UI how to launch
// a Claude Code session in it. The temp dir is recorded for removal on
// shutdown. Bearer auth applies (path under /api/).
func (s *server) handleLiveSession(w http.ResponseWriter, r *http.Request) {
	tmp, err := s.provisionMonitorDir()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, liveSessionResponse{
		LaunchCommand: fmt.Sprintf("cd %s && claude", tmp),
		TmpDir:        tmp,
	})
}
