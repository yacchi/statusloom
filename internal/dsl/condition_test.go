package dsl

import (
	"reflect"
	"strings"
	"testing"
)

// mapResolver is a test Resolver backed by a plain map. A name absent
// from the map resolves as "not found" (ok=false), matching an
// unresolvable metric.
type mapResolver map[string]Value

func (m mapResolver) ResolveMetric(name string) (Value, bool) {
	v, ok := m[name]
	return v, ok
}

func numVal(n float64) Value { return Value{Kind: ValueNumber, Num: n} }
func strVal(s string) Value  { return Value{Kind: ValueString, Str: s} }
func boolVal(b bool) Value   { return Value{Kind: ValueBool, Bool: b} }
func missingVal() Value      { return Value{Kind: ValueMissing} }

func mustParseCondition(t *testing.T, src string) Expr {
	t.Helper()
	expr, err := ParseCondition(src)
	if err != nil {
		t.Fatalf("ParseCondition(%q) unexpected error: %v", src, err)
	}
	return expr
}

func evalCondition(t *testing.T, src string, r Resolver) bool {
	t.Helper()
	expr := mustParseCondition(t, src)
	got, err := expr.Eval(r)
	if err != nil {
		t.Fatalf("Eval(%q) unexpected error: %v", src, err)
	}
	return got
}

func TestCondition_WordComparators(t *testing.T) {
	r := mapResolver{"context-percent": numVal(42)}
	cases := []struct {
		src  string
		want bool
	}{
		{"context-percent lt 50", true},
		{"context-percent lt 40", false},
		{"context-percent le 42", true},
		{"context-percent le 41", false},
		{"context-percent gt 40", true},
		{"context-percent gt 42", false},
		{"context-percent ge 42", true},
		{"context-percent ge 43", false},
		{"context-percent eq 42", true},
		{"context-percent eq 41", false},
		{"context-percent ne 41", true},
		{"context-percent ne 42", false},
	}
	for _, c := range cases {
		if got := evalCondition(t, c.src, r); got != c.want {
			t.Errorf("%s = %v, want %v", c.src, got, c.want)
		}
	}
}

func TestCondition_SymbolComparators(t *testing.T) {
	r := mapResolver{"context-percent": numVal(42)}
	cases := []struct {
		src  string
		want bool
	}{
		{"context-percent < 50", true},
		{"context-percent <= 42", true},
		{"context-percent > 40", true},
		{"context-percent >= 42", true},
		{"context-percent == 42", true},
		{"context-percent != 42", false},
	}
	for _, c := range cases {
		if got := evalCondition(t, c.src, r); got != c.want {
			t.Errorf("%s = %v, want %v", c.src, got, c.want)
		}
	}
}

func TestCondition_LogicalOperators_WordAndSymbol(t *testing.T) {
	r := mapResolver{"a": boolVal(true), "b": boolVal(false)}
	cases := []struct {
		src  string
		want bool
	}{
		{"a and b", false},
		{"a && b", false},
		{"a or b", true},
		{"a || b", true},
		{"not b", true},
		{"!b", true},
		{"a and not b", true},
		{"a && !b", true},
	}
	for _, c := range cases {
		if got := evalCondition(t, c.src, r); got != c.want {
			t.Errorf("%s = %v, want %v", c.src, got, c.want)
		}
	}
}

func TestCondition_Parentheses(t *testing.T) {
	r := mapResolver{"a": boolVal(true), "b": boolVal(false), "c": boolVal(false)}
	// not (a and b) or c == not(true and false) or false == true or false == true
	if got := evalCondition(t, "not (a and b) or c", r); !got {
		t.Errorf("not (a and b) or c = %v, want true", got)
	}
}

func TestCondition_Precedence_AndBindsTighterThanOr(t *testing.T) {
	// a or b and c should parse as: a or (b and c).
	// Discriminating assignment: a=true, b=true, c=false.
	//   correct:   a or (b and c) = true or (true and false) = true
	//   wrong assoc ((a or b) and c) = (true or true) and false = false
	r := mapResolver{"a": boolVal(true), "b": boolVal(true), "c": boolVal(false)}
	if got := evalCondition(t, "a or b and c", r); !got {
		t.Errorf("a or b and c = %v, want true (and must bind tighter than or)", got)
	}
}

func TestCondition_Precedence_NotBindsTighterThanAnd(t *testing.T) {
	// not a and b should parse as: (not a) and b.
	// Discriminating assignment: a=true, b=false.
	//   correct:      (not a) and b = false and false = false
	//   wrong grouping not (a and b) = not (true and false) = true
	r := mapResolver{"a": boolVal(true), "b": boolVal(false)}
	if got := evalCondition(t, "not a and b", r); got {
		t.Errorf("not a and b = %v, want false (not must bind tighter than and)", got)
	}
}

