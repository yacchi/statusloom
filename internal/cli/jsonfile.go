package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/yacchi/statusloom/internal/atomicfile"
)

// timeNow is a package-level indirection over time.Now so tests can pin the
// timestamp used for backup-file names (backupFile below, and setup's backup).
var timeNow = time.Now

func marshalDocument(v any) ([]byte, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// writeJSONAtomic is command-local because cache.WriteJSONAtomic intentionally
// omits the trailing newline required for user-edited configuration documents.
func writeJSONAtomic(path string, v any) error {
	b, err := marshalDocument(v)
	if err != nil {
		return err
	}
	return atomicfile.Write(path, b, true)
}

func backupFile(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	backup := fmt.Sprintf("%s.bak.%d", path, timeNow().Unix())
	if err := os.WriteFile(backup, b, 0o600); err != nil {
		return "", err
	}
	return backup, nil
}
