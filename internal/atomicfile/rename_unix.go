//go:build !windows

package atomicfile

import "os"

// Rename replaces newpath with oldpath. POSIX rename is atomic even when a
// reader has newpath open, so no retry is needed.
func Rename(oldpath, newpath string) error { return os.Rename(oldpath, newpath) }

func readFile(path string) ([]byte, error) { return os.ReadFile(path) }

func remove(path string) error { return os.Remove(path) }
