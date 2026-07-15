package webconfig

// This file implements the /api/dsl/* endpoints: the DSL-native document,
// draft, parse, serialize, preview, fields, and metrics APIs the DSL Editor
// and visual editor consume. These are the sole configuration API (the legacy
// widget-index endpoints have been removed). The AST JSON contract and the
// node-ID scheme are documented in DSL_API.md and astjson.go.
//
// Auth, Host/Origin validation, and idle-timer reset are applied uniformly by
// withSecurity (security.go) because every path here is under /api/.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/yacchi/statusloom/internal/cache"
	"github.com/yacchi/statusloom/internal/config"
	"github.com/yacchi/statusloom/internal/dsl"
	"github.com/yacchi/statusloom/internal/render"
	"github.com/yacchi/statusloom/internal/schema"
)

// maxDSLBodyBytes bounds DSL request bodies (source text can be larger than a
// JSON config, so a slightly higher cap than maxConfigBodyBytes is used).
const maxDSLBodyBytes = 2 << 20 // 2MB

// knownTool reports whether tool is a tool statusloom can serve DSL for.
func knownTool(tool string) bool {
	return tool == string(schema.ToolClaudeCode) || tool == string(schema.ToolClaudeCodeSubagent)
}

// sourceVersion is the deterministic content hash (sha256 hex) of DSL source
// text. GET and PUT compute it identically so the frontend can use it as an
// echo guard and for last-writer-wins comparisons.
func sourceVersion(src string) string {
	sum := sha256.Sum256([]byte(src))
	return hex.EncodeToString(sum[:])
}

// parseAndValidateSource parses src and, when a root was produced, appends the
// semantic validation diagnostics (mirroring config.parseAndValidate, which is
// unexported).
func parseAndValidateSource(src string) (*dsl.Document, []dsl.Diagnostic) {
	doc, diags := dsl.Parse(src)
	if doc != nil && doc.Root != nil {
		diags = append(diags, dsl.Validate(doc)...)
	}
	return doc, diags
}

// handleGetDSLDocument handles GET /api/dsl/document?tool=: it returns the
// saved <tool>.xml source and its version, or DefaultDocument with exists=false
// when the file is absent. The raw source is returned even if it is invalid, so
// the editor can display and fix it.
func (s *server) handleGetDSLDocument(w http.ResponseWriter, r *http.Request) {
	tool := r.URL.Query().Get("tool")
	if !knownTool(tool) {
		writeError(w, http.StatusBadRequest, "unknown tool")
		return
	}
	data, err := os.ReadFile(config.DocumentPath(tool))
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		src := config.DefaultDocument(tool)
		writeJSON(w, http.StatusOK, map[string]any{
			"source": src, "version": sourceVersion(src), "exists": false,
		})
		return
	}
	src := string(data)
	writeJSON(w, http.StatusOK, map[string]any{
		"source": src, "version": sourceVersion(src), "exists": true,
	})
}

// dslDocumentPutRequest is the body of PUT /api/dsl/document and PUT
// /api/dsl/draft.
type dslSourcePutRequest struct {
	Tool   string `json:"tool"`
	Source string `json:"source"`
}

// handlePutDSLDocument handles PUT /api/dsl/document: it parses and validates
// the posted source and, only when it has no error-severity diagnostics, saves
// it to <tool>.xml. A document with errors is rejected with 409 and its
// diagnostics (no write). Warning-only documents are saved. The response always
// carries the source version and the diagnostics.
func (s *server) handlePutDSLDocument(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeSourcePut(w, r)
	if !ok {
		return
	}

	_, diags := parseAndValidateSource(req.Source)
	version := sourceVersion(req.Source)

	if dsl.HasErrors(diags) {
		writeJSON(w, http.StatusConflict, map[string]any{
			"version":     version,
			"diagnostics": toDiagsJSON(diags),
		})
		return
	}

	if err := config.SaveDocumentSource(req.Tool, req.Source); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"version":     version,
		"diagnostics": toDiagsJSON(diags),
	})
}

// dslParseRequest is the body of POST /api/dsl/parse.
type dslParseRequest struct {
	Source string `json:"source"`
}

// handleParseDSL handles POST /api/dsl/parse: it parses source for the DSL
// Editor's live analysis, returning the AST (when a root was produced),
// diagnostics, and the version. It never saves.
func (s *server) handleParseDSL(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxDSLBodyBytes)
	var req dslParseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	doc, diags := parseAndValidateSource(req.Source)
	resp := map[string]any{
		"diagnostics": toDiagsJSON(diags),
		"version":     sourceVersion(req.Source),
	}
	if doc != nil && doc.Root != nil {
		ast, _ := buildAST(doc)
		resp["ast"] = ast
	}
	writeJSON(w, http.StatusOK, resp)
}

