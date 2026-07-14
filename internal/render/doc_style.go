package render

// This file holds the DSL-evaluation support helpers used by doc.go: the
// full character-decoration style model (the legacy stylize in color.go only
// covers color+bold, and color.go is off-limits for extension), the metric
// Resolver bridging dsl.Expr evaluation to the existing metricValue plumbing,
// and formatter application for <field format="...">.

import (
	"math"
	"strconv"
	"strings"

	"github.com/yacchi/statusloom/internal/config"
	"github.com/yacchi/statusloom/internal/dsl"
	"github.com/yacchi/statusloom/internal/schema"
)

// docStyle is the fully-resolved character decoration for a node. It carries
// every DSL decoration attribute so the DSL renderer can honor
// color/background/bold/dim/italic/underline/strikethrough. It is produced by
// merging a parent style with a node's own Style (nearest wins).
type docStyle struct {
	color         string
	background    string
	bold          bool
	dim           bool
	italic        bool
	underline     bool
	strikethrough bool
}

// mergeStyle overlays a node's own Style on top of the inherited style:
// a non-empty color/background or a non-nil decoration bool overrides the
// parent, so the nearest specification wins (markup.md "文字装飾").
func mergeStyle(inherited docStyle, s dsl.Style) docStyle {
	out := inherited
	if s.Color != "" {
		out.color = s.Color
	}
	if s.Background != "" {
		out.background = s.Background
	}
	if s.Bold != nil {
		out.bold = *s.Bold
	}
	if s.Dim != nil {
		out.dim = *s.Dim
	}
	if s.Italic != nil {
		out.italic = *s.Italic
	}
	if s.Underline != nil {
		out.underline = *s.Underline
	}
	if s.Strikethrough != nil {
		out.strikethrough = *s.Strikethrough
	}
	return out
}

// sgrBgParam is the background counterpart of sgrColorParam (color.go):
// ANSI16 background is 40+idx / 100+(idx-8); ansi256 is 48;5;N; truecolor
// is 48;2;r;g;b.
func sgrBgParam(c color) string {
	switch c.kind {
	case ckAnsi16:
		if c.idx < 8 {
			return strconv.Itoa(40 + c.idx)
		}
		return strconv.Itoa(100 + (c.idx - 8))
	case ckAnsi256:
		return "48;5;" + strconv.Itoa(c.idx)
	case ckTruecolor:
		return "48;2;" + strconv.Itoa(int(c.r)) + ";" +
			strconv.Itoa(int(c.g)) + ";" + strconv.Itoa(int(c.b))
	default:
		return ""
	}
}

// stylizeDoc wraps text in SGR escapes for a full docStyle, downgraded to
// level. It reuses parseColor/downgrade/sgrColorParam from color.go for the
// foreground and its sgrBgParam sibling for the background. No escapes are
// emitted at levelNone or when the style is entirely empty; a single reset
// terminates any styled run. SGR codes: bold 1, dim 2, italic 3, underline
// 4, strikethrough 9.
func stylizeDoc(text string, st docStyle, level colorLevel) string {
	if level == levelNone {
		return text
	}
	var params []string
	if st.bold {
		params = append(params, "1")
	}
	if st.dim {
		params = append(params, "2")
	}
	if st.italic {
		params = append(params, "3")
	}
	if st.underline {
		params = append(params, "4")
	}
	if st.strikethrough {
		params = append(params, "9")
	}
	if p := sgrColorParam(downgrade(parseColor(st.color), level)); p != "" {
		params = append(params, p)
	}
	if p := sgrBgParam(downgrade(parseColor(st.background), level)); p != "" {
		params = append(params, p)
	}
	if len(params) == 0 {
		return text
	}
	return "\x1b[" + strings.Join(params, ";") + "m" + text + "\x1b[0m"
}

// docResolver adapts the existing metricValue plumbing to the dsl.Resolver
// interface used by when-expressions and color-rules. self maps the special
// "self" reference to the owning field's self metric ("" when the node has
// none, which makes "self" unresolvable -> the node is hidden / the rule
// fails, per markup.md).
type docResolver struct {
	snap schema.StatusSnapshot
	cfg  config.ToolConfig
	opts Options
	self string
}

// ResolveMetric resolves a metric reference to a dsl.Value. Numeric metrics
// come from metricValue (shared with the legacy renderer so thresholds
// compare against exactly what is displayed); "git-dirty" is the DSL's new
// boolean metric (markup.md 付録), derived from the repository snapshot.
func (r docResolver) ResolveMetric(name string) (dsl.Value, bool) {
	if name == "self" {
		if r.self == "" {
			return dsl.Value{}, false
		}
		name = r.self
	}
	if name == "git-dirty" || name == "git-clean" || name == "thinking-enabled" || name == "exceeds-200k" {
		if name == "thinking-enabled" {
			if r.snap.Session.ThinkingEnabled == nil {
				return dsl.Value{}, false
			}
			return dsl.Value{Kind: dsl.ValueBool, Bool: *r.snap.Session.ThinkingEnabled}, true
		}
		if name == "exceeds-200k" {
			if r.snap.Session.Context == nil {
				return dsl.Value{}, false
			}
			return dsl.Value{Kind: dsl.ValueBool, Bool: r.snap.Session.Context.Exceeds200K}, true
		}
		if r.snap.Repository == nil {
			return dsl.Value{}, false
		}
		value := r.snap.Repository.Dirty
		if name == "git-clean" {
			value = !value
		}
		return dsl.Value{Kind: dsl.ValueBool, Bool: value}, true
	}
	v, ok := metricValue(name, r.snap, r.cfg, r.opts)
	if !ok {
		return dsl.Value{}, false
	}
	return dsl.Value{Kind: dsl.ValueNumber, Num: v}, true
}

// applyFormat applies a <field format="..."> formatter to the field's self
// metric, returning the formatted display text. ok is false when the format
// is not a value-transforming one handled here (duration/countdown/enum keep
// the default renderContent text: duration/countdown already equal their
// formatter, and enum is a pass-through), the field exposes no self metric,
// or the metric cannot be resolved. See markup.md "field値の解決" item 4.
func (e *docEval) applyFormat(f *dsl.FieldNode) (string, bool) {
	self := e.selfMetric(f.Name)
	if self == "" {
		return "", false
	}
	v, ok := metricValue(self, e.snap, e.cfg, e.opts)
	if !ok {
		return "", false
	}
	prec := f.Formatter.Precision
	switch f.Formatter.Name {
	case "percent":
		if prec == "" {
			return formatPercent(v), true
		}
		p, err := strconv.Atoi(prec)
		if err != nil {
			return formatPercent(v), true
		}
		return strconv.FormatFloat(v, 'f', p, 64) + "%", true
	case "number":
		return formatThousands(int(math.Round(v))), true
	case "compact-number":
		return formatCompactK(int(math.Round(v))), true
	case "currency":
		p := 2 // adaptive currently renders two decimals, matching the default
		if prec != "" && prec != "adaptive" {
			if n, err := strconv.Atoi(prec); err == nil {
				p = n
			}
		}
		return "$" + strconv.FormatFloat(v, 'f', p, 64), true
	default:
		// duration / countdown / enum (and any future datetime/boolean):
		// keep the renderContent default.
		return "", false
	}
}
