// Package atomicfile is the single implementation of statusloom's
// sibling-temp-file + rename write discipline, so concurrent readers (for
// example simultaneous statusline renders) see either the previous complete
// file or the new complete file, never a partial one.
package atomicfile

import (
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
)

// tmpSeq disambiguates temp file names for concurrent writes to the same path
// within one process; separate processes are told apart by the pid.
var tmpSeq uint64

// Write atomically replaces the file at path with data. It writes a sibling
// temp file (<path>.tmp.<pid>.<seq>, 0600, O_EXCL), optionally fsyncs it, and
// renames it over path. The parent directory is created (0700) if missing. The
// temp file is removed on failure.
func Write(path string, data []byte, doSync bool) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := fmt.Sprintf("%s.tmp.%d.%d", path, os.Getpid(), atomic.AddUint64(&tmpSeq, 1))
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	// Best-effort cleanup; no-op once the rename below has succeeded.
	defer os.Remove(tmp)
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if doSync {
		if err := f.Sync(); err != nil {
			f.Close()
			return err
		}
	}
	if err := f.Close(); err != nil {
		return err
	}
	return Rename(tmp, path)
}

// ReadFile is os.ReadFile for files that are replaced with Write. On Windows
// an open that races a concurrent replace can fail transiently with a sharing
// violation or access denied, so it is retried for a short, bounded time; on
// other platforms it is exactly os.ReadFile.
func ReadFile(path string) ([]byte, error) { return readFile(path) }

// Remove is os.Remove for files that other processes read concurrently (the
// refresh lease, cache entries). On Windows deleting a file that a reader has
// open fails transiently with a sharing violation or access denied, so it is
// retried for a short, bounded time; on other platforms it is exactly
// os.Remove.
func Remove(path string) error { return remove(path) }
