package webconfig

// This file renders per-field previews for the DSL field catalog endpoint
// (GET /api/dsl/fields, see dsl.go). Field metadata itself comes from the dsl
// registry (the single source of truth); only the rendered sample lives here.

import (
	"strings"
	"time"

	"github.com/yacchi/statusloom/internal/dsl"
	"github.com/yacchi/statusloom/internal/render"
	"github.com/yacchi/statusloom/internal/schema"
)

// widgetPreview is a rendered sample of a field against the "full" sample
// snapshot, so the UI's palette can show what a field looks like before it is
// placed. Text is the plain rendering; ANSI is the same rendering at
// ColorLevel ansi16 (equal to Text unless the field self-styles - no per-field
// color is configured for previews).
type widgetPreview struct {
	Text string `json:"text"`
	ANSI string `json:"ansi"`
}

// previewFallback is used when a field renders empty even against the full
// sample snapshot.
const previewFallback = "(no sample)"

// previewWidth is the terminal width previews are rendered at.
const previewWidth = 120

// previewFor renders a single-field sample through the DSL render pipeline: a
// one-line document holding just <field name="..."/> at ColorLevel ansi16,
// against the full sample snapshot. Segment.Text is the plain rendering and
// Segment.ANSI the ansi16-styled one, so one RenderDocument call yields both.
func previewFor(fieldName string, snap schema.StatusSnapshot, now time.Time) widgetPreview {
	src := `<statusloom version="1" tool="claude-code" color-level="ansi16">` +
		`<layout name="preview" active="true"><line>` +
		`<field name="` + fieldName + `"/>` +
		`</line></layout></statusloom>`
	doc, diags := dsl.Parse(src)
	if doc == nil || doc.Root == nil || dsl.HasErrors(diags) {
		return widgetPreview{Text: previewFallback, ANSI: previewFallback}
	}
	lines := render.RenderDocument(snap, doc, render.Options{Width: previewWidth, Now: now})
	if len(lines) == 1 && !lines[0].Omitted {
		var text, ansi strings.Builder
		for _, seg := range lines[0].Segments {
			if seg.Visible {
				text.WriteString(seg.Text)
				ansi.WriteString(seg.ANSI)
			}
		}
		if text.Len() > 0 {
			return widgetPreview{Text: text.String(), ANSI: ansi.String()}
		}
	}
	return widgetPreview{Text: previewFallback, ANSI: previewFallback}
}
