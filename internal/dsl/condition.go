package dsl

import (
	"fmt"
	"strconv"
	"strings"
)

// ValueKind identifies the dynamic type carried by a Value.
type ValueKind int

const (
	ValueMissing ValueKind = iota
	ValueNumber
	ValueString
	ValueBool
)

// String renders the kind for error messages (e.g. "number", "string").
func (k ValueKind) String() string {
	switch k {
	case ValueMissing:
		return "missing"
	case ValueNumber:
		return "number"
	case ValueString:
		return "string"
	case ValueBool:
		return "bool"
	default:
		return "unknown"
	}
}

// Value is a dynamically-typed value produced by resolving a metric
// reference, or by a literal in a `when`/`color-rule` condition.
type Value struct {
	Kind ValueKind
	Num  float64
	Str  string
	Bool bool
}

// Resolver looks up the current value of a named metric while evaluating
// a condition. name is passed through exactly as written in the
// expression, including the special "self" reference. ok is false when
// the metric cannot be resolved (unknown name, or no value available in
// the current snapshot); Eval treats that as an error, which callers use
// to hide the owning node.
type Resolver interface {
	ResolveMetric(name string) (Value, bool)
}

// Expr is a parsed `when` condition (see ParseCondition). It is evaluated
// against a Resolver to decide node visibility / color-rule matches.
type Expr interface {
	// Eval evaluates the expression to a boolean. Bare metric references
	// and literals are converted to bool via truthiness rules (see
	// markup.md "when式の演算子" / "primary"). Unresolved metrics and
	// type mismatches in comparisons are reported as errors.
	Eval(r Resolver) (bool, error)

	// Metrics returns every metric name referenced by the expression
	// (including "self"), deduplicated, in first-occurrence order. Used
	// by validation to check that referenced metrics/fields exist.
	Metrics() []string
}

// ParseCondition parses a `when` (or color-rule `when`) expression. It
// accepts both the first-class word operators (lt le gt ge eq ne / and or
// not) and the symbolic forms legal in decoded attribute text
// (< <= > >= == != && || !). See markup.md "when式の演算子".
//
// On a syntax error, the returned error is a *SyntaxError carrying the
// byte offset (within src) where parsing failed; its Error() message
// includes that offset.
func ParseCondition(src string) (Expr, error) {
	toks, err := lexCondition(src)
	if err != nil {
		return nil, err
	}
	p := &condParser{toks: toks}
	expr, err := p.parseOr()
	if err != nil {
		return nil, err
	}
	if p.peek().kind != tkEOF {
		return nil, newSyntaxError(p.peek().pos, "unexpected token %s after expression", tokenDesc(p.peek()))
	}
	return expr, nil
}

// SyntaxError is returned by ParseCondition (and by the lexer) for
// malformed input. Offset is a byte offset into the src passed to
// ParseCondition.
type SyntaxError struct {
	Offset  int
	Message string
}

func (e *SyntaxError) Error() string {
	return fmt.Sprintf("condition: %s (at offset %d)", e.Message, e.Offset)
}

func newSyntaxError(offset int, format string, args ...any) *SyntaxError {
	return &SyntaxError{Offset: offset, Message: fmt.Sprintf(format, args...)}
}

// ---------------------------------------------------------------------
// Lexer
// ---------------------------------------------------------------------

type tokenKind int

const (
	tkEOF tokenKind = iota
	tkLParen
	tkRParen
	tkAnd
	tkOr
	tkNot
	tkLt
	tkLe
	tkGt
	tkGe
	tkEq
	tkNe
	tkTrue
	tkFalse
	tkIdent
	tkNumber
	tkString
)

type token struct {
	kind tokenKind
	text string
	num  float64
	pos  int // byte offset of the token's first byte in src
}

// keywords maps reserved words to their token kind. Any other
// [a-z][a-z0-9-]* word lexes as tkIdent (a metric reference, including
// "self").
var keywords = map[string]tokenKind{
	"and":   tkAnd,
	"or":    tkOr,
	"not":   tkNot,
	"lt":    tkLt,
	"le":    tkLe,
	"gt":    tkGt,
	"ge":    tkGe,
	"eq":    tkEq,
	"ne":    tkNe,
	"true":  tkTrue,
	"false": tkFalse,
}

func isDigitByte(c byte) bool { return c >= '0' && c <= '9' }
func isLowerByte(c byte) bool { return c >= 'a' && c <= 'z' }

