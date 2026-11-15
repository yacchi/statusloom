//go:build windows

package webconfig

import "os"

// terminalSupported is false on Windows: creack/pty has no ConPTY backend
// (pty.Start returns ErrUnsupported), so the embedded terminal is unavailable
// and POST /api/terminal/session answers 501 with terminalUnsupportedMessage.
const terminalSupported = false

// terminateProcess ends p. Windows cannot deliver SIGTERM to an arbitrary
// process, so it kills immediately.
func terminateProcess(p *os.Process) {
	_ = p.Kill()
}
