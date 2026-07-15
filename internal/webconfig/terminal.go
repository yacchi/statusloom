package webconfig

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/coder/websocket"
	"github.com/creack/pty"
)

// maxTerminals bounds how many embedded terminals may run concurrently.
// Each terminal is a real interactive `claude` process, so this is kept
// small; requests beyond the limit get HTTP 429.
const maxTerminals = 2

// terminalReadBuffer is the size of the PTY->WebSocket copy buffer.
const terminalReadBuffer = 32 * 1024

// terminalKillGrace is how long close waits after SIGTERM before escalating
// to SIGKILL on the child process.
const terminalKillGrace = 2 * time.Second

// monitorLockName is the lockfile that enforces single-session use of the
// stable monitor workspace, and monitorLockTTL is how long a lock is honored
// before it is treated as stale (self-healing a lock whose holder crashed
// without releasing it). The TTL is generous because an interactive terminal
// may legitimately stay open for a long time.
const (
	monitorLockName = ".statusloom-monitor.lock"
	monitorLockTTL  = 12 * time.Hour
)

// monitorLock is the JSON payload of the workspace lockfile: the pid of the
// holding process and when it took the lock.
type monitorLock struct {
	PID       int       `json:"pid"`
	StartedAt time.Time `json:"startedAt"`
}

// processAlive reports whether a process with pid is currently running. It
// uses the POSIX "signal 0" probe (Signal(0) performs permission/existence
// checking without delivering a signal): a nil error means alive, EPERM means
// the process exists but is owned by another user (still alive), and anything
// else (typically ESRCH) means dead. This is a best-effort check targeting
// macOS/Linux; on platforms where it is unsupported it errs toward "dead",
// which only means a stale lock is reclaimed slightly more eagerly.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = p.Signal(syscall.Signal(0))
	if err == nil {
		return true
	}
	return errors.Is(err, syscall.EPERM)
}

// acquireMonitorLock attempts to take the single-session lock for the monitor
// workspace at dir. It returns (true, nil) when the lock was acquired,
// (false, nil) when a live holder currently owns it (the caller should refuse
// the new session), and (false, err) on an unexpected I/O error.
//
// It is modeled on cache.AcquireRefreshLease: create the lockfile with
// O_CREATE|O_EXCL; if it already exists, honor it only while its holder is
// alive and the lock is within monitorLockTTL, otherwise rename the stale lock
// aside and retry. A lock we cannot parse is treated as stale.
func acquireMonitorLock(dir string, now time.Time) (bool, error) {
	p := filepath.Join(dir, monitorLockName)
	b, err := json.Marshal(monitorLock{PID: os.Getpid(), StartedAt: now})
	if err != nil {
		return false, err
	}
	b = append(b, '\n')

	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err == nil {
		_, werr := f.Write(b)
		cerr := f.Close()
		if werr != nil {
			_ = os.Remove(p)
			return false, werr
		}
		return true, cerr
	}
	if !errors.Is(err, os.ErrExist) {
		return false, err
	}

	// A lock already exists: keep it only if its holder is alive and it is
	// still within the TTL. Otherwise reclaim it.
	if raw, rerr := os.ReadFile(p); rerr == nil {
		var old monitorLock
		if json.Unmarshal(raw, &old) == nil &&
			now.Sub(old.StartedAt) < monitorLockTTL &&
			processAlive(old.PID) {
			return false, nil
		}
	}

	stale := p + ".stale"
	if os.Rename(p, stale) != nil {
		// Lost a race to reclaim it; treat as held.
		return false, nil
	}
	_ = os.Remove(stale)
	return acquireMonitorLock(dir, now)
}

// releaseMonitorLock removes the workspace lockfile at dir, but only if it is
// still held by pid (last-writer-wins on the pid check, so we never delete a
// lock another process has since taken over).
func releaseMonitorLock(dir string, pid int) {
	p := filepath.Join(dir, monitorLockName)
	raw, err := os.ReadFile(p)
	if err != nil {
		return
	}
	var l monitorLock
	if json.Unmarshal(raw, &l) == nil && l.PID == pid {
		_ = os.Remove(p)
	}
}

