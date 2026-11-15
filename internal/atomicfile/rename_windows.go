//go:build windows

package atomicfile

import (
	"errors"
	"os"
	"syscall"
	"time"
)

const (
	errorAccessDenied     syscall.Errno = 5
	errorSharingViolation syscall.Errno = 32
	errorLockViolation    syscall.Errno = 33
)

// Rename replaces newpath with oldpath. On Windows, renaming over a file that
// another process or goroutine currently has open fails transiently with
// access denied / sharing violation, so the rename is retried for a short,
// bounded time (about 1s) before giving up.
func Rename(oldpath, newpath string) error {
	var err error
	delay := time.Millisecond
	for i := 0; i < 40; i++ {
		if err = os.Rename(oldpath, newpath); err == nil || !transient(err) {
			return err
		}
		time.Sleep(delay)
		if delay < 50*time.Millisecond {
			delay *= 2
		}
	}
	return err
}

func transient(err error) bool {
	var errno syscall.Errno
	if !errors.As(err, &errno) {
		return false
	}
	return errno == errorAccessDenied || errno == errorSharingViolation || errno == errorLockViolation
}

func readFile(path string) ([]byte, error) {
	var (
		data []byte
		err  error
	)
	delay := time.Millisecond
	for i := 0; i < 40; i++ {
		if data, err = os.ReadFile(path); err == nil || !transient(err) {
			return data, err
		}
		time.Sleep(delay)
		if delay < 50*time.Millisecond {
			delay *= 2
		}
	}
	return data, err
}

func remove(path string) error {
	var err error
	delay := time.Millisecond
	for i := 0; i < 40; i++ {
		if err = os.Remove(path); err == nil || !transient(err) {
			return err
		}
		time.Sleep(delay)
		if delay < 50*time.Millisecond {
			delay *= 2
		}
	}
	return err
}
