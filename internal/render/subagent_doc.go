package render

// This file implements the subagent render pass: turning a DSL document's
// <subagent> region (markup.md "subagent") into the single status line drawn
// for one subagentStatusLine task. It shares the same evaluation plumbing as
// the main pass (docEval / renderLineDoc) in doc.go — a <subagent> region's
// <line> is an ordinary line, just scoped to task-* fields and resolved
// through a different container-selection rule.

import (
	"github.com/yacchi/statusloom/internal/dsl"
	"github.com/yacchi/statusloom/internal/schema"
)

// RenderSubagentLine renders one subagentStatusLine task's row from the
// active layout's <subagent> region, resolving which region to use per
// markup.md's subagent container-selection rule:
//
//  1. Take the active layout.
//  2. If the layout has a <responsive> container, take the variant selected
//     for opts.Width (the same first-fit selection RenderDocument's main
//     pass would use) and its <subagent> region.
//  3. If the layout has no <responsive>, or the selected variant has no
//     <subagent> region, fall back to the layout's own <subagent> region.
//  4. If neither resolves, the task has no subagent row: an empty slice
//     (the caller treats this exactly like an all-omitted render — no
//     output line for that task).
//
// Because the main pass and the subagent pass both key their <responsive>
// selection off the same opts.Width, they always agree on which variant is
// active — the width consistency the design (plans/subagent-region-dsl.md)
// requires structurally, without extra bookkeeping.
//
// snap.Subagent is assumed non-nil (the caller is rendering one task); if the
// document or its active layout can't be resolved, this returns nil.
func RenderSubagentLine(snap schema.StatusSnapshot, doc *dsl.Document, opts Options) []DocLine {
	if doc == nil || doc.Root == nil {
		return nil
	}
	layout := activeDocLayout(doc.Root)
	if layout == nil {
		return nil
	}
	e := newDocEval(snap, doc, opts)
	sub := e.resolveSubagentRegion(layout)
	return renderSubagentRegion(e, sub)
}

// RenderSubagentNode renders one subagentStatusLine task's row from a
// specific, already-resolved <subagent> region, independent of any width
// selection. This is the preview-facing counterpart to RenderSubagentLine:
// the config editor knows exactly which container (a layout or a particular
// variant) it wants to preview and supplies that container's Subagent field
// directly, rather than re-deriving it from opts.Width.
//
// A nil sub, or a sub with no <line> (structurally impossible after
// validation, but defensively handled), yields an empty slice.
func RenderSubagentNode(snap schema.StatusSnapshot, sub *dsl.SubagentNode, doc *dsl.Document, opts Options) []DocLine {
	if sub == nil || sub.Line == nil {
		return nil
	}
	if doc == nil || doc.Root == nil {
		return nil
	}
	e := newDocEval(snap, doc, opts)
	return renderSubagentRegion(e, sub)
}

// resolveSubagentRegion implements RenderSubagentLine's container-selection
// rule (steps 2-3 of its doc comment) given the active layout. It returns nil
// when neither the selected variant nor the layout itself has a <subagent>
// region.
func (e *docEval) resolveSubagentRegion(layout *dsl.LayoutNode) *dsl.SubagentNode {
	for _, ch := range layout.Children {
		r, ok := ch.(*dsl.ResponsiveNode)
		if !ok {
			continue
		}
		idx := e.selectedVariantIndex(r)
		if idx >= 0 && idx < len(r.Variants) && r.Variants[idx].Subagent != nil {
			return r.Variants[idx].Subagent
		}
		// The layout has a <responsive>, but the selected variant has no
		// <subagent> of its own: fall back to the layout-level region.
		return layout.Subagent
	}
	// No <responsive> container: the layout-level region (possibly nil) is
	// the only candidate.
	return layout.Subagent
}

// renderSubagentRegion renders a resolved <subagent> region's single <line>
// into one DocLine, or returns nil when there is no region to render.
func renderSubagentRegion(e docEval, sub *dsl.SubagentNode) []DocLine {
	if sub == nil || sub.Line == nil {
		return nil
	}
	return []DocLine{e.renderLineDoc(sub.Line)}
}
