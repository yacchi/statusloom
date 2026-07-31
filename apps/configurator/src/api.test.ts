import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiError, createApi, liveSocketUrl, terminalSocketUrl } from "./api.ts";
import type { StatusloomNode } from "./types.ts";

const TOKEN = "a".repeat(32);

function jsonResponse(body: unknown, status = 200): Response {
    return new Response(JSON.stringify(body), {
        status,
        headers: { "Content-Type": "application/json" },
    });
}

afterEach(() => {
    vi.unstubAllGlobals();
});

describe("api.getDocument / api.getDraft", () => {
    it("GETs /api/dsl/document with the tool and bearer token", async () => {
        const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
            expect(String(input)).toBe("/api/dsl/document?tool=claude-code");
            expect(init?.method).toBe("GET");
            expect((init?.headers as Record<string, string>).Authorization).toBe(
                `Bearer ${TOKEN}`,
            );
            return jsonResponse({ source: "<statusloom/>", version: "h1", exists: true });
        });
        vi.stubGlobal("fetch", fetchMock);

        const api = createApi(TOKEN);
        expect(await api.getDocument("claude-code")).toEqual({
            source: "<statusloom/>",
            version: "h1",
            exists: true,
        });
    });

    it("GETs /api/dsl/draft with the tool", async () => {
        const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
            expect(String(input)).toBe("/api/dsl/draft?tool=claude-code");
            return jsonResponse({ source: "src", version: "h2", exists: false });
        });
        vi.stubGlobal("fetch", fetchMock);
        const api = createApi(TOKEN);
        expect(await api.getDraft("claude-code")).toEqual({
            source: "src",
            version: "h2",
            exists: false,
        });
    });
});

describe("api.putDocument", () => {
    it("resolves saved=true on 200", async () => {
        const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
            expect(String(input)).toBe("/api/dsl/document");
            expect(init?.method).toBe("PUT");
            expect(JSON.parse(String(init?.body))).toEqual({
                tool: "claude-code",
                source: "src",
            });
            return jsonResponse({ version: "h1", diagnostics: [] });
        });
        vi.stubGlobal("fetch", fetchMock);
        const api = createApi(TOKEN);
        expect(await api.putDocument("claude-code", "src")).toEqual({
            saved: true,
            version: "h1",
            diagnostics: [],
        });
    });

    it("resolves saved=false with diagnostics on 409 (error diagnostics block the save)", async () => {
        const diags = [
            { severity: "error", message: "unknown field", range: { start: 4, end: 9 } },
        ];
        vi.stubGlobal(
            "fetch",
            vi.fn(async () => jsonResponse({ version: "h1", diagnostics: diags }, 409)),
        );
        const api = createApi(TOKEN);
        const res = await api.putDocument("claude-code", "src");
        expect(res.saved).toBe(false);
        expect(res.diagnostics).toEqual(diags);
    });

    it("throws ApiError for other failure statuses", async () => {
        vi.stubGlobal(
            "fetch",
            vi.fn(async () => jsonResponse({ error: "boom" }, 500)),
        );
        const api = createApi(TOKEN);
        await expect(api.putDocument("claude-code", "src")).rejects.toMatchObject({
            status: 500,
            message: "boom",
        });
    });
});

describe("api.parse / api.serialize", () => {
    it("POSTs the source to /api/dsl/parse", async () => {
        const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
            expect(String(input)).toBe("/api/dsl/parse");
            expect(JSON.parse(String(init?.body))).toEqual({ source: "<x/>" });
            return jsonResponse({
                ast: { id: "root", kind: "statusloom", layouts: [] },
                diagnostics: [],
                version: "h1",
            });
        });
        vi.stubGlobal("fetch", fetchMock);
        const api = createApi(TOKEN);
        const res = await api.parse("<x/>");
        expect(res.ast?.kind).toBe("statusloom");
        expect(res.version).toBe("h1");
    });

    it("POSTs the AST to /api/dsl/serialize", async () => {
        const ast: StatusloomNode = { id: "root", kind: "statusloom", layouts: [] };
        const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
            expect(String(input)).toBe("/api/dsl/serialize");
            expect(JSON.parse(String(init?.body))).toEqual({ ast });
            return jsonResponse({ source: "<statusloom/>", diagnostics: [] });
        });
        vi.stubGlobal("fetch", fetchMock);
        const api = createApi(TOKEN);
        expect(await api.serialize(ast)).toEqual({ source: "<statusloom/>", diagnostics: [] });
    });
});

