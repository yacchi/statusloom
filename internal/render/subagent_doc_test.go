package render

import (
	"testing"

	"github.com/yacchi/statusloom/internal/dsl"
)

// These tests exercise RenderSubagentLine/RenderSubagentNode against inline
// DSL fixtures rather than the shipped default claude-code document: adding
// a <subagent> region to that default is a separate, concurrently developed
// phase (plans/subagent-region-dsl.md phase 2b), and pinning these tests to
// it would create a byte-for-byte dependency between the two.

// subagentDocResponsive has one <layout> with a fixed <line>, a
// <responsive> whose wide variant carries its own <subagent> region and
// whose narrow variant carries none, and a layout-level <subagent> that acts
// as the fallback. It exercises every branch of RenderSubagentLine's
// container-selection rule in one document:
//   - wide width -> the wide variant's own <subagent>
//   - narrow width -> the narrow variant is selected, but it has no
//     <subagent>, so rendering falls back to the layout-level region
const subagentDocResponsive = `<statusloom version="1" tool="claude-code" color-level="none">
  <layout name="default" active="true">
    <responsive>
      <variant>
        <line><text>AAAAAAAAAAAAAAAAAAAA</text></line>
        <subagent><line><field name="task-description" prefix="wide: "/></line></subagent>
      </variant>
      <variant>
        <line><text>B</text></line>
      </variant>
    </responsive>
    <subagent><line><field name="task-description" prefix="fallback: "/></line></subagent>
  </layout>
</statusloom>`

func TestRenderSubagentLine_ResponsiveUsesSelectedVariantSubagent(t *testing.T) {
	doc := parseDoc(t, subagentDocResponsive)
	snap := subagentSnapshot()

	// Wide enough for the wide variant's single line (natural width 20) to
	// fit: the wide variant is selected and its own <subagent> renders.
	lines := RenderSubagentLine(snap, doc, Options{Width: 30, Now: fixedNow})
	if len(lines) != 1 {
		t.Fatalf("wide width: got %d lines, want 1", len(lines))
	}
	if got, want := lineText(lines[0]), "wide: Review render pipeline changes"; got != want {
		t.Errorf("wide width: got %q, want %q", got, want)
	}

	// Narrow: the wide variant no longer fits, so the narrow variant (no
	// <subagent> of its own) is selected, and rendering falls back to the
	// layout-level <subagent>.
	lines = RenderSubagentLine(snap, doc, Options{Width: 3, Now: fixedNow})
	if len(lines) != 1 {
		t.Fatalf("narrow width: got %d lines, want 1", len(lines))
	}
	if got, want := lineText(lines[0]), "fallback: Review render pipeline changes"; got != want {
		t.Errorf("narrow width: got %q, want %q (fallback to layout-level subagent)", got, want)
	}
}

// subagentDocPlain has no <responsive>: the layout's own <subagent> is the
// only candidate, at any width.
const subagentDocPlain = `<statusloom version="1" tool="claude-code" color-level="none">
  <layout name="default" active="true">
    <line><text>Model line</text></line>
    <subagent><line><field name="task-description"/></line></subagent>
  </layout>
</statusloom>`

func TestRenderSubagentLine_NoResponsiveUsesLayoutSubagent(t *testing.T) {
	doc := parseDoc(t, subagentDocPlain)
	snap := subagentSnapshot()

	for _, width := range []int{0, 10, 120} {
		lines := RenderSubagentLine(snap, doc, Options{Width: width, Now: fixedNow})
		if len(lines) != 1 {
			t.Fatalf("width=%d: got %d lines, want 1", width, len(lines))
		}
		if got, want := lineText(lines[0]), "Review render pipeline changes"; got != want {
			t.Errorf("width=%d: got %q, want %q", width, got, want)
		}
	}
}

// subagentDocUndefined has no <subagent> anywhere: the task has no subagent
// row at all.
const subagentDocUndefined = `<statusloom version="1" tool="claude-code" color-level="none">
  <layout name="default" active="true">
    <line><text>Model line</text></line>
  </layout>
</statusloom>`

