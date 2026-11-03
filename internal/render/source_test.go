package render

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/yacchi/statusloom/internal/dsl"
	"github.com/yacchi/statusloom/internal/schema"
)

func sourceTestDoc(t *testing.T, ts int64) any {
	t.Helper()
	raw := fmt.Sprintf(`{"a":{"n":1234.5,"p":41.25,"d":312,"s":"42","b":true,"o":{},"arr":[1],"z":null,"ts":%d},"sessions":{"S1":{"v":7}}}`, ts)
	var doc any
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}
	return doc
}

func sourceTestEval(now time.Time, doc any, defined, available bool) docEval {
	snap := schema.StatusSnapshot{Session: schema.SessionSnapshot{ID: "S1"}}
	return docEval{
		snap: snap,
		opts: Options{
			Now: now,
			Sources: func(string) (any, bool, bool) {
				return doc, defined, available
			},
		},
		tool: "claude-code",
	}
}

func TestSourceFieldValues(t *testing.T) {
	now := time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC)
	doc := sourceTestDoc(t, now.Add(90*time.Minute).Unix())
	e := sourceTestEval(now, doc, true, true)

	cases := []struct {
		name   string
		sel    string
		format string
		prec   string
		want   string
		self   dsl.Value
	}{
		{"number format", "a.n", "number", "", "1,235", dsl.Value{Kind: dsl.ValueNumber, Num: 1234.5}},
		{"compact-number", "a.n", "compact-number", "", "1.2k", dsl.Value{Kind: dsl.ValueNumber, Num: 1234.5}},
		{"currency default precision", "a.n", "currency", "", "$1234.50", dsl.Value{Kind: dsl.ValueNumber, Num: 1234.5}},
		{"currency integer precision", "a.n", "currency", "1", "$1234.5", dsl.Value{Kind: dsl.ValueNumber, Num: 1234.5}},
		{"percent rounds like formatPercent", "a.p", "percent", "", "41.3%", dsl.Value{Kind: dsl.ValueNumber, Num: 41.25}},
		{"percent integer precision", "a.p", "percent", "2", "41.25%", dsl.Value{Kind: dsl.ValueNumber, Num: 41.25}},
		{"duration", "a.d", "duration", "", "5m 12s", dsl.Value{Kind: dsl.ValueNumber, Num: 312}},
		{"countdown minutes", "a.ts", "countdown", "", "1h30m", dsl.Value{Kind: dsl.ValueNumber, Num: 90}},
		{"datetime same day", "a.ts", "datetime", "", "11:30", dsl.Value{Kind: dsl.ValueNumber, Num: 90}},
		{"string kept verbatim under number format", "a.s", "number", "", "42", dsl.Value{Kind: dsl.ValueString, Str: "42"}},
		{"bool ignores format", "a.b", "percent", "", "true", dsl.Value{Kind: dsl.ValueBool, Bool: true}},
		{"session_id template", "sessions.${session_id}.v", "", "", "7", dsl.Value{Kind: dsl.ValueNumber, Num: 7}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := &dsl.FieldNode{Name: "source", Source: "x", Select: c.sel,
				Formatter: dsl.FormatterConfig{Name: c.format, Precision: c.prec}}
			got := e.sourceField(f)
			if got.text != c.want {
				t.Fatalf("text = %q, want %q", got.text, c.want)
			}
			if got.fallback {
				t.Fatalf("fallback = true for a present value")
			}
			if got.self.value == nil || *got.self.value != c.self {
				t.Fatalf("self value = %+v, want %+v", got.self.value, c.self)
			}
		})
	}
}

func TestSourceFieldAbsentValues(t *testing.T) {
	now := time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC)
	doc := sourceTestDoc(t, now.Add(90*time.Minute).Unix())

	cases := []struct {
		name string
		e    docEval
		sel  string
	}{
		{"object", sourceTestEval(now, doc, true, true), "a.o"},
		{"array", sourceTestEval(now, doc, true, true), "a.arr"},
		{"null", sourceTestEval(now, doc, true, true), "a.z"},
		{"missing path", sourceTestEval(now, doc, true, true), "missing"},
		{"syntax error", sourceTestEval(now, doc, true, true), "a["},
		{"undefined source", sourceTestEval(now, doc, false, false), "a.n"},
		{"defined but unavailable", sourceTestEval(now, doc, true, false), "a.n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := &dsl.FieldNode{Name: "source", Source: "x", Select: c.sel}
			got := c.e.sourceField(f)
			if got.text != "" || got.self.metric != "" || got.self.value != nil || got.fallback {
				t.Fatalf("absent value rendered %+v, want empty", got)
			}
		})
	}
}

func TestSourceFieldNilSourcesAndEmptyID(t *testing.T) {
	now := time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC)
	e := docEval{opts: Options{Now: now}, tool: "claude-code"}
	got := e.sourceField(&dsl.FieldNode{Name: "source", Source: "x", Select: "a"})
	if got.text != "" || got.self.value != nil {
		t.Fatalf("nil Sources rendered %+v", got)
	}
	called := false
	e.opts.Sources = func(string) (any, bool, bool) {
		called = true
		return nil, true, true
	}
	got = e.sourceField(&dsl.FieldNode{Name: "source", Select: "a"})
	if called || got.text != "" {
		t.Fatalf("empty source id must not call Sources; called=%v text=%q", called, got.text)
	}
}

func TestSourceFieldCountdownZeroNowHasNoFallback(t *testing.T) {
	now := time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC)
	doc := sourceTestDoc(t, now.Add(90*time.Minute).Unix())
	e := sourceTestEval(time.Time{}, doc, true, true)
	f := &dsl.FieldNode{Name: "source", Source: "x", Select: "a.ts",
		Formatter: dsl.FormatterConfig{Name: "countdown"}, Fallback: "model"}
	got := e.sourceField(f)
	if got.text != "" || got.self.value != nil || got.fallback {
		t.Fatalf("zero-clock countdown = %+v, want empty value with no fallback", got)
	}
}

