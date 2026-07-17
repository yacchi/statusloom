package store

// WriteFileAtomic is the sibling-temp-file + fsync + rename write discipline,
// ported from internal/config so store owns its own atomic writer and no
// store -> config dependency edge is created (plans/config-store-and-format.md
// §8.1/§8.11). It is exported because `statusloom fmt` also needs to write an
// arbitrary DSL file in place with the same crash-safety guarantees.

import (
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
)

var tempSequence uint64

// WriteFileAtomic atomically writes data to path using a sibling temp file
// (same directory), fsync, then rename, so a concurrent reader never observes
// a partial file. The parent directory is created (0700) if missing.
func WriteFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp := fmt.Sprintf("%s.tmp.%d.%d", path, os.Getpid(), atomic.AddUint64(&tempSequence, 1))
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer os.Remove(tmp)
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