func lexCondition(src string) ([]token, error) {
	var toks []token
	i := 0
	n := len(src)
	for i < n {
		c := src[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c == '(':
			toks = append(toks, token{kind: tkLParen, pos: i})
			i++
		case c == ')':
			toks = append(toks, token{kind: tkRParen, pos: i})
			i++
		case c == '<':
			if i+1 < n && src[i+1] == '=' {
				toks = append(toks, token{kind: tkLe, pos: i})
				i += 2
			} else {
				toks = append(toks, token{kind: tkLt, pos: i})
				i++
			}
		case c == '>':
			if i+1 < n && src[i+1] == '=' {
				toks = append(toks, token{kind: tkGe, pos: i})
				i += 2
			} else {
				toks = append(toks, token{kind: tkGt, pos: i})
				i++
			}
		case c == '=':
			if i+1 < n && src[i+1] == '=' {
				toks = append(toks, token{kind: tkEq, pos: i})
				i += 2
			} else {
				return nil, newSyntaxError(i, "unexpected character '='; did you mean '=='?")
			}
		case c == '!':
			if i+1 < n && src[i+1] == '=' {
				toks = append(toks, token{kind: tkNe, pos: i})
				i += 2
			} else {
				toks = append(toks, token{kind: tkNot, pos: i})
				i++
			}
		case c == '&':
			if i+1 < n && src[i+1] == '&' {
				toks = append(toks, token{kind: tkAnd, pos: i})
				i += 2
			} else {
				return nil, newSyntaxError(i, "unexpected character '&'; did you mean '&&'?")
			}
		case c == '|':
			if i+1 < n && src[i+1] == '|' {
				toks = append(toks, token{kind: tkOr, pos: i})
				i += 2
			} else {
				return nil, newSyntaxError(i, "unexpected character '|'; did you mean '||'?")
			}
		case c == '\'' || c == '"':
			tok, ni, err := lexString(src, i)
			if err != nil {
				return nil, err
			}
			toks = append(toks, tok)
			i = ni
		case c == '-' || isDigitByte(c):
			tok, ni, err := lexNumber(src, i)
			if err != nil {
				return nil, err
			}
			toks = append(toks, tok)
			i = ni
		case isLowerByte(c):
			tok, ni := lexWord(src, i)
			toks = append(toks, tok)
			i = ni
		default:
			return nil, newSyntaxError(i, "unexpected character %q", string(c))
		}
	}
	toks = append(toks, token{kind: tkEOF, pos: n})
	return toks, nil
}

func lexWord(src string, start int) (token, int) {
	i := start
	n := len(src)
	for i < n && (isLowerByte(src[i]) || isDigitByte(src[i]) || src[i] == '-') {
		i++
	}
	word := src[start:i]
	kind, ok := keywords[word]
	if !ok {
		kind = tkIdent
	}
	return token{kind: kind, text: word, pos: start}, i
}

func lexNumber(src string, start int) (token, int, error) {
	i := start
	n := len(src)
	if src[i] == '-' {
		i++
	}
	if i >= n || !isDigitByte(src[i]) {
		return token{}, 0, newSyntaxError(start, "invalid number literal")
	}
	for i < n && isDigitByte(src[i]) {
		i++
	}
	if i < n && src[i] == '.' && i+1 < n && isDigitByte(src[i+1]) {
		i++
		for i < n && isDigitByte(src[i]) {
			i++
		}
	}
	text := src[start:i]
	val, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return token{}, 0, newSyntaxError(start, "invalid number literal %q", text)
	}
	return token{kind: tkNumber, text: text, num: val, pos: start}, i, nil
}

// lexString scans a quoted string literal starting at src[start] (which
// must be a ' or " byte). Supported escapes are \\, \' and \" only, per
// markup.md "when式の演算子" -> primary -> string literal.
func lexString(src string, start int) (token, int, error) {
	quote := src[start]
	i := start + 1
	n := len(src)
	var b strings.Builder
	for i < n {
		c := src[i]
		if c == quote {
			return token{kind: tkString, text: b.String(), pos: start}, i + 1, nil
		}
		if c == '\\' {
			if i+1 >= n {
				return token{}, 0, newSyntaxError(start, "unterminated string literal")
			}
			esc := src[i+1]
			switch esc {
			case '\\', '\'', '"':
				b.WriteByte(esc)
			default:
				return token{}, 0, newSyntaxError(i, "invalid escape sequence '\\%c'", esc)
			}
			i += 2
			continue
		}
		b.WriteByte(c)
		i++
	}
	return token{}, 0, newSyntaxError(start, "unterminated string literal")
}

