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

    it("unwraps the metrics array (empty default)", async () => {
        const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
            expect(String(input)).toBe("/api/dsl/metrics?tool=claude-code");
            return jsonResponse({});
        });
        vi.stubGlobal("fetch", fetchMock);
        const api = createApi(TOKEN);
        expect(await api.getMetrics("claude-code")).toEqual([]);
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
