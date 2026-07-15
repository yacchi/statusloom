package webconfig

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// oldMonitorDirPrefix is the base-name prefix of the historical random
// temp-dir monitor workspaces (created by older statusloom versions with
// os.MkdirTemp("", "statusloom-monitor-*"), e.g.
// /private/var/folders/.../T/statusloom-monitor-1233030367). The current
// stable workspace is named "monitor-workspace" and deliberately does NOT
// match this prefix, so the janitor can never touch it.
const oldMonitorDirPrefix = "statusloom-monitor-"

// claudeConfigPath resolves ~/.claude.json. It honors $HOME first (so tests
// can redirect it) and falls back to os.UserHomeDir. It returns "" when the
// home directory cannot be determined.
func claudeConfigPath() string {
	if h := os.Getenv("HOME"); h != "" {
		return filepath.Join(h, ".claude.json")
	}
	h, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(h, ".claude.json")
}

// cleanupDeadMonitorEntries is a best-effort janitor that removes DEAD project
// entries from ~/.claude.json that were leaked by older statusloom versions,
// which used random per-launch temp-dir workspaces. It only ever DELETES dead
// entries (base name starting with statusloom-monitor-, directory no longer on
// disk); it never adds or modifies any entry, and never writes trust flags -
// granting folder trust is the user's action alone.
//
// Everything is best-effort: a missing/unreadable file, a parse error, or a
// write failure is silently ignored, so the janitor can never break
// provisioning or the server. A concurrently running `claude` may also write
// this file; we accept last-writer-wins, which is harmless because we only
// remove dead entries.
func cleanupDeadMonitorEntries() {
	path := claudeConfigPath()
	if path == "" {
		return
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return
	}
	out, changed, err := pruneDeadMonitorEntries(raw, func(p string) bool {
		_, statErr := os.Stat(p)
		return statErr == nil
	})
	if err != nil || !changed {
		return
	}

	// Atomic write: temp file in the same dir + rename, so a reader never sees
	// a half-written config.
	tmp, err := os.CreateTemp(filepath.Dir(path), ".claude.json.statusloom-*")
	if err != nil {
		return
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(out); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
	}
}

// pruneDeadMonitorEntries is the pure core of the janitor, factored out so it
// can be tested without touching the real home directory. It parses raw as a
// generic JSON object, walks the top-level "projects" object, and deletes any
// key whose base name begins with oldMonitorDirPrefix AND for which
// exists(key) reports false (the directory is gone). It never touches the
// stable "monitor-workspace" entry (which does not match the prefix), live
// entries, or any unrelated key, and never adds or modifies values.
//
// When nothing is removed it returns (raw, false, nil), so the caller leaves
// the file byte-for-byte untouched. When something is removed it returns the
// re-marshaled document with changed=true.
//
// NOTE: round-tripping through encoding/json reorders object keys (Go maps are
// unordered), so a rewrite reformats and reorders the file. Because
// ~/.claude.json is a machine-managed config this is tolerable. Numbers are
// decoded with UseNumber so integer values are preserved exactly (no float
// precision loss and no reformatting of large integers).
func pruneDeadMonitorEntries(raw []byte, exists func(string) bool) (out []byte, changed bool, err error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var doc map[string]any
	if err := dec.Decode(&doc); err != nil {
		return nil, false, err
	}

	projects, ok := doc["projects"].(map[string]any)
	if !ok {
		return raw, false, nil
	}

	for key := range projects {
		if !strings.HasPrefix(filepath.Base(key), oldMonitorDirPrefix) {
			continue
		}
		if exists(key) {
			continue
		}
		delete(projects, key)
		changed = true
	}

	if !changed {
		return raw, false, nil
	}

	out, err = json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, false, err
	}
	out = append(out, '\n')
	return out, true, nil
}
