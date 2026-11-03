package webconfig

// This file implements the /api/sources endpoints: list, create-or-replace,
// delete, and an explicit one-shot test of a source definition. Definitions
// live in <configDir>/sources.json (internal/sources); the DSL only refers to
// them by id. Contract: internal/webconfig/DSL_API.md ("Sources API").
//
// The test endpoint never writes the source cache and never fetches a
// command or http source in this process: sources.FetchOnce runs those
// through sourceProbe, which is sources.RunProbe (a `statusloom refresh
// --probe <id>` child process) in production.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"time"
	"unicode/utf8"

	"github.com/yacchi/statusloom/internal/render"
	"github.com/yacchi/statusloom/internal/sources"
)

// maxSourceBodyBytes caps the request bodies of the sources endpoints.
const maxSourceBodyBytes = 64 << 10

// maxDocumentPreviewBytes caps the documentPreview returned by the test
// endpoint so an oversized source cannot bloat the response.
const maxDocumentPreviewBytes = 4096

// sourceProbe runs a command or http source for the test endpoint. Tests
// replace it (and restore it with t.Cleanup) so they never spawn a real
// refresh process.
var sourceProbe sources.ProbeFunc = sources.RunProbe

// sourceEntryJSON is the SourceEntry shape of the sources API. Every key is
// always present; the numbers are the raw stored values (0 = default).
type sourceEntryJSON struct {
	ID                string   `json:"id"`
	Kind              string   `json:"kind"`
	Path              string   `json:"path"`
	Argv              []string `json:"argv"`
	URL               string   `json:"url"`
	AllowRemote       bool     `json:"allowRemote"`
	TTLSeconds        int      `json:"ttlSeconds"`
	StaleAfterSeconds int      `json:"staleAfterSeconds"`
	TimeoutSeconds    int      `json:"timeoutSeconds"`
	Warnings          []string `json:"warnings"`
}

// toSourceEntry converts a stored definition to its API shape, turning nil
// slices into empty arrays so the JSON never contains null.
func toSourceEntry(d sources.Definition) sourceEntryJSON {
	argv := d.Argv
	if argv == nil {
		argv = []string{}
	}
	warnings := d.Warnings()
	if warnings == nil {
		warnings = []string{}
	}
	return sourceEntryJSON{
		ID:                d.ID,
		Kind:              string(d.Kind),
		Path:              d.Path,
		Argv:              argv,
		URL:               d.URL,
		AllowRemote:       d.AllowRemote,
		TTLSeconds:        d.TTLSeconds,
		StaleAfterSeconds: d.StaleAfterSeconds,
		TimeoutSeconds:    d.TimeoutSeconds,
		Warnings:          warnings,
	}
}

// sourcePutRequest is the body of PUT /api/sources/{id}. The id comes from
// the path, so it is not part of the body.
type sourcePutRequest struct {
	Kind              string   `json:"kind"`
	Path              string   `json:"path"`
	Argv              []string `json:"argv"`
	URL               string   `json:"url"`
	AllowRemote       bool     `json:"allowRemote"`
	TTLSeconds        int      `json:"ttlSeconds"`
	StaleAfterSeconds int      `json:"staleAfterSeconds"`
	TimeoutSeconds    int      `json:"timeoutSeconds"`
}

// sourceTestRequest is the body of POST /api/sources/{id}/test.
type sourceTestRequest struct {
	Select    string `json:"select"`
	SessionID string `json:"sessionId"`
	Sample    string `json:"sample"`
}

// sourceTestResponse is the response of POST /api/sources/{id}/test. value is
// null unless the select matched.
type sourceTestResponse struct {
	OK              bool   `json:"ok"`
	Error           string `json:"error"`
	Matched         bool   `json:"matched"`
	Value           any    `json:"value"`
	Display         string `json:"display"`
	DocumentPreview string `json:"documentPreview"`
}

// handleListSources serves GET /api/sources.
func (s *server) handleListSources(w http.ResponseWriter, r *http.Request) {
	p, err := sources.Path()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	f, err := sources.Load()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	list := make([]sourceEntryJSON, 0, len(f.Sources))
	for _, id := range f.IDs() {
		def, _ := f.Get(id)
		list = append(list, toSourceEntry(def))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"path":    p,
		"sources": list,
	})
}

// handlePutSource serves PUT /api/sources/{id}: it creates or replaces the
// definition. A definition that fails Validate is rejected before sources.json
// is read or written.
func (s *server) handlePutSource(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	r.Body = http.MaxBytesReader(w, r.Body, maxSourceBodyBytes)
	var req sourcePutRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	def := sources.Definition{
		ID:                id,
		Kind:              sources.Kind(req.Kind),
		Path:              req.Path,
		Argv:              req.Argv,
		URL:               req.URL,
		AllowRemote:       req.AllowRemote,
		TTLSeconds:        req.TTLSeconds,
		StaleAfterSeconds: req.StaleAfterSeconds,
		TimeoutSeconds:    req.TimeoutSeconds,
	}
	if err := def.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	f, err := sources.Load()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := f.Put(def); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := sources.Save(f); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"source": toSourceEntry(def)})
}

// handleDeleteSource serves DELETE /api/sources/{id}.
func (s *server) handleDeleteSource(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	f, err := sources.Load()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !f.Remove(id) {
		writeError(w, http.StatusNotFound, "unknown source")
		return
	}
	if err := sources.Save(f); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"removed": true})
}

// handleTestSource serves POST /api/sources/{id}/test: it fetches the source
// once and evaluates the select against it. Fetch failures are reported in
// the 200 body (ok:false), not as HTTP errors.
func (s *server) handleTestSource(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	r.Body = http.MaxBytesReader(w, r.Body, maxSourceBodyBytes)
	var req sourceTestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	f, err := sources.Load()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	def, ok := f.Get(id)
	if !ok {
		writeError(w, http.StatusNotFound, "unknown source")
		return
	}
	snap, ok := s.previewSnapshot(w, "main", req.Sample, req.SessionID)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), def.Timeout()+5*time.Second)
	defer cancel()
	doc, err := sources.FetchOnce(ctx, def, sourceProbe)
	if err != nil {
		writeJSON(w, http.StatusOK, sourceTestResponse{
			OK:    false,
			Error: err.Error(),
		})
		return
	}
	res := render.EvalSelect(doc, req.Select, snap, os.Getenv)
	resp := sourceTestResponse{
		OK:              res.Err == nil,
		Matched:         res.Matched,
		Display:         res.Display,
		DocumentPreview: sourceDocumentPreview(doc),
	}
	if res.Err != nil {
		resp.Error = res.Err.Error()
	}
	if res.Matched {
		resp.Value = res.Value
	}
	writeJSON(w, http.StatusOK, resp)
}

// sourceDocumentPreview re-encodes a fetched document with two-space
// indentation for display. Output longer than maxDocumentPreviewBytes is cut
// on a UTF-8 boundary. It returns "" when the document cannot be encoded.
func sourceDocumentPreview(doc any) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return ""
	}
	b := bytes.TrimSuffix(buf.Bytes(), []byte("\n"))
	if len(b) > maxDocumentPreviewBytes {
		n := maxDocumentPreviewBytes
		for n > 0 && !utf8.RuneStart(b[n]) {
			n--
		}
		return string(b[:n])
	}
	return string(b)
}
