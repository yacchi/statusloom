import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";

// xterm.js needs a real canvas/DOM renderer, which jsdom lacks; mock the
// terminal + fit addon so the test can verify the WebSocket wiring (input,
// resize, output) without a browser. `captured` (hoisted so the vi.mock
// factories can reference it) exposes the onData / onResize handlers and
// recorded writes so the test can drive them directly.
const captured = vi.hoisted(() => ({
    dataHandler: undefined as ((d: string) => void) | undefined,
    resizeHandler: undefined as ((size: { cols: number; rows: number }) => void) | undefined,
    writes: [] as Uint8Array[],
    disposed: false,
    fitCalls: 0,
}));

vi.mock("@xterm/xterm", () => ({
    Terminal: class {
        cols = 80;
        rows = 24;
        loadAddon() {}
        open() {}
        write(data: Uint8Array | string) {
            captured.writes.push(data as Uint8Array);
        }
        onData(cb: (d: string) => void) {
            captured.dataHandler = cb;
            return { dispose() {} };
        }
        onResize(cb: (size: { cols: number; rows: number }) => void) {
            captured.resizeHandler = cb;
            return { dispose() {} };
        }
        dispose() {
            captured.disposed = true;
        }
    },
}));
vi.mock("@xterm/addon-fit", () => ({
    FitAddon: class {
        fit() {
            captured.fitCalls += 1;
        }
    },
}));
vi.mock("@xterm/xterm/css/xterm.css", () => ({}));

// Imported after the mocks are registered.
import { TerminalDrawer } from "./TerminalDrawer.tsx";
import type { Api } from "../api.ts";
import type { PreviewSource } from "../types.ts";

const TOKEN = "a".repeat(32);

class FakeWebSocket {
    static OPEN = 1;
    static instances: FakeWebSocket[] = [];
    url: string;
    binaryType = "blob";
    readyState = 1;
    sent: Array<string | ArrayBufferLike | ArrayBufferView> = [];
    onopen: (() => void) | null = null;
    onmessage: ((ev: { data: unknown }) => void) | null = null;
    onclose: (() => void) | null = null;
    onerror: (() => void) | null = null;
    closeCalls = 0;

    constructor(url: string) {
        this.url = url;
        FakeWebSocket.instances.push(this);
    }
    send(data: string | ArrayBufferLike | ArrayBufferView) {
        this.sent.push(data);
    }
    close() {
        this.closeCalls += 1;
    }
}

function socketMatching(fragment: string): FakeWebSocket {
    const ws = FakeWebSocket.instances.find((s) => s.url.includes(fragment));
    if (!ws) {
        throw new Error(`no WebSocket for ${fragment}`);
    }
    return ws;
}

function makeApi(overrides: Partial<Api> = {}): Api {
    return {
        getDocument: vi.fn(async () => ({ source: "", version: "v0", exists: false })),
        putDocument: vi.fn(async () => ({ saved: true, version: "v0", diagnostics: [] })),
        parse: vi.fn(async () => ({ diagnostics: [], version: "v0" })),
        serialize: vi.fn(async () => ({ source: "", diagnostics: [] })),
        getDraft: vi.fn(async () => ({ source: "", version: "v0", exists: false })),
        putDraft: vi.fn(async () => ({ saved: true, version: "v0", diagnostics: [] })),
        preview: vi.fn(async () => ({ lines: [], diagnostics: [] })),
        getFields: vi.fn(async () => []),
        getMetrics: vi.fn(async () => []),
        getSessions: vi.fn(async () => []),
        shutdown: vi.fn(async () => {}),
        startLiveSession: vi.fn(async () => ({ launchCommand: "cd /tmp && claude", tmpDir: "/tmp" })),
        startTerminalSession: vi.fn(async () => ({ terminalId: "deadbeef" })),
        ...overrides,
    };
}

