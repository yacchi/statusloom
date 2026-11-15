//go:build !windows

package webconfig

import (
	"os"
	"syscall"
)

// terminalSupported reports whether the embedded terminal (a claude process
// bound to a PTY via creack/pty) is available on this platform.
const terminalSupported = true

// terminateProcess asks p to exit gracefully (SIGTERM). The caller escalates
// to Kill after terminalKillGrace if the process is still running.
func terminateProcess(p *os.Process) {
	_ = p.Signal(syscall.SIGTERM)
}
