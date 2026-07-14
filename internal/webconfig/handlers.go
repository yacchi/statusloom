package webconfig

import (
	"net/http"
	"time"

	"github.com/yacchi/statusloom/internal/cache"
	"github.com/yacchi/statusloom/internal/schema"
)

// maxConfigBodyBytes bounds JSON request bodies (e.g. POST /api/live).
const maxConfigBodyBytes = 1 << 20 // 1MB

// toolInfo describes one supported tool for GET /api/tools.
type toolInfo struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
}

func (s *server) handleTools(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"tools": []toolInfo{
			{ID: string(schema.ToolClaudeCode), DisplayName: "Claude Code"},
		},
	})
}

// sessionInfo summarizes one cached session snapshot for GET
// /api/sessions, so the config UI can offer "preview against a real
// recent session" without shipping the full StatusSnapshot up front.
type sessionInfo struct {
	ID            string `json:"id"`
	Cwd           string `json:"cwd"`
	Model         string `json:"model"`
	Version       string `json:"version"`
	ObservedAt    string `json:"observedAt"` // RFC3339
	AgeSeconds    int    `json:"ageSeconds"`
	HasRateLimits bool   `json:"hasRateLimits"`
	HasRepo       bool   `json:"hasRepo"`
}

// liveSessionWindow bounds the observation freshness required for a
// cached session to show up in the "Live sessions" dropdown. This is a
// display filter only, independent of the on-disk snapshotTTL (24h,
// cache/snapshot.go): the UI should only ever offer sessions that are
// plausibly still active, not every session observed in the last day.
const liveSessionWindow = 10 * time.Minute

// handleSessions lists cached recent-session snapshots observed within
// liveSessionWindow (newest first), for the config UI's real-data
// preview: pick one of these ids and pass it as the preview request's
// sessionId to render against actual recent data instead of only the
// synthetic samples.
func (s *server) handleSessions(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	entries, err := cache.ListSnapshots(now)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	sessions := make([]sessionInfo, 0, len(entries))
	for _, entry := range entries {
		if now.Sub(entry.ObservedAt) > liveSessionWindow {
			continue
		}
		snap := entry.Snapshot
		model := ""
		if snap.Session.Model != nil {
			model = snap.Session.Model.DisplayName
		}
		sessions = append(sessions, sessionInfo{
			ID:            snap.Session.ID,
			Cwd:           snap.System.Cwd,
			Model:         model,
			Version:       snap.Tool.Version,
			ObservedAt:    entry.ObservedAt.Format(time.RFC3339),
			AgeSeconds:    int(now.Sub(entry.ObservedAt).Seconds()),
			HasRateLimits: snap.Account.FiveHour != nil || snap.Account.SevenDay != nil,
			HasRepo:       snap.Repository != nil,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{"sessions": sessions})
}

// Preview width bounds shared by the DSL preview endpoint (see dsl.go).
const (
	defaultPreviewWidth = 120
	minPreviewWidth     = 20
	maxPreviewWidth     = 400
)

func (s *server) handleShutdown(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	s.triggerShutdown()
}