func TestCondition_ComparisonChainingIsParseError(t *testing.T) {
	if _, err := ParseCondition("a lt b lt c"); err == nil {
		t.Fatal("expected parse error for chained comparison \"a lt b lt c\"")
	}
}

func TestCondition_NumberLiterals(t *testing.T) {
	r := mapResolver{}
	if !evalCondition(t, "42", r) {
		t.Error("42 should be truthy")
	}
	if evalCondition(t, "0", r) {
		t.Error("0 should be falsy")
	}
	if !evalCondition(t, "-1", r) {
		t.Error("-1 should be truthy")
	}
	if evalCondition(t, "0.0", r) {
		t.Error("0.0 should be falsy")
	}
	if !evalCondition(t, "3.14", r) {
		t.Error("3.14 should be truthy")
	}
	if !evalCondition(t, "-2.5", r) {
		t.Error("-2.5 should be truthy")
	}
}

func TestCondition_StringLiterals_BothQuoteStyles(t *testing.T) {
	r := mapResolver{}
	if !evalCondition(t, "'hello'", r) {
		t.Error("'hello' should be truthy")
	}
	if !evalCondition(t, `"hello"`, r) {
		t.Error(`"hello" should be truthy`)
	}
	if evalCondition(t, "''", r) {
		t.Error("'' should be falsy")
	}
	if evalCondition(t, `""`, r) {
		t.Error(`"" should be falsy`)
	}
}

func TestCondition_StringLiterals_Escapes(t *testing.T) {
	r := mapResolver{"s": strVal(`a'b"c\d`)}
	// Single-quoted literal containing an escaped single quote, a raw
	// double quote, and an escaped backslash.
	expr := mustParseCondition(t, `s eq 'a\'b"c\\d'`)
	got, err := expr.Eval(r)
	if err != nil {
		t.Fatalf("Eval error: %v", err)
	}
	if !got {
		t.Error("escaped string literal did not match expected decoded value")
	}
}

func TestCondition_BooleanLiterals(t *testing.T) {
	r := mapResolver{}
	if !evalCondition(t, "true", r) {
		t.Error("true should be truthy")
	}
	if evalCondition(t, "false", r) {
		t.Error("false should be falsy")
	}
	if evalCondition(t, "not true", r) {
		t.Error("not true should be falsy")
	}
}

func TestCondition_Truthiness_Bool(t *testing.T) {
	r := mapResolver{"b": boolVal(true)}
	if !evalCondition(t, "b", r) {
		t.Error("bool metric true should be truthy")
	}
	r2 := mapResolver{"b": boolVal(false)}
	if evalCondition(t, "b", r2) {
		t.Error("bool metric false should be falsy")
	}
}

func TestCondition_Truthiness_Number(t *testing.T) {
	if !evalCondition(t, "n", mapResolver{"n": numVal(5)}) {
		t.Error("nonzero number metric should be truthy")
	}
	if evalCondition(t, "n", mapResolver{"n": numVal(0)}) {
		t.Error("zero number metric should be falsy")
	}
}

func TestCondition_Truthiness_String(t *testing.T) {
	if !evalCondition(t, "s", mapResolver{"s": strVal("x")}) {
		t.Error("nonempty string metric should be truthy")
	}
	if evalCondition(t, "s", mapResolver{"s": strVal("")}) {
		t.Error("empty string metric should be falsy")
	}
}

func TestCondition_Truthiness_MissingIsError(t *testing.T) {
	expr := mustParseCondition(t, "x")
	if _, err := expr.Eval(mapResolver{"x": missingVal()}); err == nil {
		t.Fatal("expected error evaluating truthiness of an explicitly-missing value")
	}
}

func TestCondition_UnresolvedMetricIsError(t *testing.T) {
	expr := mustParseCondition(t, "x")
	if _, err := expr.Eval(mapResolver{}); err == nil {
		t.Fatal("expected error evaluating an unresolved metric reference")
	}
}

func TestCondition_UnresolvedMetric_NestedInLogic(t *testing.T) {
	// Even under "and"/"or", an unresolvable metric anywhere must
	// surface as an Eval error (the owning node becomes hidden), not be
	// silently short-circuited away.
	r := mapResolver{"a": boolVal(false)}
	expr := mustParseCondition(t, "a and missing")
	if _, err := expr.Eval(r); err == nil {
		t.Fatal("expected error: \"missing\" cannot be resolved")
	}
}

func TestCondition_TypeMismatchIsError(t *testing.T) {
	r := mapResolver{"n": numVal(1), "s": strVal("x")}
	expr := mustParseCondition(t, "n eq s")
	if _, err := expr.Eval(r); err == nil {
		t.Fatal("expected type mismatch error comparing number to string")
	}
}