func TestSourceFieldFallback(t *testing.T) {
	now := time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC)
	doc := sourceTestDoc(t, now.Unix())
	e := sourceTestEval(now, doc, true, true)
	e.snap.Session.Cost = &schema.CostUsage{USD: 1.5}

	cases := []struct {
		name   string
		format string
		prec   string
		raw    bool
		want   string
	}{
		{"currency default", "currency", "", false, "$1.50"},
		{"raw keeps plain number", "currency", "", true, "1.50"},
		{"integer precision carries over", "currency", "3", false, "$1.500"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := &dsl.FieldNode{Name: "source", Source: "x", Select: "a.missing", Fallback: "session-cost",
				Raw: c.raw, Formatter: dsl.FormatterConfig{Name: c.format, Precision: c.prec}}
			before := *f
			got := e.sourceField(f)
			if got.text != c.want || !got.fallback {
				t.Fatalf("got text=%q fallback=%v, want %q fallback=true", got.text, got.fallback, c.want)
			}
			if got.self.metric != "session-cost-usd" {
				t.Fatalf("self metric = %q, want session-cost-usd", got.self.metric)
			}
			if f.Name != before.Name || f.Source != before.Source || f.Select != before.Select || f.Fallback != before.Fallback {
				t.Fatalf("source field was mutated: %+v", *f)
			}
		})
	}

	// A fallback whose built-in field has no value renders empty but still counts as a fallback.
	got := e.sourceField(&dsl.FieldNode{Name: "source", Source: "x", Select: "a.missing", Fallback: "model"})
	if got.text != "" || !got.fallback {
		t.Fatalf("empty fallback = %+v, want empty text with fallback=true", got)
	}

	// A present value wins over the fallback.
	got = e.sourceField(&dsl.FieldNode{Name: "source", Source: "x", Select: "a.n", Fallback: "session-cost"})
	if got.fallback || got.text != "1234.5" {
		t.Fatalf("present value with fallback = %+v", got)
	}
}

func TestSourceAvailableAndStale(t *testing.T) {
	now := time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC)
	doc := sourceTestDoc(t, now.Unix())

	if sourceAvailable(Options{}, "x") {
		t.Fatal("nil Sources must not be available")
	}
	calls := 0
	calledOpts := Options{Sources: func(string) (any, bool, bool) {
		calls++
		return doc, true, true
	}}
	if sourceAvailable(calledOpts, "") || calls != 0 {
		t.Fatal("empty id must be unavailable without calling Sources")
	}
	if sourceAvailable(Options{Sources: func(string) (any, bool, bool) { return doc, false, false }}, "x") {
		t.Fatal("undefined source must not be available")
	}
	if sourceAvailable(Options{Sources: func(string) (any, bool, bool) { return doc, true, false }}, "x") {
		t.Fatal("defined but unavailable source must not be available")
	}
	if !sourceAvailable(Options{Sources: func(string) (any, bool, bool) { return doc, true, true }}, "x") {
		t.Fatal("defined and available source must be available")
	}

	if _, ok := sourceStaleMetric(Options{}, "x"); ok {
		t.Fatal("nil Sources must leave source-stale unresolved")
	}
	if _, ok := sourceStaleMetric(Options{Sources: func(string) (any, bool, bool) { return nil, false, false }}, "x"); ok {
		t.Fatal("undefined source must leave source-stale unresolved")
	}
	v, ok := sourceStaleMetric(Options{Sources: func(string) (any, bool, bool) { return doc, true, true }}, "x")
	if !ok || v.Kind != dsl.ValueBool || v.Bool {
		t.Fatalf("available source stale = %+v ok=%v, want false", v, ok)
	}
	v, ok = sourceStaleMetric(Options{Sources: func(string) (any, bool, bool) { return doc, true, false }}, "x")
	if !ok || v.Kind != dsl.ValueBool || !v.Bool {
		t.Fatalf("unavailable source stale = %+v ok=%v, want true", v, ok)
	}
}

func TestFormatDatetimeUsesNowLocation(t *testing.T) {
	jst := time.FixedZone("JST", 9*3600)
	now := time.Date(2026, 11, 3, 10, 0, 0, 0, jst)
	cases := []struct {
		name string
		t    time.Time
		want string
	}{
		{"same day late", time.Date(2026, 11, 3, 23, 59, 0, 0, jst), "23:59"},
		{"same day via UTC", time.Date(2026, 11, 3, 14, 0, 0, 0, time.UTC), "23:00"},
		{"next day via UTC", time.Date(2026, 11, 3, 15, 30, 0, 0, time.UTC), "11-04 00:30"},
		{"previous year", time.Date(2025, 11, 3, 10, 0, 0, 0, jst), "11-03 10:00"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := formatDatetime(c.t, now); got != c.want {
				t.Fatalf("formatDatetime = %q, want %q", got, c.want)
			}
		})
	}
}

func TestSourceNumberJSONNumber(t *testing.T) {
	now := time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC)
	doc := map[string]any{"a": map[string]any{"n": json.Number("12")}}
	e := sourceTestEval(now, doc, true, true)
	got := e.sourceField(&dsl.FieldNode{Name: "source", Source: "x", Select: "a.n"})
	if got.text != "12" || got.self.value == nil || *got.self.value != (dsl.Value{Kind: dsl.ValueNumber, Num: 12}) {
		t.Fatalf("json.Number field = %+v", got)
	}
}
