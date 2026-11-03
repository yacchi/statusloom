import { ApiError } from "./api.ts";

// Client for the external-source endpoints (/api/sources). Kept separate from
// the Api interface in api.ts so existing Api mocks stay valid.

export type SourceKind = "file" | "command" | "http";
export type SourceWarning = "allow-remote" | "short-ttl";

export interface SourceDefinition {
    kind: SourceKind;
    path?: string;
    argv?: string[];
    url?: string;
    allowRemote?: boolean;
    ttlSeconds?: number;
    staleAfterSeconds?: number;
    timeoutSeconds?: number;
}

export interface SourceEntry extends Required<SourceDefinition> {
    id: string;
    warnings: SourceWarning[];
}

export interface SourcesListResponse {
    path: string;
    sources: SourceEntry[];
}

export interface SourceTestRequest {
    select: string;
    sessionId?: string;
    sample?: string;
}

export interface SourceTestResult {
    ok: boolean;
    error: string;
    matched: boolean;
    value: unknown;
    display: string;
    documentPreview: string;
}

export interface SourcesApi {
    listSources(): Promise<SourcesListResponse>;
    putSource(id: string, def: SourceDefinition): Promise<SourceEntry>; // rejects with ApiError on 4xx/5xx
    deleteSource(id: string): Promise<void>;
    testSource(id: string, req: SourceTestRequest): Promise<SourceTestResult>;
}

// Mirrors api.ts rawRequest/request: same auth header, JSON bodies, and
// ApiError carrying the server's {"error": "..."} text.
async function request<T>(
    token: string,
    method: string,
    path: string,
    body?: unknown,
): Promise<T> {
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

    if (res.status < 200 || res.status >= 300) {
        const message =
            parsed && typeof parsed === "object" && "error" in parsed
                ? String((parsed as { error: unknown }).error)
                : `Request failed with status ${res.status}`;
        throw new ApiError(res.status, message);
    }
    return parsed as T;
}

export function createSourcesApi(token: string): SourcesApi {
    return {
        listSources: () =>
            request<SourcesListResponse>(token, "GET", "/api/sources"),
        putSource: async (id, def) => {
            const res = await request<{ source: SourceEntry }>(
                token,
                "PUT",
                `/api/sources/${encodeURIComponent(id)}`,
                def,
            );
            return res.source;
        },
        deleteSource: async (id) => {
            await request<unknown>(
                token,
                "DELETE",
                `/api/sources/${encodeURIComponent(id)}`,
            );
        },
        testSource: (id, req) =>
            request<SourceTestResult>(
                token,
                "POST",
                `/api/sources/${encodeURIComponent(id)}/test`,
                req,
            ),
    };
}