describe("api.putDraft", () => {
    it("PUTs {tool, source} to /api/dsl/draft and never blocks on diagnostics", async () => {
        const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
            expect(String(input)).toBe("/api/dsl/draft");
            expect(init?.method).toBe("PUT");
            expect(JSON.parse(String(init?.body))).toEqual({
                tool: "claude-code",
                source: "broken <",
            });
            return jsonResponse({
                version: "h3",
                diagnostics: [
                    { severity: "error", message: "bad", range: { start: 0, end: 0 } },
                ],
            });
        });
        vi.stubGlobal("fetch", fetchMock);
        const api = createApi(TOKEN);
        const res = await api.putDraft("claude-code", "broken <");
        expect(res.saved).toBe(true);
        expect(res.version).toBe("h3");
        expect(res.diagnostics).toHaveLength(1);
    });
});

describe("api.preview", () => {
    it("POSTs source/width/sample and includes sessionId + layoutIndex", async () => {
        const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
            expect(String(input)).toBe("/api/dsl/preview");
            const body = JSON.parse(String(init?.body));
            expect(body).toEqual({
                tool: "claude-code",
                source: "<x/>",
                width: 120,
                sample: "full",
                sessionId: "sess-1",
                layoutIndex: 2,
            });
            return jsonResponse({ lines: [], diagnostics: [] });
        });
        vi.stubGlobal("fetch", fetchMock);
        const api = createApi(TOKEN);
        await api.preview({
            tool: "claude-code",
            source: "<x/>",
            width: 120,
            sample: "full",
            sessionId: "sess-1",
            layoutIndex: 2,
        });
        expect(fetchMock).toHaveBeenCalledTimes(1);
    });
});

describe("api.getFields / api.getMetrics", () => {
    it("unwraps the fields array", async () => {
        const fields = [
            {
                name: "model",
                displayName: "Model",
                descriptions: { en: "e", ja: "j" },
                category: "common",
                preview: { text: "Opus", ansi: "Opus" },
            },
        ];
        const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
            expect(String(input)).toBe("/api/dsl/fields?tool=claude-code");
            return jsonResponse({ fields });
        });
        vi.stubGlobal("fetch", fetchMock);
        const api = createApi(TOKEN);
        expect(await api.getFields("claude-code")).toEqual(fields);
    });

    it("passes through requiresVar for var-taking fields", async () => {
        const fields = [
            {
                name: "env",
                displayName: "Environment Variable",
                descriptions: { en: "e", ja: "j" },
                category: "common",
                requiresVar: true,
                preview: { text: "", ansi: "" },
            },
        ];
        vi.stubGlobal("fetch", vi.fn(async () => jsonResponse({ fields })));
        const api = createApi(TOKEN);
        const got = await api.getFields("claude-code");
        expect(got[0]?.requiresVar).toBe(true);
    });

    it("unwraps the metrics array (empty default)", async () => {
        const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
            expect(String(input)).toBe("/api/dsl/metrics?tool=claude-code");
            return jsonResponse({});
        });
        vi.stubGlobal("fetch", fetchMock);
        const api = createApi(TOKEN);
        expect(await api.getMetrics("claude-code")).toEqual([]);
    });

    it("carries the percent flag through for percent-typed metrics", async () => {
        const metrics = [
            { name: "seven-day-percent", displayName: "7-day usage", descriptions: {}, percent: true },
            { name: "cost-usd", displayName: "Cost", descriptions: {} },
        ];
        const fetchMock = vi.fn(async () => jsonResponse({ metrics }));
        vi.stubGlobal("fetch", fetchMock);
        const api = createApi(TOKEN);
        expect(await api.getMetrics("claude-code")).toEqual(metrics);
    });
});

describe("api.probeUsage", () => {
    it("GETs /api/usage/probe and returns the parsed body", async () => {
        const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
            expect(String(input)).toBe("/api/usage/probe");
            expect(init?.method).toBe("GET");
            expect((init?.headers as Record<string, string>).Authorization).toBe(
                `Bearer ${TOKEN}`,
            );
            return jsonResponse({ available: true, reason: "ok", extraUsageEnabled: true });
        });
        vi.stubGlobal("fetch", fetchMock);
        const api = createApi(TOKEN);
        expect(await api.probeUsage()).toEqual({
            available: true,
            reason: "ok",
            extraUsageEnabled: true,
        });
    });

    it("throws ApiError on failure statuses (treated as unavailable by callers)", async () => {
        vi.stubGlobal(
            "fetch",
            vi.fn(async () => jsonResponse({ error: "boom" }, 500)),
        );
        const api = createApi(TOKEN);
        await expect(api.probeUsage()).rejects.toThrow(ApiError);
    });
});