// readMonitorLock reads and parses the workspace lockfile at dir. ok is false
// when the file is absent or unparseable.
func readMonitorLock(dir string) (l monitorLock, ok bool) {
	raw, err := os.ReadFile(filepath.Join(dir, monitorLockName))
	if err != nil {
		return monitorLock{}, false
	}
	if json.Unmarshal(raw, &l) != nil {
		return monitorLock{}, false
	}
	return l, true
}

// forceReleaseMonitorLock removes the workspace lockfile unconditionally,
// bypassing releaseMonitorLock's pid guard. It is used only to reclaim a lock
// this server has determined is orphaned: held by our own pid after we have
// torn down every embedded session this server was running, so no live claude
// process remains behind it.
func forceReleaseMonitorLock(dir string) {
	_ = os.Remove(filepath.Join(dir, monitorLockName))
}

// acquireWorkspaceLock takes the single-session workspace lock. The caller
// (handleTerminalSession) has already torn down any embedded session THIS
// server was running, so a lock still held by our own pid at this point is an
// orphan a previous session failed to release (e.g. its close() was skipped by
// an unclean exit) and is reclaimed. A lock held by another live process is a
// genuine cross-process conflict (a second config server or `statusloom
// monitor`) and is refused. A dead holder is reclaimed by acquireMonitorLock
// itself.
func (s *server) acquireWorkspaceLock(dir string) (bool, error) {
	locked, err := acquireMonitorLock(dir, time.Now())
	if err != nil || locked {
		return locked, err
	}
	if l, ok := readMonitorLock(dir); ok && l.PID != os.Getpid() {
		return false, nil // another live process legitimately holds it
	}
	forceReleaseMonitorLock(dir)
	return acquireMonitorLock(dir, time.Now())
}

// spawnTerminalCmd builds the command run inside an embedded terminal. It is
// a package var so tests can substitute a fake PTY process (e.g. `cat`)
// instead of depending on a real `claude` binary, which CI does not have.
//
// SECURITY: this is the one place statusloom opens a strong, interactive
// shell-like mouth - it launches a fully interactive `claude` bound to a
// PTY that a browser can read from and write to. It is reachable only over
// the loopback-bound config server, gated by the bearer token on
// POST /api/terminal/session and the query-token on GET /ws/terminal, and
// only ever runs in a throwaway working directory. Do not widen its reach.
var spawnTerminalCmd = func(dir string) *exec.Cmd {
	cmd := exec.Command("claude")
	cmd.Dir = dir
	cmd.Env = os.Environ() // inherit PATH and auth
	return cmd
}

// terminalSession is one live embedded terminal: the child process, its PTY
// master file, and the stable monitor workspace it runs in. locked records
// whether this session currently holds the workspace's single-session lock,
// so close releases it exactly once.
type terminalSession struct {
	id           string
	cmd          *exec.Cmd
	pty          *os.File
	workspaceDir string
	locked       bool

	srv       *server
	closeOnce sync.Once
}

// close tears the terminal down exactly once: it deregisters the session,
// terminates the child (SIGTERM, then SIGKILL after a grace period), closes
// the PTY, and releases the workspace lock if this session holds it. It does
// NOT remove workspaceDir: that is the stable, reused monitor workspace, which
// must persist across sessions so Claude Code's folder-trust prompt stays a
// one-time thing. Safe to call from multiple goroutines and multiple times.
func (ts *terminalSession) close() {
	ts.closeOnce.Do(func() {
		ts.srv.unregisterTerminal(ts.id)

		if ts.cmd.Process != nil {
			done := make(chan struct{})
			go func() {
				_, _ = ts.cmd.Process.Wait()
				close(done)
			}()
			_ = ts.cmd.Process.Signal(syscall.SIGTERM)
			select {
			case <-done:
			case <-time.After(terminalKillGrace):
				_ = ts.cmd.Process.Kill()
				<-done
			}
		}

		_ = ts.pty.Close()
		if ts.locked {
			releaseMonitorLock(ts.workspaceDir, os.Getpid())
		}
	})
}

