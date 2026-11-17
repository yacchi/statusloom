package modrec

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yacchi/statusloom/internal/cache"
)

var ingestNow = time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)

// ingestRequest marshals a mod-ingest request body for Ingest.
func ingestRequest(t *testing.T, body map[string]any) []byte {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	return raw
}

// ingestBatch ingests a v1 request for session s1 under boot b1 with the given events.
func ingestBatch(t *testing.T, boot string, events ...map[string]any) {
	t.Helper()
	body := map[string]any{"v": 1, "sessionId": "s1", "boot": boot, "events": events}
	if err := Ingest(ingestRequest(t, body), ingestNow); err != nil {
		t.Fatalf("Ingest() error = %v", err)
	}
}

// loadRecord reads back the stored record for session s1.
func loadRecord(t *testing.T, sessionID string) *cache.ModRecord {
	t.Helper()
	rec, err := cache.LoadModRecord(sessionID)
	if err != nil {
		t.Fatalf("LoadModRecord() error = %v", err)
	}
	if rec == nil {
		t.Fatalf("LoadModRecord(%q) = nil, want record", sessionID)
	}
	return rec
}

// recEvent builds one recorded event for a batch.
func recEvent(seq int64, name string, payload map[string]any) map[string]any {
	return map[string]any{"seq": seq, "at": 1000 + seq, "name": name, "payload": payload}
}

func TestIngestFoldsCursorEventStatsAndLastTurn(t *testing.T) {
	t.Setenv("STATUSLOOM_CACHE_DIR", t.TempDir())

	ingestBatch(t, "b1", recEvent(1, "turn.complete", map[string]any{
		"durationMs": 250, "reason": "answer",
	}))

	rec := loadRecord(t, "s1")
	snap := rec.Snapshot
	if rec.Cursor != (cache.ModCursor{Boot: "b1", Seq: 1}) {
		t.Errorf("Cursor = %+v, want {b1 1}", rec.Cursor)
	}
	if got := rec.Events["turn.complete"]; got != (cache.ModEventStat{Count: 1, LastAt: 1001}) {
		t.Errorf("Events[turn.complete] = %+v, want {1 1001}", got)
	}
	if snap.LastTurn == nil || snap.LastTurn.Duration != 250*time.Millisecond || snap.LastTurn.Reason != "answer" {
		t.Errorf("LastTurn = %+v, want {250ms answer}", snap.LastTurn)
	}
	if snap.PermissionMode != "" {
		t.Errorf("PermissionMode = %q, want empty (no classic event arrived)", snap.PermissionMode)
	}
	if !snap.UpdatedAt.Equal(ingestNow) {
		t.Errorf("UpdatedAt = %v, want %v", snap.UpdatedAt, ingestNow)
	}
}

func TestIngestRetriedBatchIsNotCountedTwice(t *testing.T) {
	t.Setenv("STATUSLOOM_CACHE_DIR", t.TempDir())

	batch := recEvent(1, "agent.spawn", map[string]any{"permissionMode": "plan"})
	ingestBatch(t, "b1", batch)
	ingestBatch(t, "b1", batch)

	rec := loadRecord(t, "s1")
	if rec.Events["agent.spawn"].Count != 1 {
		t.Errorf("Events count = %d, want 1 after a retried send", rec.Events["agent.spawn"].Count)
	}
}

func TestIngestNewBootRestartsSeqAtOne(t *testing.T) {
	t.Setenv("STATUSLOOM_CACHE_DIR", t.TempDir())

	ingestBatch(t, "b1", recEvent(1, "turn.complete", map[string]any{"durationMs": 100, "reason": "answer"}))
	ingestBatch(t, "b2", recEvent(1, "turn.complete", map[string]any{"durationMs": 200, "reason": "answer"}))

	rec := loadRecord(t, "s1")
	if rec.Events["turn.complete"].Count != 2 {
		t.Errorf("Events count = %d, want 2 when a new boot restarts seq", rec.Events["turn.complete"].Count)
	}
	if rec.Cursor != (cache.ModCursor{Boot: "b2", Seq: 1}) {
		t.Errorf("Cursor = %+v, want {b2 1}", rec.Cursor)
	}
}

func TestIngestFoldsInSeqOrder(t *testing.T) {
	t.Setenv("STATUSLOOM_CACHE_DIR", t.TempDir())

	ingestBatch(t, "b1",
		recEvent(3, "agent.spawn", map[string]any{"permissionMode": "plan"}),
		recEvent(1, "agent.spawn", map[string]any{"permissionMode": "default"}),
		recEvent(2, "agent.spawn", map[string]any{"permissionMode": "auto"}),
	)

	rec := loadRecord(t, "s1")
	if rec.Events["agent.spawn"].Count != 3 {
		t.Errorf("Events count = %d, want 3", rec.Events["agent.spawn"].Count)
	}
	if rec.Snapshot.PermissionMode != "plan" {
		t.Errorf("PermissionMode = %q, want plan (the highest seq)", rec.Snapshot.PermissionMode)
	}
	if rec.Cursor != (cache.ModCursor{Boot: "b1", Seq: 3}) {
		t.Errorf("Cursor = %+v, want {b1 3}", rec.Cursor)
	}
}