describe("api.getHistory / api.getHistoryRevision / api.restoreRevision", () => {
    it("GETs /api/history with the tool query param", async () => {
        const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
            expect(String(input)).toBe("/api/history?tool=claude-code");
            expect(init?.method).toBe("GET");
            expect((init?.headers as Record<string, string>).Authorization).toBe(
                `Bearer ${TOKEN}`,
            );
            return jsonResponse({
                revisions: [
                    {
                        id: "01958f2a-0000-7000-8000-000000000001",
                        parent: null,
                        savedAt: "2026-07-17T09:00:00Z",
                        origin: "ui",
                        meta: { name: "initial" },
                    },
                ],
                refs: { current: "01958f2a-0000-7000-8000-000000000001", draft: false },
            });
        });
        vi.stubGlobal("fetch", fetchMock);

        const api = createApi(TOKEN);
        const res = await api.getHistory("claude-code");
        expect(res.revisions).toHaveLength(1);
        expect(res.refs).toEqual({
            current: "01958f2a-0000-7000-8000-000000000001",
            draft: false,
        });
    });

    it("GETs /api/history/{id} with no tool parameter", async () => {
        const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
            expect(String(input)).toBe("/api/history/01958f2a-0000-7000-8000-000000000001");
            expect(init?.method).toBe("GET");
            return jsonResponse({
                id: "01958f2a-0000-7000-8000-000000000001",
                tool: "claude-code",
                source: "<statusloom/>",
                parent: null,
                savedAt: "2026-07-17T09:00:00Z",
                origin: "ui",
                meta: {},
            });
        });
        vi.stubGlobal("fetch", fetchMock);

        const api = createApi(TOKEN);
        const res = await api.getHistoryRevision("01958f2a-0000-7000-8000-000000000001");
        expect(res.source).toBe("<statusloom/>");
        expect(res.tool).toBe("claude-code");
    });

    it("404s from GET /api/history/{id} surface as ApiError", async () => {
        vi.stubGlobal(
            "fetch",
            vi.fn(async () => jsonResponse({ error: "unknown revision" }, 404)),
        );
        const api = createApi(TOKEN);
        await expect(api.getHistoryRevision("nope")).rejects.toThrow(ApiError);
    });

    it("POSTs /api/history/{id}/restore with no body", async () => {
        const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
            expect(String(input)).toBe(
                "/api/history/01958f2a-0000-7000-8000-000000000001/restore",
            );
            expect(init?.method).toBe("POST");
            expect(init?.body).toBeUndefined();
            return jsonResponse({
                ok: true,
                tool: "claude-code",
                current: "01958f2a-0000-7000-8000-000000000001",
            });
        });
        vi.stubGlobal("fetch", fetchMock);

        const api = createApi(TOKEN);
        const res = await api.restoreRevision("01958f2a-0000-7000-8000-000000000001");
        expect(res).toEqual({
            ok: true,
            tool: "claude-code",
            current: "01958f2a-0000-7000-8000-000000000001",
        });
    });
});

