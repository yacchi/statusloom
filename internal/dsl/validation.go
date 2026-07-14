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
}

func (v *validator) errf(r SourceRange, format string, args ...any) {
	v.diags = append(v.diags, Errorf(r, format, args...))
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
		v.validateLine(l)
	}
	if activeCount == 0 {
		if !(len(layouts) == 1 && layouts[0].Active == nil) {
			v.errf(v.rootRange, "no active layout; exactly one layout must have active=\"true\" (a single layout may omit active)")
		}
	}
}

func (v *validator) validateLine(l *LayoutNode) {
	for _, ln := range l.Lines {
		v.validateCommon(ln.Common, ln.Meta.SourceRange, "")
		for _, ch := range ln.Children {
			v.validateNode(ch)
		}
	}
}

func (v *validator) validateNode(n Node) {
	switch t := n.(type) {
	case *SpanNode:
		v.validateCommon(t.Common, t.Meta.SourceRange, "")
		for _, ch := range t.Children {
			v.validateNode(ch)
		}
	case *TextNode:
		v.validateCommon(t.Common, t.Meta.SourceRange, "")
	case *FieldNode:
		v.validateField(t)
	case *FlexNode:
		v.validateFlex(t)
	case *RawTextNode, *CommentNode:
		// nothing to validate
	}
}

func (v *validator) validateField(f *FieldNode) {
	selfMetric := ""
	if v.toolKnown {
		if f.Name == "" {
			v.errf(f.Meta.SourceRange, "<field> requires a name attribute")
		} else if def, ok := FieldByName(v.tool, f.Name); ok {
			selfMetric = def.SelfMetric
			if f.Hyperlink && !def.Linkable {
				v.errf(f.Meta.SourceRange, "field %q does not support hyperlink", f.Name)
			}
			v.diags = append(v.diags, ValidateFormatter(def, f.Formatter, f.Meta.SourceRange)...)
		} else {
			v.errf(f.Meta.SourceRange, "unknown field %q for tool %q", f.Name, v.tool)
		}
	}
	v.validateCommon(f.Common, f.Meta.SourceRange, selfMetric)
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
		if _, ok := FieldByName(v.tool, c.Optional); !ok {
			v.errf(nodeRange, "optional references unknown field %q", c.Optional)
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
