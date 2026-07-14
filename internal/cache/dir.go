// Package cache implements the shared, on-disk cache used to pass
// account-scoped and repository-scoped state between otherwise
// independent statusloom render invocations.
//
// The cache is deliberately simple: atomic JSON writes with
// last-writer-wins semantics and no locks or leases. There is no
// background worker that refreshes it; every render invocation both
// reads and (opportunistically) writes the cache for the data it has on
// hand.
package cache

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
)

// Dir returns the statusloom cache directory. It does not create the
// directory; writers create it lazily (0700) as needed.
//
// Resolution priority:
//  1. STATUSLOOM_CACHE_DIR environment variable, used verbatim.
//  2. $XDG_CACHE_HOME/statusloom.
//  3. Platform default:
//     - macOS and Linux: ~/.cache/statusloom (deliberately XDG-style on
//     macOS too, rather than ~/Library/Caches).
//     - Windows: %LocalAppData%\statusloom.
func Dir() (string, error) {
	if v := os.Getenv("STATUSLOOM_CACHE_DIR"); v != "" {
		return v, nil
	}

	if v := os.Getenv("XDG_CACHE_HOME"); v != "" {
		return filepath.Join(v, "statusloom"), nil
	}

	if runtime.GOOS == "windows" {
		if v := os.Getenv("LocalAppData"); v != "" {
			return filepath.Join(v, "statusloom"), nil
		}
		return "", errors.New("cache: LocalAppData is not set")
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".cache", "statusloom"), nil
}
