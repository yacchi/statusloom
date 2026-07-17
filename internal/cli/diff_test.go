package cli

import (
	"strings"
	"testing"
)

func TestUnifiedDiff_HeaderAndHunk(t *testing.T) {
	a := "line1\nline2\nline3\n"
	b := "line1\nchanged\nline3\n"

	got := unifiedDiff("old", "new", a, b)
	wantPrefix := "--- old\n+++ new\n@@ -1,3 +1,3 @@\n"
	if !strings.HasPrefix(got, wantPrefix) {
		t.Fatalf("diff = %q, want prefix %q", got, wantPrefix)
	}
	if !strings.Contains(got, "-line2\n") {
		t.Errorf("diff missing removed line:\n%s", got)
	}
	if !strings.Contains(got, "+changed\n") {
		t.Errorf("diff missing added line:\n%s", got)
	}
	if !strings.Contains(got, " line1\n") || !strings.Contains(got, " line3\n") {
		t.Errorf("diff missing context lines:\n%s", got)
	}
}

func TestUnifiedDiff_Insertion(t *testing.T) {
	a := "one\ntwo\n"
	b := "one\ntwo\nthree\n"
	got := unifiedDiff("a", "b", a, b)
	if !strings.Contains(got, "+three\n") {
		t.Errorf("diff missing inserted line:\n%s", got)
	}
	for _, l := range strings.Split(strings.TrimRight(got, "\n"), "\n")[3:] {
		if strings.HasPrefix(l, "-") {
			t.Errorf("pure insertion should not produce a removed line: %q", l)
		}
	}
}

func TestUnifiedDiff_Deletion(t *testing.T) {
	a := "one\ntwo\nthree\n"
	b := "one\ntwo\n"
	got := unifiedDiff("a", "b", a, b)
	if !strings.Contains(got, "-three\n") {
		t.Errorf("diff missing removed line:\n%s", got)
	}
}

func TestSplitLines(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"a\n", []string{"a"}},
		{"a\nb", []string{"a", "b"}},
		{"a\nb\n", []string{"a", "b"}},
		{"\n", []string{""}},
	}
	for _, c := range cases {
		got := splitLines(c.in)
		if len(got) != len(c.want) {
			t.Errorf("splitLines(%q) = %v, want %v", c.in, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("splitLines(%q)[%d] = %q, want %q", c.in, i, got[i], c.want[i])
			}
		}
	}
}

func TestDiffLines_IdenticalProducesOnlyContext(t *testing.T) {
	ops := diffLines([]string{"a", "b"}, []string{"a", "b"})
	for _, op := range ops {
		if op.kind != ' ' {
			t.Fatalf("identical input produced a change op: %+v", op)
		}
	}
	if len(ops) != 2 {
		t.Fatalf("len(ops) = %d, want 2", len(ops))
	}
}
