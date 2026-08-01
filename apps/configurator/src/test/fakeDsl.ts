// A fake /api/dsl/* server for App-level tests. To keep the frontend's
// serialize -> parse round trips working without reimplementing the XML DSL,
// the fake's "source text" is simply the canonical JSON encoding of the AST
// (ids/ranges stripped). parse() decodes it and assigns position-derived node
// IDs exactly like the real backend's scheme (DSL_API.md "Node IDs"), so the
// contract the frontend relies on — IDs recomputed by parse, segments
// labeled with matching IDs — holds.
//
// Error injection:
//   * a source containing "PARSE-ERROR" is a fatal parse failure (no ast)
//   * a node with when="BAD" yields an error diagnostic (ast still present)

import { vi } from "vitest";
import type {
    CommentNode,
    Diagnostic,
    FieldCatalogEntry,
    HistoryRefs,
    HistoryRevisionEntry,
    LayoutChild,
    LayoutNode,
    LineChild,
    LineNode,
    PreviewLine,
    PreviewSegment,
    ResponsiveNode,
    SpanNode,
    StatusloomNode,
    SubagentNode,
    VariantNode,
} from "../types.ts";

// ---- AST builders (ids are assigned by the fake's parse) ----

export function fld(name: string, attrs: Record<string, unknown> = {}): LineChild {
    return { id: "", kind: "field", name, ...attrs } as LineChild;
}

export function txt(value: string, attrs: Record<string, unknown> = {}): LineChild {
    return { id: "", kind: "text", value, ...attrs } as LineChild;
}

export function sep(): LineChild {
    return txt("|", { role: "separator", padding: 1 });
}

export function spn(children: LineChild[], attrs: Record<string, unknown> = {}): SpanNode {
    return { id: "", kind: "span", children, ...attrs } as SpanNode;
}

export function ln(children: LineChild[]): LineNode {
    return { id: "", kind: "line", children };
}

// One <variant> candidate inside a <responsive> (widest-first order is the
// caller's responsibility, matching the DSL). `when` sets its candidacy gate;
// omit it for an unconditional variant.
export function variant(lines: LineNode[], attrs?: { when?: string }): VariantNode {
    const v: VariantNode = { id: "", kind: "variant", lines };
    if (attrs?.when !== undefined) {
        v.when = attrs.when;
    }
    return v;
}

// A <responsive> width-adaptive container, widest variant first.
export function resp(variants: VariantNode[]): ResponsiveNode {
    return { id: "", kind: "responsive", variants };
}

export function lay(name: string, children: LayoutChild[], active?: boolean): LayoutNode {
    const l: LayoutNode = { id: "", kind: "layout", name, children };
    if (active !== undefined) {
        l.active = active;
    }
    return l;
}

export function doc(layouts: LayoutNode[], attrs: Record<string, unknown> = {}): StatusloomNode {
    return {
        id: "",
        kind: "statusloom",
        version: "1",
        tool: "claude-code",
        layouts,
        ...attrs,
    } as StatusloomNode;
}

// ---- canonical source (JSON with ids/ranges stripped) ----

function strip(value: unknown): unknown {
    if (Array.isArray(value)) {
        return value.map(strip);
    }
    if (value && typeof value === "object") {
        const out: Record<string, unknown> = {};
        for (const [k, v] of Object.entries(value)) {
            // `dirty` is a wire-only minimal-diff flag: like id/range it is not
            // part of the canonical source, so parsed nodes come back clean.
            if (k === "id" || k === "range" || k === "dirty") {
                continue;
            }
            out[k] = strip(v);
        }
        return out;
    }
    return value;
}

export function srcOf(root: StatusloomNode): string {
    return JSON.stringify(strip(root));
}

// Deterministic content hash, mirroring the backend's version semantics
// (identical source -> identical version).
export function versionOf(source: string): string {
    let h = 0;
    for (let i = 0; i < source.length; i += 1) {
        h = (h * 31 + source.charCodeAt(i)) | 0;
    }
    return "v" + (h >>> 0).toString(16);
}

// ---- position-derived node IDs (the backend's scheme) ----

