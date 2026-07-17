// Line-based diff between two source texts (used by the history panel's diff
// view). Pure and framework-agnostic, like history.ts. Deliberately not an
// external dependency (CLAUDE.md UI rule: no new external UI deps) — a plain
// LCS-based line diff is enough for readable DSL-source diffs.

export type DiffOp = "equal" | "add" | "remove";

export interface DiffLine {
    op: DiffOp;
    text: string;
}

// Longest-common-subsequence line diff of `a` (old) against `b` (new).
// O(n*m) time/space, fine for the small documents Statusloom's DSL produces.
export function diffLines(a: string, b: string): DiffLine[] {
    const linesA = a.split("\n");
    const linesB = b.split("\n");
    const n = linesA.length;
    const m = linesB.length;

    // dp[i][j] = length of the LCS of linesA[i:] and linesB[j:].
    const dp: number[][] = Array.from({ length: n + 1 }, () => new Array(m + 1).fill(0));
    for (let i = n - 1; i >= 0; i -= 1) {
        for (let j = m - 1; j >= 0; j -= 1) {
            dp[i][j] =
                linesA[i] === linesB[j]
                    ? dp[i + 1][j + 1] + 1
                    : Math.max(dp[i + 1][j], dp[i][j + 1]);
        }
    }

    const result: DiffLine[] = [];
    let i = 0;
    let j = 0;
    while (i < n && j < m) {
        if (linesA[i] === linesB[j]) {
            result.push({ op: "equal", text: linesA[i] });
            i += 1;
            j += 1;
        } else if (dp[i + 1][j] >= dp[i][j + 1]) {
            result.push({ op: "remove", text: linesA[i] });
            i += 1;
        } else {
            result.push({ op: "add", text: linesB[j] });
            j += 1;
        }
    }
    while (i < n) {
        result.push({ op: "remove", text: linesA[i] });
        i += 1;
    }
    while (j < m) {
        result.push({ op: "add", text: linesB[j] });
        j += 1;
    }
    return result;
}
