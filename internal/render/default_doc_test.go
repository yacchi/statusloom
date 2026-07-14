package render

// This test guards markup.md's requirement that the built-in DSL default
// document (config.DefaultDocument) renders identically to what migrating the
// built-in legacy Default() preset (config.MigrateFromLegacy) produces:
// the hand-authored default document and the migration path must agree.
// Equivalence is asserted on plain text only (ANSI bytes are covered by the
// doc_rendering golden), across several representative fixtures so the optional
// "5h:"/"7d:" usage labels and separator collapsing are exercised in both the
// populated and empty cases.

import (
	"strings"
	"testing"

	"github.com/yacchi/statusloom/internal/config"
	"github.com/yacchi/statusloom/internal/dsl"
	"github.com/yacchi/statusloom/internal/schema"
)

// docPlain renders a DSL document to newline-joined plain text.
func docPlain(t *testing.T, src string, snap schema.StatusSnapshot, opts Options) string {
	t.Helper()
	doc, diags := dsl.Parse(src)
	if doc == nil {
		t.Fatalf("Parse returned nil document; diags=%v", diags)
	}
	diags = append(diags, dsl.Validate(doc)...)
	if dsl.HasErrors(diags) {
		t.Fatalf("default document has validation errors: %v", diags)
	}
	lines := RenderDocument(snap, doc, opts)
	var out []string
	for _, l := range lines {
		if l.Omitted {
			continue
		}
		var b strings.Builder
		for _, s := range l.Segments {
			if s.Visible {
				b.WriteString(s.Text)
			}
		}
		out = append(out, b.String())
	}
	return strings.Join(out, "\n")
}

// TestDefaultDocument_PlainMatchesMigratedDefault asserts that the built-in
// default document and the migration of the built-in legacy Default() preset
// render to the same plain text across every fixture. The migration form uses
// " | " separator values and prefix-based labels rather than the hand-authored
// default document, so this is a meaningful agreement check between the two
// independent producers of the "default" configuration.
func TestDefaultDocument_PlainMatchesMigratedDefault(t *testing.T) {
	opts := Options{Now: fixedNow}
	defaultSrc := config.DefaultDocument("claude-code")

	doc, warnings := config.MigrateFromLegacy(*config.Default(), "claude-code")
	for _, w := range warnings {
		if strings.HasPrefix(w, "error:") {
			t.Fatalf("migration produced an error diagnostic: %s", w)
		}
	}

	cases := map[string]schema.StatusSnapshot{
		"full":           fullSnapshot(),
		"no-rate-limits": noRateLimitsSnapshot(),
		"minimal":        minimalSnapshot(),
	}
	for name, snap := range cases {
		t.Run(name, func(t *testing.T) {
			want := docPlain(t, defaultSrc, snap, opts)
			got := docPlain(t, doc.Source, snap, opts)
			if got != want {
				t.Errorf("plain output mismatch\ndefault doc: %q\nmigrated:    %q", want, got)
			}
		})
	}
}

// TestMigratedTemplate_LabelHiddenWhenValueEmpty pins the optional-gating
// behavior directly: a migrated templated field ("5h: {value}") must render
// nothing at all — label included — when its value is empty, exactly like the
// legacy template did.
func TestMigratedTemplate_LabelHiddenWhenValueEmpty(t *testing.T) {
	opts := Options{Now: fixedNow}
	cfg := *config.Default()
	tool := cfg.Tools["claude-code"]
	tool.Layouts = []config.Layout{{
		Name: "Default",
		Lines: [][]config.WidgetSpec{{
			{Type: "model"},
			{Type: "separator"},
			{Type: "five-hour-usage", Template: "5h: {value}"},
		}},
	}}
	cfg.Tools["claude-code"] = tool
	doc, _ := config.MigrateFromLegacy(cfg, "claude-code")

	// Value present: label renders.
	if got := docPlain(t, doc.Source, fullSnapshot(), opts); !strings.Contains(got, "5h: 27%") {
		t.Errorf("populated: %q, want it to contain %q", got, "5h: 27%")
	}
	// Value empty: neither label nor trailing separator remains.
	got := docPlain(t, doc.Source, noRateLimitsSnapshot(), opts)
	if strings.Contains(got, "5h:") {
		t.Errorf("empty value: %q, want no %q label", got, "5h:")
	}
	if want := "Opus"; got != want {
		t.Errorf("empty value: %q, want %q (separator collapsed too)", got, want)
	}
}

// noRateLimitsSnapshot is fullSnapshot without any account rate-limit data,
// so the usage/reset fields (and their "5h:"/"7d:" labels) are all empty.
func noRateLimitsSnapshot() schema.StatusSnapshot {
	s := fullSnapshot()
	s.Account = schema.AccountSnapshot{}
	return s
}

// minimalSnapshot has only a model and version, so most fields are empty and
// the fallback-line boundary behavior of separators is exercised.
func minimalSnapshot() schema.StatusSnapshot {
	return schema.StatusSnapshot{
		Tool: schema.ToolSnapshot{ID: schema.ToolClaudeCode, Version: "2.1.153"},
		Session: schema.SessionSnapshot{
			ID:    "sess-1",
			Model: &schema.ModelInfo{ID: "claude-opus", DisplayName: "Opus"},
		},
	}
}
