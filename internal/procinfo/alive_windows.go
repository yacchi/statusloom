//go:build windows

package procinfo

import (
	"errors"
	"syscall"
)

// processQueryLimitedInformation is PROCESS_QUERY_LIMITED_INFORMATION, the
// least-privileged access right that still permits GetExitCodeProcess.
const processQueryLimitedInformation = 0x1000

// stillActive is STILL_ACTIVE, the exit code GetExitCodeProcess reports for a
// process that has not exited yet.
const stillActive = 259

// errorInvalidParameter is ERROR_INVALID_PARAMETER, OpenProcess's answer for
// a pid that names no process (not exported by package syscall).
const errorInvalidParameter = syscall.Errno(87)

// alive opens the process with the minimal query right and checks that it has
// not exited (Windows has no signal-0 probe). Only two answers prove the
// process is gone: OpenProcess failing with ERROR_INVALID_PARAMETER (no such
// pid), and an exit code other than STILL_ACTIVE (the process object outlives
// the process while someone holds a handle to it). ERROR_ACCESS_DENIED (a more
// privileged process) and every other failure are "cannot tell", reported as
// alive. A process that exited with code 259 reads as alive, which errs the
// safe way.
func alive(pid int) bool {
	if int64(pid) > int64(^uint32(0)) {
		return false // no Windows pid is this large
	}
	h, err := syscall.OpenProcess(processQueryLimitedInformation, false, uint32(pid))
	if err != nil {
		return !errors.Is(err, errorInvalidParameter)
	}
	defer syscall.CloseHandle(h)
	var code uint32
	if err := syscall.GetExitCodeProcess(h, &code); err != nil {
		return true
	}
	return code == stillActive
}
