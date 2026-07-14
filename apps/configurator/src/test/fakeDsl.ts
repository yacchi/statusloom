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
    LayoutNode,
    LineChild,
    LineNode,
    PreviewLine,
    PreviewSegment,
    SpanNode,
    StatusloomNode,
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

export function lay(name: string, lines: LineNode[], active?: boolean): LayoutNode {
    const l: LayoutNode = { id: "", kind: "layout", name, lines };
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
        l.lines.forEach((line, j) => {
            line.id = `L${i}.${j}`;
            assignChildIds(line.children, line.id);
        });
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
    for (const l of root.layouts) {
        for (const line of l.lines) {
            line.children.forEach(visit);
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

export function fakePreview(
    source: string,
    sample: string,
    layoutIndex: number,
): {
    lines: PreviewLine[];
    diagnostics: Diagnostic[];
    fallback?: { ansi: string; active: boolean };
} {
    const { ast, diagnostics } = fakeParse(source);
    if (!ast) {
        return { lines: [], diagnostics };
    }
    const data = sample === "early-session" ? EARLY_SAMPLE : FULL_SAMPLE;
    const li = Math.max(0, Math.min(layoutIndex, ast.layouts.length - 1));
    const layout = ast.layouts[li];
    const lines: PreviewLine[] = (layout?.lines ?? []).map((line) => {
        const segments: PreviewSegment[] = [];
        renderChildren(line.children, data, segments);
        const visible = segments.filter((s) => s.visible);
        return {
            omitted: visible.length === 0,
            ansi: visible.map((s) => s.ansi).join(""),
            segments,
        };
    });
    const allOmitted = lines.every((l) => l.omitted);
    return {
        lines,
        diagnostics,
        fallback: { ansi: allOmitted ? "Opus 4.8 | v1.0" : "", active: allOmitted },
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
];

export interface FakeServer {
    // Saved document / shared draft sources (JSON-encoded ASTs).
    document: string;
    draft: string | null;
    draftFails: boolean;
    putDraftBodies: string[];
    putDocumentBodies: string[];
    fetchMock: ReturnType<typeof vi.fn>;
}

function jsonResponse(body: unknown, status = 200): Response {
    return new Response(JSON.stringify(body), {
        status,
        headers: { "Content-Type": "application/json" },
    });
}

export function installFakeDslServer(initial: StatusloomNode): FakeServer {
    const server: FakeServer = {
        document: srcOf(initial),
        draft: null,
        draftFails: false,
        putDraftBodies: [],
        putDocumentBodies: [],
        fetchMock: vi.fn(),
    };

    server.fetchMock.mockImplementation(
        async (input: RequestInfo | URL, init?: RequestInit) => {
            const url = String(input);
            const method = init?.method ?? "GET";
            const body = init?.body ? JSON.parse(String(init.body)) : undefined;

            if (url.startsWith("/api/dsl/fields")) {
                return jsonResponse({ fields: FIELDS });
            }
            if (url.startsWith("/api/dsl/metrics")) {
                return jsonResponse({
                    metrics: [
                        {
                            name: "five-hour-percent",
                            displayName: "5-Hour Usage (%)",
                            descriptions: { en: "5h percent", ja: "5時間%" },
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
                    fakePreview(body.source, body.sample ?? "full", body.layoutIndex ?? 0),
                );
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
