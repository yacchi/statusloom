package dsl

import "testing"

func TestFormatCondition(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		// Symbolic operators normalize to words.
		{"and-symbol", "a && b", "a and b"},
		{"or-symbol", "a || b", "a or b"},
		{"not-symbol", "!a", "not a"},
		{"ge-symbol", "context-percent >= 80", "context-percent ge 80"},
		{"le-symbol", "x <= 5", "x le 5"},
		{"lt-symbol", "x < 5", "x lt 5"},
		{"gt-symbol", "x > 5", "x gt 5"},
		{"eq-symbol", "x == 5", "x eq 5"},
		{"ne-symbol", "x != 5", "x ne 5"},
		// Word operators are preserved.
		{"word-ops", "x ge 90 and y le 10", "x ge 90 and y le 10"},
		// Redundant parentheses are dropped.
		{"redundant-parens", "(a)", "a"},
		{"redundant-outer", "(a or b)", "a or b"},
		// and binds tighter than or: no parens needed.
		{"prec-no-parens", "a or b and c", "a or b and c"},
		// Required parentheses are kept.
		{"required-parens", "a and (b or c)", "a and (b or c)"},
		{"required-left", "(a or b) and c", "(a or b) and c"},
		// not precedence: `not a and b` is `(not a) and b`.
		{"not-prec", "not a and b", "not a and b"},
		{"not-group", "not (a and b)", "not (a and b)"},
		{"double-not", "not not a", "not not a"},
		// A compound operand of a comparison needs parentheses.
		{"compound-operand", "(a or b) eq true", "(a or b) eq true"},
		// Literals.
		{"number", "x ge 80", "x ge 80"},
		{"float", "x ge 90.5", "x ge 90.5"},
		{"bool", "git-dirty eq true", "git-dirty eq true"},
		{"string-double-to-single", `model eq "opus"`, `model eq 'opus'`},
		{"string-amp", "model ne 'a&b'", "model ne 'a&b'"},
		{"string-escape-quote", `model eq 'a\'b'`, `model eq 'a\'b'`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := FormatCondition(tc.in)
			if err != nil {
				t.Fatalf("FormatCondition(%q) error: %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("FormatCondition(%q) = %q, want %q", tc.in, got, tc.want)
			}
			// The normalized form must itself parse and re-normalize to the
			// same string (idempotence).
			again, err := FormatCondition(got)
			if err != nil {
				t.Fatalf("re-format(%q) error: %v", got, err)
			}
			if again != got {
				t.Errorf("FormatCondition not idempotent: %q -> %q", got, again)
			}
		})
	}
}

func TestFormatCondition_Error(t *testing.T) {
	if _, err := FormatCondition("a and"); err == nil {
		t.Error("expected error for a trailing operator")
	}
	if _, err := FormatCondition("&"); err == nil {
		t.Error("expected error for a lone ampersand")
	}
}