// dslSerializeRequest is the body of POST /api/dsl/serialize.
type dslSerializeRequest struct {
	AST astNodeJSON `json:"ast"`
	// BaseSource, when present, is the source the AST's node ranges index
	// into (the client's last valid document). It enables minimal-diff
	// serialization: unchanged nodes are emitted verbatim from BaseSource and
	// only dirty / range-less nodes are regenerated. Omitted = whole-document
	// canonical form.
	BaseSource *string `json:"baseSource"`
}

// handleSerializeDSL handles POST /api/dsl/serialize: it turns an AST (the
// visual editor's working tree) back into DSL source and reports any
// diagnostics from re-parsing that source. When the request carries a
// baseSource, unchanged nodes are reused verbatim (minimal-diff serialization,
// markup.md "DSL表現の維持"); otherwise the whole-document canonical form is
// returned.
func (s *server) handleSerializeDSL(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxDSLBodyBytes)
	var req dslSerializeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	doc, err := jsonToDocument(req.AST)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var src string
	if req.BaseSource != nil {
		doc.Source = *req.BaseSource
		src = dsl.SerializeMinimal(doc)
	} else {
		src = dsl.Serialize(doc)
	}
	_, diags := parseAndValidateSource(src)
	writeJSON(w, http.StatusOK, map[string]any{
		"source":      src,
		"diagnostics": toDiagsJSON(diags),
	})
}

// handleGetDSLDraft handles GET /api/dsl/draft?tool=: it returns the shared
// draft source (<tool>.draft.xml), falling back to the saved document and then
// the built-in default when no draft exists. exists reflects the draft file's
// presence.
func (s *server) handleGetDSLDraft(w http.ResponseWriter, r *http.Request) {
	tool := r.URL.Query().Get("tool")
	if !knownTool(tool) {
		writeError(w, http.StatusBadRequest, "unknown tool")
		return
	}
	src, exists, err := config.LoadDraftDocumentSource(tool)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"source": src, "version": sourceVersion(src), "exists": exists,
	})
}

// handlePutDSLDraft handles PUT /api/dsl/draft: it saves the posted source to
// the shared draft (<tool>.draft.xml) unconditionally (last-writer-wins). The
// draft tolerates in-progress, invalid input; parse diagnostics are returned
// for the editor's benefit but never block the write.
func (s *server) handlePutDSLDraft(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeSourcePut(w, r)
	if !ok {
		return
	}
	_, diags := parseAndValidateSource(req.Source)
	if err := config.SaveDraftDocumentSource(req.Tool, req.Source); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"version":     sourceVersion(req.Source),
		"diagnostics": toDiagsJSON(diags),
	})
}

// decodeSourcePut decodes a {tool, source} body and validates the tool. It
// writes the error response and returns ok=false on any problem.
func decodeSourcePut(w http.ResponseWriter, r *http.Request) (dslSourcePutRequest, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxDSLBodyBytes)
	var req dslSourcePutRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return req, false
	}
	if !knownTool(req.Tool) {
		writeError(w, http.StatusBadRequest, "unknown tool")
		return req, false
	}
	return req, true
}

// dslPreviewRequest is the body of POST /api/dsl/preview.
type dslPreviewRequest struct {
	Tool        string `json:"tool"`
	Source      string `json:"source"`
	Width       int    `json:"width"`
	Sample      string `json:"sample"`
	SessionID   string `json:"sessionId"`
	LayoutIndex int    `json:"layoutIndex"`
}

// dslPreviewSegment is one leaf node's rendered result within a preview line,
// referenced by node ID (empty for decoration segments with no source node,
// e.g. the fallback line or a <line>'s own prefix/suffix).
type dslPreviewSegment struct {
	NodeID  string `json:"nodeId"`
	Text    string `json:"text"`
	ANSI    string `json:"ansi"`
	Visible bool   `json:"visible"`
}

type dslPreviewLine struct {
	Omitted  bool                `json:"omitted"`
	ANSI     string              `json:"ansi"`
	Segments []dslPreviewSegment `json:"segments"`
}

