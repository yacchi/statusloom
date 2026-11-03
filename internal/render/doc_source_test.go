package render

import (
	"strings"
	"testing"
	"time"

	"github.com/yacchi/statusloom/internal/dsl"
	"github.com/yacchi/statusloom/internal/schema"
)

// dswLookup fakes Options.Sources: prof is available with a document, down is
// defined but unavailable, anything else is undefined.
func dswLookup() func(string) (any, bool, bool) {
	return func(id string) (any, bool, bool) {
		switch id {
		case "prof":
			return map[string]any{"name": "work", "pct": 42.0}, true, true
		case "down":
			return nil, true, false
		}
		return nil, false, false
	}
}

func dswOpts() Options {
	return Options{Width: 120, Now: fixedNow, Sources: dswLookup()}
}

func dswRender(t *testing.T, snap schema.StatusSnapshot, body string, opts Options) DocLine {
	t.Helper()
	lines := RenderDocument(snap, parseDoc(t, wrap(body)), opts)
	if len(lines) == 0 {
		t.Fatalf("no lines for %s", body)
	}
	return lines[0]
}

func dswText(t *testing.T, body string, opts Options) string {
	t.Helper()
	return lineText(dswRender(t, fullSnapshot(), body, opts))
}

func dswANSI16(t *testing.T, snap schema.StatusSnapshot, body string, opts Options) DocLine {
	t.Helper()
	src := strings.Replace(wrap(body), `color-level="none"`, `color-level="ansi16"`, 1)
	lines := RenderDocument(snap, parseDoc(t, src), opts)
	if len(lines) == 0 {
		t.Fatalf("no lines for %s", body)
	}
	return lines[0]
}

func dswLineANSI(dl DocLine) string {
	var b strings.Builder
	for _, s := range dl.Segments {
		b.WriteString(s.ANSI)
	}
	return b.String()
}

func TestDocSourceFieldSelfGate(t *testing.T) {
	cases := []struct{ body, want string }{
		{`<text>S</text><field name="source" source="prof" select="pct" prefix="[" suffix="]" when="self ge 40"/>`, "S[42]"},
		{`<text>S</text><field name="source" source="prof" select="pct" prefix="[" suffix="]" when="self lt 40"/>`, "S"},
		{`<text>S</text><field name="source" source="prof" select="name" when='self eq "work"'/>`, "Swork"},
	}
	for _, id := range []string{"missing", "down"} {
		for _, w := range []string{"self ge 0", "self ne 5", "not (self eq 5)"} {
			cases = append(cases, struct{ body, want string }{
				`<text>S</text><field name="source" source="` + id + `" select="pct" prefix="P" when="` + w + `"/>`, "S",
			})
		}
		cases = append(cases, struct{ body, want string }{
			`<text>S</text><field name="source" source="` + id + `" select="pct" prefix="P"/>`, "SP",
		})
	}
	for _, c := range cases {
		if got := dswText(t, c.body, dswOpts()); got != c.want {
			t.Errorf("%s: got %q, want %q", c.body, got, c.want)
		}
	}
}

func TestDocSourceFieldColorRules(t *testing.T) {
	body := func(id string) string {
		return `<field name="source" source="` + id + `" select="pct" prefix="P" color="green">` +
			`<color-rule when="self ge 40" color="red"/><color-rule when="not (self ge 40)" color="yellow"/></field>`
	}
	if got := dswLineANSI(dswANSI16(t, fullSnapshot(), body("prof"), dswOpts())); !strings.Contains(got, "\x1b[31m") {
		t.Errorf("prof: want red, got %q", got)
	}
	for _, id := range []string{"missing", "down"} {
		got := dswLineANSI(dswANSI16(t, fullSnapshot(), body(id), dswOpts()))
		if !strings.Contains(got, "\x1b[32m") || strings.Contains(got, "\x1b[31m") || strings.Contains(got, "\x1b[33m") {
			t.Errorf("%s: want base green only, got %q", id, got)
		}
	}
}

