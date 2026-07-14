package dsl

import "testing"

func TestSourceRange_Len(t *testing.T) {
	cases := []struct {
		r    SourceRange
		want int
	}{
		{SourceRange{0, 0}, 0},
		{SourceRange{3, 7}, 4},
		{SourceRange{5, 5}, 0},
		{SourceRange{7, 3}, 0}, // malformed: End < Start
	}
	for _, c := range cases {
		if got := c.r.Len(); got != c.want {
			t.Errorf("SourceRange%+v.Len() = %d, want %d", c.r, got, c.want)
		}
	}
}

func TestSourceRange_IsZero(t *testing.T) {
	if !(SourceRange{}).IsZero() {
		t.Error("zero-value SourceRange should be IsZero")
	}
	if (SourceRange{Start: 1, End: 1}).IsZero() {
		t.Error("SourceRange{1,1} should not be IsZero")
	}
}

func TestSourceRange_Contains(t *testing.T) {
	r := SourceRange{Start: 5, End: 10}
	for _, off := range []int{5, 7, 9} {
		if !r.Contains(off) {
			t.Errorf("Contains(%d) = false, want true", off)
		}
	}
	for _, off := range []int{4, 10, 11} {
		if r.Contains(off) {
			t.Errorf("Contains(%d) = true, want false", off)
		}
	}
}

func TestSourceRange_Slice(t *testing.T) {
	src := "hello world"
	if got := (SourceRange{0, 5}).Slice(src); got != "hello" {
		t.Errorf("Slice() = %q, want %q", got, "hello")
	}
	if got := (SourceRange{6, 11}).Slice(src); got != "world" {
		t.Errorf("Slice() = %q, want %q", got, "world")
	}
	if got := (SourceRange{0, 100}).Slice(src); got != "" {
		t.Errorf("out-of-bounds Slice() = %q, want empty", got)
	}
	if got := (SourceRange{-1, 5}).Slice(src); got != "" {
		t.Errorf("negative-start Slice() = %q, want empty", got)
	}
	if got := (SourceRange{5, 2}).Slice(src); got != "" {
		t.Errorf("inverted Slice() = %q, want empty", got)
	}
}
