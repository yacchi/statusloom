package render

import (
	"strings"
	"testing"

	"github.com/yacchi/statusloom/internal/dsl"
	"github.com/yacchi/statusloom/internal/schema"
)

// respFitDoc has one <responsive> with three single-line variants of natural
// widths 10 / 5 / 1 (fixed <text> content), so first-fit selection is exact.
const respFitDoc = `<statusloom version="1" tool="claude-code" color-level="none">
  <layout name="a" active="true">
    <responsive>
      <variant><line><text>AAAAAAAAAA</text></line></variant>
      <variant><line><text>BBBBB</text></line></variant>
      <variant><line><text>C</text></line></variant>
    </responsive>
  </layout>
</statusloom>`

func TestRenderResponsive_FirstFitSelection(t *testing.T) {
	cases := []struct {
		name  string
		width int
		want  string
	}{
		{"wide picks first", 20, "AAAAAAAAAA"},
		{"medium picks middle", 7, "BBBBB"},
		{"narrow picks last-fitting", 3, "C"},
		{"unknown width picks first", 0, "AAAAAAAAAA"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := renderDocStr(t, respFitDoc, fullSnapshot(), Options{Width: tc.width, Now: fixedNow})
			if got != tc.want {
				t.Errorf("width=%d: got %q, want %q", tc.width, got, tc.want)
			}
		})
	}
}

// TestRenderResponsive_NoneFitPicksLast: when no variant fits, the last
// (most-compact) variant is used even though it too overflows.
func TestRenderResponsive_NoneFitPicksLast(t *testing.T) {
	src := `<statusloom version="1" tool="claude-code" color-level="none">
  <layout name="a" active="true">
    <responsive>
      <variant><line><text>AAAAAAAAAAAAAAAAAAAAAAAAAAAAAA</text></line></variant>
      <variant><line><text>BBBBBBBBBBBBBBBBBBBB</text></line></variant>
      <variant><line><text>CCCCCCCCCCCCCCC</text></line></variant>
    </responsive>
  </layout>
</statusloom>`
	got := renderDocStr(t, src, fullSnapshot(), Options{Width: 10, Now: fixedNow})
	if want := "CCCCCCCCCCCCCCC"; got != want {
		t.Errorf("got %q, want %q (last variant)", got, want)
	}
}

// TestRenderResponsive_MultiLineVariantFit: a variant fits only when ALL its
// lines fit; a two-line variant is rejected when its widest line overflows.
func TestRenderResponsive_MultiLineVariantFit(t *testing.T) {
	src := `<statusloom version="1" tool="claude-code" color-level="none">
  <layout name="a" active="true">
    <responsive>
      <variant>
        <line><text>AAAAAAAA</text></line>
        <line><text>BBBBBBBBBBBB</text></line>
      </variant>
      <variant><line><text>CCCCCC</text></line></variant>
    </responsive>
  </layout>
</statusloom>`

	// width 15: both lines of variant 0 fit (8 and 12 <= 15).
	if got, want := renderDocStr(t, src, fullSnapshot(), Options{Width: 15, Now: fixedNow}), "AAAAAAAA\nBBBBBBBBBBBB"; got != want {
		t.Errorf("width=15: got %q, want %q", got, want)
	}
	// width 10: variant 0's second line (12) overflows -> variant 1.
	if got, want := renderDocStr(t, src, fullSnapshot(), Options{Width: 10, Now: fixedNow}), "CCCCCC"; got != want {
		t.Errorf("width=10: got %q, want %q", got, want)
	}
}