func TestDocSourceFieldFallbackParity(t *testing.T) {
	const attrs = `format="percent" precision="1" min-width="8" align="right" prefix="[" suffix="]" color="green" when="self ge 20"><color-rule when="self ge 20" color="yellow"/></field>`
	b := dswANSI16(t, fullSnapshot(), `<field name="five-hour-usage" `+attrs, dswOpts())
	if len(b.Segments) != 1 || b.Segments[0].Text != "[   27.0%]" || !strings.Contains(b.Segments[0].ANSI, "\x1b[33m") {
		t.Fatalf("built-in baseline unexpected: %+v", b.Segments)
	}
	nilSources := dswOpts()
	nilSources.Sources = nil
	cases := []struct {
		id, sel string
		opts    Options
	}{
		{"missing", "pct", dswOpts()},
		{"down", "pct", dswOpts()},
		{"prof", "nope", dswOpts()},
		{"prof", "pct", nilSources},
	}
	for _, c := range cases {
		a := dswANSI16(t, fullSnapshot(),
			`<field name="source" source="`+c.id+`" select="`+c.sel+`" fallback="five-hour-usage" `+attrs, c.opts)
		if len(a.Segments) != 1 {
			t.Errorf("%s/%s: want 1 segment, got %+v", c.id, c.sel, a.Segments)
			continue
		}
		seg := a.Segments[0]
		if seg.Text != b.Segments[0].Text || seg.ANSI != b.Segments[0].ANSI {
			t.Errorf("%s/%s: got %q/%q, want %q/%q", c.id, c.sel, seg.Text, seg.ANSI, b.Segments[0].Text, b.Segments[0].ANSI)
		}
		fn, ok := seg.Node.(*dsl.FieldNode)
		if !ok || fn.Name != "source" || fn.Fallback != "five-hour-usage" {
			t.Errorf("%s/%s: segment node is not the source field: %#v", c.id, c.sel, seg.Node)
		}
	}
	hidden := `<text>S</text><field name="source" source="missing" select="pct" fallback="five-hour-usage" ` +
		strings.Replace(attrs, `when="self ge 20"`, `when="self ge 50"`, 1)
	if got := dswText(t, hidden, dswOpts()); got != "S" {
		t.Errorf("fallback self ge 50: got %q, want S", got)
	}
}

func TestDocSourceOptional(t *testing.T) {
	nilSources := dswOpts()
	nilSources.Sources = nil
	cases := []struct {
		ref  string
		opts Options
		want string
	}{
		{"source:prof", dswOpts(), "SA"},
		{"source:down", dswOpts(), "S"},
		{"source:missing", dswOpts(), "S"},
		{"source", dswOpts(), "S"},
		{"source:prof", nilSources, "S"},
	}
	for _, c := range cases {
		body := `<text>S</text><span optional="` + c.ref + `"><text>A</text></span>`
		if got := dswText(t, body, c.opts); got != c.want {
			t.Errorf("optional=%q (sources nil=%v): got %q, want %q", c.ref, c.opts.Sources == nil, got, c.want)
		}
	}
	// Availability only: the select is not evaluated by optional.
	body := `<text>S</text><field name="source" source="prof" select="nope" optional="source:prof" prefix="P"/>`
	if got := dswText(t, body, dswOpts()); got != "SP" {
		t.Errorf("optional ignores select: got %q, want SP", got)
	}
}

func TestDocSourceStaleMetric(t *testing.T) {
	nilSources := dswOpts()
	nilSources.Sources = nil
	cases := []struct {
		id   string
		opts Options
		want string
	}{
		{"down", dswOpts(), "ST"},
		{"prof", dswOpts(), "SF"},
		{"missing", dswOpts(), "S"},
		{"prof", nilSources, "S"},
	}
	for _, c := range cases {
		body := `<text>S</text><span when="source-stale:` + c.id + `"><text>T</text></span>` +
			`<span when="not source-stale:` + c.id + `"><text>F</text></span>`
		if got := dswText(t, body, c.opts); got != c.want {
			t.Errorf("source-stale:%s (sources nil=%v): got %q, want %q", c.id, c.opts.Sources == nil, got, c.want)
		}
	}
}