function assignChildIds(children: LineChild[], parentId: string): void {
    children.forEach((c, k) => {
        c.id = `${parentId}.${k}`;
        if (c.kind === "span") {
            assignChildIds(c.children, c.id);
        }
        if (c.kind === "span" || c.kind === "text") {
            c.colorRules?.forEach((r, i) => {
                r.id = `${c.id}.cr${i}`;
            });
        }
        if (c.kind === "field") {
            c.colorRules?.forEach((r, i) => {
                r.id = `${c.id}.cr${i}`;
            });
        }
    });
}

// Stamps a <subagent> region's shared-id scheme: the region and its single
// <line> both take `id` ("L{i}.s" / "L{i}.{p}.v{v}.s"), and the line's
// children continue as "...s.{k}" (DSL_API.md "Node IDs").
function assignSubagentIds(sub: SubagentNode, id: string): void {
    sub.id = id;
    sub.line.id = id;
    assignChildIds(sub.line.children, id);
}

// Assigns "L{i}.{p}" to a layout's line/responsive child, its comments the
// "L{i}.{p}.c{k}" form, and recurses into a line's own children / a
// responsive's variants ("L{i}.{p}.v{v}", lines "L{i}.{p}.v{v}.{j}",
// variant comments "L{i}.{p}.v{v}.c{k}", subagent "L{i}.{p}.v{v}.s") —
// mirrors DSL_API.md "Node IDs".
function assignLayoutChildIds(children: LayoutChild[], layoutId: string): void {
    children.forEach((child, p) => {
        const id = `${layoutId}.${p}`;
        child.id = id;
        if (child.kind === "line") {
            assignChildIds(child.children, id);
            return;
        }
        child.comments?.forEach((c, k) => {
            c.id = `${id}.c${k}`;
        });
        child.variants.forEach((v, vi) => {
            const variantId = `${id}.v${vi}`;
            v.id = variantId;
            v.comments?.forEach((c, k) => {
                c.id = `${variantId}.c${k}`;
            });
            v.lines.forEach((line, j) => {
                line.id = `${variantId}.${j}`;
                assignChildIds(line.children, line.id);
            });
            if (v.subagent) {
                assignSubagentIds(v.subagent, `${variantId}.s`);
            }
        });
    });
}

export function assignIds(root: StatusloomNode): StatusloomNode {
    root.id = "root";
    if (root.git) {
        root.git.id = "git";
        root.git.kind = "git";
    }
    root.comments?.forEach((c: CommentNode, k: number) => {
        c.id = `root.c${k}`;
    });
    root.layouts.forEach((l, i) => {
        l.id = `L${i}`;
        l.comments?.forEach((c, k) => {
            c.id = `L${i}.c${k}`;
        });
        if (l.subagent) {
            assignSubagentIds(l.subagent, `L${i}.s`);
        }
        assignLayoutChildIds(l.children, l.id);
    });
    return root;
}

// ---- parse / validate ----

function collectDiagnostics(root: StatusloomNode): Diagnostic[] {
    const diags: Diagnostic[] = [];
    const visit = (node: LineChild) => {
        if ("when" in node && (node as { when?: string }).when === "BAD") {
            diags.push({
                severity: "error",
                message: "invalid when expression",
                range: { start: 0, end: 0 },
            });
        }
        if (node.kind === "span") {
            node.children.forEach(visit);
        }
    };
    const visitLine = (line: LineNode) => line.children.forEach(visit);
    for (const l of root.layouts) {
        for (const child of l.children) {
            if (child.kind === "line") {
                visitLine(child);
            } else {
                child.variants.forEach((v) => v.lines.forEach(visitLine));
            }
        }
    }
    return diags;
}

export function fakeParse(source: string): {
    ast?: StatusloomNode;
    diagnostics: Diagnostic[];
    version: string;
} {
    const version = versionOf(source);
    if (source.includes("PARSE-ERROR")) {
        return {
            diagnostics: [
                { severity: "error", message: "not well-formed", range: { start: 0, end: 3 } },
            ],
            version,
        };
    }
    let parsed: unknown;
    try {
        parsed = JSON.parse(source);
    } catch {
        return {
            diagnostics: [
                { severity: "error", message: "not well-formed", range: { start: 0, end: 0 } },
            ],
            version,
        };
    }
    if (!parsed || typeof parsed !== "object" || (parsed as { kind?: string }).kind !== "statusloom") {
        return {
            diagnostics: [
                { severity: "error", message: "root must be statusloom", range: { start: 0, end: 0 } },
            ],
            version,
        };
    }
    const ast = assignIds(parsed as StatusloomNode);
    return { ast, diagnostics: collectDiagnostics(ast), version };
}

