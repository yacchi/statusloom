package cache

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeSettings writes body to a settings.json in a fresh temp dir and returns its path.
func writeSettings(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func isolateCacheDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("STATUSLOOM_CACHE_DIR", dir)
	return dir
}

func TestClaudeRefreshIntervalExamples(t *testing.T) {
	isolateCacheDir(t)
	cases := []struct {
		name string
		body string
		want time.Duration
		ok   bool
	}{
		{"integer", `{"statusLine":{"refreshInterval":30}}`, 30 * time.Second, true},
		{"fractional", `{"statusLine":{"refreshInterval":1.5}}`, 1500 * time.Millisecond, true},
		{"lower boundary", `{"statusLine":{"refreshInterval":1}}`, time.Second, true},
		{"below boundary", `{"statusLine":{"refreshInterval":0.99}}`, 0, false},
		{"half second", `{"statusLine":{"refreshInterval":0.5}}`, 0, false},
		{"zero", `{"statusLine":{"refreshInterval":0}}`, 0, false},
		{"negative", `{"statusLine":{"refreshInterval":-3}}`, 0, false},
		{"string", `{"statusLine":{"refreshInterval":"30"}}`, 0, false},
		{"null", `{"statusLine":{"refreshInterval":null}}`, 0, false},
		{"absent key", `{"statusLine":{}}`, 0, false},
		{"statusLine not object", `{"statusLine":"x"}`, 0, false},
		{"top-level array", `[]`, 0, false},
		{"not json", `not json`, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ClaudeRefreshInterval(writeSettings(t, tc.body))
			if ok != tc.ok || got != tc.want {
				t.Fatalf("ClaudeRefreshInterval = (%v, %v), want (%v, %v)", got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestClaudeRefreshIntervalMissingOrUnreadable(t *testing.T) {
	cacheDir := isolateCacheDir(t)

	if got, ok := ClaudeRefreshInterval(""); ok || got != 0 {
		t.Fatalf(`ClaudeRefreshInterval("") = (%v, %v), want (0, false)`, got, ok)
	}

	missing := filepath.Join(t.TempDir(), "missing.json")
	if got, ok := ClaudeRefreshInterval(missing); ok || got != 0 {
		t.Fatalf("missing file = (%v, %v), want (0, false)", got, ok)
	}
	if _, err := os.Stat(filepath.Join(cacheDir, "refresh", "claude-settings.json")); err == nil {
		t.Fatal("a missing settings file must not create a memo")
	}

	// A directory passes os.Stat but cannot be read: still unknown, never a panic.
	if got, ok := ClaudeRefreshInterval(t.TempDir()); ok || got != 0 {
		t.Fatalf("unreadable path = (%v, %v), want (0, false)", got, ok)
	}
}

func TestClaudeRefreshIntervalReparsesEditedFile(t *testing.T) {
	isolateCacheDir(t)
	p := writeSettings(t, `{"statusLine":{"refreshInterval":30}}`)
	if got, ok := ClaudeRefreshInterval(p); !ok || got != 30*time.Second {
		t.Fatalf("first call = (%v, %v), want (30s, true)", got, ok)
	}

	// Different size and a different mtime: the memo must not be served.
	if err := os.WriteFile(p, []byte(`{"statusLine":{"refreshInterval":60}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(p, later, later); err != nil {
		t.Fatal(err)
	}
	if got, ok := ClaudeRefreshInterval(p); !ok || got != 60*time.Second {
		t.Fatalf("after edit = (%v, %v), want (60s, true)", got, ok)
	}
}

func TestClaudeRefreshIntervalServesMemoWithoutReparsing(t *testing.T) {
	cacheDir := isolateCacheDir(t)
	p := writeSettings(t, `{"statusLine":{"refreshInterval":30}}`)
	if _, ok := ClaudeRefreshInterval(p); !ok {
		t.Fatal("first call should be known")
	}

	// Tamper with the memo's cached value while keeping path/size/modTime: a hit
	// proves the settings file was not parsed again.
	memoPath := filepath.Join(cacheDir, "refresh", "claude-settings.json")
	var memo claudeSettingsMemo
	if ok, err := ReadJSON(memoPath, &memo); err != nil || !ok {
		t.Fatalf("memo not written: ok=%v err=%v", ok, err)
	}
	memo.Seconds = 99
	if err := WriteJSONAtomic(memoPath, memo); err != nil {
		t.Fatal(err)
	}

	if got, ok := ClaudeRefreshInterval(p); !ok || got != 99*time.Second {
		t.Fatalf("memo hit = (%v, %v), want (99s, true)", got, ok)
	}
}

func TestClaudeRefreshIntervalCorruptMemoFallsBackAndIsRewritten(t *testing.T) {
	cacheDir := isolateCacheDir(t)
	p := writeSettings(t, `{"statusLine":{"refreshInterval":45}}`)

	memoDir := filepath.Join(cacheDir, "refresh")
	if err := os.MkdirAll(memoDir, 0o700); err != nil {
		t.Fatal(err)
	}
	memoPath := filepath.Join(memoDir, "claude-settings.json")
	if err := os.WriteFile(memoPath, []byte("garbage"), 0o600); err != nil {
		t.Fatal(err)
	}

	if got, ok := ClaudeRefreshInterval(p); !ok || got != 45*time.Second {
		t.Fatalf("corrupt memo = (%v, %v), want (45s, true)", got, ok)
	}
	var memo claudeSettingsMemo
	if ok, err := ReadJSON(memoPath, &memo); err != nil || !ok || memo.Seconds != 45 || !memo.Known {
		t.Fatalf("memo not rewritten valid: ok=%v err=%v memo=%+v", ok, err, memo)
	}
}

func TestClaudeRefreshIntervalFractionalThroughMemo(t *testing.T) {
	isolateCacheDir(t)
	p := writeSettings(t, `{"statusLine":{"refreshInterval":1.5}}`)
	for i := range 2 {
		if got, ok := ClaudeRefreshInterval(p); !ok || got != 1500*time.Millisecond {
			t.Fatalf("call %d = (%v, %v), want (1.5s, true)", i, got, ok)
		}
	}
}