// handlePreviewDSL handles POST /api/dsl/preview: it parses source, renders the
// requested layout, and returns per-line, per-node segments referenced by node
// ID (matching the AST from /api/dsl/parse), plus diagnostics and the fallback
// line. Invalid (unparseable) source yields empty lines and diagnostics only.
func (s *server) handlePreviewDSL(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxDSLBodyBytes)
	var req dslPreviewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !knownTool(req.Tool) {
		writeError(w, http.StatusBadRequest, "unknown tool")
		return
	}

	doc, diags := parseAndValidateSource(req.Source)
	if doc == nil || doc.Root == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"lines":       []dslPreviewLine{},
			"diagnostics": toDiagsJSON(diags),
		})
		return
	}

	snap, ok := s.previewSnapshot(w, req.Tool, req.Sample, req.SessionID)
	if !ok {
		return
	}

	opts := render.Options{Width: clampPreviewWidth(req.Width), Now: time.Now()}
	_, ids := buildAST(doc)

	var docLines []render.DocLine
	withActiveLayout(doc, req.LayoutIndex, func() {
		docLines = render.RenderDocument(snap, doc, opts)
	})

	lines := make([]dslPreviewLine, 0, len(docLines))
	allOmitted := true
	for _, dl := range docLines {
		if !dl.Omitted {
			allOmitted = false
		}
		var ansi strings.Builder
		segs := make([]dslPreviewSegment, 0, len(dl.Segments))
		for _, seg := range dl.Segments {
			ansi.WriteString(seg.ANSI)
			nodeID := ""
			if seg.Node != nil {
				nodeID = ids[seg.Node]
			}
			segs = append(segs, dslPreviewSegment{
				NodeID: nodeID, Text: seg.Text, ANSI: seg.ANSI, Visible: seg.Visible,
			})
		}
		lines = append(lines, dslPreviewLine{Omitted: dl.Omitted, ANSI: ansi.String(), Segments: segs})
	}

	fallbackANSI := ""
	if allOmitted {
		withActiveLayout(doc, req.LayoutIndex, func() {
			// With every line omitted, RenderDocumentString returns the
			// fallback line (model + tool-version) for the previewed layout.
			fallbackANSI = render.RenderDocumentString(snap, doc, opts)
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"lines":       lines,
		"diagnostics": toDiagsJSON(diags),
		"fallback":    map[string]any{"ansi": fallbackANSI, "active": allOmitted},
	})
}

// previewSnapshot resolves the snapshot a preview renders against: a real
// cached session (sessionId, as listed by GET /api/sessions), else a named
// sample (defaulting to defaultSampleForTool(tool) — a session-shaped sample
// for tool="claude-code", a subagentStatusLine-shaped one for
// tool="claude-code-subagent"). It writes the error response and returns
// ok=false on failure.
func (s *server) previewSnapshot(w http.ResponseWriter, tool, sample, sessionID string) (schema.StatusSnapshot, bool) {
	if sessionID != "" {
		entry, err := cache.LoadSnapshot(sessionID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return schema.StatusSnapshot{}, false
		}
		if entry == nil {
			writeError(w, http.StatusBadRequest, "unknown session")
			return schema.StatusSnapshot{}, false
		}
		return entry.Snapshot, true
	}
	name := sample
	if name == "" {
		name = defaultSampleForTool(tool)
	}
	snap, ok := sampleSnapshot(name, time.Now())
	if !ok {
		writeError(w, http.StatusBadRequest, "unknown sample")
		return schema.StatusSnapshot{}, false
	}
	return snap, true
}

func clampPreviewWidth(width int) int {
	if width == 0 {
		return defaultPreviewWidth
	}
	if width < minPreviewWidth {
		return minPreviewWidth
	}
	if width > maxPreviewWidth {
		return maxPreviewWidth
	}
	return width
}

// withActiveLayout temporarily makes layout idx (clamped) the active one, runs
// fn, then restores the original active flags. This lets the preview render a
// layout other than the document's own active one without mutating the AST the
// node-ID map was built from (the same node pointers are rendered, so segment
// IDs stay valid). It is safe because each request parses its own document.
func withActiveLayout(doc *dsl.Document, idx int, fn func()) {
	layouts := doc.Root.Layouts
	if len(layouts) == 0 {
		fn()
		return
	}
	if idx < 0 {
		idx = 0
	}
	if idx >= len(layouts) {
		idx = len(layouts) - 1
	}
	saved := make([]*bool, len(layouts))
	tru := true
	for i, l := range layouts {
		saved[i] = l.Active
		if i == idx {
			l.Active = &tru
		} else {
			l.Active = nil
		}
	}
	defer func() {
		for i, l := range layouts {
			l.Active = saved[i]
		}
	}()
	fn()
}

