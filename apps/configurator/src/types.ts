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

// <subagent> — a subagent-region container held in a dedicated
// LayoutNode.subagent / VariantNode.subagent field (never a Children entry),
// so it is always rendered below its container's lines. It holds exactly one
// <line> (task-* fields). Its node ID is its container's ID with a ".s"
// suffix ("L{i}.s" / "L{i}.{p}.v{v}.s"), and — since it holds at most one
// line — that line SHARES the region's ID rather than gaining an extra
// nesting level (its children then read "L{i}.s.{k}"). Carries no attributes
// of its own; the inner <line>/<span>/<field> carry decoration/when/optional.
export interface SubagentNode extends AstBase {
    kind: "subagent";
    line: LineNode;
    comments?: CommentNode[];
}

// A layout's ordered child: its rendered rows (LineNode) interleaved with any
// width-adaptive containers (ResponsiveNode). A responsive-free layout's
// children are all LineNode, and its node IDs are unchanged from before this
// type existed (see DSL_API.md "Node IDs").
export type LayoutChild = LineNode | ResponsiveNode;

// <variant> — one rendering candidate inside a <responsive>. Never appears
// outside a ResponsiveNode's `variants`; carries no attributes of its own.
export interface VariantNode extends AstBase {
    kind: "variant";
    lines: LineNode[];
    // Optional <subagent> region rendered below this variant's lines.
    subagent?: SubagentNode;
    comments?: CommentNode[];
}

// <responsive> — a layout-child-only, width-adaptive container: the renderer
// picks the first variant (widest first) all of whose lines fit the terminal
// width, falling back to the last variant when none fit (unknown width ->
// the first/widest variant). Carries no attributes of its own.
export interface ResponsiveNode extends AstBase {
    kind: "responsive";
    variants: VariantNode[];
    comments?: CommentNode[];
}

export interface LayoutNode extends AstBase {
    kind: "layout";
    name?: string;
    active?: boolean;
    children: LayoutChild[];
    // Optional <subagent> region rendered below the layout's lines (used when
    // the layout has no <responsive>; otherwise each variant carries its own).
    subagent?: SubagentNode;
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
    | ResponsiveNode
    | VariantNode
    | SubagentNode
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

export type SampleKind = "full" | "early-session" | "subagent-running" | "subagent-completed";

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
    // When true, every <responsive>'s variant is rendered (each with its
    // real values, as if selected) instead of only the width-selected one,
    // and the response's selectedVariants map identifies the active index
    // per responsive. The canvas needs this so every variant card shows
    // real output rather than falling back to placeholder widget names for
    // the non-selected variants.
    allVariants?: boolean;
    // Which region of the document to render. Omitted / "main" renders the
    // status line itself (subagent regions ignored). "subagent" renders each
    // width-adaptive container's <subagent> line — one PreviewLine per task of
    // the sample — returned in `subagentPreview` keyed by container node ID.
    section?: "main" | "subagent";
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
    // Present (possibly {}) when the request had `allVariants: true`: maps
    // each <responsive>'s AST node ID ("L{i}.{p}") to the variant index that
    // `width` would actually select. Absent when `allVariants` was omitted
    // or false.
    selectedVariants?: Record<string, number>;
    // Present when the request had `section: "subagent"`: maps each
    // width-adaptive container's node ID (a responsive layout's variant IDs
    // "L{i}.{p}.v{v}", or the layout's own ID "L{i}" for a responsive-free
    // layout) to that container's subagent line rendered once per sample task.
    // A container with no subagent (its own nor a layout-level fallback) is
    // omitted from the map.
    subagentPreview?: Record<string, PreviewLine[]>;
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
    // Names a runtime capability this field depends on (e.g. "oauth-usage"
    // for the authenticated usage API). Absent means always available.
    capability?: string;
    // Single-field rendering against the full sample snapshot.
    preview: {
        text: string;
        ansi: string;
    };
}

// GET /api/usage/probe: whether the authenticated OAuth usage API is
// reachable, gating oauth-usage-capability fields in the palette.
export interface UsageProbe {
    available: boolean;
    reason: string;
    extraUsageEnabled?: boolean;
}

// GET /api/dsl/metrics entry: named metrics for when / color-rule editing.
export interface Metric {
    name: string;
    displayName: string;
    descriptions: Record<string, string>;
    // True for a 0..100 percent-typed metric (e.g. seven-day-percent). Used to
    // filter the color-rule threshold bar's source-metric selector.
    percent?: boolean;
}

// GET /api/tools entry: one document (tool) the configurator can edit. The
// list — and its order — comes entirely from the backend; the frontend never
// hardcodes which tools exist.
export interface ToolInfo {
    id: string;
    displayName: string;
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

// ---- history (GET /api/history, GET /api/history/{id}, POST
// /api/history/{id}/restore — DSL_API.md "History API") ----

export interface HistoryMeta {
    name?: string;
    description?: string;
    author?: string;
    // Free-form Markdown prose (the exchange format's pre-fence body text,
    // internal/exchange.Meta.Notes) carried alongside the revision. Not
    // currently surfaced in the history panel's UI.
    notes?: string;
}

// One revision in GET /api/history's listing, deliberately without `source`
// (fetch it via getHistoryRevision) so the listing stays light with many
// revisions.
export interface HistoryRevisionEntry {
    id: string;
    parent: string | null;
    savedAt: string; // RFC3339
    origin: string; // "ui" | "cli" | "import"
    meta: HistoryMeta;
}

// `current` is the tool's current committed revision id; `draft` reports
// only whether a draft working node exists (never its source).
export interface HistoryRefs {
    current: string;
    draft: boolean;
}

export interface HistoryListResponse {
    revisions: HistoryRevisionEntry[];
    refs: HistoryRefs;
}

// GET /api/history/{id}: the listing entry plus the tool it belongs to and
// its full source text.
export interface HistoryRevisionDetail extends HistoryRevisionEntry {
    tool: string;
    source: string;
}

// POST /api/history/{id}/restore.
export interface HistoryRestoreResponse {
    ok: boolean;
    tool: string;
    current: string;
}

// ---- exchange (*.sloom.md Markdown) ----
//
// POST /api/exchange/import's request body is raw *.sloom.md text, not JSON
// (see api.ts's importExchange) — only the response is JSON, mirroring
// PutSourceResponse's saved/diagnostics shape but also reporting which tool
// and revision id the import produced (derived server-side from the
// document's own `<statusloom tool="...">` attribute, since the request
// carries no tool of its own).
export interface ImportExchangeResponse {
    // False when the import was rejected (409) because the extracted DSL has
    // error-severity diagnostics, or the Markdown envelope itself failed to
    // decode.
    saved: boolean;
    tool?: string;
    revision?: string;
    diagnostics: Diagnostic[];
}
