import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { LiveMonitor } from "./LiveMonitor.tsx";
import type { Api } from "../api.ts";
import type { PreviewSource } from "../types.ts";

const TOKEN = "a".repeat(32);

// Minimal fake WebSocket: jsdom does not implement one at all, and the real
// browser API would try to hit the network anyway. Tests drive the
// connection lifecycle by calling the on* handlers directly.
class FakeWebSocket {
    static instances: FakeWebSocket[] = [];
    url: string;
    onopen: (() => void) | null = null;
    onmessage: ((ev: { data: string }) => void) | null = null;
    onclose: (() => void) | null = null;
    onerror: (() => void) | null = null;
    closeCalls = 0;

    constructor(url: string) {
        this.url = url;
        FakeWebSocket.instances.push(this);
    }

    close() {
        this.closeCalls += 1;
    }
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
        startLiveSession: vi.fn(async () => ({
            launchCommand: "cd /tmp/statusloom-live-1 && claude",
            tmpDir: "/tmp/statusloom-live-1",
        })),
        startTerminalSession: vi.fn(async () => ({ terminalId: "deadbeef" })),
        ...overrides,
    };
}

function latestSocket(): FakeWebSocket {
    const ws = FakeWebSocket.instances[FakeWebSocket.instances.length - 1];
    if (!ws) {
        throw new Error("no WebSocket instance was created");
    }
    return ws;
}

