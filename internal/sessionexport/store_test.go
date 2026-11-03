package sessionexport

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
)

// storeIsolateState points XDG_STATE_HOME at a temp dir and returns it. Every test that resolves
// the state dir calls this so nothing touches the developer's real ~/.local/state.
func storeIsolateState(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)
	return dir
}

// storeAssertNotCreated fails when the statusloom directory exists: path helpers must not create.
func storeAssertNotCreated(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected %s not to exist, stat err = %v", path, err)
	}
}

func storeInt64(v int64) *int64 { return &v }

func TestStoreBaseDirAbsoluteXDG(t *testing.T) {
	xdg := storeIsolateState(t)
	got, err := BaseDir()
	if err != nil {
		t.Fatalf("BaseDir: %v", err)
	}
	want := filepath.Join(xdg, "statusloom")
	if got != want {
		t.Fatalf("BaseDir = %q, want %q", got, want)
	}
	storeAssertNotCreated(t, got)
}

func TestStoreHelperPaths(t *testing.T) {
	xdg := storeIsolateState(t)
	base := filepath.Join(xdg, "statusloom")

	public, err := PublicPath()
	if err != nil || public != filepath.Join(base, "sessions.json") {
		t.Fatalf("PublicPath = %q, %v", public, err)
	}
	dir, err := DetailDir()
	if err != nil || dir != filepath.Join(base, "sessions") {
		t.Fatalf("DetailDir = %q, %v", dir, err)
	}
	detail, err := DetailPath(4242, 1793672985)
	if err != nil || detail != filepath.Join(base, "sessions", "4242-1793672985.json") {
		t.Fatalf("DetailPath = %q, %v", detail, err)
	}
	pending, err := pendingPath()
	if err != nil || pending != filepath.Join(base, ".merge-pending") {
		t.Fatalf("pendingPath = %q, %v", pending, err)
	}
	storeAssertNotCreated(t, base)
}

func TestStoreBaseDirRelativeXDGFallsBackToHome(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("HOME fallback rule is unix-specific in this test")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", "state")

	got, err := BaseDir()
	if err != nil {
		t.Fatalf("BaseDir: %v", err)
	}
	want := filepath.Join(home, ".local", "state", "statusloom")
	if got != want {
		t.Fatalf("BaseDir = %q, want %q", got, want)
	}
	storeAssertNotCreated(t, got)
}

func TestStoreBaseDirEmptyXDGUsesHome(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("HOME fallback rule is unix-specific in this test")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", "")

	got, err := BaseDir()
	if err != nil {
		t.Fatalf("BaseDir: %v", err)
	}
	if want := filepath.Join(home, ".local", "state", "statusloom"); got != want {
		t.Fatalf("BaseDir = %q, want %q", got, want)
	}
}

func TestStoreNoHomeReturnsError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("UserHomeDir without HOME is unix-specific in this test")
	}
	t.Setenv("HOME", "")
	t.Setenv("XDG_STATE_HOME", "")

	if got, err := BaseDir(); err == nil || got != "" {
		t.Fatalf("BaseDir = %q, %v; want \"\" and an error", got, err)
	}
	if got, err := PublicPath(); err == nil || got != "" {
		t.Fatalf("PublicPath = %q, %v; want \"\" and an error", got, err)
	}
	if got, err := DetailDir(); err == nil || got != "" {
		t.Fatalf("DetailDir = %q, %v; want \"\" and an error", got, err)
	}
	if got, err := DetailPath(1, 2); err == nil || got != "" {
		t.Fatalf("DetailPath = %q, %v; want \"\" and an error", got, err)
	}
	if got, err := pendingPath(); err == nil || got != "" {
		t.Fatalf("pendingPath = %q, %v; want \"\" and an error", got, err)
	}
}

func TestStoreParseDetailNameAccepts(t *testing.T) {
	cases := []struct {
		name      string
		pid       int
		startedAt int64
	}{
		{"4242-1793672985.json", 4242, 1793672985},
		{"1-0.json", 1, 0},
	}
	if strconv.IntSize == 64 {
		cases = append(cases, struct {
			name      string
			pid       int
			startedAt int64
		}{"9223372036854775807-9223372036854775807.json", 9223372036854775807, 9223372036854775807})
	}
	for _, c := range cases {
		pid, startedAt, ok := ParseDetailName(c.name)
		if !ok || pid != c.pid || startedAt != c.startedAt {
			t.Errorf("ParseDetailName(%q) = (%d, %d, %v), want (%d, %d, true)",
				c.name, pid, startedAt, ok, c.pid, c.startedAt)
		}
	}
}

