package cli

// unifiedDiff renders a line-based unified diff between two whole-document
// DSL sources, using only the standard library: an O(n*m) longest-common-
// subsequence dynamic program (diffLines) to compute the edit script, then a
// single hunk covering the whole document (config documents are small, so
// this stays simple and correct rather than replicating a multi-hunk
// context-collapsing algorithm). It never shells out to an external `diff`
// binary (plans/config-store-and-format.md §8.12).

import (
	"fmt"
	"strings"
)

// diffOp is one line of an edit script turning a into b: ' ' (unchanged,
// present in both), '-' (only in a), or '+' (only in b).
type diffOp struct {
	kind byte
	line string
}

// diffLines computes the edit script turning a into b via the classic LCS
// (longest common subsequence) dynamic program, preferring to consume a
// (delete) before b (insert) when both moves are equally good, so equal runs
// align the same way a standard line-diff would.
func diffLines(a, b []string) []diffOp {
	n, m := len(a), len(b)
	// lcs[i][j] = length of the LCS of a[i:] and b[j:].
	lcs := make([][]int, n+1)
	for i := range lcs {
		lcs[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else if lcs[i+1][j] >= lcs[i][j+1] {
				lcs[i][j] = lcs[i+1][j]
			} else {
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}

	ops := make([]diffOp, 0, n+m)
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case a[i] == b[j]:
			ops = append(ops, diffOp{' ', a[i]})
			i++
			j++
		case lcs[i+1][j] >= lcs[i][j+1]:
			ops = append(ops, diffOp{'-', a[i]})
			i++
		default:
			ops = append(ops, diffOp{'+', b[j]})
			j++
		}
	}
	for ; i < n; i++ {
		ops = append(ops, diffOp{'-', a[i]})
	}
	for ; j < m; j++ {
		ops = append(ops, diffOp{'+', b[j]})
	}
	return ops
}

// splitLines splits whole-document text into lines, dropping the single
// trailing empty element strings.Split produces for a trailing "\n" (so a
// document that ends with a newline doesn't get a spurious empty final
// line). Two texts differing only in trailing-newline presence therefore
// diff as identical; that is an accepted simplification for this internal
// tool, not a byte-exact diff replacement.
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	lines := strings.Split(s, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// unifiedDiff renders a unified diff of a (fromLabel) against b (toLabel):
// a "---"/"+++" header pair, one "@@" hunk header spanning the whole
// document, then every line prefixed ' '/'-'/'+' per diffLines. Callers
// should skip calling this when a == b (there is nothing to show).
func unifiedDiff(fromLabel, toLabel, a, b string) string {
	aLines := splitLines(a)
	bLines := splitLines(b)
	ops := diffLines(aLines, bLines)

	var out strings.Builder
	fmt.Fprintf(&out, "--- %s\n", fromLabel)
	fmt.Fprintf(&out, "+++ %s\n", toLabel)
	fmt.Fprintf(&out, "@@ -1,%d +1,%d @@\n", len(aLines), len(bLines))
	for _, op := range ops {
		fmt.Fprintf(&out, "%c%s\n", op.kind, op.line)
	}
	return out.String()
}