func TestCondition_StringOrderingIsError(t *testing.T) {
	r := mapResolver{"s": strVal("a")}
	expr := mustParseCondition(t, "s lt 'b'")
	if _, err := expr.Eval(r); err == nil {
		t.Fatal("expected error: strings only support eq/ne")
	}
}

func TestCondition_StringEqualityIsAllowed(t *testing.T) {
	r := mapResolver{"s": strVal("a")}
	if !evalCondition(t, "s eq 'a'", r) {
		t.Error(`s eq 'a' = false, want true`)
	}
	if evalCondition(t, "s eq 'b'", r) {
		t.Error(`s eq 'b' = true, want false`)
	}
}

func TestCondition_BoolOrderingIsError(t *testing.T) {
	r := mapResolver{"b": boolVal(true)}
	expr := mustParseCondition(t, "b lt true")
	if _, err := expr.Eval(r); err == nil {
		t.Fatal("expected error: booleans only support eq/ne")
	}
}

func TestCondition_BoolEqualityIsAllowed(t *testing.T) {
	r := mapResolver{"b": boolVal(true)}
	if !evalCondition(t, "b eq true", r) {
		t.Error("b eq true = false, want true")
	}
	if !evalCondition(t, "b ne false", r) {
		t.Error("b ne false = false, want true")
	}
}

func TestCondition_SelfIsAnOrdinaryIdentifier(t *testing.T) {
	r := mapResolver{"self": numVal(90)}
	if !evalCondition(t, "self ge 80", r) {
		t.Error("self ge 80 = false, want true")
	}
}

func TestCondition_Metrics_DedupedInOrder(t *testing.T) {
	expr := mustParseCondition(t, "a and (b or a) and not c")
	got := expr.Metrics()
	want := []string{"a", "b", "c"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Metrics() = %v, want %v", got, want)
	}
}

func TestCondition_Metrics_LiteralsContributeNone(t *testing.T) {
	expr := mustParseCondition(t, "true and 'x' eq 'y' and 1 gt 0")
	if got := expr.Metrics(); len(got) != 0 {
		t.Errorf("Metrics() = %v, want empty", got)
	}
}

func TestCondition_ParseError_HasOffset(t *testing.T) {
	_, err := ParseCondition("a lt")
	if err == nil {
		t.Fatal("expected parse error")
	}
	se, ok := err.(*SyntaxError)
	if !ok {
		t.Fatalf("error type = %T, want *SyntaxError", err)
	}
	if se.Offset != len("a lt") {
		t.Errorf("Offset = %d, want %d", se.Offset, len("a lt"))
	}
	if !strings.Contains(err.Error(), "4") {
		t.Errorf("error message %q does not mention the offset", err.Error())
	}
}

func TestCondition_ParseError_UnknownCharacter(t *testing.T) {
	if _, err := ParseCondition("a @ b"); err == nil {
		t.Fatal("expected parse error for unknown character '@'")
	}
}

func TestCondition_ParseError_UnescapedSymbolsRejected(t *testing.T) {
	// Raw '<' and '&&' must lex fine when passed directly (post-XML
	// decoding they arrive unescaped); this test only guards that a lone
	// '&' or '|' (not doubled) is rejected rather than silently accepted.
	if _, err := ParseCondition("a & b"); err == nil {
		t.Fatal("expected parse error for lone '&'")
	}
	if _, err := ParseCondition("a | b"); err == nil {
		t.Fatal("expected parse error for lone '|'")
	}
	if _, err := ParseCondition("a = b"); err == nil {
		t.Fatal("expected parse error for lone '='")
	}
}

func TestCondition_ParseError_UnterminatedString(t *testing.T) {
	if _, err := ParseCondition("a eq 'unterminated"); err == nil {
		t.Fatal("expected parse error for unterminated string literal")
	}
}

func TestCondition_ParseError_UnmatchedParen(t *testing.T) {
	if _, err := ParseCondition("(a and b"); err == nil {
		t.Fatal("expected parse error for unmatched '('")
	}
}

func TestCondition_ParseError_EmptyExpression(t *testing.T) {
	if _, err := ParseCondition(""); err == nil {
		t.Fatal("expected parse error for empty expression")
	}
}

func TestCondition_ParseError_TrailingTokens(t *testing.T) {
	if _, err := ParseCondition("a eq 1 b"); err == nil {
		t.Fatal("expected parse error for trailing tokens after a complete expression")
	}
}

func TestCondition_MixedWordAndSymbolForms(t *testing.T) {
	r := mapResolver{"a": boolVal(true), "b": boolVal(true), "n": numVal(5)}
	if !evalCondition(t, "a and n > 0 || not b", r) {
		t.Error("mixed word/symbol expression evaluated incorrectly")
	}
}
