package cache

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
)

// tmpSeq disambiguates temp file names for multiple concurrent writes to
// the same path from within a single process (e.g. concurrent tests, or a
// future in-process caller). Real statusloom render invocations are
// separate OS processes, so os.Getpid() alone is normally sufficient;
// the counter is a cheap extra safety net.
var tmpSeq uint64

// WriteJSONAtomic marshals v as indented JSON and atomically replaces the
// file at path. It writes to a sibling temp file (<path>.tmp.<pid>.<seq>)
// in the same directory, fsyncs it, closes it, and renames it over path,
// so that concurrent readers never observe a partially written file -
// they see either the previous complete file or the new complete file.
//
// The parent directory is created (0700) if it does not already exist.
func WriteJSONAtomic(path string, v any) error {
	return writeJSONAtomic(path, v, true)
}

// WriteJSONAtomicNoSync is WriteJSONAtomic without the fsync: the same indented JSON
// (json.MarshalIndent(v, "", "  ")), the same MkdirAll(dir, 0o700), the same sibling temp file
// <path>.tmp.<pid>.<seq> opened O_WRONLY|O_CREATE|O_EXCL 0o600, the same best-effort removal of
// the temp file on failure and the same rename over path — but the temp file is NOT synced
// before the rename. Concurrent readers still see either the previous or the new complete file;
// only durability across a crash or power loss is given up. Meant for render-path state that is
// rewritten constantly and is cheap to lose.
func WriteJSONAtomicNoSync(path string, v any) error {
	return writeJSONAtomic(path, v, false)
}

// writeJSONAtomic holds the write logic shared by WriteJSONAtomic and
// WriteJSONAtomicNoSync. When doSync is true the temp file is fsynced before
// the rename.
func writeJSONAtomic(path string, v any, doSync bool) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}

	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}

	seq := atomic.AddUint64(&tmpSeq, 1)
	tmp := fmt.Sprintf("%s.tmp.%d.%d", path, os.Getpid(), seq)

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

	return os.Rename(tmp, path)
}

// ReadJSON unmarshals the JSON file at path into out. A missing file is
// not an error: it returns (false, nil) and leaves out untouched.
func ReadJSON(path string, out any) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	if err := json.Unmarshal(data, out); err != nil {
		return false, err
	}
	return true, nil
}
