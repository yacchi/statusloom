// Parsing and formatting of the SIMPLE shape of a `when` expression —
// "<metric> <op> <value>" — so the UI can offer pickers instead of demanding
// that the whole DSL condition grammar be typed by hand.
//
// The DSL's grammar (internal/dsl/condition.go) is larger than this: and/or/not,
// parentheses, several comparisons. Anything beyond one comparison stays a raw
// text edit; parseSimpleWhen returns null for it and the caller falls back to a
// free-text box. That keeps this module honest — it never rewrites an
// expression it did not fully understand.

export type WhenOp = "eq" | "ne" | "lt" | "le" | "gt" | "ge";

export const WHEN_OPS: readonly WhenOp[] = ["eq", "ne", "lt", "le", "gt", "ge"];

// The operators a string-valued metric (MetricDef.Text) accepts: ordering
// comparisons are meaningless for it, and the backend rejects them.
export const TEXT_WHEN_OPS: readonly WhenOp[] = ["eq", "ne"];

export interface SimpleWhen {
    metric: string;
    op: WhenOp;
    // The comparison value WITHOUT quotes, i.e. what a user types in a box.
    value: string;
    // Whether the value was (and must be re-emitted as) a quoted string
    // literal. A string metric compares against `"claude_team"`; a number or
    // boolean is bare (`80`, `true`).
    quoted: boolean;
}

// One comparison and nothing else. The value alternatives are ordered so a
// quoted literal wins over the bare form, and the bare form deliberately
// excludes whitespace: that is what makes `a eq 1 and b eq 2` fail to match
// (its value would have to contain spaces) instead of being mis-parsed as a
// single comparison against "1 and b eq 2".
const SIMPLE_WHEN =
    /^\s*([a-z][a-z0-9-]*)\s+(lt|le|gt|ge|eq|ne)\s+(?:"([^"]*)"|'([^']*)'|([^\s"']+))\s*$/;

// parseSimpleWhen returns the three parts of a single-comparison expression, or
// null when `expr` is empty or anything more complex.
export function parseSimpleWhen(expr: string | undefined): SimpleWhen | null {
    if (!expr) {
        return null;
    }
    const m = SIMPLE_WHEN.exec(expr);
    if (!m) {
        return null;
    }
    const [, metric, op, dq, sq, bare] = m;
    const quoted = dq !== undefined || sq !== undefined;
    return {
        metric,
        op: op as WhenOp,
        value: quoted ? (dq ?? sq ?? "") : bare,
        quoted,
    };
}

// formatSimpleWhen renders the parts back into DSL source. An empty value
// yields "" (no condition) rather than a syntactically broken comparison, so a
// half-filled builder simply leaves the variant unconditional.
export function formatSimpleWhen(c: SimpleWhen): string {
    if (c.metric === "" || c.value === "") {
        return "";
    }
    return c.quoted
        ? `${c.metric} ${c.op} "${c.value}"`
        : `${c.metric} ${c.op} ${c.value}`;
}