describe("LiveMonitor", () => {
    beforeEach(() => {
        FakeWebSocket.instances = [];
        vi.stubGlobal("WebSocket", FakeWebSocket as unknown as typeof WebSocket);
    });

    afterEach(() => {
        vi.unstubAllGlobals();
        vi.useRealTimers();
    });

    function renderMonitor(api: Api, previewSource: PreviewSource, onPreviewSourceChange = vi.fn()) {
        return {
            onPreviewSourceChange,
            ...render(
                <LiveMonitor
                    api={api}
                    token={TOKEN}
                    previewSource={previewSource}
                    onPreviewSourceChange={onPreviewSourceChange}
                    onOpenEmbedded={vi.fn()}
                    embeddedOpen={false}
                />,
            ),
        };
    }

    it("starts a live session, opens a socket, and shows the launch command", async () => {
        const api = makeApi();
        renderMonitor(api, { kind: "sample", sample: "full" });

        fireEvent.click(screen.getByText("Start live monitor"));

        await waitFor(() => expect(api.startLiveSession).toHaveBeenCalledTimes(1));
        await waitFor(() => expect(FakeWebSocket.instances.length).toBe(1));
        expect(latestSocket().url).toBe(`ws://localhost:3000/ws/live?token=${TOKEN}`);
        expect(screen.getByTestId("live-launch-command").textContent).toBe(
            "cd /tmp/statusloom-live-1 && claude",
        );
    });

    it("switches previewSource to the session id on the first matching live-update", async () => {
        const api = makeApi();
        const { onPreviewSourceChange } = renderMonitor(api, { kind: "sample", sample: "full" });

        fireEvent.click(screen.getByText("Start live monitor"));
        await waitFor(() => expect(FakeWebSocket.instances.length).toBe(1));
        const ws = latestSocket();

        act(() => {
            ws.onopen?.();
        });
        expect(screen.getByTestId("live-monitor-status").textContent).toContain(
            "waiting for monitor session",
        );

        act(() => {
            ws.onmessage?.({
                data: JSON.stringify({
                    type: "live-update",
                    sessionId: "sess-1",
                    observedAt: "2026-07-11T10:00:00Z",
                }),
            });
        });

        expect(onPreviewSourceChange).toHaveBeenCalledWith({ kind: "session", id: "sess-1" });
        expect(screen.getByTestId("live-monitor-status").textContent).toContain("monitor active");
    });

    it("ignores live-update noise for a different session once locked on", async () => {
        const api = makeApi();
        const { onPreviewSourceChange } = renderMonitor(api, { kind: "sample", sample: "full" });

        fireEvent.click(screen.getByText("Start live monitor"));
        await waitFor(() => expect(FakeWebSocket.instances.length).toBe(1));
        const ws = latestSocket();

        act(() => {
            ws.onmessage?.({
                data: JSON.stringify({ type: "live-update", sessionId: "sess-1", observedAt: "t" }),
            });
        });
        expect(onPreviewSourceChange).toHaveBeenCalledTimes(1);

        act(() => {
            ws.onmessage?.({
                data: JSON.stringify({ type: "live-update", sessionId: "sess-2", observedAt: "t" }),
            });
        });
        // The unrelated session's update must not trigger another switch.
        expect(onPreviewSourceChange).toHaveBeenCalledTimes(1);

        act(() => {
            ws.onmessage?.({
                data: JSON.stringify({ type: "live-update", sessionId: "sess-1", observedAt: "t2" }),
            });
        });
        expect(onPreviewSourceChange).toHaveBeenCalledTimes(2);
    });

    it("disengages live mode (closing the socket) when the user manually picks a different sample", async () => {
        const api = makeApi();
        const onPreviewSourceChange = vi.fn();
        const { rerender } = render(
            <LiveMonitor
                api={api}
                token={TOKEN}
                previewSource={{ kind: "sample", sample: "full" }}
                onPreviewSourceChange={onPreviewSourceChange}
                onOpenEmbedded={vi.fn()}
                embeddedOpen={false}
            />,
        );

        fireEvent.click(screen.getByText("Start live monitor"));
        await waitFor(() => expect(FakeWebSocket.instances.length).toBe(1));
        const ws = latestSocket();
        act(() => {
            ws.onmessage?.({
                data: JSON.stringify({ type: "live-update", sessionId: "sess-1", observedAt: "t" }),
            });
        });
        await waitFor(() => expect(screen.getByTestId("live-monitor-status")).toBeTruthy());

        // User manually re-selects a different sample from the Canvas selector.
        rerender(
            <LiveMonitor
                api={api}
                token={TOKEN}
                previewSource={{ kind: "sample", sample: "early-session" }}
                onPreviewSourceChange={onPreviewSourceChange}
                onOpenEmbedded={vi.fn()}
                embeddedOpen={false}
            />,
        );

        await waitFor(() => expect(ws.closeCalls).toBe(1));
        expect(screen.getByText("Start live monitor")).toBeTruthy();
    });

    it("attempts to reconnect on an unexpected close, and falls back to waiting quietly if it never succeeds", async () => {
        vi.useFakeTimers();
        const api = makeApi();
        renderMonitor(api, { kind: "sample", sample: "full" });

        await act(async () => {
            fireEvent.click(screen.getByText("Start live monitor"));
            await Promise.resolve();
            await Promise.resolve();
        });
        expect(FakeWebSocket.instances.length).toBe(1);
        const first = latestSocket();

        act(() => {
            first.onclose?.();
        });
        // Backoff delay elapses -> a new socket is opened.
        await act(async () => {
            await vi.advanceTimersByTimeAsync(1000);
        });
        expect(FakeWebSocket.instances.length).toBe(2);

        // No error banner is shown while quietly retrying/waiting, and the
        // launch command / Stop affordance stay visible throughout.
        expect(screen.queryByText(/Could not/i)).toBeNull();
        expect(screen.getByText("Stop live monitor")).toBeTruthy();
    });

    it("stops live mode and closes the socket when the user clicks Stop", async () => {
        const api = makeApi();
        renderMonitor(api, { kind: "sample", sample: "full" });

        fireEvent.click(screen.getByText("Start live monitor"));
        await waitFor(() => expect(FakeWebSocket.instances.length).toBe(1));
        const ws = latestSocket();
        act(() => {
            ws.onopen?.();
        });

        fireEvent.click(screen.getByText("Stop live monitor"));
        expect(ws.closeCalls).toBe(1);
        expect(screen.getByText("Start live monitor")).toBeTruthy();
    });
});
