//go:build windows

package cli

import (
	"os/exec"
	"syscall"
)

// createNoWindow is CREATE_NO_WINDOW (not exported by package syscall).
const createNoWindow = 0x08000000

// detachRefreshProcess configures the refresh worker spawn on Windows. The
// worker runs git (a console program) for every due cwd, so it must never
// pop up or flash a console window: CREATE_NO_WINDOW gives it a windowless
// console that its git children inherit. CREATE_NEW_PROCESS_GROUP keeps a
// Ctrl+C in the terminal that launched the render from reaching it.
//
// No job-object breakaway is needed: verified against Claude Code v2.1.325 on
// Windows, the statusline command (run through Git Bash) and everything it
// starts are in no job. What does reach the worker is Claude Code's
// cancellation of a superseded or timed-out statusline, `taskkill /PID <shell>
// /T /F`, which walks the parent-pid tree; relayRefreshArgs handles that.
func detachRefreshProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP | createNoWindow}
}
