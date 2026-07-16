// Convenience layer that maps a percent field's color-rules to / from a simple
// "threshold bar" model WITHOUT changing the DSL, renderer, or API: the bar is
// purely UI sugar over the existing `<color-rule when="self ge N" color=".."/>`
// mechanism (first-match-wins evaluation; a non-matching value falls back to
// the node's base color). The DSL stays the single source of truth.
//
// A bar is a base color plus ascending breakpoints t1 < t2 < ... < tk over the
// 0..100 range, splitting it into k+1 half-open bands [t_i, t_{i+1}). Band 0
// (from 0) is the node's own color (`node.color`, the same attribute the
// Decoration section edits — one source, no duplication); each higher band i
// is produced by a color-rule `self ge t_i`, emitted in DESCENDING order so
// first-match-wins picks the right band.

import type { ColorRuleNode } from "./types.ts";

// Shape common to field/span nodes as far as the threshold bar is concerned:
// a base color plus the color-rule list it drives / is driven by. Both
// FieldNode and SpanNode structurally satisfy this.
export interface ColorRuleHost {
    color?: string;
    colorRules?: ColorRuleNode[];
}

// Default source metric when the caller doesn't pick one: a field's own
// value (the historical, and still most common, `self ge N` pattern).
const DEFAULT_SOURCE = "self";

