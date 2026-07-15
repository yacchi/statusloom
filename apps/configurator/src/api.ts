// API client for the local Statusloom web configurator (the DSL-native
// /api/dsl/* endpoints; see internal/webconfig/DSL_API.md).
//
// The bearer token is read once from location.hash at startup and kept only in
// memory (passed explicitly into each call). It is never persisted to storage.

import type {
    DocumentResponse,
    LiveSessionInfo,
    Metric,
    ParseResponse,
    PreviewRequest,
    PreviewResponse,
    PutSourceResponse,
    SerializeResponse,
    SessionSummary,
    StatusloomNode,
    TerminalSessionInfo,
    FieldCatalogEntry,
    ToolInfo,
    UsageProbe,
} from "./types.ts";

export class ApiError extends Error {
    readonly status: number;
    constructor(status: number, message: string) {
        super(message);
        this.name = "ApiError";
        this.status = status;
    }
}

// Extract the 32-hex startup token from the URL fragment (#token=<hex>).
// Returns null when absent or malformed.
export function readToken(hash: string): string | null {
    const raw = hash.startsWith("#") ? hash.slice(1) : hash;
    const params = new URLSearchParams(raw);
    const token = params.get("token");
    if (token && /^[0-9a-fA-F]{32}$/.test(token)) {
        return token;
    }
    return null;
}

// Builds the browser-WebSocket URL for GET /ws/live. The token travels as a
// query parameter because browsers cannot attach custom headers (e.g.
// Authorization) to a WebSocket handshake.
export function liveSocketUrl(token: string): string {
    const proto = window.location.protocol === "https:" ? "wss" : "ws";
    return `${proto}://${window.location.host}/ws/live?token=${encodeURIComponent(token)}`;
}

// Builds the browser-WebSocket URL for GET /ws/terminal. Both the token and
// the terminal id travel as query parameters (browsers cannot attach custom
// headers to a WebSocket handshake).
export function terminalSocketUrl(token: string, id: string): string {
    const proto = window.location.protocol === "https:" ? "wss" : "ws";
    return `${proto}://${window.location.host}/ws/terminal?token=${encodeURIComponent(
        token,
    )}&id=${encodeURIComponent(id)}`;
}

async function rawRequest(
    token: string,
    method: string,
    path: string,
    body?: unknown,
): Promise<{ status: number; parsed: unknown }> {
    const headers: Record<string, string> = {
        Authorization: `Bearer ${token}`,
    };
    if (body !== undefined) {
        headers["Content-Type"] = "application/json";
    }

    let res: Response;
    try {
        res = await fetch(path, {
            method,
            headers,
            body: body === undefined ? undefined : JSON.stringify(body),
        });
    } catch (err) {
        throw new ApiError(0, `Network error: ${(err as Error).message}`);
    }

    const text = await res.text();
    let parsed: unknown = undefined;
    if (text.length > 0) {
        try {
            parsed = JSON.parse(text);
        } catch {
            parsed = undefined;
        }
    }
    return { status: res.status, parsed };
}

function errorMessage(status: number, parsed: unknown): string {
    return parsed && typeof parsed === "object" && "error" in parsed
        ? String((parsed as { error: unknown }).error)
        : `Request failed with status ${status}`;
}

async function request<T>(
    token: string,
    method: string,
    path: string,
    body?: unknown,
): Promise<T> {
    const { status, parsed } = await rawRequest(token, method, path, body);
    if (status < 200 || status >= 300) {
        throw new ApiError(status, errorMessage(status, parsed));
    }
    return parsed as T;
}

