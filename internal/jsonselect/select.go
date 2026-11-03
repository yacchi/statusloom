// Package jsonselect implements the select attribute of
// <field name="source">: a small path language that picks one value out of a
// JSON document produced by encoding/json.
//
// Grammar (one optional bare key first, then segments):
//
//	expr     := ""                       // the document root
//	          | first segment*
//	first    := key | segment            // "a.b" is the same as ".a.b"
//	segment  := "." key
//	          | "[" index "]"            // index: ASCII digits only
//	          | "[" fkey "=" value "]"   // split at the first "=" outside a template
//
// A template ${name} may appear in keys, fkeys and values. It is read as one
// unit (so ${workspace.project_dir} is not split at its dot) and, at eval time,
// replaced by the variable's value used literally, never re-parsed.
//
// Full JSONPath is out of scope; this package only covers what the source
// definitions in sources.json need to point at a value.
package jsonselect

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// KnownVars lists the template variable names accepted inside ${...}, in this order.
var KnownVars = []string{"session_id", "cwd", "workspace.project_dir", "CLAUDE_CONFIG_DIR"}

// Vars resolves a template variable; an unknown or unset variable yields "".
type Vars func(name string) string

// SyntaxError is returned by Parse; Offset is a byte offset into the source.
type SyntaxError struct {
	Offset  int
	Message string
}

func (e *SyntaxError) Error() string {
	return fmt.Sprintf("select: %s (at offset %d)", e.Message, e.Offset)
}

func newSyntaxError(offset int, format string, args ...any) *SyntaxError {
	return &SyntaxError{Offset: offset, Message: fmt.Sprintf(format, args...)}
}

// part is one piece of a key, fkey or value: either a literal run or a
// template variable reference.
type part struct {
	lit   string
	name  string
	isVar bool
}

type segmentKind int

const (
	segKey segmentKind = iota
	segIndex
	segFilter
)

type segment struct {
	kind  segmentKind
	key   []part // segKey: member name; segFilter: filter key
	value []part // segFilter only
	index int    // segIndex only
}

// Expr is a compiled select expression.
type Expr struct {
	segs []segment
}

// Parse compiles a select expression. Parse("") returns a root selector.
func Parse(src string) (*Expr, error) {
	e := &Expr{}
	if src == "" {
		return e, nil
	}

	i := 0
	switch src[0] {
	case '.', '[':
		// Handled by the loop below.
	default:
		// A leading bare key is allowed only at offset 0.
		parts, end, err := readKey(src, 0)
		if err != nil {
			return nil, err
		}
		e.segs = append(e.segs, segment{kind: segKey, key: parts})
		i = end
	}

	for i < len(src) {
		switch src[i] {
		case '.':
			parts, end, err := readKey(src, i+1)
			if err != nil {
				return nil, err
			}
			if len(parts) == 0 {
				return nil, newSyntaxError(i+1, `empty key after "."`)
			}
			e.segs = append(e.segs, segment{kind: segKey, key: parts})
			i = end
		case '[':
			seg, end, err := readBracket(src, i)
			if err != nil {
				return nil, err
			}
			e.segs = append(e.segs, seg)
			i = end
		default:
			return nil, newSyntaxError(i, `expected "." or "["`)
		}
	}
	return e, nil
}

// readText reads units from src[i:] until it reaches a literal byte listed in
// stops or the end of src. Templates are consumed whole, so a stop byte inside
// ${...} never ends the run. It returns the parts and the index where reading
// stopped (len(src) at the end).
func readText(src string, i int, stops string) ([]part, int, error) {
	var parts []part
	var lit []byte
	flush := func() {
		if len(lit) > 0 {
			parts = append(parts, part{lit: string(lit)})
			lit = lit[:0]
		}
	}

	for i < len(src) {
		c := src[i]
		if c == '$' && i+1 < len(src) && src[i+1] == '{' {
			name, next, err := readTemplate(src, i)
			if err != nil {
				return nil, 0, err
			}
			flush()
			parts = append(parts, part{name: name, isVar: true})
			i = next
			continue
		}
		if strings.IndexByte(stops, c) >= 0 {
			break
		}
		lit = append(lit, c)
		i++
	}
	flush()
	return parts, i, nil
}

// readTemplate reads "${name}" starting at the "$" at offset i.
func readTemplate(src string, i int) (string, int, error) {
	j := strings.IndexByte(src[i+2:], '}')
	if j < 0 {
		return "", 0, newSyntaxError(i, `unterminated "${"`)
	}
	name := src[i+2 : i+2+j]
	if !isKnownVar(name) {
		return "", 0, newSyntaxError(i, "unknown variable %q", name)
	}
	return name, i + 2 + j + 1, nil
}

func isKnownVar(name string) bool {
	for _, v := range KnownVars {
		if v == name {
			return true
		}
	}
	return false
}

