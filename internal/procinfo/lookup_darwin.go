//go:build darwin

package procinfo

import (
	"fmt"
	"syscall"
	"unsafe"
)

func init() {
	platformLookup = sysctlLookup
}

// sysctlLookup reads kern.proc.pid.<pid> through the raw SYS___SYSCTL call. The name-based
// syscall.Sysctl("kern.proc.pid.N") fails with ENOENT, so the MIB is passed directly.
func sysctlLookup(pid int) (Info, error) {
	mib := [4]int32{1 /*CTL_KERN*/, 14 /*KERN_PROC*/, 1 /*KERN_PROC_PID*/, int32(pid)}
	var buf [kinfoSize]byte
	n := uintptr(kinfoSize)
	_, _, errno := syscall.Syscall6(syscall.SYS___SYSCTL,
		uintptr(unsafe.Pointer(&mib[0])), 4,
		uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&n)),
		0, 0)
	if errno == syscall.ESRCH {
		return Info{}, ErrNotFound
	}
	if errno != 0 {
		return Info{}, fmt.Errorf("procinfo: sysctl kern.proc.pid.%d: %w", pid, errno)
	}
	return kinfoParse(pid, buf[:n])
}
