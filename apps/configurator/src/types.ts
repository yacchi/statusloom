// TypeScript mirror of the DSL API wire contract (internal/webconfig/DSL_API.md).
// The AST JSON shape MUST stay in sync with internal/webconfig/astjson.go:
// omitted attributes are absent from the node object (never null/""), and a
// boolean present as `false` means an explicit `false` (overriding an
// inherited `true`).

// ---- diagnostics ----

export type Severity = "error" | "warning";

// Byte-offset span into the submitted DSL source ({0,0} = no location).
export interface AstRange {
    start: number;
    end: number;
}

export interface Diagnostic {
    severity: Severity;
    message: string;
    range: AstRange;
}

export function hasErrors(diags: readonly Diagnostic[]): boolean {
    return diags.some((d) => d.severity === "error");
}

// ---- AST JSON ----
//
// Node IDs are deterministic, position-derived paths (see DSL_API.md "Node
// IDs"): "root", "git", "L{i}", "L{i}.{j}", "{parent}.{k}", "{owner}.cr{c}",
// "root.c{k}", "L{i}.c{k}". They are stable across parses of the same source
// and match the IDs preview segments report. Nodes created client-side carry
// an empty id until the next serialize -> parse round trip assigns one.

// Drawable-node display attributes shared by line/span/text/field. Keys are
// the DSL attribute names verbatim (kebab-case).
export interface CommonAttrs {
    color?: string;
    background?: string;
    bold?: boolean;
    dim?: boolean;
    italic?: boolean;
    underline?: boolean;
    strikethrough?: boolean;
    // `padding` is emitted when left and right are equal; otherwise the
    // individual keys are emitted.
    padding?: number;
    "padding-left"?: number;
    "padding-right"?: number;
    prefix?: string;
    suffix?: string;
    optional?: string;
    when?: string;
}

interface AstBase {
    id: string;
    kind: string;
    // Absent on client-created nodes; the serializer tolerates that.
    range?: AstRange;
    // Wire-only flag: set by the visual-editor edit helpers on a node whose
    // content changed, so POST /api/dsl/serialize with a `baseSource` (the
    // minimal-diff path) regenerates only that node and reuses the rest
    // verbatim. Never emitted by the server; ignored by display/selection.
    dirty?: boolean;
}

export interface ColorRuleNode extends AstBase {
    kind: "color-rule";
    when?: string;
    color?: string;
}

export interface FieldNode extends AstBase, CommonAttrs {
    kind: "field";
    name?: string;
    format?: string;
    precision?: string;
    currency?: string;
    // Emitted only when true.
    raw?: boolean;
    hyperlink?: boolean;
    colorRules?: ColorRuleNode[];
}

export interface TextNode extends AstBase, CommonAttrs {
    kind: "text";
    value: string;
    role?: string; // "" is never emitted; "separator" is the only valid value
    colorRules?: ColorRuleNode[];
}

export interface FlexNode extends AstBase {
    kind: "flex";
    size?: string; // "full" (default when absent) | "full-minus-<N>"
}

export interface RawTextNode extends AstBase {
    kind: "raw-text";
    value: string;
}

export interface CommentNode extends AstBase {
    kind: "comment";
    value: string;
}

export interface SpanNode extends AstBase, CommonAttrs {
    kind: "span";
    children: LineChild[];
    colorRules?: ColorRuleNode[];
}

// Mixed-content child of a <line> or <span>.
export type LineChild =
    | SpanNode
    | TextNode
    | FieldNode
    | FlexNode
    | RawTextNode
    | CommentNode;

export interface LineNode extends AstBase, CommonAttrs {
    kind: "line";
    children: LineChild[];
}

export interface LayoutNode extends AstBase {
    kind: "layout";
    name?: string;
    active?: boolean;
    lines: LineNode[];
    comments?: CommentNode[];
}

export interface GitNode extends AstBase {
    kind: "git";
    "cache-ttl-ms"?: number;
    "timeout-ms"?: number;
    "include-untracked"?: boolean;
    "collect-numstat"?: boolean;
}

export interface StatusloomNode extends AstBase {
    kind: "statusloom";
    version?: string;
    tool?: string;
    "color-level"?: string;
    "output-style"?: "standard" | "powerline";
    "compact-threshold"?: number;
    "context-percentage-mode"?: string;
    "context-reserve-tokens"?: number;
    git?: GitNode;
    layouts: LayoutNode[];
    comments?: CommentNode[];
}

// Any addressable AST node.
export type AstNode =
    | StatusloomNode
    | GitNode
    | LayoutNode
    | LineNode
    | LineChild
    | ColorRuleNode;

// ---- endpoint request/response shapes ----

// GET /api/dsl/document and GET /api/dsl/draft.
export interface DocumentResponse {
    source: string;
    version: string; // sha256 hex of `source`
    exists: boolean;
}

