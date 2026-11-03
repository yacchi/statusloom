// Package procinfo reads a process's parent pid, kernel start time and
// executable name without running any command (no ps). It uses the Go
// standard library only. Platform readers register themselves through
// platformLookup from init(); a platform without a reader gets
// ErrUnsupported from Lookup.
package procinfo

import "errors"

// Info describes one process.
type Info struct {
	PID       int
	PPID      int
	StartedAt int64  // process start time, Unix epoch SECONDS, truncated (never rounded)
	Comm      string // kernel executable name without directory, cut at the first NUL
}

// ErrNotFound: the process does not exist, or it is a zombie (exited, not yet reaped).
var ErrNotFound = errors.New("procinfo: no such process")

// ErrUnsupported: this platform has no reader (every GOOS except darwin and linux).
var ErrUnsupported = errors.New("procinfo: unsupported platform")

// platformLookup is the platform reader. It stays nil unless a platform file's init()
// sets it (lookup_darwin.go, lookup_linux.go). Lookup calls it only for pid > 0.
var platformLookup func(pid int) (Info, error)

// Lookup returns Info for pid:
//
//	pid <= 0                       -> ErrNotFound (checked first, on every platform)
//	no platform reader registered  -> ErrUnsupported
//	otherwise                      -> the platform reader's result, unchanged.
//
// Any failure other than "gone" (permission, unexpected format, short read) is a non-nil
// error that is neither ErrNotFound nor ErrUnsupported, so callers can tell "gone" from
// "cannot tell". Callers test errors with errors.Is.
func Lookup(pid int) (Info, error) {
	if pid <= 0 {
		return Info{}, ErrNotFound
	}
	if platformLookup == nil {
		return Info{}, ErrUnsupported
	}
	return platformLookup(pid)
}
