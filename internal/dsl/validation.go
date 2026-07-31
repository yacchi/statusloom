package dsl

import (
	"errors"
	"strings"
)

// Validate performs the semantic checks that depend on the registry,
// condition parser, and formatter validator (markup.md "validation").
// Structural/well-formedness problems are already reported by Parse; call
// Validate on the Document that Parse returned. A nil document or a
// document with no root yields no diagnostics (Parse reported that case).
func Validate(doc *Document) []Diagnostic {
	if doc == nil || doc.Root == nil {
		return nil
	}
	v := &validator{src: doc.Source, rootRange: doc.Root.Meta.SourceRange}
	v.validateRoot(doc.Root)
	return v.diags
}

type validator struct {
	diags     []Diagnostic
	src       string
	tool      string
	toolKnown bool
	rootRange SourceRange
	// enforceSubagentScope is true only for tools whose catalog mixes
	// subagent (task-*) and non-subagent fields (currently just the unified
	// "claude-code" catalog). It gates the field-scope check so a
	// single-scope tool (every field the same Category, no <subagent>
	// wrapper expected) is not rejected for placing task-* fields at top
	// level.
	enforceSubagentScope bool
}

// toolHasMixedScope reports whether a tool's catalog contains both
// subagent (Category=="subagent") and non-subagent fields, which is the
// signal that its documents distinguish a <subagent> region and must have
// the field-scope rule enforced.
func toolHasMixedScope(tool string) bool {
	hasMain, hasSub := false, false
	for _, f := range Fields(tool) {
		if f.Category == "subagent" {
			hasSub = true
		} else {
			hasMain = true
		}
	}
	return hasMain && hasSub
}

func (v *validator) errf(r SourceRange, format string, args ...any) {
	v.diags = append(v.diags, Errorf(r, format, args...))
}

func (v *validator) warnf(r SourceRange, format string, args ...any) {
	v.diags = append(v.diags, Warnf(r, format, args...))
}

var validColorLevels = map[string]bool{"none": true, "ansi16": true, "ansi256": true, "truecolor": true}
var validContextModes = map[string]bool{"raw": true, "usable": true, "both": true}

func (v *validator) validateRoot(root *StatusloomNode) {
	r := root.Meta.SourceRange
	if root.Version != "1" {
		v.errf(r, "version must be \"1\", got %q", root.Version)
	}
	v.tool = root.Tool
	v.toolKnown = Fields(root.Tool) != nil
	if !v.toolKnown {
		v.errf(r, "unknown tool %q", root.Tool)
	}
	v.enforceSubagentScope = toolHasMixedScope(root.Tool)
	if cl := root.Settings.ColorLevel; cl != "" && !validColorLevels[cl] {
		v.errf(r, "invalid color-level %q: expected none, ansi16, ansi256, or truecolor", cl)
	}
	if s := root.Settings.OutputStyle; s != "" && s != "standard" && s != "powerline" {
		v.errf(r, "invalid output-style %q: expected standard or powerline", s)
	}
	if m := root.Settings.ContextPercentageMode; m != "" && !validContextModes[m] {
		v.errf(r, "invalid context-percentage-mode %q: expected raw, usable, or both", m)
	}
	v.validateLayouts(root.Layouts)
}

func (v *validator) validateLayouts(layouts []*LayoutNode) {
	if len(layouts) == 0 {
		v.errf(v.rootRange, "at least one <layout> is required")
		return
	}
	seen := make(map[string]bool)
	activeCount := 0
	for _, l := range layouts {
		if l.Name == "" {
			v.errf(l.Meta.SourceRange, "<layout> requires a name attribute")
		} else if seen[l.Name] {
			v.errf(l.Meta.SourceRange, "duplicate layout name %q", l.Name)
		} else {
			seen[l.Name] = true
		}
		if l.Active != nil && *l.Active {
			activeCount++
			if activeCount > 1 {
				v.errf(l.Meta.SourceRange, "multiple active layouts; exactly one layout may be active")
			}
		}
		v.validateLayoutChildren(l)
	}
	if activeCount == 0 {
		if !(len(layouts) == 1 && layouts[0].Active == nil) {
			v.errf(v.rootRange, "no active layout; exactly one layout must have active=\"true\" (a single layout may omit active)")
		}
	}
}

// validateLayoutChildren validates a layout's ordered line / responsive
// children. A <responsive> must hold at least one <variant>, and each
// <variant> at least one <line> (markup.md "responsive"); placement/nesting
// violations were already reported by the parser.
func (v *validator) validateLayoutChildren(l *LayoutNode) {
	for _, ch := range l.Children {
		switch c := ch.(type) {
		case *LineNode:
			v.validateLineNode(c, false)
		case *ResponsiveNode:
			v.validateResponsive(c)
		}
	}
	if l.Subagent != nil {
		v.validateSubagent(l.Subagent)
	}
}