// TestRenderResponsive_FlexCountsAsZeroWidth: a variant's flex piece is width 0
// for fit selection, so the variant is chosen on its fixed content alone and
// the flex then expands to fill.
func TestRenderResponsive_FlexCountsAsZeroWidth(t *testing.T) {
	src := `<statusloom version="1" tool="claude-code" color-level="none">
  <layout name="a" active="true">
    <responsive>
      <variant><line><text>XX</text><flex/></line></variant>
      <variant><line><text>Y</text></line></variant>
    </responsive>
  </layout>
</statusloom>`
	// Natural width of variant 0 is 2 (flex = 0), so it fits width 5 and the
	// flex fills the remaining 3 columns.
	if got, want := renderDocStr(t, src, fullSnapshot(), Options{Width: 5, Now: fixedNow}), "XX   "; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestRenderResponsive_FixedLineOrdering: a fixed <line> and a <responsive> in
// a layout render in document order, with the responsive contributing its
// selected variant's lines in place.
func TestRenderResponsive_FixedLineOrdering(t *testing.T) {
	src := `<statusloom version="1" tool="claude-code" color-level="none">
  <layout name="a" active="true">
    <line><text>HEAD</text></line>
    <responsive>
      <variant><line><text>WIDE</text></line></variant>
      <variant><line><text>N</text></line></variant>
    </responsive>
  </layout>
</statusloom>`
	if got, want := renderDocStr(t, src, fullSnapshot(), Options{Width: 2, Now: fixedNow}), "HEAD\nN"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestRenderDocumentPreview_AllVariantsWithSelection: unlike RenderDocument
// (which emits only the width-selected variant's lines), RenderDocumentPreview
// renders every variant's lines and separately reports which index the same
// width would select — the config editor renders every candidate with the
// real one marked, instead of falling back to ghost/placeholder chips for the
// non-selected variants.
func TestRenderDocumentPreview_AllVariantsWithSelection(t *testing.T) {
	src := `<statusloom version="1" tool="claude-code" color-level="none">
  <layout name="a" active="true">
    <responsive>
      <variant><line><text>AAAAAAAAAA</text></line></variant>
      <variant><line><text>BBBBB</text></line></variant>
      <variant><line><text>C</text></line></variant>
    </responsive>
  </layout>
</statusloom>`
	doc := parseDoc(t, src)
	opts := Options{Width: 7, Now: fixedNow}
	lines, selected := RenderDocumentPreview(fullSnapshot(), doc, opts)

	// (a) every variant's lines are present, not just the selected one.
	var got []string
	for _, ln := range lines {
		var b strings.Builder
		for _, seg := range ln.Segments {
			b.WriteString(seg.Text)
		}
		got = append(got, b.String())
	}
	want := []string{"AAAAAAAAAA", "BBBBB", "C"}
	if len(got) != len(want) {
		t.Fatalf("lines = %v, want %v", got, want)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("line %d = %q, want %q", i, got[i], w)
		}
	}

	// (b) the selection map reports the same index RenderDocument (via
	// selectVariant/selectedVariantIndex) would actually pick at width=7:
	// variant 0 (10 chars) overflows, variant 1 (5 chars) fits -> index 1.
	if len(selected) != 1 {
		t.Fatalf("selected = %v, want exactly one responsive entry", selected)
	}
	responsive := doc.Root.Layouts[0].Children[0].(*dsl.ResponsiveNode)
	idx, ok := selected[responsive]
	if !ok {
		t.Fatalf("selected map missing the responsive node: %v", selected)
	}
	if idx != 1 {
		t.Errorf("selected index = %d, want 1", idx)
	}

	// Cross-check against RenderDocument's own choice for the same width.
	renderWant := renderDocStr(t, src, fullSnapshot(), opts)
	if renderWant != want[idx] {
		t.Fatalf("sanity: RenderDocument picked %q, preview map points at %q", renderWant, want[idx])
	}
}

// respWhenDoc gates its two wide variants on the account type; the last
// variant is unconditional, so there is always at least one candidate.
const respWhenDoc = `<statusloom version="1" tool="claude-code" color-level="none">
  <layout name="a" active="true">
    <responsive>
      <variant when="account-type eq &#34;claude_team&#34;"><line><text>TEAM</text></line></variant>
      <variant when="account-type eq &#34;claude_max&#34;"><line><text>MAX</text></line></variant>
      <variant><line><text>ANY</text></line></variant>
    </responsive>
  </layout>
</statusloom>`

func accountOpts(width int, accountType string) Options {
	opts := Options{Width: width, Now: fixedNow}
	if accountType != "" {
		opts.Profile = func() *schema.AccountProfile {
			return &schema.AccountProfile{Type: accountType}
		}
	}
	return opts
}

// A variant's `when` decides CANDIDACY: only the variants whose condition
// holds take part in the width first-fit, and an unresolvable metric (no
// account profile at all) gates the variant out rather than matching.
func TestRenderResponsive_WhenGatesCandidacy(t *testing.T) {
	cases := []struct {
		name        string
		accountType string
		want        string
	}{
		{"team account picks the team variant", "claude_team", "TEAM"},
		{"max account skips team and picks max", "claude_max", "MAX"},
		{"unknown account value falls through to the unconditional variant", "claude_other", "ANY"},
		{"no profile at all falls through to the unconditional variant", "", "ANY"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := renderDocStr(t, respWhenDoc, fullSnapshot(), accountOpts(80, tc.accountType))
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// The width fallback ("no candidate fits -> the last one") must pick the last
// ELIGIBLE variant, never a gated-out one.
func TestRenderResponsive_WhenGatedVariantNeverUsedAsWidthFallback(t *testing.T) {
	src := `<statusloom version="1" tool="claude-code" color-level="none">
  <layout name="a" active="true">
    <responsive>
      <variant><line><text>AAAAAAAAAAAAAAA</text></line></variant>
      <variant when="account-type eq &#34;claude_team&#34;"><line><text>TTTTTTTTTT</text></line></variant>
    </responsive>
  </layout>
</statusloom>`
	// Width 5: neither variant fits. On a max account the team variant is not a
	// candidate, so the fallback is variant 0.
	if got := renderDocStr(t, src, fullSnapshot(), accountOpts(5, "claude_max")); got != "AAAAAAAAAAAAAAA" {
		t.Errorf("max account: got %q, want the unconditional variant", got)
	}
	// On a team account it is a candidate, and being last it becomes the
	// fallback.
	if got := renderDocStr(t, src, fullSnapshot(), accountOpts(5, "claude_team")); got != "TTTTTTTTTT" {
		t.Errorf("team account: got %q, want the gated variant", got)
	}
}

// Every variant gated out: the <responsive> contributes no lines at all, so
// the document falls back exactly as it would with no visible content.
func TestRenderResponsive_AllVariantsGatedOut(t *testing.T) {
	src := `<statusloom version="1" tool="claude-code" color-level="none">
  <layout name="a" active="true">
    <responsive>
      <variant when="account-type eq &#34;claude_team&#34;"><line><text>TEAM</text></line></variant>
    </responsive>
  </layout>
</statusloom>`
	lines := RenderDocument(fullSnapshot(), parseDoc(t, src), accountOpts(80, "claude_max"))
	if len(lines) != 0 {
		t.Fatalf("lines = %v, want none (every variant gated out)", lines)
	}
}

// The unknown-width path (Width <= 0) must also respect the gate: it picks the
// first ELIGIBLE variant, not simply variant 0.
func TestRenderResponsive_WhenGateAppliesAtUnknownWidth(t *testing.T) {
	got := renderDocStr(t, respWhenDoc, fullSnapshot(), accountOpts(0, "claude_max"))
	if want := "MAX"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