func TestStoreParseDetailNameRejects(t *testing.T) {
	rejects := []string{
		"0-5.json",
		"01-5.json",
		"1-05.json",
		"+1-2.json",
		"-1-2.json",
		"a-1.json",
		"1-2.JSON",
		"1-2.json.tmp.3.4",
		".1-2.json",
		"1-2.json~",
		"1-2-3.json",
		"1 -2.json",
		"1-.json",
		"-.json",
		".json",
		"",
		"1-9223372036854775808.json",
		"99999999999999999999-1.json",
	}
	for _, name := range rejects {
		pid, startedAt, ok := ParseDetailName(name)
		if ok || pid != 0 || startedAt != 0 {
			t.Errorf("ParseDetailName(%q) = (%d, %d, %v), want (0, 0, false)", name, pid, startedAt, ok)
		}
	}
}

func TestStoreRowSameSummaryIgnoresSeenAt(t *testing.T) {
	a := Row{PID: 1, StartedAt: 2, SeenAt: 100}
	b := a
	b.SeenAt = 999
	if !a.SameSummary(b) {
		t.Fatal("rows differing only in SeenAt must have the same summary")
	}
}

func TestStoreRowSameSummaryDetectsChanges(t *testing.T) {
	base := Row{
		PID:            1,
		StartedAt:      2,
		SessionID:      "s",
		TranscriptPath: "/t.jsonl",
		ConfigDir:      "/c",
		Cwd:            "/w",
		Model:          "m",
		DetailPath:     "/d.json",
		PromptCache:    &PromptCache{TTL: "1h", ExpiresAt: storeInt64(10)},
	}
	mutations := map[string]func(r *Row){
		"DetailPath": func(r *Row) { r.DetailPath = "/other.json" },
		"ConfigDir":  func(r *Row) { r.ConfigDir = "/other" },
		"TTL":        func(r *Row) { r.PromptCache.TTL = "5m" },
		"ExpiresAt":  func(r *Row) { r.PromptCache.ExpiresAt = storeInt64(11) },
		"ExpiresAtToNil": func(r *Row) {
			r.PromptCache.ExpiresAt = nil
		},
		"PromptCacheToNil": func(r *Row) { r.PromptCache = nil },
	}
	for name, mutate := range mutations {
		other := base
		pc := *base.PromptCache
		other.PromptCache = &pc
		mutate(&other)
		if base.SameSummary(other) {
			t.Errorf("%s change not detected", name)
		}
	}
}

func TestStoreRowSameSummaryComparesPromptCacheByValue(t *testing.T) {
	a := Row{PromptCache: &PromptCache{TTL: "1h", ExpiresAt: storeInt64(10)}}
	b := Row{PromptCache: &PromptCache{TTL: "1h", ExpiresAt: storeInt64(10)}}
	if !a.SameSummary(b) {
		t.Fatal("distinct PromptCache pointers with equal values must have the same summary")
	}

	nilA := Row{}
	nilB := Row{}
	if !nilA.SameSummary(nilB) {
		t.Fatal("nil PromptCache on both sides must be the same")
	}
	if nilA.SameSummary(Row{PromptCache: &PromptCache{}}) {
		t.Fatal("nil vs non-nil PromptCache must differ")
	}
}

func TestStoreRowJSONKeysAndOmission(t *testing.T) {
	full := Row{
		PID:            12345,
		StartedAt:      1793672985,
		SessionID:      "s",
		TranscriptPath: "/t.jsonl",
		ConfigDir:      "/c",
		Cwd:            "/w",
		Model:          "m",
		PromptCache:    &PromptCache{TTL: "1h", ExpiresAt: storeInt64(1793676625)},
		SeenAt:         1793673030,
		DetailPath:     "/d.json",
	}
	got, err := json.Marshal(full)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"pid":12345,"startedAt":1793672985,"sessionId":"s","transcriptPath":"/t.jsonl",` +
		`"configDir":"/c","cwd":"/w","model":"m","promptCache":{"ttl":"1h","expiresAt":1793676625},` +
		`"seenAt":1793673030,"detailPath":"/d.json"}`
	if string(got) != want {
		t.Fatalf("Row JSON =\n%s\nwant\n%s", got, want)
	}

	noCache, err := json.Marshal(Row{})
	if err != nil {
		t.Fatal(err)
	}
	if containsKey(noCache, "promptCache") {
		t.Fatalf("nil PromptCache must be omitted: %s", noCache)
	}

	onlyExpiry, err := json.Marshal(PromptCache{ExpiresAt: storeInt64(1793676625)})
	if err != nil {
		t.Fatal(err)
	}
	if string(onlyExpiry) != `{"expiresAt":1793676625}` {
		t.Fatalf("PromptCache with only ExpiresAt = %s", onlyExpiry)
	}

	onlyTTL, err := json.Marshal(PromptCache{TTL: "5m"})
	if err != nil {
		t.Fatal(err)
	}
	if string(onlyTTL) != `{"ttl":"5m"}` {
		t.Fatalf("PromptCache with only TTL = %s", onlyTTL)
	}
}

// containsKey reports whether the top-level JSON object in data has the key.
func containsKey(data []byte, key string) bool {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		return false
	}
	_, ok := m[key]
	return ok
}