// registerTerminal atomically enforces the concurrency limit and stores ts.
// It returns false (and does not store) when maxTerminals are already live.
func (s *server) registerTerminal(ts *terminalSession) bool {
	s.liveMu.Lock()
	defer s.liveMu.Unlock()
	if len(s.terminals) >= maxTerminals {
		return false
	}
	if s.terminals == nil {
		s.terminals = make(map[string]*terminalSession)
	}
	s.terminals[ts.id] = ts
	return true
}

// unregisterTerminal removes id from the registry (idempotent).
func (s *server) unregisterTerminal(id string) {
	s.liveMu.Lock()
	defer s.liveMu.Unlock()
	delete(s.terminals, id)
}

// lookupTerminal returns the session for id, or nil.
func (s *server) lookupTerminal(id string) *terminalSession {
	s.liveMu.Lock()
	defer s.liveMu.Unlock()
	return s.terminals[id]
}

// terminalCount returns the number of live terminals.
func (s *server) terminalCount() int {
	s.liveMu.Lock()
	defer s.liveMu.Unlock()
	return len(s.terminals)
}

// closeAllTerminals tears down every live terminal. Called from Serve's
// shutdown path. It snapshots the registry under the lock, then closes each
// session without holding it (close reacquires the lock to deregister).
func (s *server) closeAllTerminals() {
	s.liveMu.Lock()
	list := make([]*terminalSession, 0, len(s.terminals))
	for _, ts := range s.terminals {
		list = append(list, ts)
	}
	s.liveMu.Unlock()
	for _, ts := range list {
		ts.close()
	}
}

// terminalSessionResponse is the body of POST /api/terminal/session.
type terminalSessionResponse struct {
	TerminalID string `json:"terminalId"`
}

// handleTerminalSession handles POST /api/terminal/session: it provisions the
// stable monitor workspace (reusing provisionMonitorDir, so the spawned
// session's statusLine emits to /api/live and live preview updates flow
// automatically) and launches an interactive `claude` bound to a PTY in that
// directory. The terminal is registered under a random id returned to the
// caller, which the browser then connects to over GET /ws/terminal. Bearer
// auth applies (path under /api/).
//
// Because the workspace is a single shared directory, a per-workspace lockfile
// guards it: at most one embedded-terminal/monitor session may use it at a
// time. If a live session already holds the lock the request is refused with
// 409 Conflict; a stale lock (dead holder or past TTL) is reclaimed.
//
// SECURITY: see spawnTerminalCmd - this opens a real interactive claude.
func (s *server) handleTerminalSession(w http.ResponseWriter, r *http.Request) {
	// Serialize session creation: two rapid requests must not both take over
	// and spawn a claude at the same time.
	s.spawnMu.Lock()
	defer s.spawnMu.Unlock()

	// Take over: tear down any embedded session this server is already running
	// (killing its claude and releasing the workspace lock) so a "Restart"
	// always yields a fresh session instead of colliding with a stale,
	// abandoned, or still-live previous one. This is what makes restart robust
	// against a session whose lock was stranded by an unclean teardown.
	// Cross-process holders are NOT touched here; acquireWorkspaceLock refuses
	// them below.
	s.closeAllTerminals()

	dir, err := s.provisionMonitorDir()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Single-session guard: acquire the workspace lock before spawning claude.
	locked, err := s.acquireWorkspaceLock(dir)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to lock workspace: "+err.Error())
		return
	}
	if !locked {
		writeError(w, http.StatusConflict, "a monitor/terminal session is already running for this workspace")
		return
	}
	// From here we own the lock and must release it on every failure path.

	cmd := spawnTerminalCmd(dir)
	ptyFile, err := pty.Start(cmd)
	if err != nil {
		releaseMonitorLock(dir, os.Getpid())
		writeError(w, http.StatusInternalServerError, "failed to start terminal: "+err.Error())
		return
	}

	id, err := generateToken()
	if err != nil {
		_ = ptyFile.Close()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
		releaseMonitorLock(dir, os.Getpid())
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	ts := &terminalSession{id: id, cmd: cmd, pty: ptyFile, workspaceDir: dir, locked: true, srv: s}
	if !s.registerTerminal(ts) {
		// Lost a race for the last slot; tear this one down (which releases the
		// lock) and reject.
		ts.close()
		writeError(w, http.StatusTooManyRequests, "too many terminals")
		return
	}

	writeJSON(w, http.StatusOK, terminalSessionResponse{TerminalID: id})
}

