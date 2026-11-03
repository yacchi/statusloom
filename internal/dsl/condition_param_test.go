package dsl

import (
	"errors"
	"reflect"
	"testing"
)

// recordingResolver records every metric name it is asked to resolve so a
// test can check the full `name:param` reference reaches the resolver.
type recordingResolver struct {
	names []string
}

func (r *recordingResolver) ResolveMetric(name string) (Value, bool) {
	r.names = append(r.names, name)
	return boolVal(true), true
}

func TestParseConditionParamReference(t *testing.T) {
	cases := []struct {
		name        string
		src         string
		wantMetrics []string
	}{
		{"single", "source-stale:cc", []string{"source-stale:cc"}},
		{"with-and", "source-stale:a and cache-warm", []string{"source-stale:a", "cache-warm"}},
		{"keyword-prefix-not", "not:x", []string{"not:x"}},
		{"keyword-prefix-and", "and:x", []string{"and:x"}},
		{"digits-and-hyphen", "source-stale:cc-1", []string{"source-stale:cc-1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			expr, err := ParseCondition(tc.src)
			if err != nil {
				t.Fatalf("ParseCondition(%q) unexpected error: %v", tc.src, err)
			}
			if got := expr.Metrics(); !reflect.DeepEqual(got, tc.wantMetrics) {
				t.Fatalf("Metrics() = %v, want %v", got, tc.wantMetrics)
			}
		})
	}
}

func TestParseConditionParamErrors(t *testing.T) {
	cases := []struct {
		name   string
		src    string
		offset int
	}{
		{"trailing-colon", "source-stale:", 12},
		{"digit-after-colon", "a:1", 1},
		{"space-after-colon", "a: b", 1},
		{"leading-colon", ":x", 0},
		{"uppercase-param", "source-stale:Cc", 12},
		{"second-colon", "a:b:c", 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseCondition(tc.src)
			if err == nil {
				t.Fatalf("ParseCondition(%q) expected error", tc.src)
			}
			var synErr *SyntaxError
			if !errors.As(err, &synErr) {
				t.Fatalf("ParseCondition(%q) error type = %T, want *SyntaxError", tc.src, err)
			}
			if synErr.Offset != tc.offset {
				t.Fatalf("ParseCondition(%q) offset = %d, want %d (%v)", tc.src, synErr.Offset, tc.offset, err)
			}
			if want := `unexpected character ":"`; synErr.Message != want {
				t.Fatalf("ParseCondition(%q) message = %q, want %q", tc.src, synErr.Message, want)
			}
		})
	}
}

func TestConditionParamResolverReceivesFullName(t *testing.T) {
	expr := mustParseCondition(t, "source-stale:cc")
	r := &recordingResolver{}
	if _, err := expr.Eval(r); err != nil {
		t.Fatalf("Eval unexpected error: %v", err)
	}
	if want := []string{"source-stale:cc"}; !reflect.DeepEqual(r.names, want) {
		t.Fatalf("resolver names = %v, want %v", r.names, want)
	}
}

func TestFormatConditionParamRoundTrip(t *testing.T) {
	cases := []string{
		"not source-stale:cc",
		"source-stale:a and (source-stale:b or source-stale:c)",
	}
	for _, src := range cases {
		got, err := FormatCondition(src)
		if err != nil {
			t.Fatalf("FormatCondition(%q) error: %v", src, err)
		}
		if got != src {
			t.Fatalf("FormatCondition(%q) = %q, want unchanged", src, got)
		}
	}
}