// dslDescriptions is the localized-description shape shared by the fields and
// metrics endpoints.
type dslDescriptions struct {
	EN string `json:"en"`
	JA string `json:"ja"`
}

// dslFieldEntry is one field in GET /api/dsl/fields, sourced entirely from the
// dsl registry (the single source of truth) plus a rendered preview.
type dslFieldEntry struct {
	Name         string          `json:"name"`
	DisplayName  string          `json:"displayName"`
	Descriptions dslDescriptions `json:"descriptions"`
	Category     string          `json:"category"`
	Linkable     bool            `json:"linkable,omitempty"`
	SelfMetric   string          `json:"selfMetric,omitempty"`
	Formats      []string        `json:"formats,omitempty"`
	Capability   string          `json:"capability,omitempty"`
	Preview      widgetPreview   `json:"preview"`
}

// handleDSLFields handles GET /api/dsl/fields?tool=: the field catalog for the
// visual editor's palette, built from the dsl registry. Each entry carries a
// rendered preview (previewFor) produced against the full sample snapshot.
func (s *server) handleDSLFields(w http.ResponseWriter, r *http.Request) {
	tool := r.URL.Query().Get("tool")
	if !knownTool(tool) {
		writeError(w, http.StatusBadRequest, "unknown tool")
		return
	}
	now := time.Now()
	snap, _ := sampleSnapshot(defaultSampleForTool(tool), now)
	overlayRealAccountUsage(&snap, now)
	fields := dsl.Fields(tool)
	out := make([]dslFieldEntry, 0, len(fields))
	for _, f := range fields {
		out = append(out, dslFieldEntry{
			Name:         f.Name,
			DisplayName:  f.DisplayName,
			Descriptions: dslDescriptions{EN: f.Descriptions.EN, JA: f.Descriptions.JA},
			Category:     f.Category,
			Linkable:     f.Linkable,
			SelfMetric:   f.SelfMetric,
			Formats:      f.Formats,
			Capability:   f.Capability,
			Preview:      previewFor(tool, f.Name, snap, now),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"fields": out})
}

// overlayRealAccountUsage replaces snap.Account's extra-usage / per-model
// weekly-usage fields with the user's real cached values, when the usage-API
// probe (handleUsageProbe in usageprobe.go) has successfully persisted them
// to the shared account-usage cache (accountUsageKey). Fields with no cached
// value are left as the synthetic fullSample values so previews still look
// realistic before the probe has ever run.
func overlayRealAccountUsage(snap *schema.StatusSnapshot, now time.Time) {
	env, _, ok := cache.LoadAccountUsage(accountUsageKey, now)
	if !ok {
		return
	}
	if env.ExtraUsage != nil {
		snap.Account.ExtraUsage = &schema.ExtraUsage{
			Enabled:         env.ExtraUsage.Enabled,
			MonthlyLimitUSD: env.ExtraUsage.MonthlyLimit,
			UsedCreditsUSD:  env.ExtraUsage.UsedCredits,
			Utilization:     env.ExtraUsage.Utilization,
		}
	}
	if env.SevenDayOpus != nil {
		snap.Account.SevenDayOpus = &schema.RateWindow{UsedPercentage: env.SevenDayOpus.UsedPercentage, ResetsAt: env.SevenDayOpus.ResetsAt}
	}
	if env.SevenDaySonnet != nil {
		snap.Account.SevenDaySonnet = &schema.RateWindow{UsedPercentage: env.SevenDaySonnet.UsedPercentage, ResetsAt: env.SevenDaySonnet.ResetsAt}
	}
}

// dslMetricEntry is one metric in GET /api/dsl/metrics.
type dslMetricEntry struct {
	Name         string          `json:"name"`
	DisplayName  string          `json:"displayName"`
	Descriptions dslDescriptions `json:"descriptions"`
}

// handleDSLMetrics handles GET /api/dsl/metrics?tool=: the named-metric catalog
// (for when/color-rule editing), built from the dsl registry.
func (s *server) handleDSLMetrics(w http.ResponseWriter, r *http.Request) {
	tool := r.URL.Query().Get("tool")
	if !knownTool(tool) {
		writeError(w, http.StatusBadRequest, "unknown tool")
		return
	}
	metrics := dsl.Metrics(tool)
	out := make([]dslMetricEntry, 0, len(metrics))
	for _, m := range metrics {
		out = append(out, dslMetricEntry{
			Name:         m.Name,
			DisplayName:  m.DisplayName,
			Descriptions: dslDescriptions{EN: m.Descriptions.EN, JA: m.Descriptions.JA},
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"metrics": out})
}