// terminalResizeMessage is the browser->server text-frame control message.
type terminalResizeMessage struct {
	Type string `json:"type"`
	Cols int    `json:"cols"`
	Rows int    `json:"rows"`
}

// handleTerminalWS handles GET /ws/terminal, the byte pipe between a browser
// terminal emulator and the embedded claude PTY. Host and Origin are
// validated by withSecurity (the /ws/ path is not under /api/, so bearer auth
// is NOT applied); this handler authenticates via the ?token= query parameter
// (constant-time) and resolves the terminal via ?id=.
//
// Frame protocol (fixed contract with the frontend):
//   - server->browser: Binary frame = raw PTY output bytes.
//   - browser->server: Binary frame = raw bytes written to the PTY (stdin).
//   - browser->server: Text frame  = JSON {"type":"resize","cols":N,"rows":N}
//     -> pty.Setsize.
func (s *server) handleTerminalWS(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if subtle.ConstantTimeCompare([]byte(token), []byte(s.token)) != 1 {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	ts := s.lookupTerminal(r.URL.Query().Get("id"))
	if ts == nil {
		writeError(w, http.StatusNotFound, "unknown terminal")
		return
	}

	// Connecting and any I/O counts as activity; keep the server alive while
	// a terminal is attached.
	s.touch()

	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		// Origin already validated by withSecurity (see handleLiveWS).
		InsecureSkipVerify: true,
	})
	if err != nil {
		return
	}
	ctx := r.Context()

	// PTY -> WS: stream raw output as binary frames. On PTY EOF/error (the
	// child exited or was killed), tear the terminal down, which also unblocks
	// the read loop below.
	go func() {
		buf := make([]byte, terminalReadBuffer)
		for {
			n, rerr := ts.pty.Read(buf)
			if n > 0 {
				s.touch()
				writeCtx, cancel := context.WithTimeout(ctx, wsWriteTimeout)
				werr := c.Write(writeCtx, websocket.MessageBinary, buf[:n])
				cancel()
				if werr != nil {
					break
				}
			}
			if rerr != nil {
				break
			}
		}
		ts.close()
		_ = c.Close(websocket.StatusNormalClosure, "")
	}()

	// WS -> PTY: binary frames are stdin bytes; text frames are control
	// messages (resize). Loop until the client disconnects or errors.
	for {
		typ, data, rerr := c.Read(ctx)
		if rerr != nil {
			break
		}
		s.touch()
		switch typ {
		case websocket.MessageBinary:
			if _, werr := ts.pty.Write(data); werr != nil {
				goto done
			}
		case websocket.MessageText:
			var msg terminalResizeMessage
			if err := json.Unmarshal(data, &msg); err == nil && msg.Type == "resize" {
				_ = pty.Setsize(ts.pty, &pty.Winsize{
					Cols: uint16(msg.Cols),
					Rows: uint16(msg.Rows),
				})
			}
		}
	}
done:
	// Client side ended: kill the child, close the PTY (unblocks the reader
	// goroutine), release the workspace lock, and force-close the socket. The
	// stable workspace dir itself is left in place.
	ts.close()
	_ = c.CloseNow()
}