describe("api.importExchange", () => {
    it("POSTs the raw Markdown body (not JSON) and resolves saved=true on 200", async () => {
        const md = '---\nformat: "statusloom/config"\nformatVersion: 1\n---\n\n```xml\n<statusloom/>\n```\n';
        const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
            expect(String(input)).toBe("/api/exchange/import");
            expect(init?.method).toBe("POST");
            expect(init?.body).toBe(md);
            expect((init?.headers as Record<string, string>).Authorization).toBe(
                `Bearer ${TOKEN}`,
            );
            expect((init?.headers as Record<string, string>)["Content-Type"]).toContain(
                "text/markdown",
            );
            return jsonResponse({ tool: "claude-code", revision: "01958f2a-...", diagnostics: [] });
        });
        vi.stubGlobal("fetch", fetchMock);

        const api = createApi(TOKEN);
        const res = await api.importExchange(md);
        expect(res).toEqual({
            saved: true,
            tool: "claude-code",
            revision: "01958f2a-...",
            diagnostics: [],
        });
    });

    it("resolves saved=false with diagnostics on 409 (error diagnostics block the save)", async () => {
        const diags = [{ severity: "error", message: "unknown field", range: { start: 4, end: 9 } }];
        vi.stubGlobal(
            "fetch",
            vi.fn(async () => jsonResponse({ diagnostics: diags }, 409)),
        );
        const api = createApi(TOKEN);
        const res = await api.importExchange("---\n---\n```xml\n<x/>\n```\n");
        expect(res.saved).toBe(false);
        expect(res.diagnostics).toEqual(diags);
    });

    it("throws ApiError for other failure statuses (e.g. malformed Markdown, 400)", async () => {
        vi.stubGlobal(
            "fetch",
            vi.fn(async () => jsonResponse({ error: "no ```xml fence found" }, 400)),
        );
        const api = createApi(TOKEN);
        await expect(api.importExchange("not markdown")).rejects.toMatchObject({
            status: 400,
            message: "no ```xml fence found",
        });
    });
});

describe("api.getExportMarkdown", () => {
    it("GETs /api/exchange/export with the tool query param and returns the raw body", async () => {
        const md = '---\nformat: "statusloom/config"\nformatVersion: 1\n---\n\n```xml\n<statusloom/>\n```\n';
        const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
            expect(String(input)).toBe("/api/exchange/export?tool=claude-code");
            expect((init?.headers as Record<string, string>).Authorization).toBe(
                `Bearer ${TOKEN}`,
            );
            return new Response(md, { status: 200, headers: { "Content-Type": "text/markdown" } });
        });
        vi.stubGlobal("fetch", fetchMock);

        const api = createApi(TOKEN);
        expect(await api.getExportMarkdown("claude-code")).toBe(md);
    });

    it("throws ApiError (404) when the tool has no current revision", async () => {
        vi.stubGlobal(
            "fetch",
            vi.fn(async () => jsonResponse({ error: "no saved configuration for tool" }, 404)),
        );
        const api = createApi(TOKEN);
        await expect(api.getExportMarkdown("claude-code")).rejects.toMatchObject({
            status: 404,
        });
    });
});

describe("api.getSessions", () => {
    it("returns an empty array when the response omits `sessions`", async () => {
        vi.stubGlobal(
            "fetch",
            vi.fn(async () => jsonResponse({})),
        );
        const api = createApi(TOKEN);
        expect(await api.getSessions()).toEqual([]);
    });

    it("throws ApiError with the response status on failure", async () => {
        vi.stubGlobal(
            "fetch",
            vi.fn(async () => jsonResponse({ error: "boom" }, 500)),
        );
        const api = createApi(TOKEN);
        await expect(api.getSessions()).rejects.toThrow(ApiError);
    });
});

describe("api.startLiveSession / api.startTerminalSession", () => {
    it("POSTs /api/live/session and returns the launch info", async () => {
        const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
            expect(String(input)).toBe("/api/live/session");
            expect(init?.method).toBe("POST");
            return jsonResponse({
                launchCommand: "cd /tmp/statusloom-live-1 && claude",
                tmpDir: "/tmp/statusloom-live-1",
            });
        });
        vi.stubGlobal("fetch", fetchMock);
        const api = createApi(TOKEN);
        expect(await api.startLiveSession()).toEqual({
            launchCommand: "cd /tmp/statusloom-live-1 && claude",
            tmpDir: "/tmp/statusloom-live-1",
        });
    });

    it("POSTs /api/terminal/session and surfaces the concurrent-limit (429)", async () => {
        vi.stubGlobal(
            "fetch",
            vi.fn(async () => jsonResponse({ error: "too many sessions" }, 429)),
        );
        const api = createApi(TOKEN);
        await expect(api.startTerminalSession()).rejects.toMatchObject({ status: 429 });
    });
});

describe("socket URLs", () => {
    it("liveSocketUrl builds a ws:// URL with the token as a query param", () => {
        // jsdom's default location is http://localhost:3000/
        expect(liveSocketUrl(TOKEN)).toBe(`ws://localhost:3000/ws/live?token=${TOKEN}`);
    });

    it("terminalSocketUrl includes both the token and the terminal id", () => {
        expect(terminalSocketUrl(TOKEN, "deadbeef")).toBe(
            `ws://localhost:3000/ws/terminal?token=${TOKEN}&id=deadbeef`,
        );
    });
});