// ---- preview rendering ----

export const FULL_SAMPLE: Record<string, string> = {
    model: "Opus 4.8",
    "git-branch": "main",
    "five-hour-usage": "32%",
    "context-length": "64k",
};

export const EARLY_SAMPLE: Record<string, string> = {
    model: "Opus 4.8",
};

// Subagent-region samples: one record per running/completed task (mirrors the
// backend's subagent-running/subagent-completed samples, 3 tasks each). Each
// record is a field-name -> value map consumed by renderLine like the main
// samples above.
export const SUBAGENT_RUNNING_TASKS: Record<string, string>[] = [
    { "task-description": "Review render pipeline", "task-model": "Opus 4.8", "task-status": "running" },
    { "task-description": "Fix flaky drag test", "task-model": "Sonnet 4.5", "task-status": "running" },
    { "task-description": "Update DSL docs", "task-model": "Haiku 4.5", "task-status": "running" },
];

export const SUBAGENT_COMPLETED_TASKS: Record<string, string>[] = [
    { "task-description": "Review render pipeline", "task-model": "Opus 4.8", "task-status": "done" },
    { "task-description": "Fix flaky drag test", "task-model": "Sonnet 4.5", "task-status": "done" },
    { "task-description": "Update DSL docs", "task-model": "Haiku 4.5", "task-status": "done" },
];

function renderChildren(
    children: LineChild[],
    sample: Record<string, string>,
    out: PreviewSegment[],
): void {
    for (const c of children) {
        switch (c.kind) {
            case "field": {
                if (c.optional && !sample[c.optional]) {
                    break; // gated off: no segments at all
                }
                const value = c.name ? (sample[c.name] ?? "") : "";
                const textVal = (c.prefix ?? "") + value + (c.suffix ?? "");
                out.push({
                    nodeId: c.id,
                    text: value === "" ? "" : textVal,
                    ansi: value === "" ? "" : textVal,
                    visible: value !== "",
                });
                break;
            }
            case "text": {
                out.push({ nodeId: c.id, text: c.value, ansi: c.value, visible: c.value !== "" });
                break;
            }
            case "raw-text": {
                out.push({ nodeId: c.id, text: c.value, ansi: c.value, visible: c.value !== "" });
                break;
            }
            case "flex": {
                out.push({ nodeId: c.id, text: " ", ansi: " ", visible: true });
                break;
            }
            case "span": {
                if (c.optional && !sample[c.optional]) {
                    break; // gated off, including prefix/suffix
                }
                if (c.prefix) {
                    out.push({ nodeId: c.id, text: c.prefix, ansi: c.prefix, visible: true });
                }
                renderChildren(c.children, sample, out);
                if (c.suffix) {
                    out.push({ nodeId: c.id, text: c.suffix, ansi: c.suffix, visible: true });
                }
                break;
            }
            case "comment":
                break;
        }
    }
}

function renderLine(line: LineNode, sample: Record<string, string>): PreviewLine {
    const segments: PreviewSegment[] = [];
    renderChildren(line.children, sample, segments);
    const visible = segments.filter((s) => s.visible);
    return {
        omitted: visible.length === 0,
        ansi: visible.map((s) => s.ansi).join(""),
        segments,
    };
}

// A line's natural width: the visible-content length it would render at
// (markup.md/plans/responsive-container-design.md §1 "flatten + gate ... の
// 可視な content/separator piece の visibleWidth(plain) 合算"; a <flex>
// contributes 0, since it only fills residual space). Simplified for the
// fake (no ANSI/wide-char accounting) — good enough to exercise first-fit
// variant selection in tests.
function nodeNaturalWidth(node: LineChild, sample: Record<string, string>): number {
    switch (node.kind) {
        case "field": {
            if (node.optional && !sample[node.optional]) {
                return 0;
            }
            const value = node.name ? (sample[node.name] ?? "") : "";
            return value === "" ? 0 : ((node.prefix ?? "") + value + (node.suffix ?? "")).length;
        }
        case "text":
        case "raw-text":
            return node.value.length;
        case "flex":
            return 0;
        case "span": {
            if (node.optional && !sample[node.optional]) {
                return 0;
            }
            const inner = node.children.reduce((sum, c) => sum + nodeNaturalWidth(c, sample), 0);
            return (node.prefix?.length ?? 0) + inner + (node.suffix?.length ?? 0);
        }
        case "comment":
            return 0;
    }
}

