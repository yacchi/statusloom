package cache

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// claudeSettingsMemoSchemaVersion versions the memo record below. A memo with a
// different version is ignored and rebuilt from the settings file.
const claudeSettingsMemoSchemaVersion = 1

// claudeSettingsMemo caches the result of parsing Claude Code's settings file.
// The render path runs once per update, so re-parsing settings.json every time
// is avoided by keying the memo on the file's path, size and modification time.
type claudeSettingsMemo struct {
	SchemaVersion int     `json:"schemaVersion"`
	Path          string  `json:"path"`
	Size          int64   `json:"size"`
	ModTime       string  `json:"modTime"`
	Known         bool    `json:"known"`
	Seconds       float64 `json:"seconds"`
}

// ClaudeRefreshInterval returns Claude Code's statusLine.refreshInterval (seconds) from
// the settings file at settingsPath (callers pass claudecfg.SettingsPath(getenv)).
// ok=false means unknown: settingsPath is "", the file is missing / unreadable / not a JSON
// object, or statusLine.refreshInterval is absent, not a number, or < 1. A fractional
// value is kept (time.Duration(v * float64(time.Second))).
//
// It is cheap enough for every render: one os.Stat of settingsPath, then the parsed
// result is reused from the memo file <cache>/refresh/claude-settings.json when that memo
// records the same path, size and modification time (nanoseconds). On a mismatch the
// settings file is parsed and the memo rewritten (WriteJSONAtomic). Memo read/write
// failures are ignored (the function then just parses). A missing settings file is not
// memoized.
func ClaudeRefreshInterval(settingsPath string) (time.Duration, bool) {
	if settingsPath == "" {
		return 0, false
	}
	fi, err := os.Stat(settingsPath)
	if err != nil {
		return 0, false
	}

	memoPath := ""
	if dir, err := refreshDir(); err == nil {
		memoPath = filepath.Join(dir, "claude-settings.json")
	}

	if memoPath != "" {
		var memo claudeSettingsMemo
		if ok, err := ReadJSON(memoPath, &memo); err == nil && ok &&
			memo.SchemaVersion == claudeSettingsMemoSchemaVersion &&
			memo.Path == settingsPath &&
			memo.Size == fi.Size() {
			if mt, err := time.Parse(time.RFC3339Nano, memo.ModTime); err == nil && mt.Equal(fi.ModTime()) {
				return memoInterval(memo)
			}
		}
	}

	data, err := os.ReadFile(settingsPath)
	if err != nil {
		return 0, false
	}
	var settings map[string]any
	if json.Unmarshal(data, &settings) != nil {
		return 0, false
	}

	seconds, known := parseRefreshInterval(settings)

	if memoPath != "" {
		// The memo is advisory: a failed write just means the next render parses again.
		_ = WriteJSONAtomic(memoPath, claudeSettingsMemo{
			SchemaVersion: claudeSettingsMemoSchemaVersion,
			Path:          settingsPath,
			Size:          fi.Size(),
			ModTime:       fi.ModTime().UTC().Format(time.RFC3339Nano),
			Known:         known,
			Seconds:       seconds,
		})
	}

	if !known {
		return 0, false
	}
	return time.Duration(seconds * float64(time.Second)), true
}

// parseRefreshInterval extracts statusLine.refreshInterval from a decoded settings
// object. Only numbers of at least one second count as known.
func parseRefreshInterval(settings map[string]any) (float64, bool) {
	statusLine, ok := settings["statusLine"].(map[string]any)
	if !ok {
		return 0, false
	}
	n, ok := statusLine["refreshInterval"].(float64)
	if !ok || n < 1 {
		return 0, false
	}
	return n, true
}

func memoInterval(memo claudeSettingsMemo) (time.Duration, bool) {
	if !memo.Known {
		return 0, false
	}
	return time.Duration(memo.Seconds * float64(time.Second)), true
}
