//go:build !windows

package webconfig

import (
	"os/exec"
	"testing"
	"time"

	"github.com/creack/pty"
)

// TestTerminalCloseDoesNotWedgeOnUndrainedPTY reproduces the Save & Close hang:
// a child that ignores SIGTERM and floods its tty while nobody reads the PTY
// master. close() must still return, because it keeps the master drained while
// it escalates to SIGKILL and bounds the wait for the child.
func TestTerminalCloseDoesNotWedgeOnUndrainedPTY(t *testing.T) {
	cmd := exec.Command("sh", "-c", "trap '' TERM; exec yes")
	ptmx, err := pty.Start(cmd)
	if err != nil {
		t.Fatalf("pty.Start: %v", err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill() })

	// Let the child fill the PTY buffer so it is blocked on write.
	time.Sleep(300 * time.Millisecond)

	s := newServer("test-token", 0, time.Hour)
	t.Cleanup(s.stopIdleTimer)
	ts := &terminalSession{id: "close-test", cmd: cmd, pty: ptmx, srv: s, locked: false}

	done := make(chan struct{})
	go func() {
		ts.close()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(3*terminalKillGrace + 2*time.Second):
		t.Fatal("close() did not return: PTY teardown is wedged on an undrained master")
	}
}