function lineNaturalWidth(line: LineNode, sample: Record<string, string>): number {
    return line.children.reduce((sum, c) => sum + nodeNaturalWidth(c, sample), 0);
}

// First-fit variant selection (plans/responsive-container-design.md §1):
// the first variant all of whose lines fit `width`, the last variant when
// none fit, and the first/widest variant when the width is unknown (<= 0).
export function selectVariant(
    responsive: ResponsiveNode,
    sample: Record<string, string>,
    width: number,
): number {
    if (width <= 0) {
        return 0;
    }
    for (let v = 0; v < responsive.variants.length; v += 1) {
        if (responsive.variants[v].lines.every((line) => lineNaturalWidth(line, sample) <= width)) {
            return v;
        }
    }
    return responsive.variants.length - 1;
}

// fakePreview mirrors handlePreviewDSL (internal/webconfig/dsl.go): by
// default a <responsive> contributes only its width-selected variant's
// lines (matching the real statusline); with allVariants every variant's
// lines are rendered (each with its real sample values, as if selected) and
// selectedVariants reports the index each responsive would actually pick —
// see DSL_API.md "allVariants".
export function fakePreview(
    source: string,
    sample: string,
    layoutIndex: number,
    width: number,
    allVariants = false,
    section: "main" | "subagent" = "main",
): {
    lines: PreviewLine[];
    diagnostics: Diagnostic[];
    fallback?: { ansi: string; active: boolean };
    selectedVariants?: Record<string, number>;
    subagentPreview?: Record<string, PreviewLine[]>;
} {
    const { ast, diagnostics } = fakeParse(source);
    if (!ast) {
        return { lines: [], diagnostics };
    }
    const li = Math.max(0, Math.min(layoutIndex, ast.layouts.length - 1));
    const layout = ast.layouts[li];

    // Subagent pass: render each width-adaptive container's <subagent> line
    // once per sample task, keyed by container node id (variant ids for a
    // responsive layout, else the layout id). Mirrors DSL_API.md "section".
    if (section === "subagent") {
        const tasks =
            sample === "subagent-completed"
                ? SUBAGENT_COMPLETED_TASKS
                : SUBAGENT_RUNNING_TASKS;
        const subagentPreview: Record<string, PreviewLine[]> = {};
        const hasResponsive = (layout?.children ?? []).some((c) => c.kind === "responsive");
        if (!hasResponsive) {
            if (layout?.subagent) {
                subagentPreview[layout.id] = tasks.map((t) =>
                    renderLine(layout.subagent!.line, t),
                );
            }
        } else {
            for (const child of layout?.children ?? []) {
                if (child.kind !== "responsive") {
                    continue;
                }
                for (const v of child.variants) {
                    if (v.subagent) {
                        subagentPreview[v.id] = tasks.map((t) => renderLine(v.subagent!.line, t));
                    }
                }
            }
        }
        return { lines: [], diagnostics, subagentPreview };
    }

    const data = sample === "early-session" ? EARLY_SAMPLE : FULL_SAMPLE;
    const lines: PreviewLine[] = [];
    const selectedVariants: Record<string, number> = {};
    for (const child of layout?.children ?? []) {
        if (child.kind === "line") {
            lines.push(renderLine(child, data));
            continue;
        }
        const v = selectVariant(child, data, width);
        selectedVariants[child.id] = v;
        const variants = allVariants ? child.variants : [child.variants[v]];
        for (const variant of variants) {
            for (const line of variant?.lines ?? []) {
                lines.push(renderLine(line, data));
            }
        }
    }
    const allOmitted = lines.every((l) => l.omitted);
    return {
        lines,
        diagnostics,
        fallback: { ansi: allOmitted ? "Opus 4.8 | v1.0" : "", active: allOmitted },
        ...(allVariants ? { selectedVariants } : {}),
    };
}

// ---- the fetch mock ----

