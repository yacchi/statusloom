//go:build !windows

package cli

import (
	"os/exec"
	"syscall"
	"testing"
)

// TestDetachRefreshProcessOwnProcessGroup checks that a worker started with
// detachRefreshProcess leads a process group (and session) of its own, so the
// SIGTERM Claude Code sends to the statusline command's process group on
// cancel and on exit (observed on macOS) cannot reach it.
func TestDetachRefreshProcessOwnProcessGroup(t *testing.T) {
	cmd := exec.Command("sleep", "5")
	detachRefreshProcess(cmd)
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot start sleep: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	pid := cmd.Process.Pid
	pgid, err := syscall.Getpgid(pid)
	if err != nil {
		t.Fatal(err)
	}
	if pgid != pid {
		t.Fatalf("worker pgid = %d, want its own pid %d (a new process group)", pgid, pid)
	}
	if pgid == syscall.Getpgrp() {
		t.Fatalf("worker shares the spawner's process group %d", pgid)
	}
}