describe("TerminalDrawer", () => {
    beforeEach(() => {
        captured.dataHandler = undefined;
        captured.resizeHandler = undefined;
        captured.writes = [];
        captured.disposed = false;
        captured.fitCalls = 0;
        FakeWebSocket.instances = [];
        vi.stubGlobal("WebSocket", FakeWebSocket as unknown as typeof WebSocket);
        vi.stubGlobal(
            "ResizeObserver",
            class {
                observe() {}
                disconnect() {}
            },
        );
    });

    afterEach(() => {
        vi.unstubAllGlobals();
    });

    // Rendered "open" with startNonce=1 auto-starts a session (the sidebar
    // trigger's role), so each test begins with a live drawer.
    function renderDrawer(api: Api, extra?: { onSetOpen?: () => void; onPreviewSourceChange?: () => void }) {
        const previewSource: PreviewSource = { kind: "sample", sample: "full" };
        const onSetOpen = extra?.onSetOpen ?? vi.fn();
        const onPreviewSourceChange = extra?.onPreviewSourceChange ?? vi.fn();
        return {
            onSetOpen,
            onPreviewSourceChange,
            ...render(
                <TerminalDrawer
                    api={api}
                    token={TOKEN}
                    previewSource={previewSource}
                    onPreviewSourceChange={onPreviewSourceChange}
                    startNonce={1}
                    open={true}
                    onSetOpen={onSetOpen}
                />,
            ),
        };
    }

    it("auto-starts on the nonce and opens an arraybuffer /ws/terminal socket", async () => {
        const api = makeApi();
        renderDrawer(api);

        await waitFor(() => expect(api.startTerminalSession).toHaveBeenCalledTimes(1));
        const ws = await waitFor(() => socketMatching("/ws/terminal"));
        expect(ws.url).toBe(`ws://localhost:3000/ws/terminal?token=${TOKEN}&id=deadbeef`);
        expect(ws.binaryType).toBe("arraybuffer");
    });

    it("sends terminal input as binary bytes", async () => {
        const api = makeApi();
        renderDrawer(api);
        const ws = await waitFor(() => socketMatching("/ws/terminal"));

        act(() => captured.dataHandler?.("hi"));

        expect(ws.sent).toHaveLength(1);
        const sent = ws.sent[0];
        expect(typeof sent).not.toBe("string");
        const bytes = new Uint8Array(sent as ArrayBufferLike);
        expect(Array.from(bytes)).toEqual(Array.from(new TextEncoder().encode("hi")));
    });

    it("sends a resize as a JSON text frame", async () => {
        const api = makeApi();
        renderDrawer(api);
        const ws = await waitFor(() => socketMatching("/ws/terminal"));

        act(() => captured.resizeHandler?.({ cols: 100, rows: 40 }));

        const textFrame = (ws.sent as unknown[]).find((f) => typeof f === "string") as string;
        expect(JSON.parse(textFrame)).toEqual({ type: "resize", cols: 100, rows: 40 });
    });

    it("writes received binary output to the terminal", async () => {
        const api = makeApi();
        renderDrawer(api);
        const ws = await waitFor(() => socketMatching("/ws/terminal"));

        const payload = new TextEncoder().encode("out");
        act(() => ws.onmessage?.({ data: payload.buffer }));

        expect(captured.writes).toHaveLength(1);
        expect(Array.from(captured.writes[0])).toEqual(Array.from(payload));
    });

    it("switches previewSource on a live-update from the spawned session", async () => {
        const api = makeApi();
        const { onPreviewSourceChange } = renderDrawer(api, { onPreviewSourceChange: vi.fn() });
        const live = await waitFor(() => socketMatching("/ws/live"));

        act(() =>
            live.onmessage?.({
                data: JSON.stringify({ type: "live-update", sessionId: "sess-9", observedAt: "t" }),
            }),
        );

        expect(onPreviewSourceChange).toHaveBeenCalledWith({ kind: "session", id: "sess-9" });
    });

    it("Stop closes the socket and disposes the terminal", async () => {
        const api = makeApi();
        renderDrawer(api);
        const ws = await waitFor(() => socketMatching("/ws/terminal"));

        fireEvent.click(screen.getByTestId("terminal-stop"));

        expect(ws.closeCalls).toBe(1);
        expect(captured.disposed).toBe(true);
        // After Stop the drawer offers a Restart.
        expect(screen.getByText("Restart")).toBeTruthy();
    });

    it("collapse (close) hides the drawer but keeps the socket alive", async () => {
        const api = makeApi();
        const { onSetOpen } = renderDrawer(api, { onSetOpen: vi.fn() });
        const ws = await waitFor(() => socketMatching("/ws/terminal"));

        fireEvent.click(screen.getByTestId("terminal-toggle"));

        // Closing only asks the parent to hide; the session must not be torn down.
        expect(onSetOpen).toHaveBeenCalledWith(false);
        expect(ws.closeCalls).toBe(0);
        expect(captured.disposed).toBe(false);
    });

    it("shows an error and opens no terminal socket when spawning fails", async () => {
        const api = makeApi({
            startTerminalSession: vi.fn(async () => {
                throw new Error("too many sessions");
            }),
        });
        renderDrawer(api);

        await waitFor(() => expect(screen.getByText("too many sessions")).toBeTruthy());
        expect(FakeWebSocket.instances.some((s) => s.url.includes("/ws/terminal"))).toBe(false);
    });
});
