//go:build !windows

package cli

import (
	"os/exec"
	"syscall"
)

// detachRefreshProcess configures the refresh relay and worker spawns outside
// Windows: each starts in a session (and so a process group) of its own.
//
// Verified against Claude Code v2.1.325 on macOS: the statusline command runs
// as the leader of a new process group, and when Claude Code cancels it (an
// update superseding a statusline that is still running) or exits while it
// runs, it sends SIGTERM to that whole group, reaching a worker left in it even
// after the render had exited and the worker had been reparented to init. A
// command that has already exited is not signalled. Setsid also drops the
// controlling terminal, so closing the terminal a render was started from does
// not SIGHUP the worker. The descendant kill is handled by relayRefreshArgs.
func detachRefreshProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