func TestDocSourceVariantCandidacy(t *testing.T) {
	variant := func(when, text string) string {
		w := ""
		if when != "" {
			w = ` when="` + when + `"`
		}
		return `<variant` + w + `><line><text>` + text + `</text></line></variant>`
	}
	src := `<statusloom version="1" tool="claude-code" color-level="none"><layout name="l" active="true"><responsive>` +
		variant("self", "A") +
		variant("source-stale:missing", "B") +
		variant("not source-stale:missing", "C") +
		variant("source-stale:down", "D") +
		variant("", "E") +
		`</responsive></layout></statusloom>`
	doc := parseDoc(t, src)
	opts := Options{Now: fixedNow, Sources: dswLookup()}
	if got := RenderDocumentString(fullSnapshot(), doc, opts); got != "D" {
		t.Errorf("with sources: got %q, want D", got)
	}
	opts.Sources = nil
	if got := RenderDocumentString(fullSnapshot(), doc, opts); got != "E" {
		t.Errorf("sources nil: got %q, want E", got)
	}
}

func TestDocPromptCacheTypedMetrics(t *testing.T) {
	body := `<text>S</text><span when="cache-warm"><text>W</text></span><span when="not cache-warm"><text>C</text></span>` +
		`<span when='cache-ttl eq "1h"'><text>H</text></span><span when='cache-ttl ne "1h"'><text>N</text></span>`
	cases := []struct {
		pc   *schema.PromptCache
		want string
	}{
		{&schema.PromptCache{Warm: true, TTL: "1h"}, "SWH"},
		{&schema.PromptCache{Warm: false, TTL: "5m"}, "SCN"},
		{&schema.PromptCache{Warm: true}, "SW"},
		{nil, "S"},
	}
	for _, c := range cases {
		snap := fullSnapshot()
		snap.Session.PromptCache = c.pc
		if got := lineText(dswRender(t, snap, body, dswOpts())); got != c.want {
			t.Errorf("%+v: got %q, want %q", c.pc, got, c.want)
		}
	}
	for _, warm := range []bool{true, false} {
		snap := fullSnapshot()
		snap.Session.PromptCache = &schema.PromptCache{Warm: warm}
		want := "S"
		if warm {
			want = "Swarm"
		}
		if got := lineText(dswRender(t, snap, `<text>S</text><field name="cache-warm" when="self"/>`, dswOpts())); got != want {
			t.Errorf("cache-warm when=self warm=%v: got %q, want %q", warm, got, want)
		}
	}
}

func TestDocPromptCacheDatetime(t *testing.T) {
	at := func(d time.Duration) *time.Time {
		v := fixedNow.Add(d)
		return &v
	}
	cases := []struct {
		exp  *time.Time
		want string
	}{
		{at(30 * time.Minute), "S10:30"},
		{at(24*time.Hour + 5*time.Minute), "S07-12 10:05"},
		{nil, "S"},
	}
	for _, c := range cases {
		snap := fullSnapshot()
		snap.Session.PromptCache = &schema.PromptCache{Warm: true, TTL: "5m", ExpiresAt: c.exp}
		if got := lineText(dswRender(t, snap, `<text>S</text><field name="cache-expires-at" format="datetime"/>`, dswOpts())); got != c.want {
			t.Errorf("datetime %v: got %q, want %q", c.exp, got, c.want)
		}
		plain := lineText(dswRender(t, snap, `<text>S</text><field name="cache-expires-at"/>`, dswOpts()))
		countdown := lineText(dswRender(t, snap, `<text>S</text><field name="cache-expires-at" format="countdown"/>`, dswOpts()))
		if plain != countdown {
			t.Errorf("countdown %v: got %q, want default %q", c.exp, countdown, plain)
		}
	}
}
