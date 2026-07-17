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
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/yacchi/statusloom/internal/cache"
	"github.com/yacchi/statusloom/internal/config"
	"github.com/yacchi/statusloom/internal/dsl"
	"github.com/yacchi/statusloom/internal/render"
	"github.com/yacchi/statusloom/internal/samples"
	"github.com/yacchi/statusloom/internal/schema"
	"github.com/yacchi/statusloom/internal/store"
)

// maxDSLBodyBytes bounds DSL request bodies (source text can be larger than a
// JSON config, so a slightly higher cap than maxConfigBodyBytes is used).
const maxDSLBodyBytes = 2 << 20 // 2MB

// knownTool reports whether tool is a tool statusloom can serve DSL for.
// "claude-code" is the only DSL-served tool: its <subagent> region (markup.md
// "subagent") folds the subagentStatusLine task-* fields into the unified
// document (plans/subagent-region-dsl.md), so there is no separate
// subagent-only tool id to accept here.
func knownTool(tool string) bool {
	return tool == string(schema.ToolClaudeCode)
}

// dslSourceVersion is the version webconfig reports for POST /api/dsl/parse
// and /api/dsl/serialize, whose request bodies carry no tool (only
// "claude-code" is DSL-served today; §3.3/§8.3 unify every echo-guard version
// on store.SourceVersion, so this is that single spelling under the one tool
// these two endpoints ever see).
func dslSourceVersion(src string) string {
	return store.SourceVersion(string(schema.ToolClaudeCode), src)
}

// handleGetDSLDocument handles GET /api/dsl/document?tool=: it returns tool's
// current committed source (from the internal store) and its version, or
// DefaultDocument with exists=false when the store holds no current revision
// for tool yet.
func (s *server) handleGetDSLDocument(w http.ResponseWriter, r *http.Request) {
	tool := r.URL.Query().Get("tool")
	if !knownTool(tool) {
		writeError(w, http.StatusBadRequest, "unknown tool")
		return
	}
	st, err := store.Open()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	rev, _, ok, err := st.Current(tool)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		src := config.DefaultDocument(tool)
		writeJSON(w, http.StatusOK, map[string]any{
			"source": src, "version": store.SourceVersion(tool, src), "exists": false,
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"source": rev.Source, "version": store.SourceVersion(tool, rev.Source), "exists": true,
	})
}

// dslDocumentPutRequest is the body of PUT /api/dsl/document and PUT
// /api/dsl/draft.
type dslSourcePutRequest struct {
	Tool   string `json:"tool"`
	Source string `json:"source"`
}

// handlePutDSLDocument handles PUT /api/dsl/document: it saves the posted
// source through the store's Save validation boundary (dsl.ParseAndValidate),
// which only commits a new revision (origin "ui") when there are no
// error-severity diagnostics. A document with errors is rejected with 409 and
// its diagnostics (no write, no new revision). Warning-only documents are
// saved. The response always carries the source version and the diagnostics.
func (s *server) handlePutDSLDocument(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeSourcePut(w, r)
	if !ok {
		return
	}

	version := store.SourceVersion(req.Tool, req.Source)

	st, err := store.Open()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	_, diags, err := st.Save(req.Tool, req.Source, "ui", store.Meta{})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if dsl.HasErrors(diags) {
		writeJSON(w, http.StatusConflict, map[string]any{
			"version":     version,
			"diagnostics": toDiagsJSON(diags),
		})
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
	doc, diags := dsl.ParseAndValidate(req.Source)
	resp := map[string]any{
		"diagnostics": toDiagsJSON(diags),
		"version":     dslSourceVersion(req.Source),
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
	_, diags := dsl.ParseAndValidate(src)
	writeJSON(w, http.StatusOK, map[string]any{
		"source":      src,
		"diagnostics": toDiagsJSON(diags),
	})
}

// handleGetDSLDraft handles GET /api/dsl/draft?tool=: it returns the tool's
// draft working node source from the internal store, falling back to the
// saved document and then the built-in default when no draft exists yet.
// exists reflects the draft's presence (not the fallback's).
func (s *server) handleGetDSLDraft(w http.ResponseWriter, r *http.Request) {
	tool := r.URL.Query().Get("tool")
	if !knownTool(tool) {
		writeError(w, http.StatusBadRequest, "unknown tool")
		return
	}
	st, err := store.Open()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if src, ok, err := st.ReadDraft(tool); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	} else if ok {
		writeJSON(w, http.StatusOK, map[string]any{
			"source": src, "version": store.SourceVersion(tool, src), "exists": true,
		})
		return
	}

	// No draft: fall back to the current committed document, then the
	// built-in default.
	rev, _, ok, err := st.Current(tool)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	src := config.DefaultDocument(tool)
	if ok {
		src = rev.Source
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"source": src, "version": store.SourceVersion(tool, src), "exists": false,
	})
}

