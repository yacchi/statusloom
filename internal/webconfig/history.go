package webconfig

// This file implements the history endpoints (§8.3/§8.11):
//
//	GET  /api/history?tool=       — tool's revision listing + refs
//	GET  /api/history/{id}        — one revision's source and meta
//	POST /api/history/{id}/restore — restore (repoint current, discard draft)
//
// Like every /api/* path these run behind withSecurity (auth + Host/Origin;
// security.go). {id} is a UUIDv7 and therefore globally unique across every
// tool sharing the store, so the by-id endpoints need no ?tool= — the
// revision's own Tool field resolves it (§8.3).

import (
	"net/http"
	"time"

	"github.com/yacchi/statusloom/internal/schema"
	"github.com/yacchi/statusloom/internal/store"
)

// historyMeta mirrors store.Meta for the wire response (kept as its own type
// rather than reusing store.Meta directly so the JSON shape here is the
// contract, independent of the store package's internal field set).
type historyMeta struct {
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	Author      string `json:"author,omitempty"`
	Notes       string `json:"notes,omitempty"`
}

func toHistoryMeta(m store.Meta) historyMeta {
	return historyMeta{Name: m.Name, Description: m.Description, Author: m.Author, Notes: m.Notes}
}

// historyRevisionEntry is one revision in GET /api/history's listing:
// id/parent/savedAt/origin/meta, deliberately without the source text (kept
// out of the listing payload; GET /api/history/{id} carries it) so the
// listing stays light even with many revisions.
type historyRevisionEntry struct {
	ID      string      `json:"id"`
	Parent  *string     `json:"parent"`
	SavedAt string      `json:"savedAt"`
	Origin  string      `json:"origin"`
	Meta    historyMeta `json:"meta"`
}

// historyRefs mirrors refs.<tool> for GET /api/history's response: the
// current revision id, and whether a draft working node exists (never the
// draft's own source — that stays behind GET /api/dsl/draft).
type historyRefs struct {
	Current string `json:"current"`
	Draft   bool   `json:"draft"`
}

// handleGetHistory handles GET /api/history?tool= (tool defaults to
// claude-code): the tool's revisions (oldest-first, store.Revisions' order)
// plus its refs.
func (s *server) handleGetHistory(w http.ResponseWriter, r *http.Request) {
	tool := r.URL.Query().Get("tool")
	if tool == "" {
		tool = string(schema.ToolClaudeCode)
	}
	if !knownTool(tool) {
		writeError(w, http.StatusBadRequest, "unknown tool")
		return
	}

	st, err := store.Open()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	revs, err := st.Revisions(tool)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	entries := make([]historyRevisionEntry, 0, len(revs))
	for _, rev := range revs {
		entries = append(entries, historyRevisionEntry{
			ID:      rev.ID,
			Parent:  rev.Parent,
			SavedAt: rev.SavedAt.Format(time.RFC3339),
			Origin:  rev.Origin,
			Meta:    toHistoryMeta(rev.Meta),
		})
	}

	_, curID, _, err := st.Current(tool)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	_, draftExists, err := st.ReadDraft(tool)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"revisions": entries,
		"refs":      historyRefs{Current: curID, Draft: draftExists},
	})
}

// handleGetHistoryByID handles GET /api/history/{id}: the revision's source
// and meta. Unknown id is 404.
func (s *server) handleGetHistoryByID(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	st, err := store.Open()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	rev, ok, err := st.Revision(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "unknown revision")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"id":      id,
		"tool":    rev.Tool,
		"source":  rev.Source,
		"parent":  rev.Parent,
		"savedAt": rev.SavedAt.Format(time.RFC3339),
		"origin":  rev.Origin,
		"meta":    toHistoryMeta(rev.Meta),
	})
}

// handlePostHistoryRestore handles POST /api/history/{id}/restore: it
// resolves id's tool from the revision itself (ids are globally unique) and
// runs store.Restore, which repoints that tool's current ref at id and
// discards its draft working node (§4.2). Unknown id is 404.
func (s *server) handlePostHistoryRestore(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	st, err := store.Open()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	rev, ok, err := st.Revision(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "unknown revision")
		return
	}

	if err := st.Restore(rev.Tool, id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"tool":    rev.Tool,
		"current": id,
	})
}