func TestRenderSubagentLine_UndefinedYieldsEmpty(t *testing.T) {
	doc := parseDoc(t, subagentDocUndefined)
	snap := subagentSnapshot()

	lines := RenderSubagentLine(snap, doc, Options{Width: 120, Now: fixedNow})
	if len(lines) != 0 {
		t.Errorf("got %d lines, want 0 (no <subagent> region defined)", len(lines))
	}
}

func TestRenderSubagentLine_NilDocument(t *testing.T) {
	if lines := RenderSubagentLine(subagentSnapshot(), nil, Options{Width: 120, Now: fixedNow}); lines != nil {
		t.Errorf("nil document: got %v, want nil", lines)
	}
	if lines := RenderSubagentLine(subagentSnapshot(), &dsl.Document{}, Options{Width: 120, Now: fixedNow}); lines != nil {
		t.Errorf("nil root: got %v, want nil", lines)
	}
}

// TestRenderSubagentLine_TaskFieldsReflectSnapshot confirms task-* field
// values in a <subagent> region come from schema.StatusSnapshot.Subagent,
// through the same field-resolution/formatter plumbing the main pass uses.
func TestRenderSubagentLine_TaskFieldsReflectSnapshot(t *testing.T) {
	const src = `<statusloom version="1" tool="claude-code" color-level="none">
  <layout name="default" active="true">
    <line><text>Model line</text></line>
    <subagent>
      <line>
        <field name="task-description"/>
        <text> - </text>
        <field name="task-model"/>
        <text> - </text>
        <field name="task-tokens"/>
        <text> - </text>
        <field name="task-context-percent"/>
      </line>
    </subagent>
  </layout>
</statusloom>`
	doc := parseDoc(t, src)
	snap := subagentSnapshot()

	lines := RenderSubagentLine(snap, doc, Options{Width: 120, Now: fixedNow})
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1", len(lines))
	}
	want := "Review render pipeline changes - Opus 4.8 - 28,454 - 14.2%"
	if got := lineText(lines[0]); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestRenderSubagentNode_UsesGivenRegionRegardlessOfWidth confirms
// RenderSubagentNode renders exactly the region it is given, independent of
// opts.Width's variant-selection outcome: passing the wide variant's
// <subagent> at a narrow width (which RenderSubagentLine would resolve to
// the layout-level fallback for) still renders the wide variant's design.
func TestRenderSubagentNode_UsesGivenRegionRegardlessOfWidth(t *testing.T) {
	doc := parseDoc(t, subagentDocResponsive)
	snap := subagentSnapshot()

	layout := doc.Root.Layouts[0]
	responsive, ok := layout.Children[0].(*dsl.ResponsiveNode)
	if !ok {
		t.Fatalf("layout child 0 is not a *dsl.ResponsiveNode: %T", layout.Children[0])
	}
	wideSubagent := responsive.Variants[0].Subagent
	if wideSubagent == nil {
		t.Fatal("wide variant has no <subagent>; fixture changed?")
	}

	lines := RenderSubagentNode(snap, wideSubagent, doc, Options{Width: 3, Now: fixedNow})
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1", len(lines))
	}
	if got, want := lineText(lines[0]), "wide: Review render pipeline changes"; got != want {
		t.Errorf("got %q, want %q (region given directly, not re-resolved by width)", got, want)
	}
}

func TestRenderSubagentNode_NilOrEmptyRegion(t *testing.T) {
	doc := parseDoc(t, subagentDocPlain)
	snap := subagentSnapshot()
	opts := Options{Width: 120, Now: fixedNow}

	if lines := RenderSubagentNode(snap, nil, doc, opts); lines != nil {
		t.Errorf("nil region: got %v, want nil", lines)
	}
	if lines := RenderSubagentNode(snap, &dsl.SubagentNode{}, doc, opts); lines != nil {
		t.Errorf("region with nil Line: got %v, want nil", lines)
	}
	if lines := RenderSubagentNode(snap, &dsl.SubagentNode{}, nil, opts); lines != nil {
		t.Errorf("nil document: got %v, want nil", lines)
	}
}

// lineText concatenates a DocLine's segments' plain text, mirroring
// joinVisibleLines' ANSI-joining shape but without escapes (color-level
// "none" in these fixtures already means Text == ANSI, but Text reads more
// naturally in test expectations).
func lineText(dl DocLine) string {
	var out string
	for _, s := range dl.Segments {
		out += s.Text
	}
	return out
}
