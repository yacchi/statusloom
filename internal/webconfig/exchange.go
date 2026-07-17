package webconfig

// This file implements the Markdown exchange-format endpoints (*.sloom.md;
// plans/config-store-and-format.md §6, §8.13):
//
//	POST /api/exchange/import        — decode a raw Markdown body, validate,
//	                                    and save it as a new revision (origin
//	                                    "import")
//	GET  /api/exchange/export?tool=  — encode tool's current revision as
//	                                    Markdown
//
// Frontmatter/XML-fence parsing is internal/exchange's sole responsibility
// (a self-contained package with no store/config/dsl dependency); this file
// only wires Decode/Encode to the store's validation boundary (store.Save)
// and Current, mirroring PUT/GET /api/dsl/document's pattern (dsl.go). Like
// every /api/* path these run behind withSecurity (auth + Host/Origin;
// security.go).

import (
	"io"
	"net/http"

	"github.com/yacchi/statusloom/internal/dsl"
	"github.com/yacchi/statusloom/internal/exchange"
	"github.com/yacchi/statusloom/internal/schema"
	"github.com/yacchi/statusloom/internal/store"
)

// maxExchangeBodyBytes bounds the Markdown import body (the same order of
// magnitude as maxDSLBodyBytes; frontmatter plus a fenced DSL document is not
// meaningfully larger than raw DSL source).
const maxExchangeBodyBytes = 2 << 20 // 2MB

// handleImportExchange handles POST /api/exchange/import. Unlike every other
// /api/* POST/PUT here, the body is a raw *.sloom.md document (frontmatter +
// fenced DSL text), not JSON: the client posts the file's bytes verbatim. It
// is decoded via exchange.Decode, then the extracted DSL is parsed to learn
// which tool it targets (its own `<statusloom tool="...">` attribute — the
// request carries no separate tool field), and saved through the store's
// validation boundary (store.Save, origin "import") using the decoded meta
// (name/description/author). An invalid Markdown envelope, an unrecognized
// tool, or error-severity DSL diagnostics all reject the import with 409 (or
// 400 for a request-shape problem) and leave the store untouched; success
// (including a warning-only or dedup no-op save) returns 200 with the
// resulting revision id.
func (s *server) handleImportExchange(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxExchangeBodyBytes)
	data, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	meta, xmlSource, err := exchange.Decode(data)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	doc, diags := dsl.ParseAndValidate(xmlSource)
	if doc == nil || doc.Root == nil || doc.Root.Tool == "" {
		writeJSON(w, http.StatusConflict, map[string]any{
			"diagnostics": toDiagsJSON(diags),
		})
		return
	}
	tool := doc.Root.Tool
	if !knownTool(tool) {
		writeError(w, http.StatusBadRequest, "unknown tool")
		return
	}

	st, err := store.Open()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	id, saveDiags, err := st.Save(tool, xmlSource, "import", store.Meta{
		Name:        meta.Name,
		Description: meta.Description,
		Author:      meta.Author,
		Notes:       meta.Notes,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if dsl.HasErrors(saveDiags) {
		writeJSON(w, http.StatusConflict, map[string]any{
			"diagnostics": toDiagsJSON(saveDiags),
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"tool":        tool,
		"revision":    id,
		"diagnostics": toDiagsJSON(saveDiags),
	})
}

// handleExportExchange handles GET /api/exchange/export?tool=: it encodes
// tool's current committed revision (source + meta) as a *.sloom.md Markdown
// document (exchange.Encode) and returns it verbatim as text/markdown (not
// JSON — the response body is the file's exact bytes). A tool with no
// current revision is 404: there is deliberately no DefaultDocument
// fallback, since exporting the built-in default would misrepresent it as a
// saved configuration.
func (s *server) handleExportExchange(w http.ResponseWriter, r *http.Request) {
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
	rev, _, ok, err := st.Current(tool)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "no saved configuration for tool")
		return
	}

	md := exchange.Encode(exchange.Meta{
		Name:        rev.Meta.Name,
		Description: rev.Meta.Description,
		Author:      rev.Meta.Author,
		Notes:       rev.Meta.Notes,
	}, rev.Source)

	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(md)
}