func (v *validator) validateResponsive(r *ResponsiveNode) {
	if len(r.Variants) == 0 {
		v.errf(r.Meta.SourceRange, "<responsive> requires at least one <variant>")
	}
	for _, vr := range r.Variants {
		if len(vr.Lines) == 0 {
			v.errf(vr.Meta.SourceRange, "<variant> requires at least one <line>")
		}
		for _, ln := range vr.Lines {
			v.validateLineNode(ln, false)
		}
		if vr.Subagent != nil {
			v.validateSubagent(vr.Subagent)
		}
	}
}

// validateSubagent checks a <subagent> region: it must hold exactly one
// <line> (markup.md "subagent"), whose fields are validated with the
// subagent scope in force. Structural nesting violations (extra lines,
// disallowed <responsive>/<variant>) were already reported by the parser.
func (v *validator) validateSubagent(sa *SubagentNode) {
	if sa.Line == nil {
		v.errf(sa.Meta.SourceRange, "<subagent> requires exactly one <line>")
		return
	}
	v.validateLineNode(sa.Line, true)
}

func (v *validator) validateLineNode(ln *LineNode, inSubagent bool) {
	v.validateCommon(ln.Common, ln.Meta.SourceRange, "")
	for _, ch := range ln.Children {
		v.validateNode(ch, inSubagent)
	}
}

func (v *validator) validateNode(n Node, inSubagent bool) {
	switch t := n.(type) {
	case *SpanNode:
		v.validateCommon(t.Common, t.Meta.SourceRange, "")
		for _, ch := range t.Children {
			v.validateNode(ch, inSubagent)
		}
	case *TextNode:
		v.validateCommon(t.Common, t.Meta.SourceRange, "")
	case *FieldNode:
		v.validateField(t, inSubagent)
	case *FlexNode:
		v.validateFlex(t)
	case *RawTextNode, *CommentNode:
		// nothing to validate
	}
}

// validAligns is the set of accepted `align` attribute values on <field>
// ("" is unspecified/left and is not in this set; validated separately).
var validAligns = map[string]bool{"left": true, "right": true}

func (v *validator) validateField(f *FieldNode, inSubagent bool) {
	if f.Align != "" && !validAligns[f.Align] {
		v.errf(f.Meta.SourceRange, "invalid align %q: expected left or right", f.Align)
	}
	selfMetric := ""
	if v.toolKnown {
		if f.Name == "" {
			v.errf(f.Meta.SourceRange, "<field> requires a name attribute")
		} else if def, ok := FieldByName(v.tool, f.Name); ok {
			selfMetric = def.SelfMetric
			if v.enforceSubagentScope {
				if inSubagent && def.Category != "subagent" {
					v.errf(f.Meta.SourceRange, "field %q can only be used inside <subagent>", f.Name)
				} else if !inSubagent && def.Category == "subagent" {
					v.errf(f.Meta.SourceRange, "field %q cannot be used outside <subagent>", f.Name)
				}
			}
			if f.Hyperlink && !def.Linkable {
				v.errf(f.Meta.SourceRange, "field %q does not support hyperlink", f.Name)
			}
			v.validateVar(f, def)
			v.diags = append(v.diags, ValidateFormatter(def, f.Formatter, f.Meta.SourceRange)...)
		} else {
			v.errf(f.Meta.SourceRange, "unknown field %q for tool %q", f.Name, v.tool)
		}
	}
	v.validateCommon(f.Common, f.Meta.SourceRange, selfMetric)
}

// validateVar checks the `var` / `unmask` attribute pair against the field
// definition (markup.md "env").
//
// A missing var is NOT diagnosed at all. The visual editor inserts <field
// name="env"/> the moment the palette item is dropped, so an error would make
// the document unsavable mid-edit - and a warning is just as wrong, because it
// would then print to the agent's stderr on every single render for a node
// that is merely empty. A var-less env field renders empty exactly like any
// other field with no data available, which needs no diagnostic.
func (v *validator) validateVar(f *FieldNode, def FieldDef) {
	r := f.Meta.SourceRange
	if !def.RequiresVar {
		if f.Var != "" {
			v.errf(r, "field %q does not take a var attribute", f.Name)
		}
		if f.Unmask {
			v.errf(r, "field %q does not take an unmask attribute", f.Name)
		}
		return
	}

	if f.Var == "" {
		// unmask alone is still an error: it is meaningless without a
		// variable, so it can only be a mistake rather than a mid-edit state.
		if f.Unmask {
			v.errf(r, "unmask requires a var attribute")
		}
		return
	}
	// An environment variable name cannot contain "=" (the separator in the
	// underlying environ) or a NUL, so such a var can never resolve.
	if strings.ContainsAny(f.Var, "=\x00") {
		v.errf(r, "invalid var %q: an environment variable name may not contain %q", f.Var, "=")
		return
	}
	switch {
	case f.Unmask && IsSecretEnvName(f.Var):
		v.warnf(r, "var %q looks like a credential and unmask renders it in clear text; it will be visible in screenshots and screen shares", f.Var)
	case IsSecretEnvName(f.Var):
		v.warnf(r, "var %q looks like a credential, so its value renders as %q; add unmask to show it anyway", f.Var, SecretMask)
	}
}

