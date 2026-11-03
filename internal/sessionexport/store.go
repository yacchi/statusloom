// Package sessionexport writes per-process detail files of Claude Code statusline
// payloads and merges them into the public sessions.json (contract session-export-v1).
package sessionexport

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// PublicVersion is the "version" of <base>/sessions.json.
const PublicVersion = 1

// detailVersion is the "version" of a detail file (internal).
const detailVersion = 1

// PromptCache is the public promptCache object.
type PromptCache struct {
	TTL       string `json:"ttl,omitempty"`
	ExpiresAt *int64 `json:"expiresAt,omitempty"`
}

// Row is one element of the public "sessions" array. Field order = JSON key order of v1.
type Row struct {
	PID            int          `json:"pid"`
	StartedAt      int64        `json:"startedAt"`
	SessionID      string       `json:"sessionId"`
	TranscriptPath string       `json:"transcriptPath"`
	ConfigDir      string       `json:"configDir"`
	Cwd            string       `json:"cwd"`
	Model          string       `json:"model"`
	PromptCache    *PromptCache `json:"promptCache,omitempty"`
	SeenAt         int64        `json:"seenAt"`
	DetailPath     string       `json:"detailPath"`
}

// SameSummary reports whether r and o carry the same summary: every field EXCEPT SeenAt is
// equal. PromptCache is compared by value: nil vs nil equal; nil vs non-nil different; else
// TTL equal and ExpiresAt both nil or both non-nil with equal values.
func (r Row) SameSummary(o Row) bool {
	return r.PID == o.PID &&
		r.StartedAt == o.StartedAt &&
		r.SessionID == o.SessionID &&
		r.TranscriptPath == o.TranscriptPath &&
		r.ConfigDir == o.ConfigDir &&
		r.Cwd == o.Cwd &&
		r.Model == o.Model &&
		r.DetailPath == o.DetailPath &&
		storeSamePromptCache(r.PromptCache, o.PromptCache)
}

// storeSamePromptCache compares two promptCache objects by value.
func storeSamePromptCache(a, b *PromptCache) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	if a.TTL != b.TTL {
		return false
	}
	if (a.ExpiresAt == nil) != (b.ExpiresAt == nil) {
		return false
	}
	if a.ExpiresAt == nil {
		return true
	}
	return *a.ExpiresAt == *b.ExpiresAt
}

// Public is the whole public file.
type Public struct {
	Version  int   `json:"version"`
	MergedAt int64 `json:"mergedAt"`
	Sessions []Row `json:"sessions"` // never null when written: no rows -> []
}

// Detail is one per-process detail file. INTERNAL: its shape may change with detailVersion.
type Detail struct {
	Version   int             `json:"version"`   // detailVersion
	WrittenAt time.Time       `json:"writtenAt"` // the render clock (Observe's now) of the last write
	Row       Row             `json:"row"`       // the row exactly as it is to be published
	Payload   json.RawMessage `json:"payload"`   // the raw statusline stdin payload
}

// BaseDir returns filepath.Join($XDG_STATE_HOME, "statusloom") when XDG_STATE_HOME is non-empty
// AND absolute (filepath.IsAbs); otherwise filepath.Join(os.UserHomeDir(), ".local", "state",
// "statusloom") (UserHomeDir error -> returned). Same rule on every OS (no Windows branch).
// Reads the process environment (os.Getenv) like cache.Dir. No other override exists.
// Does not create anything.
func BaseDir() (string, error) {
	if v := os.Getenv("XDG_STATE_HOME"); v != "" && filepath.IsAbs(v) {
		return filepath.Join(v, "statusloom"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "state", "statusloom"), nil
}

// PublicPath returns <base>/sessions.json.
func PublicPath() (string, error) {
	base, err := BaseDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "sessions.json"), nil
}

// DetailDir returns <base>/sessions.
func DetailDir() (string, error) {
	base, err := BaseDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "sessions"), nil
}

// DetailPath returns <base>/sessions/<pid>-<startedAt>.json.
func DetailPath(pid int, startedAt int64) (string, error) {
	dir, err := DetailDir()
	if err != nil {
		return "", err
	}
	name := strconv.Itoa(pid) + "-" + strconv.FormatInt(startedAt, 10) + ".json"
	return filepath.Join(dir, name), nil
}

// pendingPath returns <base>/.merge-pending (internal marker).
func pendingPath() (string, error) {
	base, err := BaseDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, ".merge-pending"), nil
}

// ParseDetailName parses a detail file name. ok is true only when name is exactly
// strconv.Itoa(pid) + "-" + strconv.FormatInt(startedAt, 10) + ".json" for some pid >= 1 and
// startedAt >= 0: no sign, no leading zeros, no spaces, lower-case ".json", nothing before or
// after, values that fit their types. Otherwise (0, 0, false).
func ParseDetailName(name string) (pid int, startedAt int64, ok bool) {
	stem, ok := strings.CutSuffix(name, ".json")
	if !ok {
		return 0, 0, false
	}
	a, b, ok := strings.Cut(stem, "-")
	if !ok || !storeAllDigits(a) || !storeAllDigits(b) {
		return 0, 0, false
	}
	pid, err := strconv.Atoi(a)
	if err != nil || pid < 1 {
		return 0, 0, false
	}
	startedAt, err = strconv.ParseInt(b, 10, 64)
	if err != nil {
		return 0, 0, false
	}
	if strconv.Itoa(pid)+"-"+strconv.FormatInt(startedAt, 10)+".json" != name {
		return 0, 0, false
	}
	return pid, startedAt, true
}

// storeAllDigits reports whether s is non-empty and consists of ASCII digits only.
func storeAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