export const FIELDS: FieldCatalogEntry[] = [
    {
        name: "model",
        displayName: "Model",
        descriptions: { en: "Model name", ja: "モデル名" },
        category: "common",
        preview: { text: "Opus 4.8", ansi: "Opus 4.8" },
    },
    {
        name: "git-branch",
        displayName: "Git Branch",
        descriptions: { en: "Current branch", ja: "ブランチ" },
        category: "common",
        preview: { text: "main", ansi: "main" },
    },
    {
        name: "five-hour-usage",
        displayName: "5-Hour Usage",
        descriptions: { en: "5h usage", ja: "5時間使用量" },
        category: "claude",
        selfMetric: "five-hour-percent",
        formats: ["percent"],
        preview: { text: "32%", ansi: "32%" },
    },
    // Subagent-region (task-*) fields are part of the single claude-code
    // catalog now (category "subagent" groups them in the palette and scopes
    // them to the <subagent> region — see markup.md).
    {
        name: "task-description",
        displayName: "Task Description",
        descriptions: { en: "Task description", ja: "タスクの説明" },
        category: "subagent",
        preview: {
            text: "Review render pipeline changes",
            ansi: "Review render pipeline changes",
        },
    },
    {
        name: "task-model",
        displayName: "Task Model",
        descriptions: { en: "Task model", ja: "タスクのモデル" },
        category: "subagent",
        preview: { text: "Opus 4.8", ansi: "Opus 4.8" },
    },
];

export interface FakeServer {
    // Saved document / shared draft sources (JSON-encoded ASTs).
    document: string;
    draft: string | null;
    draftFails: boolean;
    putDraftBodies: string[];
    putDocumentBodies: string[];
    fetchMock: ReturnType<typeof vi.fn>;
    // GET /api/usage/probe response. Mutate before render() (or before the
    // app's mount-time probe fires) to simulate the usage-API being
    // reachable; defaults to the same "no token" shape the real backend
    // returns when no OAuth credentials are present.
    usageProbe: { available: boolean; reason: string; extraUsageEnabled?: boolean };
    // GET /api/history + GET /api/history/{id} backing store. Two revisions
    // by default ("rev-1" the parent, "rev-2" the current one), each with
    // its own source so a restore visibly changes `document`. Mutate
    // `history.refs`/`history.revisions` or `historySources` before render()
    // to change what a test's history panel sees.
    history: { revisions: HistoryRevisionEntry[]; refs: HistoryRefs };
    historySources: Record<string, string>;
    // Artificial delay (ms) applied to POST /api/history/{id}/restore's
    // response, so tests can put a debounce deadline mid-flight to prove it
    // was settled beforehand rather than merely racing to finish first.
    restoreDelayMs: number;
}

function jsonResponse(body: unknown, status = 200): Response {
    return new Response(JSON.stringify(body), {
        status,
        headers: { "Content-Type": "application/json" },
    });
}