// PUT /api/dsl/document (200 saved / 409 error diagnostics, nothing written)
// and PUT /api/dsl/draft (always 200; the draft write never blocks).
export interface PutSourceResponse {
    // False when the document PUT was rejected (409) because `source` has
    // error-severity diagnostics.
    saved: boolean;
    version: string;
    diagnostics: Diagnostic[];
}

// POST /api/dsl/parse.
export interface ParseResponse {
    // Present only when a root element was parsed (absent for a fatal XML
    // well-formedness error). May coexist with error diagnostics.
    ast?: StatusloomNode;
    diagnostics: Diagnostic[];
    version: string;
}

// POST /api/dsl/serialize request. `baseSource` (the client's last valid
// source, into which the AST node ranges index) enables minimal-diff
// serialization; omit it for whole-document canonical output.
export interface SerializeRequest {
    ast: StatusloomNode;
    baseSource?: string;
}

// POST /api/dsl/serialize response.
export interface SerializeResponse {
    source: string;
    diagnostics: Diagnostic[];
}

export type SampleKind = "full" | "early-session";

// POST /api/dsl/preview.
export interface PreviewRequest {
    tool: string;
    source: string;
    width: number;
    // Synthetic sample data; ignored by the backend when `sessionId` is set.
    sample: SampleKind;
    // When non-empty, render this captured session's snapshot instead of
    // `sample`. Unknown ids are rejected with 400.
    sessionId?: string;
    // Index of the layout being edited (clamped); the backend renders this
    // layout rather than the document's active one.
    layoutIndex?: number;
}

export interface PreviewSegment {
    // AST node ID of the owning node ("" for decoration segments with no
    // source node, e.g. a <line>'s own prefix/suffix). Span
    // prefix/suffix/padding segments carry the span's node ID.
    nodeId: string;
    text: string;
    ansi: string;
    visible: boolean;
}

export interface PreviewLine {
    omitted: boolean;
    ansi: string; // concatenated visible-segment ANSI for the whole line
    segments: PreviewSegment[];
}

export interface PreviewResponse {
    // Empty (with diagnostics) when the source is unparseable; keeping the
    // last good render is the client's responsibility.
    lines: PreviewLine[];
    diagnostics: Diagnostic[];
    // Present when the source parsed. `active === true` means every line is
    // omitted for the sample data, so the real status line falls back to
    // `ansi` (the built-in model + tool-version line).
    fallback?: {
        ansi: string;
        active: boolean;
    };
}

// GET /api/dsl/fields entry: the palette catalog, from the Go DSL registry.
export interface FieldCatalogEntry {
    name: string;
    displayName: string;
    descriptions: Record<string, string>; // {"en","ja"}
    category: string; // "common" | "claude"
    // True when this field supports the `hyperlink` attribute.
    linkable?: boolean;
    // The metric this field exposes as "self" in when/color-rule conditions.
    selfMetric?: string;
    // Formatter names applicable to this field (absent = no formatter).
    formats?: string[];
    // Single-field rendering against the full sample snapshot.
    preview: {
        text: string;
        ansi: string;
    };
}

// GET /api/dsl/metrics entry: named metrics for when / color-rule editing.
export interface Metric {
    name: string;
    displayName: string;
    descriptions: Record<string, string>;
}

// A previously captured real session usable as a preview data source (GET
// /api/sessions), newest first.
export interface SessionSummary {
    id: string;
    cwd: string;
    model: string;
    version: string;
    observedAt: string;
    ageSeconds: number;
    hasRateLimits: boolean;
    hasRepo: boolean;
}

// ---- UI-only types (not part of the frontend/backend JSON contract) ----

// The preview canvas's data source: either a synthetic sample or a captured
// real session. Only one of `sample`/`sessionId` is ever sent to
// POST /api/dsl/preview at a time (see App.tsx's preview dispatch effect).
export type PreviewSource =
    | { kind: "sample"; sample: SampleKind }
    | { kind: "session"; id: string };

// ---- live monitor (L2) ----

// Response of POST /api/live/session: a freshly provisioned monitored
// directory plus the shell command the user runs (in another terminal) to
// launch the coding agent inside it.
export interface LiveSessionInfo {
    launchCommand: string;
    tmpDir: string;
}

// A message pushed over GET /ws/live?token=<hex> each time the monitored
// session renders. `sessionId` matches a `SessionSummary.id` once the backend
// has captured that render.
export interface LiveUpdate {
    type: "live-update";
    sessionId: string;
    observedAt: string;
}

// ---- embedded terminal (L3) ----

// Response of POST /api/terminal/session: an id identifying a freshly spawned
// PTY-backed coding-agent process. The browser connects to it via
// GET /ws/terminal?token=<hex>&id=<terminalId>.
export interface TerminalSessionInfo {
    terminalId: string;
}

// Text frame the browser sends over /ws/terminal to inform the PTY of a
// viewport size change (all other browser->server frames are raw input bytes).
export interface TerminalResize {
    type: "resize";
    cols: number;
    rows: number;
}
