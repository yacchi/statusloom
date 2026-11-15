package store

// WriteFileAtomic is the sibling-temp-file + fsync + rename write discipline,
// ported from internal/config so store owns its own atomic writer and no
// store -> config dependency edge is created (plans/config-store-and-format.md
// §8.1/§8.11). It is exported because `statusloom fmt` also needs to write an
// arbitrary DSL file in place with the same crash-safety guarantees.

import "github.com/yacchi/statusloom/internal/atomicfile"

// WriteFileAtomic atomically writes data to path (sibling temp file, fsync,
// rename; see atomicfile.Write), so a concurrent reader never observes a
// partial file. The parent directory is created (0700) if missing.
func WriteFileAtomic(path string, data []byte) error {
	return atomicfile.Write(path, data, true)
}
