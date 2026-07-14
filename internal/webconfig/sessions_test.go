package webconfig

import (
	"net/http"
	"testing"
	"time"

	"github.com/yacchi/statusloom/internal/cache"
	"github.com/yacchi/statusloom/internal/schema"
)

// sessionsResponse mirrors the GET /api/sessions response shape for
// decoding in tests.
type sessionsResponse struct {
	Sessions []struct {
		ID            string `json:"id"`
		Cwd           string `json:"cwd"`
		Model         string `json:"model"`
		Version       string `json:"version"`
		ObservedAt    string `json:"observedAt"`
		AgeSeconds    int    `json:"ageSeconds"`
		HasRateLimits bool   `json:"hasRateLimits"`
		HasRepo       bool   `json:"hasRepo"`
	} `json:"sessions"`
}

func getSessions(t *testing.T, ts *testServer) sessionsResponse {
	t.Helper()
	resp := authedRequest(t, ts, "GET", "/api/sessions", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	var body sessionsResponse
	if err := decodeJSON(resp.Body, &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return body
}

func TestSessions_EmptyWhenNoneCached(t *testing.T) {
	t.Setenv("STATUSLOOM_CACHE_DIR", t.TempDir())
	ts := startTestServer(t, time.Hour)

	body := getSessions(t, ts)
	if body.Sessions == nil {
		t.Fatalf("sessions = nil, want an empty (non-null) slice")
	}
	if len(body.Sessions) != 0 {
		t.Fatalf("len(sessions) = %d, want 0", len(body.Sessions))
	}
}

func TestSessions_ListsNewestFirstWithSummaryFields(t *testing.T) {
	t.Setenv("STATUSLOOM_CACHE_DIR", t.TempDir())
	ts := startTestServer(t, time.Hour)

	model := schema.ModelInfo{ID: "claude-opus-4-8", DisplayName: "Opus 4.8"}
	older := schema.StatusSnapshot{
		Tool:    schema.ToolSnapshot{ID: schema.ToolClaudeCode, Version: "2.1.150"},
		Session: schema.SessionSnapshot{ID: "sess-older", Model: &model},
		System:  schema.SystemSnapshot{Cwd: "/Users/dev/old-project"},
	}
	newer := schema.StatusSnapshot{
		Tool:    schema.ToolSnapshot{ID: schema.ToolClaudeCode, Version: "2.1.200"},
		Session: schema.SessionSnapshot{ID: "sess-newer", Model: &model},
		Repository: &schema.RepositorySnapshot{
			Root: "/Users/dev/new-project", Branch: "main",
		},
		Account: schema.AccountSnapshot{
			FiveHour: &schema.RateWindow{UsedPercentage: 10, ResetsAt: time.Now().Add(time.Hour)},
		},
		System: schema.SystemSnapshot{Cwd: "/Users/dev/new-project"},
	}

	// Both timestamps must fall within liveSessionWindow (10 minutes) of
	// now, or handleSessions would filter them out as stale. Keep a gap
	// between them so ordering (newer first) is still meaningfully tested.
	olderAt := time.Now().Add(-3 * time.Minute)
	newerAt := time.Now().Add(-1 * time.Minute)
	if err := cache.StoreSnapshot("sess-older", older, olderAt); err != nil {
		t.Fatalf("StoreSnapshot(older) error = %v", err)
	}
	if err := cache.StoreSnapshot("sess-newer", newer, newerAt); err != nil {
		t.Fatalf("StoreSnapshot(newer) error = %v", err)
	}

	body := getSessions(t, ts)
	if len(body.Sessions) != 2 {
		t.Fatalf("len(sessions) = %d, want 2", len(body.Sessions))
	}

	first, second := body.Sessions[0], body.Sessions[1]
	if first.ID != "sess-newer" || second.ID != "sess-older" {
		t.Fatalf("order = [%q, %q], want [sess-newer, sess-older]", first.ID, second.ID)
	}

	if first.Cwd != "/Users/dev/new-project" {
		t.Errorf("sessions[0].cwd = %q, want /Users/dev/new-project", first.Cwd)
	}
	if first.Model != "Opus 4.8" {
		t.Errorf("sessions[0].model = %q, want Opus 4.8", first.Model)
	}
	if first.Version != "2.1.200" {
		t.Errorf("sessions[0].version = %q, want 2.1.200", first.Version)
	}
	if !first.HasRateLimits {
		t.Error("sessions[0].hasRateLimits = false, want true")
	}
	if !first.HasRepo {
		t.Error("sessions[0].hasRepo = false, want true")
	}
	if first.AgeSeconds < 0 {
		t.Errorf("sessions[0].ageSeconds = %d, want >= 0", first.AgeSeconds)
	}
	if _, err := time.Parse(time.RFC3339, first.ObservedAt); err != nil {
		t.Errorf("sessions[0].observedAt = %q, not RFC3339: %v", first.ObservedAt, err)
	}

	if second.HasRateLimits {
		t.Error("sessions[1].hasRateLimits = true, want false")
	}
	if second.HasRepo {
		t.Error("sessions[1].hasRepo = true, want false")
	}
}

// TestSessions_ExcludesStaleSessions verifies that handleSessions applies
// liveSessionWindow as a display filter on top of cache.ListSnapshots:
// a session observed outside the window is omitted from the dropdown even
// though it is still well within the on-disk snapshotTTL (24h) and would
// still be returned by cache.ListSnapshots itself.
func TestSessions_ExcludesStaleSessions(t *testing.T) {
	t.Setenv("STATUSLOOM_CACHE_DIR", t.TempDir())
	ts := startTestServer(t, time.Hour)

	fresh := schema.StatusSnapshot{
		Tool:    schema.ToolSnapshot{ID: schema.ToolClaudeCode, Version: "2.1.200"},
		Session: schema.SessionSnapshot{ID: "sess-fresh"},
		System:  schema.SystemSnapshot{Cwd: "/Users/dev/fresh-project"},
	}
	stale := schema.StatusSnapshot{
		Tool:    schema.ToolSnapshot{ID: schema.ToolClaudeCode, Version: "2.1.200"},
		Session: schema.SessionSnapshot{ID: "sess-stale"},
		System:  schema.SystemSnapshot{Cwd: "/Users/dev/stale-project"},
	}

	if err := cache.StoreSnapshot("sess-fresh", fresh, time.Now().Add(-time.Minute)); err != nil {
		t.Fatalf("StoreSnapshot(fresh) error = %v", err)
	}
	if err := cache.StoreSnapshot("sess-stale", stale, time.Now().Add(-20*time.Minute)); err != nil {
		t.Fatalf("StoreSnapshot(stale) error = %v", err)
	}

	body := getSessions(t, ts)
	if len(body.Sessions) != 1 {
		t.Fatalf("len(sessions) = %d, want 1 (stale session should be excluded)", len(body.Sessions))
	}
	if body.Sessions[0].ID != "sess-fresh" {
		t.Errorf("sessions[0].id = %q, want sess-fresh", body.Sessions[0].ID)
	}
}