// tokenDesc renders a token for use in "unexpected token" error messages.
func tokenDesc(t token) string {
	switch t.kind {
	case tkEOF:
		return "end of expression"
	case tkLParen:
		return "'('"
	case tkRParen:
		return "')'"
	case tkAnd:
		return "'and'"
	case tkOr:
		return "'or'"
	case tkNot:
		return "'not'"
	case tkLt:
		return "'lt'"
	case tkLe:
		return "'le'"
	case tkGt:
		return "'gt'"
	case tkGe:
		return "'ge'"
	case tkEq:
		return "'eq'"
	case tkNe:
		return "'ne'"
	case tkTrue:
		return "'true'"
	case tkFalse:
		return "'false'"
	case tkNumber:
		return fmt.Sprintf("number %q", t.text)
	case tkString:
		return fmt.Sprintf("string %q", t.text)
	case tkIdent:
		return fmt.Sprintf("identifier %q", t.text)
	default:
		return "token"
	}
}

// ---------------------------------------------------------------------
// Parser
//
// Precedence, loosest to tightest (markup.md "when式の演算子"):
//
//	or < and < not < comparison < primary
//
// i.e. or_expr wraps and_expr, and_expr wraps not_expr, not_expr wraps
// comparison, comparison wraps primary (with at most one comparison
// operator - chaining such as "a lt b lt c" is a parse error because the
// second "lt" is not a valid continuation once a comparison has been
// parsed).
// ---------------------------------------------------------------------

type condParser struct {
	toks []token
	pos  int
}

func (p *condParser) peek() token { return p.toks[p.pos] }

func (p *condParser) advance() token {
	t := p.toks[p.pos]
	if p.pos < len(p.toks)-1 {
		p.pos++
	}
	return t
}

func (p *condParser) parseOr() (Expr, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.peek().kind == tkOr {
		p.advance()
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = &orExpr{left: left, right: right}
	}
	return left, nil
}

func (p *condParser) parseAnd() (Expr, error) {
	left, err := p.parseNot()
	if err != nil {
		return nil, err
	}
	for p.peek().kind == tkAnd {
		p.advance()
		right, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		left = &andExpr{left: left, right: right}
	}
	return left, nil
}

func (p *condParser) parseNot() (Expr, error) {
	if p.peek().kind == tkNot {
		p.advance()
		operand, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		return &notExpr{operand: operand}, nil
	}
	return p.parseComparison()
}

var compareOps = map[tokenKind]compareOp{
	tkLt: opLt, tkLe: opLe, tkGt: opGt, tkGe: opGe, tkEq: opEq, tkNe: opNe,
}

func (p *condParser) parseComparison() (Expr, error) {
	left, err := p.parsePrimary()
	if err != nil {
		return nil, err
	}
	if op, ok := compareOps[p.peek().kind]; ok {
		p.advance()
		right, err := p.parsePrimary()
		if err != nil {
			return nil, err
		}
		return &comparisonExpr{op: op, left: left, right: right}, nil
	}
	return left, nil
}

func (p *condParser) parsePrimary() (Expr, error) {
	t := p.peek()
	switch t.kind {
	case tkLParen:
		p.advance()
		inner, err := p.parseOr()
		if err != nil {
			return nil, err
		}
		if p.peek().kind != tkRParen {
			return nil, newSyntaxError(p.peek().pos, "expected ')', got %s", tokenDesc(p.peek()))
		}
		p.advance()
		return inner, nil
	case tkNumber:
		p.advance()
		return &numberLit{val: t.num}, nil
	case tkString:
		p.advance()
		return &stringLit{val: t.text}, nil
	case tkTrue:
		p.advance()
		return &boolLit{val: true}, nil
	case tkFalse:
		p.advance()
		return &boolLit{val: false}, nil
	case tkIdent:
		p.advance()
		return &metricRef{name: t.text}, nil
	case tkEOF:
		return nil, newSyntaxError(t.pos, "unexpected end of expression")
	default:
		return nil, newSyntaxError(t.pos, "unexpected token %s", tokenDesc(t))
	}
}

// ---------------------------------------------------------------------
// Formatting (word-form canonicalization)
//
// FormatCondition re-emits a parsed condition in the first-class word form
// (lt le gt ge eq ne / and or not), which `statusloom fmt` normalizes to
// (markup.md "when式の演算子"). Symbolic operators become words; string
// literals use single quotes (legal raw in XML attribute values); redundant
// parentheses are dropped and only precedence-required ones are re-added.
// ---------------------------------------------------------------------

