package cache

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yacchi/statusloom/internal/schema"
)

func TestModRecordRoundTrip(t *testing.T) {
	t.Setenv("STATUSLOOM_CACHE_DIR", t.TempDir())

	updated := time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC)
	rec := &ModRecord{
		SessionID: "abc",
		Cursor:    ModCursor{Boot: "b1", Seq: 3},
		Events:    map[string]ModEventStat{"x": {Count: 2, LastAt: 5}},
		Snapshot: schema.ModSnapshot{
			UpdatedAt:     updated,
			ModelSwitches: 4,
			Agents:        &schema.ModAgents{Running: 1, Tokens: 900},
		},
	}
	if err := StoreModRecord(rec); err != nil {
		t.Fatalf("StoreModRecord() error = %v", err)
	}

	got, err := LoadModRecord("abc")
	if err != nil {
		t.Fatalf("LoadModRecord() error = %v", err)
	}
	if got == nil {
		t.Fatalf("LoadModRecord() = nil, want record")
	}
	if got.SchemaVersion != 1 {
		t.Errorf("SchemaVersion = %d, want 1", got.SchemaVersion)
	}
	if got.SessionID != "abc" || got.Cursor != rec.Cursor {
		t.Errorf("identity/cursor = %q %+v, want abc %+v", got.SessionID, got.Cursor, rec.Cursor)
	}
	if got.Events["x"] != rec.Events["x"] {
		t.Errorf("Events[x] = %+v, want %+v", got.Events["x"], rec.Events["x"])
	}
	if got.Snapshot.ModelSwitches != 4 {
		t.Errorf("Snapshot.ModelSwitches = %d, want 4", got.Snapshot.ModelSwitches)
	}
	if !got.Snapshot.UpdatedAt.Equal(updated) {
		t.Errorf("Snapshot.UpdatedAt = %v, want %v", got.Snapshot.UpdatedAt, updated)
	}
	if got.Snapshot.Agents == nil || got.Snapshot.Agents.Running != 1 || got.Snapshot.Agents.Tokens != 900 {
		t.Errorf("Snapshot.Agents = %+v, want Running 1, Tokens 900", got.Snapshot.Agents)
	}
}

func TestModRecordSanitizedSessionID(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("STATUSLOOM_CACHE_DIR", dir)

	rec := &ModRecord{SessionID: "A/b c", Cursor: ModCursor{Boot: "b", Seq: 1}}
	if err := StoreModRecord(rec); err != nil {
		t.Fatalf("StoreModRecord() error = %v", err)
	}

	sessionsDir := filepath.Join(dir, "mod", "sessions")
	entries, err := os.ReadDir(sessionsDir)
	if err != nil {
		t.Fatalf("ReadDir(%s) error = %v", sessionsDir, err)
	}
	if len(entries) != 1 || entries[0].IsDir() {
		t.Fatalf("sessions dir entries = %v, want exactly one file", entries)
	}

	got, err := LoadModRecord("A/b c")
	if err != nil {
		t.Fatalf("LoadModRecord() error = %v", err)
	}
	if got == nil || got.SessionID != "A/b c" {
		t.Fatalf("LoadModRecord() = %+v, want session A/b c", got)
	}
}

func TestModRecordEmptySessionIDCreatesNothing(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("STATUSLOOM_CACHE_DIR", dir)

	if got, err := LoadModRecord(""); got != nil || err != nil {
		t.Fatalf("LoadModRecord(\"\") = %+v, %v; want nil, nil", got, err)
	}
	if err := StoreModRecord(&ModRecord{}); err == nil {
		t.Fatalf("StoreModRecord(empty id) error = nil, want error")
	}
	if _, err := os.Stat(filepath.Join(dir, "mod")); !os.IsNotExist(err) {
		t.Fatalf("mod dir exists after rejected store: stat error = %v", err)
	}
}

func TestLoadModRecordIgnoresBrokenFiles(t *testing.T) {
	cases := map[string]string{
		"wrong schema version": `{"schemaVersion":2,"sessionId":"s"}`,
		"not json":             `not json`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("STATUSLOOM_CACHE_DIR", dir)

			sessionsDir := filepath.Join(dir, "mod", "sessions")
			if err := os.MkdirAll(sessionsDir, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(sessionsDir, "s.json"), []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}

			got, err := LoadModRecord("s")
			if got != nil || err != nil {
				t.Fatalf("LoadModRecord() = %+v, %v; want nil, nil", got, err)
			}
		})
	}
}

func TestPruneModRecords(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("STATUSLOOM_CACHE_DIR", dir)

	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	day := 24 * time.Hour
	sessionsDir := filepath.Join(dir, "mod", "sessions")
	if err := os.MkdirAll(sessionsDir, 0o700); err != nil {
		t.Fatal(err)
	}

	writeAged := func(name string, age time.Duration) {
		t.Helper()
		path := filepath.Join(sessionsDir, name)
		if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
		mtime := now.Add(-age)
		if err := os.Chtimes(path, mtime, mtime); err != nil {
			t.Fatal(err)
		}
	}
	exists := func(name string) bool {
		t.Helper()
		_, err := os.Stat(filepath.Join(sessionsDir, name))
		return err == nil
	}

	writeAged("a.json", 8*day)
	writeAged("b.json", 6*day)
	writeAged("c.txt", 8*day)
	writeAged("d.json.tmp.1", 8*day)

	PruneModRecords(now)

	if exists("a.json") {
		t.Errorf("a.json (8 days old) should be removed")
	}
	for _, name := range []string{"b.json", "c.txt", "d.json.tmp.1"} {
		if !exists(name) {
			t.Errorf("%s should be kept", name)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "mod", ".pruned")); err != nil {
		t.Errorf("marker .pruned missing after first run: %v", err)
	}

	writeAged("e.json", 8*day)

	// Within the hour the run is skipped, so e.json survives.
	PruneModRecords(now.Add(30 * time.Minute))
	if !exists("e.json") {
		t.Errorf("e.json should survive a prune 30 minutes after the previous run")
	}

	PruneModRecords(now.Add(2 * time.Hour))
	if exists("e.json") {
		t.Errorf("e.json should be removed by a prune 2 hours after the previous run")
	}
}