func TestIngestDropsForeignSessionEvents(t *testing.T) {
	t.Setenv("STATUSLOOM_CACHE_DIR", t.TempDir())

	ingestBatch(t, "b1",
		recEvent(1, "turn.complete", map[string]any{"session_id": "other", "durationMs": 100, "reason": "error"}),
		recEvent(2, "turn.complete", map[string]any{"durationMs": 200, "reason": "answer"}),
	)

	rec := loadRecord(t, "s1")
	if _, ok := rec.Events["turn.complete"]; !ok {
		t.Fatalf("Events has no turn.complete entry, want one from the session-less event")
	}
	if rec.Events["turn.complete"].Count != 1 {
		t.Errorf("Events count = %d, want 1 (foreign event dropped)", rec.Events["turn.complete"].Count)
	}
	if rec.Snapshot.LastTurn == nil || rec.Snapshot.LastTurn.Reason != "answer" {
		t.Errorf("LastTurn = %+v, want the session-less turn (answer)", rec.Snapshot.LastTurn)
	}
}

func TestIngestSubagentDoesNotSetPermissionMode(t *testing.T) {
	t.Setenv("STATUSLOOM_CACHE_DIR", t.TempDir())

	ingestBatch(t, "b1",
		recEvent(1, "agent.spawn", map[string]any{"permissionMode": "plan"}),
		recEvent(2, "agent.spawn", map[string]any{"parentAgentId": "a1", "permissionMode": "dontAsk"}),
	)

	rec := loadRecord(t, "s1")
	if rec.Snapshot.PermissionMode != "plan" {
		t.Errorf("PermissionMode = %q, want plan (subagent must not overwrite)", rec.Snapshot.PermissionMode)
	}
}

func TestIngestSubagentTurnDoesNotSetLastTurn(t *testing.T) {
	t.Setenv("STATUSLOOM_CACHE_DIR", t.TempDir())

	ingestBatch(t, "b1",
		recEvent(1, "turn.complete", map[string]any{"durationMs": 4200, "reason": "answer"}),
		recEvent(2, "turn.complete", map[string]any{"durationMs": 9000, "reason": "error", "agentId": "x"}),
	)

	rec := loadRecord(t, "s1")
	if rec.Snapshot.LastTurn == nil {
		t.Fatalf("LastTurn = nil, want main-loop turn")
	}
	if rec.Snapshot.LastTurn.Duration != 4200*time.Millisecond || rec.Snapshot.LastTurn.Reason != "answer" {
		t.Errorf("LastTurn = %+v, want {4.2s answer}", *rec.Snapshot.LastTurn)
	}
}

func TestIngestUnknownEventOnlyCountsEvents(t *testing.T) {
	t.Setenv("STATUSLOOM_CACHE_DIR", t.TempDir())

	ingestBatch(t, "b1", recEvent(1, "custom.event", map[string]any{
		"tool_name": "Bash", "reason": "x",
	}))

	rec := loadRecord(t, "s1")
	if rec.Events["custom.event"].Count != 1 {
		t.Errorf("Events count = %d, want 1", rec.Events["custom.event"].Count)
	}
	snap := rec.Snapshot
	if snap.PermissionMode != "" || snap.LastTurn != nil {
		t.Errorf("snapshot changed by an unknown event: %+v", snap)
	}
}
func TestIngestAgentSpawnSetsMainPermissionMode(t *testing.T) {
	t.Setenv("STATUSLOOM_CACHE_DIR", t.TempDir())

	ingestBatch(t, "b1", recEvent(1, "agent.spawn", map[string]any{"permissionMode": "plan"}))

	if got := loadRecord(t, "s1").Snapshot.PermissionMode; got != "plan" {
		t.Errorf("PermissionMode = %q, want plan", got)
	}
}