// handlePutDSLDraft handles PUT /api/dsl/draft: it writes the posted source to
// the tool's draft working node in the internal store unconditionally
// (last-writer-wins). The draft tolerates in-progress, invalid input; parse
// diagnostics are returned for the editor's benefit but never block the
// write.
func (s *server) handlePutDSLDraft(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeSourcePut(w, r)
	if !ok {
		return
	}
	_, diags := dsl.ParseAndValidate(req.Source)
	st, err := store.Open()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := st.WriteDraft(req.Tool, req.Source); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"version":     store.SourceVersion(req.Tool, req.Source),
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
	// AllVariants renders every <responsive>'s variant (not just the
	// width-selected one) for the config editor's canvas, which needs every
	// candidate's real values rather than falling back to placeholder chips
	// for the non-selected ones. The default (false) keeps the original
	// selected-only behavior for the real statusline preview.
	AllVariants bool `json:"allVariants"`
	// Section selects which region of the document this request previews
	// (markup.md "subagent"; plans/subagent-region-dsl.md "Preview契約"):
	// "" / "main" (default) previews the main status line exactly as before
	// (the <subagent> region is never rendered here, matching the real
	// "claude" render pass). "subagent" instead renders the active layout's
	// <subagent> region(s), one row per task of a subagent sample, and
	// returns them in the response's subagentPreview map instead of lines.
	Section string `json:"section"`
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

// handlePreviewDSL handles POST /api/dsl/preview: it parses source, renders
// the requested layout, and returns per-line, per-node segments referenced by
// node ID (matching the AST from /api/dsl/parse), plus diagnostics and the
// fallback line. Invalid (unparseable) source yields empty lines and
// diagnostics only.
//
// Section (default "main") selects which region of the document is
// previewed. "subagent" delegates entirely to handleSubagentSectionPreview,
// whose response shape differs (subagentPreview instead of populated lines);
// see that function and DSL_API.md.
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

	doc, diags := dsl.ParseAndValidate(req.Source)
	if doc == nil || doc.Root == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"lines":       []dslPreviewLine{},
			"diagnostics": toDiagsJSON(diags),
		})
		return
	}

	opts := render.Options{Width: clampPreviewWidth(req.Width), Now: time.Now()}
	_, ids := buildAST(doc)

	if req.Section == "subagent" {
		s.handleSubagentSectionPreview(w, doc, ids, req, diags, opts)
		return
	}

	snap, ok := s.previewSnapshot(w, "main", req.Sample, req.SessionID)
	if !ok {
		return
	}

	var docLines []render.DocLine
	var selectedVariants map[string]int
	if req.AllVariants {
		// Every variant's real values, plus which one this width would
		// actually select — the config editor's canvas shows every
		// candidate instead of falling back to placeholder chips for the
		// non-selected variants (they'd otherwise have no preview data at
		// all, since RenderDocument only emits the selected one).
		var selected map[*dsl.ResponsiveNode]int
		withActiveLayout(doc, req.LayoutIndex, func() {
			docLines, selected = render.RenderDocumentPreview(snap, doc, opts)
		})
		selectedVariants = selectedVariantIDs(doc, selected)
	} else {
		withActiveLayout(doc, req.LayoutIndex, func() {
			docLines = render.RenderDocument(snap, doc, opts)
		})
	}

	lines := docLinesToPreviewLines(docLines, ids)
	allOmitted := allDocLinesOmitted(docLines)

	fallbackANSI := ""
	if allOmitted {
		withActiveLayout(doc, req.LayoutIndex, func() {
			// With every line omitted, RenderDocumentString returns the
			// fallback line (model + tool-version) for the previewed layout.
			fallbackANSI = render.RenderDocumentString(snap, doc, opts)
		})
	}

	resp := map[string]any{
		"lines":       lines,
		"diagnostics": toDiagsJSON(diags),
		"fallback":    map[string]any{"ansi": fallbackANSI, "active": allOmitted},
	}
	if req.AllVariants {
		if selectedVariants == nil {
			selectedVariants = map[string]int{}
		}
		resp["selectedVariants"] = selectedVariants
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleSubagentSectionPreview handles POST /api/dsl/preview when
// section=="subagent" (plans/subagent-region-dsl.md "Preview契約"): rather
// than the main status line, it renders every subagent-preview container of
// the (possibly layout-index-selected) active layout — one entry per
// <responsive> variant when the layout has one, or a single layout-level
// entry otherwise; see subagentContainers — against a subagent sample's task
// list (one rendered row per task), and returns them keyed by container node
// ID in subagentPreview instead of populating lines.
//
// selectedVariants is computed the same way as the main path's (rendered
// against the main-section default sample / sessionId, since the <subagent>
// region's own container selection always follows the same width-selected
// variant as the main lines — markup.md "subagent" 幅一貫性) whenever
// AllVariants was requested, so the editor can mark which container is the
// one the real statusline would actually use.
func (s *server) handleSubagentSectionPreview(w http.ResponseWriter, doc *dsl.Document, ids map[dsl.Node]string, req dslPreviewRequest, diags []dsl.Diagnostic, opts render.Options) {
	sampleName := req.Sample
	if sampleName == "" {
		sampleName = samples.DefaultForSection("subagent")
	}
	tasks, ok := samples.SubagentTasks(sampleName, time.Now())
	if !ok {
		writeError(w, http.StatusBadRequest, "unknown sample")
		return
	}

	var mainSnap schema.StatusSnapshot
	if req.AllVariants {
		var snapOK bool
		mainSnap, snapOK = s.previewSnapshot(w, "main", "", req.SessionID)
		if !snapOK {
			return
		}
	}

	subagentPreview := map[string][]dslPreviewLine{}
	var selectedVariants map[string]int
	withActiveLayout(doc, req.LayoutIndex, func() {
		if len(doc.Root.Layouts) == 0 {
			return
		}
		idx := clampLayoutIndex(doc, req.LayoutIndex)
		layout := doc.Root.Layouts[idx]
		layoutID := fmt.Sprintf("L%d", idx)
		for containerID, sub := range subagentContainers(layout, layoutID) {
			lines := make([]dslPreviewLine, 0, len(tasks))
			for i := range tasks {
				taskSnap := schema.StatusSnapshot{
					Tool:     schema.ToolSnapshot{ID: schema.ToolClaudeCode, Version: "2.1.210"},
					System:   schema.SystemSnapshot{Cwd: "/Users/dev/myapp"},
					Subagent: &tasks[i],
				}
				docLines := render.RenderSubagentNode(taskSnap, sub, doc, opts)
				lines = append(lines, docLinesToPreviewLines(docLines, ids)...)
			}
			subagentPreview[containerID] = lines
		}
		if req.AllVariants {
			_, selected := render.RenderDocumentPreview(mainSnap, doc, opts)
			selectedVariants = selectedVariantIDs(doc, selected)
		}
	})

	resp := map[string]any{
		"lines":           []dslPreviewLine{},
		"diagnostics":     toDiagsJSON(diags),
		"fallback":        map[string]any{"ansi": "", "active": false},
		"subagentPreview": subagentPreview,
	}
	if req.AllVariants {
		if selectedVariants == nil {
			selectedVariants = map[string]int{}
		}
		resp["selectedVariants"] = selectedVariants
	}
	writeJSON(w, http.StatusOK, resp)
}

// docLinesToPreviewLines converts rendered DocLines into the wire
// dslPreviewLine shape, labeling each segment with its AST node ID via ids
// (the buildAST-produced dsl.Node -> id map). Shared by the main and
// subagent-section preview paths so a segment's nodeId is computed identically
// in both.
func docLinesToPreviewLines(docLines []render.DocLine, ids map[dsl.Node]string) []dslPreviewLine {
	lines := make([]dslPreviewLine, 0, len(docLines))
	for _, dl := range docLines {
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
	return lines
}

// allDocLinesOmitted reports whether every line is omitted (main-path fallback
// trigger).
func allDocLinesOmitted(docLines []render.DocLine) bool {
	for _, dl := range docLines {
		if !dl.Omitted {
			return false
		}
	}
	return true
}

// selectedVariantIDs turns RenderDocumentPreview's *dsl.ResponsiveNode -> index
// selection map into the id-keyed form the response reports (responseIDs maps
// each <responsive> to its AST node ID; see responsiveIDs).
func selectedVariantIDs(doc *dsl.Document, selected map[*dsl.ResponsiveNode]int) map[string]int {
	if len(selected) == 0 {
		return nil
	}
	rids := responsiveIDs(doc.Root)
	out := make(map[string]int, len(selected))
	for rn, idx := range selected {
		if id, ok := rids[rn]; ok {
			out[id] = idx
		}
	}
	return out
}

// previewSnapshot resolves the snapshot a preview renders against: a real
// cached session (sessionId, as listed by GET /api/sessions), else a named
// sample (defaulting to samples.DefaultForSection(section)). It writes the
// error response and returns ok=false on failure.
func (s *server) previewSnapshot(w http.ResponseWriter, section, sample, sessionID string) (schema.StatusSnapshot, bool) {
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
		name = samples.DefaultForSection(section)
	}
	snap, ok := samples.Snapshot(name, time.Now())
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

// clampLayoutIndex clamps idx into [0, len(doc.Root.Layouts)-1] (0 for an
// empty document), matching withActiveLayout's own clamping so a caller that
// needs the layout's AST ID ("L{i}") independently — e.g.
// handleSubagentSectionPreview — computes the exact same index.
func clampLayoutIndex(doc *dsl.Document, idx int) int {
	n := len(doc.Root.Layouts)
	if n == 0 {
		return 0
	}
	if idx < 0 {
		return 0
	}
	if idx >= n {
		return n - 1
	}
	return idx
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
	idx = clampLayoutIndex(doc, idx)
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
// rendered preview (previewFor) produced against the "full" sample snapshot,
// which (since the merged claude-code catalog includes the task-* subagent
// fields, markup.md "subagent") carries a Subagent task too, so those fields
// preview real values instead of falling back to previewFallback.
func (s *server) handleDSLFields(w http.ResponseWriter, r *http.Request) {
	tool := r.URL.Query().Get("tool")
	if !knownTool(tool) {
		writeError(w, http.StatusBadRequest, "unknown tool")
		return
	}
	now := time.Now()
	snap, _ := samples.Snapshot(samples.Full, now)
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
	// Percent marks a 0..100-scale percentage metric (dsl.MetricDef.Percent);
	// omitted (false) for non-percent metrics.
	Percent bool `json:"percent,omitempty"`
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
			Percent:      m.Percent,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"metrics": out})
}