// Operator/operand precedence, loosest (or) to tightest (primary), matching
// the parser. A child whose precedence is below the context minimum is
// wrapped in parentheses.
const (
	precOr      = 1
	precAnd     = 2
	precNot     = 3
	precCompare = 4
	precPrimary = 5
)

// exprFormatter is implemented by every condition AST node so a condition can
// be re-serialized in word form. It is separate from the exported Expr
// interface so adding it does not change Expr's contract.
type exprFormatter interface {
	// formatNode returns the node's word-form text and its precedence.
	formatNode() (string, int)
}

// FormatCondition parses src and re-emits it in canonical word form. It
// returns the same error as ParseCondition on malformed input; callers that
// want to keep the original text on a parse failure (e.g. `fmt`) should do so
// explicitly.
func FormatCondition(src string) (string, error) {
	e, err := ParseCondition(src)
	if err != nil {
		return "", err
	}
	return formatChild(e, 0), nil
}

// formatChild formats e for a context requiring at least minPrec, adding
// parentheses when e binds looser than the context allows.
func formatChild(e Expr, minPrec int) string {
	f, ok := e.(exprFormatter)
	if !ok {
		return ""
	}
	s, prec := f.formatNode()
	if prec < minPrec {
		return "(" + s + ")"
	}
	return s
}

func (e *orExpr) formatNode() (string, int) {
	return formatChild(e.left, precOr) + " or " + formatChild(e.right, precAnd), precOr
}

func (e *andExpr) formatNode() (string, int) {
	return formatChild(e.left, precAnd) + " and " + formatChild(e.right, precNot), precAnd
}

func (e *notExpr) formatNode() (string, int) {
	return "not " + formatChild(e.operand, precNot), precNot
}

var compareOpWords = map[compareOp]string{
	opLt: "lt", opLe: "le", opGt: "gt", opGe: "ge", opEq: "eq", opNe: "ne",
}

func (e *comparisonExpr) formatNode() (string, int) {
	return formatChild(e.left, precPrimary) + " " + compareOpWords[e.op] + " " + formatChild(e.right, precPrimary), precCompare
}

func (m *metricRef) formatNode() (string, int) { return m.name, precPrimary }

func (n *numberLit) formatNode() (string, int) {
	return strconv.FormatFloat(n.val, 'f', -1, 64), precPrimary
}

func (s *stringLit) formatNode() (string, int) { return quoteCondString(s.val), precPrimary }

func (b *boolLit) formatNode() (string, int) {
	if b.val {
		return "true", precPrimary
	}
	return "false", precPrimary
}

// quoteCondString single-quotes a string literal, escaping backslashes and
// single quotes (the escapes the lexer accepts). Single quotes are legal raw
// in an XML double-quoted attribute value, so the word form needs no XML
// entity encoding here.
func quoteCondString(s string) string {
	r := strings.NewReplacer("\\", "\\\\", "'", "\\'")
	return "'" + r.Replace(s) + "'"
}

// ---------------------------------------------------------------------
// AST node types
// ---------------------------------------------------------------------

// valueProducer is implemented by primary nodes (metric references and
// literals) that can yield a typed Value, as opposed to compound nodes
// (and/or/not/comparison) which only produce a bool. evalValue uses this
// to let a parenthesized compound expression stand in for a comparison
// operand: its bool result is wrapped as a Value of kind ValueBool.
type valueProducer interface {
	valueOf(r Resolver) (Value, error)
}

func evalValue(e Expr, r Resolver) (Value, error) {
	if vp, ok := e.(valueProducer); ok {
		return vp.valueOf(r)
	}
	b, err := e.Eval(r)
	if err != nil {
		return Value{}, err
	}
	return Value{Kind: ValueBool, Bool: b}, nil
}

func truthiness(v Value) (bool, error) {
	switch v.Kind {
	case ValueBool:
		return v.Bool, nil
	case ValueNumber:
		return v.Num != 0, nil
	case ValueString:
		return v.Str != "", nil
	default:
		return false, fmt.Errorf("cannot use a missing value as a condition")
	}
}

func mergeMetrics(lists ...[]string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, list := range lists {
		for _, m := range list {
			if !seen[m] {
				seen[m] = true
				out = append(out, m)
			}
		}
	}
	return out
}

type orExpr struct{ left, right Expr }