func TestIngestSessionModelSwitchesAndFallback(t *testing.T) {
	t.Setenv("STATUSLOOM_CACHE_DIR", t.TempDir())

	turn := func(seq int64, session, usage string) map[string]any {
		p := map[string]any{"durationMs": 10, "reason": "answer"}
		if session != "" {
			p["sessionModel"] = session
		}
		if usage != "" {
			p["usageModel"] = usage
		}
		return recEvent(seq, "turn.complete", p)
	}
	ingestBatch(t, "b1",
		turn(1, "claude-opus-4-8", "claude-opus-4-8"),
		turn(2, "claude-opus-4-8", "claude-opus-4-8"),
	)
	snap := loadRecord(t, "s1").Snapshot
	if snap.ModelSwitches != 0 || snap.SessionModel != "Opus 4.8" || snap.TurnModel != "Opus 4.8" {
		t.Errorf("same model: switches, session, turn = %d, %q, %q, want 0, Opus 4.8, Opus 4.8", snap.ModelSwitches, snap.SessionModel, snap.TurnModel)
	}

	ingestBatch(t, "b1", turn(3, "claude-sonnet-5", "claude-sonnet-5"))
	snap = loadRecord(t, "s1").Snapshot
	if snap.ModelSwitches != 1 || snap.SessionModel != "Sonnet 5" {
		t.Errorf("switched: switches, session = %d, %q, want 1, Sonnet 5", snap.ModelSwitches, snap.SessionModel)
	}

	// The turn was answered by another model than the session's: a fallback, not a switch.
	ingestBatch(t, "b1", turn(4, "claude-sonnet-5", "claude-opus-4-8"))
	snap = loadRecord(t, "s1").Snapshot
	if snap.ModelSwitches != 1 || snap.SessionModel != "Sonnet 5" || snap.TurnModel != "Opus 4.8" {
		t.Errorf("fallback: switches, session, turn = %d, %q, %q, want 1, Sonnet 5, Opus 4.8", snap.ModelSwitches, snap.SessionModel, snap.TurnModel)
	}

	// A turn that reported no usage model clears the turn model and keeps the session model.
	ingestBatch(t, "b1", turn(5, "claude-sonnet-5", ""))
	snap = loadRecord(t, "s1").Snapshot
	if snap.TurnModel != "" || snap.SessionModel != "Sonnet 5" || snap.ModelSwitches != 1 {
		t.Errorf("no usage: session, turn, switches = %q, %q, %d, want Sonnet 5, empty, 1", snap.SessionModel, snap.TurnModel, snap.ModelSwitches)
	}
}

func TestIngestRejectsUnsupportedVersionWithoutWriting(t *testing.T) {
	cases := map[string]string{
		"v2":       `{"v":2,"sessionId":"s1","boot":"b1","events":[]}`,
		"v string": `{"v":"1","sessionId":"s1","boot":"b1","events":[]}`,
		"no v":     `{"sessionId":"s1","boot":"b1","events":[]}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("STATUSLOOM_CACHE_DIR", dir)

			err := Ingest([]byte(body), ingestNow)
			if !errors.Is(err, ErrUnsupportedVersion) {
				t.Fatalf("Ingest() error = %v, want ErrUnsupportedVersion", err)
			}
			assertNoSessionFiles(t, dir)
		})
	}
}

func TestIngestRejectsInvalidRequestsWithoutWriting(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("STATUSLOOM_CACHE_DIR", dir)

	err := Ingest([]byte(`{"v":1,"sessionId":"","boot":"b1","events":[]}`), ingestNow)
	if err == nil || err.Error() != "mod-ingest: empty sessionId" {
		t.Errorf("Ingest(empty sessionId) error = %v, want mod-ingest: empty sessionId", err)
	}

	err = Ingest([]byte(`{`), ingestNow)
	if err == nil || !strings.HasPrefix(err.Error(), "mod-ingest: invalid request:") {
		t.Errorf("Ingest(invalid JSON) error = %v, want prefix mod-ingest: invalid request:", err)
	}

	assertNoSessionFiles(t, dir)
}

func TestIngestAgentsQueryAtIsWired(t *testing.T) {
	t.Setenv("STATUSLOOM_CACHE_DIR", t.TempDir())

	body := map[string]any{
		"v": 1, "sessionId": "s1", "boot": "b1", "events": []any{},
		"queries": map[string]any{"agents": map[string]any{"at": 5000, "value": []any{}}},
	}
	if err := Ingest(ingestRequest(t, body), ingestNow); err != nil {
		t.Fatalf("Ingest() error = %v", err)
	}

	rec := loadRecord(t, "s1")
	if rec.Snapshot.Agents == nil {
		t.Fatalf("Agents = nil, want a summary from the agents query")
	}
	if !rec.Snapshot.Agents.At.Equal(time.UnixMilli(5000)) {
		t.Errorf("Agents.At = %v, want %v", rec.Snapshot.Agents.At, time.UnixMilli(5000))
	}
}

// assertNoSessionFiles fails when the cache dir holds any mod session record.
func assertNoSessionFiles(t *testing.T, cacheDir string) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(cacheDir, "mod", "sessions"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("ReadDir() error = %v", err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".json") {
			t.Errorf("found session file %q, want none", e.Name())
		}
	}
}