export interface Api {
    // GET /api/tools — the documents (tools) the configurator can switch
    // between, in display order.
    getTools(): Promise<ToolInfo[]>;
    // GET /api/dsl/document — the saved <tool>.xml (or the built-in default).
    getDocument(tool: string): Promise<DocumentResponse>;
    // PUT /api/dsl/document — save. A 409 (error diagnostics; nothing written)
    // resolves with saved=false rather than throwing.
    putDocument(tool: string, source: string): Promise<PutSourceResponse>;
    // POST /api/dsl/parse — live analysis for the DSL Editor; never writes.
    parse(source: string): Promise<ParseResponse>;
    // POST /api/dsl/serialize — AST back to DSL source. When `baseSource` is
    // given, unchanged nodes are reused verbatim (minimal-diff); otherwise the
    // whole-document canonical form is returned.
    serialize(ast: StatusloomNode, baseSource?: string): Promise<SerializeResponse>;
    // GET/PUT /api/dsl/draft — the shared draft source (last-writer-wins).
    getDraft(tool: string): Promise<DocumentResponse>;
    putDraft(tool: string, source: string): Promise<PutSourceResponse>;
    // POST /api/dsl/preview — render `source` and return node-ID segments.
    preview(req: PreviewRequest): Promise<PreviewResponse>;
    // GET /api/dsl/fields — the field catalog for the palette.
    getFields(tool: string): Promise<FieldCatalogEntry[]>;
    // GET /api/dsl/metrics — named metrics for when / color-rule editing.
    getMetrics(tool: string): Promise<Metric[]>;
    // GET /api/usage/probe — availability of the authenticated OAuth usage
    // API, gating oauth-usage-capability fields in the palette.
    probeUsage(): Promise<UsageProbe>;
    getSessions(): Promise<SessionSummary[]>;
    shutdown(): Promise<void>;
    startLiveSession(): Promise<LiveSessionInfo>;
    startTerminalSession(): Promise<TerminalSessionInfo>;
}

export function createApi(token: string): Api {
    return {
        async getTools() {
            const res = await request<{ tools?: ToolInfo[] }>(token, "GET", "/api/tools");
            return res.tools ?? [];
        },
        async getDocument(tool: string) {
            return request<DocumentResponse>(
                token,
                "GET",
                `/api/dsl/document?tool=${encodeURIComponent(tool)}`,
            );
        },
        async putDocument(tool: string, source: string) {
            const { status, parsed } = await rawRequest(token, "PUT", "/api/dsl/document", {
                tool,
                source,
            });
            if (status === 200 || status === 409) {
                const body = parsed as {
                    version: string;
                    diagnostics?: PutSourceResponse["diagnostics"];
                };
                return {
                    saved: status === 200,
                    version: body.version,
                    diagnostics: body.diagnostics ?? [],
                };
            }
            throw new ApiError(status, errorMessage(status, parsed));
        },
        async parse(source: string) {
            return request<ParseResponse>(token, "POST", "/api/dsl/parse", { source });
        },
        async serialize(ast: StatusloomNode, baseSource?: string) {
            const body: { ast: StatusloomNode; baseSource?: string } = { ast };
            if (baseSource !== undefined) {
                body.baseSource = baseSource;
            }
            return request<SerializeResponse>(token, "POST", "/api/dsl/serialize", body);
        },
        async getDraft(tool: string) {
            return request<DocumentResponse>(
                token,
                "GET",
                `/api/dsl/draft?tool=${encodeURIComponent(tool)}`,
            );
        },
        async putDraft(tool: string, source: string) {
            const res = await request<{
                version: string;
                diagnostics?: PutSourceResponse["diagnostics"];
            }>(token, "PUT", "/api/dsl/draft", { tool, source });
            return { saved: true, version: res.version, diagnostics: res.diagnostics ?? [] };
        },
        async preview(req: PreviewRequest) {
            return request<PreviewResponse>(token, "POST", "/api/dsl/preview", req);
        },
        async getFields(tool: string) {
            const res = await request<{ fields?: FieldCatalogEntry[] }>(
                token,
                "GET",
                `/api/dsl/fields?tool=${encodeURIComponent(tool)}`,
            );
            return res.fields ?? [];
        },
        async getMetrics(tool: string) {
            const res = await request<{ metrics?: Metric[] }>(
                token,
                "GET",
                `/api/dsl/metrics?tool=${encodeURIComponent(tool)}`,
            );
            return res.metrics ?? [];
        },
        async probeUsage() {
            return request<UsageProbe>(token, "GET", "/api/usage/probe");
        },
        async getSessions() {
            const res = await request<{ sessions?: SessionSummary[] }>(
                token,
                "GET",
                "/api/sessions",
            );
            return res.sessions ?? [];
        },
        async shutdown() {
            await request<{ ok: boolean }>(token, "POST", "/api/shutdown");
        },
        async startLiveSession() {
            return request<LiveSessionInfo>(token, "POST", "/api/live/session");
        },
        async startTerminalSession() {
            return request<TerminalSessionInfo>(token, "POST", "/api/terminal/session");
        },
    };
}