func (e *orExpr) Eval(r Resolver) (bool, error) {
	l, err := e.left.Eval(r)
	if err != nil {
		return false, err
	}
	rr, err := e.right.Eval(r)
	if err != nil {
		return false, err
	}
	return l || rr, nil
}

func (e *orExpr) Metrics() []string {
	return mergeMetrics(e.left.Metrics(), e.right.Metrics())
}

type andExpr struct{ left, right Expr }

func (e *andExpr) Eval(r Resolver) (bool, error) {
	l, err := e.left.Eval(r)
	if err != nil {
		return false, err
	}
	rr, err := e.right.Eval(r)
	if err != nil {
		return false, err
	}
	return l && rr, nil
}

func (e *andExpr) Metrics() []string {
	return mergeMetrics(e.left.Metrics(), e.right.Metrics())
}

type notExpr struct{ operand Expr }

func (e *notExpr) Eval(r Resolver) (bool, error) {
	v, err := e.operand.Eval(r)
	if err != nil {
		return false, err
	}
	return !v, nil
}

func (e *notExpr) Metrics() []string { return e.operand.Metrics() }

type compareOp int

const (
	opLt compareOp = iota
	opLe
	opGt
	opGe
	opEq
	opNe
)

type comparisonExpr struct {
	op          compareOp
	left, right Expr
}

func (e *comparisonExpr) Eval(r Resolver) (bool, error) {
	lv, err := evalValue(e.left, r)
	if err != nil {
		return false, err
	}
	rv, err := evalValue(e.right, r)
	if err != nil {
		return false, err
	}
	return compareValues(e.op, lv, rv)
}

func (e *comparisonExpr) Metrics() []string {
	return mergeMetrics(e.left.Metrics(), e.right.Metrics())
}

func compareValues(op compareOp, l, r Value) (bool, error) {
	if l.Kind != r.Kind {
		return false, fmt.Errorf("type mismatch in comparison: %s vs %s", l.Kind, r.Kind)
	}
	switch l.Kind {
	case ValueNumber:
		switch op {
		case opLt:
			return l.Num < r.Num, nil
		case opLe:
			return l.Num <= r.Num, nil
		case opGt:
			return l.Num > r.Num, nil
		case opGe:
			return l.Num >= r.Num, nil
		case opEq:
			return l.Num == r.Num, nil
		case opNe:
			return l.Num != r.Num, nil
		}
	case ValueString:
		switch op {
		case opEq:
			return l.Str == r.Str, nil
		case opNe:
			return l.Str != r.Str, nil
		default:
			return false, fmt.Errorf("string values only support eq/ne comparisons")
		}
	case ValueBool:
		switch op {
		case opEq:
			return l.Bool == r.Bool, nil
		case opNe:
			return l.Bool != r.Bool, nil
		default:
			return false, fmt.Errorf("boolean values only support eq/ne comparisons")
		}
	case ValueMissing:
		return false, fmt.Errorf("cannot compare missing values")
	}
	return false, fmt.Errorf("unsupported value kind in comparison")
}

type metricRef struct{ name string }

func (m *metricRef) valueOf(r Resolver) (Value, error) {
	v, ok := r.ResolveMetric(m.name)
	if !ok || v.Kind == ValueMissing {
		return Value{}, fmt.Errorf("metric %q could not be resolved", m.name)
	}
	return v, nil
}

func (m *metricRef) Eval(r Resolver) (bool, error) {
	v, err := m.valueOf(r)
	if err != nil {
		return false, err
	}
	return truthiness(v)
}

func (m *metricRef) Metrics() []string { return []string{m.name} }

type numberLit struct{ val float64 }

func (n *numberLit) valueOf(Resolver) (Value, error) {
	return Value{Kind: ValueNumber, Num: n.val}, nil
}
func (n *numberLit) Eval(Resolver) (bool, error) { return n.val != 0, nil }
func (n *numberLit) Metrics() []string           { return nil }

type stringLit struct{ val string }

func (s *stringLit) valueOf(Resolver) (Value, error) {
	return Value{Kind: ValueString, Str: s.val}, nil
}
func (s *stringLit) Eval(Resolver) (bool, error) { return s.val != "", nil }
func (s *stringLit) Metrics() []string           { return nil }

type boolLit struct{ val bool }

func (b *boolLit) valueOf(Resolver) (Value, error) {
	return Value{Kind: ValueBool, Bool: b.val}, nil
}
func (b *boolLit) Eval(Resolver) (bool, error) { return b.val, nil }
func (b *boolLit) Metrics() []string           { return nil }