// readKey reads a key starting at i. A literal "]" or "=" cannot appear in a
// key outside brackets, so it is reported as unexpected.
func readKey(src string, i int) ([]part, int, error) {
	parts, end, err := readText(src, i, ".[]=")
	if err != nil {
		return nil, 0, err
	}
	if end < len(src) && (src[end] == ']' || src[end] == '=') {
		return nil, 0, newSyntaxError(end, "unexpected %q", string(src[end]))
	}
	return parts, end, nil
}

// readBracket parses the "[...]" segment whose "[" is at offset i. It returns
// the segment and the offset just past the closing "]".
func readBracket(src string, i int) (segment, int, error) {
	// The bracket closes at the first literal "]"; a "]" inside ${...} does not count.
	_, end, err := readText(src, i+1, "]")
	if err != nil {
		return segment{}, 0, err
	}
	if end >= len(src) {
		return segment{}, 0, newSyntaxError(i, `unterminated "["`)
	}

	body := src[i+1 : end]
	if isIndexBody(body) {
		n, err := strconv.Atoi(body)
		if err != nil {
			return segment{}, 0, newSyntaxError(i, "index too large")
		}
		return segment{kind: segIndex, index: n}, end + 1, nil
	}

	// Filter: split at the first literal "=" (templates skipped). Only the
	// text before it is checked for stray "." or "[".
	_, eq, err := readText(src[:end], i+1, "=")
	if err != nil {
		return segment{}, 0, err
	}
	if eq >= end {
		return segment{}, 0, newSyntaxError(i, "expected an index or key=value")
	}
	_, bad, err := readText(src[:eq], i+1, ".[")
	if err != nil {
		return segment{}, 0, err
	}
	if bad < eq {
		return segment{}, 0, newSyntaxError(bad, "invalid character %q in filter key", string(src[bad]))
	}
	keyParts, _, err := readText(src[:eq], i+1, "")
	if err != nil {
		return segment{}, 0, err
	}
	if len(keyParts) == 0 {
		return segment{}, 0, newSyntaxError(i, "empty key in filter")
	}
	// The value may contain ".", "=" and "["; the bracket close is already fixed at end.
	valueParts, _, err := readText(src[:end], eq+1, "")
	if err != nil {
		return segment{}, 0, err
	}
	return segment{kind: segFilter, key: keyParts, value: valueParts}, end + 1, nil
}

func isIndexBody(s string) bool {
	if s == "" {
		return false
	}
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// Eval walks doc (a value produced by encoding/json.Unmarshal into `any`;
// json.Number is accepted wherever a scalar may appear). ok is false when the
// path does not exist. A JSON null at the path returns (nil, true). vars may be
// nil (every variable is "").
func (e *Expr) Eval(doc any, vars Vars) (v any, ok bool) {
	cur := doc
	for _, s := range e.segs {
		next, found := step(cur, s, vars)
		if !found {
			return nil, false
		}
		cur = next
	}
	return cur, true
}

func step(cur any, s segment, vars Vars) (any, bool) {
	switch s.kind {
	case segKey:
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		v, ok := m[substitute(s.key, vars)]
		if !ok {
			return nil, false
		}
		return v, true
	case segIndex:
		arr, ok := cur.([]any)
		if !ok || s.index >= len(arr) {
			return nil, false
		}
		return arr[s.index], true
	case segFilter:
		arr, ok := cur.([]any)
		if !ok {
			return nil, false
		}
		return firstMatch(arr, substitute(s.key, vars), substitute(s.value, vars))
	}
	return nil, false
}

// firstMatch returns the first object element whose member key is a scalar
// whose canonical form equals want. JSON null never matches.
func firstMatch(arr []any, key, want string) (any, bool) {
	for _, el := range arr {
		m, ok := el.(map[string]any)
		if !ok {
			continue
		}
		member, ok := m[key]
		if !ok {
			continue
		}
		s, ok := Canonical(member)
		if ok && s == want {
			return el, true
		}
	}
	return nil, false
}

// substitute concatenates the literal parts and the resolved variables. The
// result is used literally and never re-parsed.
func substitute(parts []part, vars Vars) string {
	var b strings.Builder
	for _, p := range parts {
		if !p.isVar {
			b.WriteString(p.lit)
			continue
		}
		if vars != nil {
			b.WriteString(vars(p.name))
		}
	}
	return b.String()
}

// Vars returns the template variable names the expression references,
// deduplicated, in first-occurrence order.
func (e *Expr) Vars() []string {
	var out []string
	seen := map[string]bool{}
	add := func(parts []part) {
		for _, p := range parts {
			if p.isVar && !seen[p.name] {
				seen[p.name] = true
				out = append(out, p.name)
			}
		}
	}
	for _, s := range e.segs {
		add(s.key)
		add(s.value)
	}
	return out
}

// Canonical renders a JSON scalar in the canonical comparison/display form.
// ok is false for nil, maps, slices and any other type.
func Canonical(v any) (s string, ok bool) {
	switch x := v.(type) {
	case string:
		return x, true
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64), true
	case bool:
		return strconv.FormatBool(x), true
	case json.Number:
		f, err := x.Float64()
		if err != nil {
			return string(x), true
		}
		return strconv.FormatFloat(f, 'f', -1, 64), true
	}
	return "", false
}
