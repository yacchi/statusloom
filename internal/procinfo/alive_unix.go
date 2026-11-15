//go:build unix

package procinfo

import (
	"errors"
	"syscall"
)

// alive is the POSIX "signal 0" probe: kill(pid, 0) checks existence and
// permission without delivering a signal. Only ESRCH proves the process is
// gone; nil (ours) and EPERM (someone else's) mean it exists, and any other
// error is "cannot tell", reported as alive. A zombie (exited, not yet reaped)
// still answers signal 0 and so reads as alive, which errs the safe way.
//
// It calls syscall.Kill directly instead of os.FindProcess(pid).Signal:
// os.Process.Signal rewrites ESRCH to os.ErrProcessDone (and on Linux may go
// through a pidfd), so an ESRCH check on its error never matches and every
// dead pid would read as alive.
func alive(pid int) bool {
	err := syscall.Kill(pid, syscall.Signal(0))
	return err == nil || !errors.Is(err, syscall.ESRCH)
}