func (v *validator) validateFlex(f *FlexNode) {
	s := f.Size
	switch {
	case s == "" || s == "full":
		// ok
	case strings.HasPrefix(s, "full-minus-"):
		if !isPositiveInt(s[len("full-minus-"):]) {
			v.errf(f.Meta.SourceRange, "invalid flex size %q: full-minus-<N> requires a positive integer", s)
		}
	default:
		v.errf(f.Meta.SourceRange, "invalid flex size %q: expected \"full\" or \"full-minus-<N>\"", s)
	}
}

// validateCommon checks color formats, optional/when references, and
// color-rules of a node's common attributes. selfMetric is the owning
// node's self metric (non-empty only for fields that have one); it
// governs whether "self" may appear in when/color-rule expressions.
func (v *validator) validateCommon(c CommonAttributes, nodeRange SourceRange, selfMetric string) {
	if c.Style.Color != "" && !validColor(c.Style.Color) {
		v.errf(nodeRange, "invalid color %q", c.Style.Color)
	}
	if c.Style.Background != "" && !validColor(c.Style.Background) {
		v.errf(nodeRange, "invalid background color %q", c.Style.Background)
	}
	if c.Optional != "" && v.toolKnown {
		name, varName := ParseOptional(c.Optional)
		def, ok := FieldByName(v.tool, name)
		switch {
		case !ok:
			v.errf(nodeRange, "optional references unknown field %q", name)
		case varName != "" && !def.RequiresVar:
			v.errf(nodeRange, "optional %q: field %q does not take a var", c.Optional, name)
		case varName == "" && def.RequiresVar:
			v.errf(nodeRange, "optional %q requires a variable name, as in optional=%q", c.Optional, name+":VAR_NAME")
		}
	}
	if c.When != "" {
		v.validateCondition(c.When, nodeRange, selfMetric)
	}
	for _, cr := range c.ColorRules {
		if cr.When == "" {
			v.errf(cr.Meta.SourceRange, "<color-rule> requires a when attribute")
		}
		if cr.Color == "" {
			v.errf(cr.Meta.SourceRange, "<color-rule> requires a color attribute")
		} else if !validColor(cr.Color) {
			v.errf(cr.Meta.SourceRange, "invalid color %q", cr.Color)
		}
		if cr.When != "" {
			v.validateCondition(cr.When, cr.Meta.SourceRange, selfMetric)
		}
	}
}

// validateCondition parses a when expression and checks that every metric
// it references exists (or is a permitted "self"). baseRange is the range
// of the owning node/color-rule; the expression's own byte offsets are
// mapped back into the `when` attribute value where possible.
func (v *validator) validateCondition(expr string, baseRange SourceRange, selfMetric string) {
	rng := baseRange
	if r, ok := attrValueRange(v.src, baseRange, "when"); ok {
		rng = r
	}
	e, err := ParseCondition(expr)
	if err != nil {
		var se *SyntaxError
		if errors.As(err, &se) {
			v.errf(offsetInto(rng, se.Offset), "invalid when expression: %s", se.Message)
		} else {
			v.errf(rng, "invalid when expression: %v", err)
		}
		return
	}
	for _, m := range e.Metrics() {
		if m == "self" {
			if selfMetric == "" {
				v.errf(rng, "self is not available here; only a <field> with a self metric may reference self")
			}
			continue
		}
		if v.toolKnown {
			if _, ok := MetricByName(v.tool, m); !ok {
				v.errf(rng, "unknown metric %q", m)
			}
		}
	}
}

// namedColors is the set of ANSI-16 color names accepted in color/
// background attributes (markup.md "color/background"; the kebab-case
// equivalents of render's ansi16Names palette).
var namedColors = map[string]bool{
	"black": true, "red": true, "green": true, "yellow": true,
	"blue": true, "magenta": true, "cyan": true, "white": true,
	"bright-black": true, "bright-red": true, "bright-green": true, "bright-yellow": true,
	"bright-blue": true, "bright-magenta": true, "bright-cyan": true, "bright-white": true,
}

// validColor reports whether s is a valid color value: a named 16-color,
// ansi256:N (0..255), or #rrggbb.
func validColor(s string) bool {
	if namedColors[s] {
		return true
	}
	if rest, ok := strings.CutPrefix(s, "ansi256:"); ok {
		n, valid := parseNonNegInt(rest)
		return valid && n <= 255
	}
	if strings.HasPrefix(s, "#") && len(s) == 7 {
		for i := 1; i < 7; i++ {
			if !isHexDigit(s[i]) {
				return false
			}
		}
		return true
	}
	return false
}

func isHexDigit(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}
