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
//	          | "[" cond ("," cond)* "]" // filter: an element matches when ALL conds match
//	cond     := fkey "=" value          // split at the first "=" of the cond
//	fkey     := key ("." key)*          // a path resolved inside the element object
//
// A filter returns the first array element that is an object and for which
// every cond matches. A cond matches when the value at fkey's path is a scalar
// whose canonical form equals value. A missing key, a non-object on the way,
// or a null never matches.
//
// "," and "=" are structural only outside a template. A value therefore cannot
// contain "," or "]" (it may contain "=", "." and "["). An empty cond, as in
// "[a=1,]" or "[,a=1]", is a SyntaxError.
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

// cond is one "fkey=value" test of a filter. path holds the fkey's dot-separated
// parts, each one a member name resolved inside the element object.
type cond struct {
	path  [][]part
	value []part
}

type segment struct {
	kind  segmentKind
	key   []part // segKey only: member name
	conds []cond // segFilter only
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

	if body == "" {
		return segment{}, 0, newSyntaxError(i, "expected an index or key=value")
	}

	// Filter: one or more conds separated by literal ",".
	var conds []cond
	pos := i + 1
	for {
		c, next, err := readCond(src, pos, end, i)
		if err != nil {
			return segment{}, 0, err
		}
		conds = append(conds, c)
		if next >= end {
			break
		}
		pos = next + 1
	}
	return segment{kind: segFilter, conds: conds}, end + 1, nil
}

// readCond parses the cond spanning src[cs:] up to the next literal "," or the
// bracket close at end. open is the offset of the "[" that owns the cond. It
// returns the cond and the offset where it stopped (a "," or end).
func readCond(src string, cs, end, open int) (cond, int, error) {
	_, ce, err := readText(src[:end], cs, ",")
	if err != nil {
		return cond{}, 0, err
	}
	if ce == cs {
		return cond{}, 0, newSyntaxError(cs, "empty condition in filter")
	}

	// Split at the first literal "=" (templates skipped).
	_, eq, err := readText(src[:ce], cs, "=")
	if err != nil {
		return cond{}, 0, err
	}
	if eq >= ce {
		return cond{}, 0, newSyntaxError(open, "expected an index or key=value")
	}
	path, err := readFilterKey(src[:eq], cs, open)
	if err != nil {
		return cond{}, 0, err
	}
	// The value may contain ".", "=" and "["; it ends at ce.
	valueParts, _, err := readText(src[:ce], eq+1, "")
	if err != nil {
		return cond{}, 0, err
	}
	return cond{path: path, value: valueParts}, ce, nil
}

// readFilterKey splits the fkey src[cs:] at "." into member names. A "[" is
// invalid in a filter key. An empty member name is reported at open.
func readFilterKey(src string, cs, open int) ([][]part, error) {
	_, bad, err := readText(src, cs, "[")
	if err != nil {
		return nil, err
	}
	if bad < len(src) {
		return nil, newSyntaxError(bad, "invalid character %q in filter key", string(src[bad]))
	}
	var path [][]part
	pos := cs
	for {
		parts, next, err := readText(src, pos, ".")
		if err != nil {
			return nil, err
		}
		if len(parts) == 0 {
			return nil, newSyntaxError(open, "empty key in filter")
		}
		path = append(path, parts)
		if next >= len(src) {
			return path, nil
		}
		pos = next + 1
	}
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
		return firstMatch(arr, s.conds, vars)
	}
	return nil, false
}

// firstMatch returns the first object element for which every cond matches.
func firstMatch(arr []any, conds []cond, vars Vars) (any, bool) {
	for _, el := range arr {
		if matchAll(el, conds, vars) {
			return el, true
		}
	}
	return nil, false
}

// matchAll reports whether el is an object for which every cond matches.
func matchAll(el any, conds []cond, vars Vars) bool {
	for _, c := range conds {
		if !c.match(el, vars) {
			return false
		}
	}
	return true
}

// match reports whether the value at the cond's path inside el is a scalar
// whose canonical form equals the cond's value. A missing key or a non-object
// on the way does not match, and neither does a JSON null.
func (c cond) match(el any, vars Vars) bool {
	cur := el
	for _, name := range c.path {
		m, ok := cur.(map[string]any)
		if !ok {
			return false
		}
		v, ok := m[substitute(name, vars)]
		if !ok {
			return false
		}
		cur = v
	}
	s, ok := Canonical(cur)
	return ok && s == substitute(c.value, vars)
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
		for _, c := range s.conds {
			for _, name := range c.path {
				add(name)
			}
			add(c.value)
		}
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