// Escapes a string for verbatim use inside a RegExp. Metric names are
// `[a-z0-9-]+` in practice, so this is defense-in-depth rather than a fix for
// an observed problem.
function escapeRegExp(s: string): string {
    return s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

export interface Band {
    // Lower bound (inclusive) of the band on the 0..100 scale. The base band
    // is always 0; higher bands carry their breakpoint value.
    from: number;
    // DSL color (ANSI kebab-case name or #hex), or undefined for
    // "inherit / none" (the base band's undefined means the node has no color).
    color: string | undefined;
}

// Smallest allowed gap between adjacent breakpoints (and the 0 / 100 edges),
// in percent. Keeps every band clickable and the generated rules strictly
// monotonic.
export const MIN_GAP = 1;

// Try to read a node's base color + color-rules as a threshold bar driven by
// `source` (a metric name, or "self" for the node's own value). Returns null
// when the rules are anything the bar cannot faithfully represent (compound /
// negated conditions, a different or mixed metric, operators other than
// ge/gt, non-descending or duplicate thresholds, values outside 0..100). The
// caller then keeps the raw "Advanced" editor. Empty color-rules parse to a
// single base band, so every eligible node starts with an (empty) bar.
export function parseThresholdBands(
    node: ColorRuleHost,
    source: string = DEFAULT_SOURCE,
): Band[] | null {
    const rules = node.colorRules ?? [];
    const parsed: { n: number; color: string | undefined }[] = [];
    for (const r of rules) {
        const t = parseThresholdRule(r, source);
        if (!t) {
            return null;
        }
        parsed.push(t);
    }
    // The generated form is strictly descending; require the same so the round
    // trip is exact and first-match-wins semantics stay unambiguous.
    for (let i = 1; i < parsed.length; i += 1) {
        if (parsed[i].n >= parsed[i - 1].n) {
            return null;
        }
    }
    // Ascending bands: base first, then each breakpoint from low to high.
    const bands: Band[] = [{ from: 0, color: node.color }];
    for (let i = parsed.length - 1; i >= 0; i -= 1) {
        bands.push({ from: parsed[i].n, color: parsed[i].color });
    }
    return bands;
}

// A rule qualifies only if its `when` is exactly `<source> <ge|gt> <number>`
// with the number strictly inside (0, 100). `gt` is accepted on the way in
// but the bar normalizes everything to `ge` on the way out (see
// buildColorRules).
function parseThresholdRule(
    rule: ColorRuleNode,
    source: string = DEFAULT_SOURCE,
): { n: number; color: string | undefined } | null {
    const when = (rule.when ?? "").trim();
    const re = new RegExp(`^${escapeRegExp(source)}\\s+(?:ge|gt)\\s+(\\d+(?:\\.\\d+)?)$`);
    const m = re.exec(when);
    if (!m) {
        return null;
    }
    const n = Number(m[1]);
    if (!(n > 0 && n < 100)) {
        return null;
    }
    return { n, color: rule.color };
}

// Build the base color + color-rule list from bar bands (band 0 = base color)
// driven by `source` (a metric name, or "self" for the node's own value).
// Higher bands emit `<source> ge <from>` in descending order (first match
// wins).
export function buildColorRules(
    bands: Band[],
    source: string = DEFAULT_SOURCE,
): {
    color: string | undefined;
    colorRules: ColorRuleNode[];
} {
    const rules: ColorRuleNode[] = [];
    for (let i = bands.length - 1; i >= 1; i -= 1) {
        rules.push({
            id: "",
            kind: "color-rule",
            when: `${source} ge ${bands[i].from}`,
            color: bands[i].color,
        });
    }
    return { color: bands[0]?.color, colorRules: rules };
}

// Infer the source metric a node's existing color-rules are driven by, by
// reading it off the first rule (`<metric> <ge|gt> <number>`) and then
// confirming the whole rule list is a valid strictly-descending threshold
// bar for that same metric. Returns null when there are no rules yet, or the
// rules are not a bar the source selector can represent (mixed metrics,
// compound conditions, non-descending thresholds, etc).
export function detectThresholdSource(node: ColorRuleHost): string | null {
    const rules = node.colorRules ?? [];
    if (rules.length === 0) {
        return null;
    }
    const first = /^([a-z0-9-]+)\s+(?:ge|gt)\s+\d+(?:\.\d+)?$/.exec((rules[0].when ?? "").trim());
    if (!first) {
        return null;
    }
    const source = first[1];
    return parseThresholdBands(node, source) !== null ? source : null;
}

// Insert a breakpoint at `value`, splitting the band it lands in; the new upper
// band inherits that band's color (the caller opens its picker so the user can
// recolor it right away). Returns index -1 (unchanged bands) when `value`
// collides with an existing breakpoint or the 0 / 100 edges.
export function addBreakpoint(bands: Band[], value: number): { bands: Band[]; index: number } {
    const v = Math.round(value);
    if (v < MIN_GAP || v > 100 - MIN_GAP) {
        return { bands, index: -1 };
    }
    if (bands.some((b) => Math.abs(b.from - v) < MIN_GAP)) {
        return { bands, index: -1 };
    }
    let index = bands.length;
    for (let i = 0; i < bands.length; i += 1) {
        if (bands[i].from > v) {
            index = i;
            break;
        }
    }
    const inheritFrom = bands[index - 1] ?? bands[0];
    const next = [...bands];
    next.splice(index, 0, { from: v, color: inheritFrom.color });
    return { bands: next, index };
}

// Move the breakpoint at band index `i` (i >= 1) to `value`, clamped to stay
// strictly between its neighbors (keeps bands ascending and non-empty). Band 0
// (the base) has no breakpoint and cannot be moved.
export function moveBreakpoint(bands: Band[], i: number, value: number): Band[] {
    if (i < 1 || i >= bands.length) {
        return bands;
    }
    const lo = bands[i - 1].from + MIN_GAP;
    const hi = (bands[i + 1]?.from ?? 100) - MIN_GAP;
    const v = Math.round(Math.min(Math.max(value, lo), hi));
    const next = [...bands];
    next[i] = { ...next[i], from: v };
    return next;
}

// Remove the breakpoint at band index `i` (i >= 1); band 0 (base) is permanent.
export function removeBreakpoint(bands: Band[], i: number): Band[] {
    if (i < 1 || i >= bands.length) {
        return bands;
    }
    return bands.filter((_, j) => j !== i);
}

// Recolor band `i` (0 = base color; higher = a threshold band).
export function setBandColor(bands: Band[], i: number, color: string | undefined): Band[] {
    if (i < 0 || i >= bands.length) {
        return bands;
    }
    const next = [...bands];
    next[i] = { ...next[i], color };
    return next;
}
