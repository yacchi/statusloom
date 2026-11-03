package render

import (
	"encoding/json"
	"strconv"
	"time"

	"github.com/yacchi/statusloom/internal/dsl"
	"github.com/yacchi/statusloom/internal/jsonselect"
)

// selfRef is what `self` resolves to on one node. It replaces the self-metric name
// string threaded through gate / condTrue / resolveColorRules / docResolver.
type selfRef struct {
	metric string     // a metric name resolved through docResolver ("" = none)
	value  *dsl.Value // a direct value (a present source value); wins over metric
}

// selfMetricRef wraps a self-metric name; "" yields a selfRef whose self is unresolvable.
func selfMetricRef(metric string) selfRef {
	return selfRef{metric: metric}
}

// sourceFieldResult is how one <field name="source"> renders.
type sourceFieldResult struct {
	text     string  // display text before min-width padding (§3); "" when there is nothing to show
	self     selfRef // §4
	fallback bool    // true when the text came from the fallback field
}

// sourceField resolves a source field: value lookup (§2), display (§3), self (§4) and
// fallback rendering (§5). It reads e.snap, e.opts, e.tool and may call the existing
// e.fieldText / e.selfMetric for the fallback.
func (e *docEval) sourceField(f *dsl.FieldNode) sourceFieldResult {
	if v, ok := e.sourceValue(f); ok {
		return e.sourcePresent(f, v)
	}
	if f.Fallback == "" {
		return sourceFieldResult{}
	}
	// Shallow copy: the fallback renders as the built-in field it names, with the
	// same format/precision/currency/raw/unmask attributes, and never recurses.
	fb := *f
	fb.Name = f.Fallback
	fb.Source = ""
	fb.Select = ""
	fb.Fallback = ""
	return sourceFieldResult{
		text:     e.fieldText(&fb),
		self:     selfMetricRef(e.selfMetric(f.Fallback)),
		fallback: true,
	}
}

// sourceAvailable backs optional="source:<id>": true iff opts.Sources != nil, id != ""
// and Sources(id) reports defined && available.
func sourceAvailable(opts Options, id string) bool {
	if opts.Sources == nil || id == "" {
		return false
	}
	_, defined, available := opts.Sources(id)
	return defined && available
}

// sourceStaleMetric backs the source-stale:<id> metric: ok=false (unresolved) when
// opts.Sources is nil, id is "" or the source is undefined; otherwise
// dsl.Value{Kind: dsl.ValueBool, Bool: !available}, true.
func sourceStaleMetric(opts Options, id string) (dsl.Value, bool) {
	if opts.Sources == nil || id == "" {
		return dsl.Value{}, false
	}
	_, defined, available := opts.Sources(id)
	if !defined {
		return dsl.Value{}, false
	}
	return dsl.Value{Kind: dsl.ValueBool, Bool: !available}, true
}

// formatDatetime renders t in now's location: "15:04" when t falls on the same calendar
// day as now, otherwise "01-02 15:04". Backs format="datetime" for source fields and
// for cache-expires-at (pc-render).
func formatDatetime(t, now time.Time) string {
	lt := t.In(now.Location())
	ly, lm, ld := lt.Date()
	ny, nm, nd := now.Date()
	if ly == ny && lm == nm && ld == nd {
		return lt.Format("15:04")
	}
	return lt.Format("01-02 15:04")
}

// sourceValue looks up a source field's selected value (§2). Only scalars are
// present; objects, arrays, null and any lookup failure are absent. The document
// is decoded once and never re-marshalled.
func (e *docEval) sourceValue(f *dsl.FieldNode) (any, bool) {
	if e.opts.Sources == nil || f.Source == "" {
		return nil, false
	}
	doc, defined, available := e.opts.Sources(f.Source)
	if !defined || !available {
		return nil, false
	}
	expr, err := jsonselect.Parse(f.Select)
	if err != nil {
		return nil, false
	}
	raw, ok := expr.Eval(doc, SelectVars(e.snap, e.opts.Env))
	if !ok {
		return nil, false
	}
	switch x := raw.(type) {
	case string, bool, float64:
		return x, true
	case json.Number:
		n, err := x.Float64()
		if err != nil {
			return nil, false
		}
		return n, true
	}
	return nil, false
}

// sourcePresent renders a present scalar value (§3) and derives its self (§4).
func (e *docEval) sourcePresent(f *dsl.FieldNode, v any) sourceFieldResult {
	switch x := v.(type) {
	case bool:
		return sourceFieldResult{
			text: strconv.FormatBool(x),
			self: selfRef{value: &dsl.Value{Kind: dsl.ValueBool, Bool: x}},
		}
	case string:
		return sourceFieldResult{
			text: x,
			self: selfRef{value: &dsl.Value{Kind: dsl.ValueString, Str: x}},
		}
	case float64:
		return sourceFieldResult{
			text: e.sourceNumberText(f, x),
			self: e.sourceNumberSelf(f, x),
		}
	}
	return sourceFieldResult{}
}

// sourceNumberText is the display text of a present number (§3). It mirrors the
// precision handling of applyFormat (doc_style.go).
func (e *docEval) sourceNumberText(f *dsl.FieldNode, v float64) string {
	prec := f.Formatter.Precision
	switch f.Formatter.Name {
	case "percent":
		if p, err := strconv.Atoi(prec); err == nil {
			return strconv.FormatFloat(v, 'f', p, 64) + "%"
		}
		return formatPercent(v)
	case "number":
		return formatThousands(roundInt(v))
	case "compact-number":
		return formatCompactK(roundInt(v))
	case "currency":
		p := 2 // adaptive and non-integer precision render two decimals
		if n, err := strconv.Atoi(prec); err == nil {
			p = n
		}
		return "$" + strconv.FormatFloat(v, 'f', p, 64)
	case "duration":
		return formatDuration(time.Duration(v * float64(time.Second)))
	case "countdown":
		if e.opts.Now.IsZero() {
			return ""
		}
		return countdown(e.opts.Now, time.Unix(int64(v), 0))
	case "datetime":
		return formatDatetime(time.Unix(int64(v), 0), e.opts.Now)
	}
	// no format, enum, or an unknown format on a number: the canonical form.
	s, _ := jsonselect.Canonical(v)
	return s
}

// sourceNumberSelf is the self of a present number (§4). countdown/datetime
// values are minutes until the epoch value, floored at 0 and unresolved without a
// clock, matching the *-reset-minutes self metrics.
func (e *docEval) sourceNumberSelf(f *dsl.FieldNode, v float64) selfRef {
	switch f.Formatter.Name {
	case "countdown", "datetime":
		if e.opts.Now.IsZero() {
			return selfRef{}
		}
		mins := time.Unix(int64(v), 0).Sub(e.opts.Now).Minutes()
		if mins < 0 {
			mins = 0
		}
		return selfRef{value: &dsl.Value{Kind: dsl.ValueNumber, Num: mins}}
	}
	return selfRef{value: &dsl.Value{Kind: dsl.ValueNumber, Num: v}}
}