export function installFakeDslServer(initial: StatusloomNode): FakeServer {
    const initialSource = srcOf(initial);
    const server: FakeServer = {
        document: initialSource,
        draft: null,
        draftFails: false,
        putDraftBodies: [],
        putDocumentBodies: [],
        fetchMock: vi.fn(),
        usageProbe: { available: false, reason: "no-token" },
        history: {
            revisions: [
                {
                    id: "rev-1",
                    parent: null,
                    savedAt: "2026-01-01T00:00:00Z",
                    origin: "cli",
                    meta: {},
                },
                {
                    id: "rev-2",
                    parent: "rev-1",
                    savedAt: "2026-01-02T00:00:00Z",
                    origin: "ui",
                    meta: {},
                },
            ],
            refs: { current: "rev-2", draft: false },
        },
        historySources: { "rev-1": initialSource, "rev-2": initialSource },
        restoreDelayMs: 0,
    };

    server.fetchMock.mockImplementation(
        async (input: RequestInfo | URL, init?: RequestInit) => {
            const url = String(input);
            const method = init?.method ?? "GET";
            const body = init?.body ? JSON.parse(String(init.body)) : undefined;

            if (url.endsWith("/api/tools")) {
                return jsonResponse({
                    tools: [{ id: "claude-code", displayName: "Claude Code" }],
                });
            }
            if (url.startsWith("/api/dsl/fields")) {
                return jsonResponse({ fields: FIELDS });
            }
            if (url.endsWith("/api/usage/probe")) {
                return jsonResponse(server.usageProbe);
            }
            if (url.startsWith("/api/dsl/metrics")) {
                return jsonResponse({
                    metrics: [
                        {
                            name: "five-hour-percent",
                            displayName: "5-Hour Usage (%)",
                            descriptions: { en: "5h percent", ja: "5時間%" },
                            percent: true,
                        },
                        // A string metric with a closed value set: what the
                        // when-condition builder offers a <select> for.
                        {
                            name: "account-type",
                            displayName: "Account Type",
                            descriptions: { en: "account kind", ja: "アカウント種別" },
                            text: true,
                            values: ["claude_max", "claude_team"],
                        },
                    ],
                });
            }
            if (url.endsWith("/api/sessions")) {
                return jsonResponse({ sessions: [] });
            }
            if (url.startsWith("/api/dsl/document")) {
                if (method === "GET") {
                    return jsonResponse({
                        source: server.document,
                        version: versionOf(server.document),
                        exists: true,
                    });
                }
                // PUT
                const res = fakeParse(body.source);
                if (res.diagnostics.some((d) => d.severity === "error")) {
                    return jsonResponse(
                        { version: res.version, diagnostics: res.diagnostics },
                        409,
                    );
                }
                server.document = body.source;
                server.putDocumentBodies.push(body.source);
                return jsonResponse({ version: res.version, diagnostics: res.diagnostics });
            }
            if (url.startsWith("/api/dsl/draft")) {
                if (server.draftFails) {
                    return jsonResponse({ error: "not found" }, 404);
                }
                if (method === "PUT") {
                    server.draft = body.source;
                    server.putDraftBodies.push(body.source);
                    const res = fakeParse(body.source);
                    return jsonResponse({ version: res.version, diagnostics: res.diagnostics });
                }
                const source = server.draft ?? server.document;
                return jsonResponse({
                    source,
                    version: versionOf(source),
                    exists: server.draft !== null,
                });
            }
            if (url.endsWith("/api/dsl/parse")) {
                const res = fakeParse(body.source);
                return jsonResponse(res);
            }
            if (url.endsWith("/api/dsl/serialize")) {
                const ast = body.ast as StatusloomNode;
                if (!ast || ast.kind !== "statusloom") {
                    return jsonResponse({ error: "root must be statusloom" }, 400);
                }
                const source = srcOf(ast);
                return jsonResponse({ source, diagnostics: fakeParse(source).diagnostics });
            }
            if (url.endsWith("/api/dsl/preview")) {
                return jsonResponse(
                    fakePreview(
                        body.source,
                        body.sample ?? "full",
                        body.layoutIndex ?? 0,
                        body.width ?? 120,
                        body.allVariants ?? false,
                        body.section ?? "main",
                    ),
                );
            }
            if (url.startsWith("/api/history/") && url.endsWith("/restore")) {
                const id = decodeURIComponent(
                    url.slice("/api/history/".length, -"/restore".length),
                );
                if (server.restoreDelayMs > 0) {
                    await new Promise((r) => setTimeout(r, server.restoreDelayMs));
                }
                const source = server.historySources[id];
                if (source === undefined) {
                    return jsonResponse({ error: "unknown revision" }, 404);
                }
                // A restore discards the draft working node and repoints
                // `current` at the restored revision (plans/config-store-
                // and-format.md §4.2), mirroring the real store's semantics.
                server.document = source;
                server.draft = null;
                server.history.refs = { current: id, draft: false };
                return jsonResponse({ ok: true, tool: "claude-code", current: id });
            }
            if (url.startsWith("/api/history/")) {
                const id = decodeURIComponent(url.slice("/api/history/".length));
                const entry = server.history.revisions.find((r) => r.id === id);
                const source = server.historySources[id];
                if (!entry || source === undefined) {
                    return jsonResponse({ error: "unknown revision" }, 404);
                }
                return jsonResponse({ ...entry, tool: "claude-code", source });
            }
            if (url.startsWith("/api/history?")) {
                return jsonResponse(server.history);
            }
            return jsonResponse({ error: "not found" }, 404);
        },
    );

    vi.stubGlobal("fetch", server.fetchMock);
    return server;
}

// A minimal single-layout document used across App-level tests: one layout,
// two lines of one model field each.
export function defaultTestDoc(): StatusloomNode {
    return doc([lay("Default", [ln([fld("model")]), ln([fld("model")])], true)]);
}
