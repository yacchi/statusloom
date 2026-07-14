package webconfig

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
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
// master file, and the throwaway working directory it runs in.
type terminalSession struct {
	id     string
	cmd    *exec.Cmd
	pty    *os.File
	tmpDir string

	srv       *server
	closeOnce sync.Once
}

// close tears the terminal down exactly once: it deregisters the session,
// terminates the child (SIGTERM, then SIGKILL after a grace period), closes
// the PTY, and removes the throwaway working directory. Safe to call from
// multiple goroutines and multiple times.
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
		_ = os.RemoveAll(ts.tmpDir)
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

// handleTerminalSession handles POST /api/terminal/session: it provisions a
// throwaway monitor working directory (reusing provisionMonitorDir, so the
// spawned session's statusLine emits to /api/live and live preview updates
// flow automatically) and launches an interactive `claude` bound to a PTY in
// that directory. The terminal is registered under a random id returned to
// the caller, which the browser then connects to over GET /ws/terminal.
// Bearer auth applies (path under /api/).
//
// SECURITY: see spawnTerminalCmd - this opens a real interactive claude.
func (s *server) handleTerminalSession(w http.ResponseWriter, r *http.Request) {
	// Cheap pre-check for the common overflow case (registerTerminal is the
	// authoritative, race-free gate below).
	if s.terminalCount() >= maxTerminals {
		writeError(w, http.StatusTooManyRequests, "too many terminals")
		return
	}

	tmp, err := s.provisionMonitorDir()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	cmd := spawnTerminalCmd(tmp)
	ptyFile, err := pty.Start(cmd)
	if err != nil {
		_ = os.RemoveAll(tmp) // also tracked in tmpDirs; double removal is harmless
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
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	ts := &terminalSession{id: id, cmd: cmd, pty: ptyFile, tmpDir: tmp, srv: s}
	if !s.registerTerminal(ts) {
		// Lost a race for the last slot; tear this one down and reject.
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
	// goroutine), remove the temp dir, and force-close the socket.
	ts.close()
	_ = c.CloseNow()
}
