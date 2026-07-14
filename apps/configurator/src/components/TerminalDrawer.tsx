// Embedded terminal (L3), docked as a full-width drawer at the bottom of the
// window (like a CloudShell / VS Code integrated terminal).
//
// The session lifecycle and wire protocol are UNCHANGED from the original
// sidebar embedded terminal — only the placement/layout moved:
//   server → browser  Binary frame = raw PTY output bytes → term.write()
//   browser → server  Binary frame = term.onData() input bytes
//   browser → server  Text frame   = JSON {"type":"resize","cols","rows"}
// binaryType is "arraybuffer". The spawned agent emits to /api/live, so the
// live preview stays driven by the SAME useLiveUpdates hook.
//
// Trigger vs. body: the "Start embedded session" trigger lives in the sidebar
// (LiveMonitor). It bumps `startNonce` and sets `open`, which this drawer
// reconciles. "Close" (collapse) only hides the drawer while keeping the
// socket alive; "Stop" tears the socket down so the backend kills the process
// and removes the temp dir.

import { useCallback, useEffect, useRef, useState } from "react";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import "@xterm/xterm/css/xterm.css";
import type { Api } from "../api.ts";
import { terminalSocketUrl } from "../api.ts";
import { t, useLang } from "../i18n.ts";
import type { PreviewSource, TerminalResize } from "../types.ts";
import { useLiveUpdates } from "../useLiveUpdates.ts";

const MIN_HEIGHT = 160;
const DEFAULT_HEIGHT = 300;
function maxHeight(): number {
    return typeof window !== "undefined" ? Math.round(window.innerHeight * 0.7) : 640;
}

function sourceEquals(a: PreviewSource, b: PreviewSource): boolean {
    if (a.kind === "session" && b.kind === "session") {
        return a.id === b.id;
    }
    if (a.kind === "sample" && b.kind === "sample") {
        return a.sample === b.sample;
    }
    return false;
}

type Phase = "idle" | "starting" | "running" | "closed" | "failed";

// The drawer's session status, lifted to the parent so the footer bar can show
// it and its actions without a second visible bar.
export interface TerminalStatus {
    phase: Phase;
    connected: boolean;
}

export interface TerminalDrawerProps {
    api: Api;
    token: string;
    previewSource: PreviewSource;
    onPreviewSourceChange: (source: PreviewSource) => void;
    // Incremented by the sidebar trigger to (re)start a session.
    startNonce: number;
    // Whether the drawer is expanded. Collapsing keeps the session alive.
    open: boolean;
    onSetOpen: (open: boolean) => void;
    // Reports phase/connected changes so the footer can reflect them.
    onStatusChange?: (status: TerminalStatus) => void;
}

export function TerminalDrawer({
    api,
    token,
    previewSource,
    onPreviewSourceChange,
    startNonce,
    open,
    onSetOpen,
    onStatusChange,
}: TerminalDrawerProps) {
    const lang = useLang();
    const [phase, setPhase] = useState<Phase>("idle");
    const [terminalId, setTerminalId] = useState<string | null>(null);
    const [connected, setConnected] = useState(false);
    const [error, setError] = useState<string | null>(null);
    const [height, setHeight] = useState(DEFAULT_HEIGHT);

    const containerRef = useRef<HTMLDivElement | null>(null);
    const termRef = useRef<Terminal | null>(null);
    const fitRef = useRef<FitAddon | null>(null);
    const wsRef = useRef<WebSocket | null>(null);

    // Preview integration: track the source we pushed ourselves so a manual
    // selection by the user can be told apart from our own live update.
    const expectedSourceRef = useRef<PreviewSource | null>(null);
    const lastSeenSourceRef = useRef<PreviewSource>(previewSource);

    const onUpdate = useCallback(
        (sessionId: string) => {
            const next: PreviewSource = { kind: "session", id: sessionId };
            expectedSourceRef.current = next;
            onPreviewSourceChange(next);
        },
        [onPreviewSourceChange],
    );
    const live = useLiveUpdates(token, onUpdate);

    const doFit = useCallback(() => {
        try {
            fitRef.current?.fit();
        } catch {
            // fit() can throw before the container has a measurable size.
        }
    }, []);

    // Build the xterm terminal and the /ws/terminal socket once we have a
    // terminalId, and tear both down on stop/unmount. (Unchanged wiring.)
    useEffect(() => {
        if (!terminalId) {
            return;
        }
        const container = containerRef.current;
        if (!container) {
            return;
        }
        let disposed = false;

        const term = new Terminal({
            fontSize: 13,
            fontFamily: 'ui-monospace, "SF Mono", Menlo, monospace',
            cursorBlink: true,
            convertEol: false,
        });
        const fit = new FitAddon();
        term.loadAddon(fit);
        term.open(container);
        try {
            fit.fit();
        } catch {
            // The ResizeObserver below will retry once measurable.
        }
        termRef.current = term;
        fitRef.current = fit;

        let ws: WebSocket;
        try {
            ws = new WebSocket(terminalSocketUrl(token, terminalId));
        } catch (err) {
            setError((err as Error).message);
            setPhase("failed");
            term.dispose();
            termRef.current = null;
            fitRef.current = null;
            return;
        }
        ws.binaryType = "arraybuffer";
        wsRef.current = ws;

        const sendResize = (cols: number, rows: number) => {
            if (ws.readyState === WebSocket.OPEN) {
                const msg: TerminalResize = { type: "resize", cols, rows };
                ws.send(JSON.stringify(msg));
            }
        };

        ws.onopen = () => {
            if (disposed) {
                return;
            }
            setConnected(true);
            try {
                fit.fit();
            } catch {
                // see above
            }
            sendResize(term.cols, term.rows);
        };
        ws.onmessage = (ev: MessageEvent) => {
            if (disposed) {
                return;
            }
            const data = ev.data;
            if (typeof data === "string") {
                // The contract is binary output, but a text frame (should it
                // ever arrive) is still writable verbatim.
                term.write(data);
            } else if (ArrayBuffer.isView(data)) {
                const view = data as ArrayBufferView;
                term.write(new Uint8Array(view.buffer, view.byteOffset, view.byteLength));
            } else if (data && typeof (data as Blob).arrayBuffer === "function") {
                // binaryType defaults to "blob" on some engines; decode it.
                (data as Blob).arrayBuffer().then((buf) => {
                    if (!disposed) {
                        term.write(new Uint8Array(buf));
                    }
                });
            } else if (data) {
                // Assume an ArrayBuffer (binaryType is set to "arraybuffer").
                try {
                    term.write(new Uint8Array(data as ArrayBuffer));
                } catch {
                    // Unknown frame type; ignore rather than crash the socket.
                }
            }
        };
        ws.onclose = () => {
            if (disposed) {
                return;
            }
            setConnected(false);
            setPhase("closed");
        };
        ws.onerror = () => {
            // onclose always follows.
        };

        const dataSub = term.onData((d: string) => {
            if (ws.readyState === WebSocket.OPEN) {
                ws.send(new TextEncoder().encode(d));
            }
        });
        const resizeSub = term.onResize(({ cols, rows }: { cols: number; rows: number }) => {
            sendResize(cols, rows);
        });

        let ro: ResizeObserver | null = null;
        if (typeof ResizeObserver !== "undefined") {
            ro = new ResizeObserver(() => doFit());
            ro.observe(container);
        }

        return () => {
            disposed = true;
            ro?.disconnect();
            dataSub.dispose();
            resizeSub.dispose();
            ws.onopen = null;
            ws.onmessage = null;
            ws.onclose = null;
            ws.onerror = null;
            ws.close();
            term.dispose();
            wsRef.current = null;
            termRef.current = null;
            fitRef.current = null;
        };
    }, [terminalId, token, doFit]);

    // A ref mirror of `phase` plus an in-flight guard so `start` can bail
    // synchronously — protecting against a double-spawn from StrictMode's
    // double-invoked effects or a rapid re-trigger — without depending on
    // (and being recreated by) phase changes.
    const phaseRef = useRef<Phase>(phase);
    phaseRef.current = phase;
    const spawnGuardRef = useRef(false);

    const start = useCallback(async () => {
        if (spawnGuardRef.current) {
            return; // a spawn is already in flight
        }
        if (phaseRef.current === "starting" || phaseRef.current === "running") {
            return; // a session is already live; the trigger just reopens
        }
        spawnGuardRef.current = true;
        setPhase("starting");
        setError(null);
        try {
            const info = await api.startTerminalSession();
            expectedSourceRef.current = null;
            live.start();
            setConnected(false);
            setTerminalId(info.terminalId);
            setPhase("running");
        } catch (err) {
            setPhase("failed");
            setError((err as Error).message);
        } finally {
            spawnGuardRef.current = false;
        }
    }, [api, live]);

    const stop = useCallback(() => {
        // Dropping the terminalId tears down the effect (which closes the
        // socket → backend kills the process and removes the temp dir).
        setTerminalId(null);
        setConnected(false);
        live.stop();
        expectedSourceRef.current = null;
        setPhase("closed");
    }, [live]);

    // Report status changes to the parent (footer bar mirror). Kept in a ref so
    // the effect does not re-run just because the callback identity changed.
    const onStatusChangeRef = useRef(onStatusChange);
    onStatusChangeRef.current = onStatusChange;
    useEffect(() => {
        onStatusChangeRef.current?.({ phase, connected });
    }, [phase, connected]);

    // (Re)start when the sidebar trigger bumps the nonce.
    const startRef = useRef(start);
    startRef.current = start;
    useEffect(() => {
        if (startNonce > 0) {
            startRef.current();
        }
    }, [startNonce]);

    // Re-fit whenever the drawer is expanded or its height changes, deferred a
    // frame so the new layout is measurable.
    useEffect(() => {
        if (!open || !termRef.current) {
            return;
        }
        const raf = window.requestAnimationFrame(() => doFit());
        return () => window.cancelAnimationFrame(raf);
    }, [open, height, connected, doFit]);

    // While running, a manual preview-source change (via the Canvas selector)
    // disengages the live preview subscription but leaves the terminal running.
    useEffect(() => {
        const prev = lastSeenSourceRef.current;
        lastSeenSourceRef.current = previewSource;
        if (phase !== "running") {
            return;
        }
        if (sourceEquals(previewSource, prev)) {
            return;
        }
        if (expectedSourceRef.current && sourceEquals(previewSource, expectedSourceRef.current)) {
            return;
        }
        live.stop();
        expectedSourceRef.current = null;
    }, [previewSource, phase, live]);

    // ---- height drag ----
    const dragRef = useRef<{ startY: number; startHeight: number } | null>(null);
    const onHandlePointerDown = useCallback(
        (e: React.PointerEvent) => {
            e.preventDefault();
            dragRef.current = { startY: e.clientY, startHeight: height };
            const onMove = (ev: PointerEvent) => {
                const drag = dragRef.current;
                if (!drag) {
                    return;
                }
                // Dragging up (smaller clientY) grows the drawer.
                const next = drag.startHeight + (drag.startY - ev.clientY);
                setHeight(Math.max(MIN_HEIGHT, Math.min(maxHeight(), next)));
            };
            const onUp = () => {
                dragRef.current = null;
                window.removeEventListener("pointermove", onMove);
                window.removeEventListener("pointerup", onUp);
            };
            window.addEventListener("pointermove", onMove);
            window.addEventListener("pointerup", onUp);
        },
        [height],
    );

    const mounted = phase !== "idle";
    if (!mounted && !open) {
        return null;
    }

    // Collapsed: no visible bar at all. The footer bar shows the status and
    // actions instead; here only the (hidden) xterm host stays mounted so the
    // socket/terminal survive across collapse/expand.
    const expanded = open;

    return (
        <div
            className={"terminal-drawer" + (expanded ? " expanded" : " collapsed")}
            style={expanded ? { height: `${height}px` } : undefined}
        >
            {expanded ? (
                <>
                    <div
                        className="terminal-drawer-resize"
                        onPointerDown={onHandlePointerDown}
                        role="separator"
                        aria-label="Resize terminal"
                    />

                    <div className="terminal-drawer-header">
                        <strong>{t(lang, "embeddedTerminalTitle")}</strong>
                        <span
                            className="live-monitor-status"
                            data-testid="terminal-drawer-status"
                        >
                            <span
                                className={`live-status-dot ${
                                    phase === "running" && connected
                                        ? "connected"
                                        : phase === "closed" || phase === "failed"
                                          ? ""
                                          : "connecting"
                                }`}
                            />
                            {phase === "failed"
                                ? t(lang, "embeddedTerminalClosed")
                                : phase === "closed"
                                  ? t(lang, "embeddedTerminalClosed")
                                  : connected
                                    ? t(lang, "embeddedTerminalConnected")
                                    : t(lang, "embeddedTerminalConnecting")}
                        </span>
                        <span
                            className="terminal-drawer-warn"
                            title={t(lang, "embeddedTerminalWarning")}
                        >
                            ⚠
                        </span>
                        <span className="spacer" />
                        {phase === "closed" || phase === "failed" ? (
                            <button type="button" onClick={() => start()}>
                                {t(lang, "embeddedTerminalRestart")}
                            </button>
                        ) : (
                            <button type="button" onClick={stop} data-testid="terminal-stop">
                                {t(lang, "embeddedTerminalStop")}
                            </button>
                        )}
                        <button
                            type="button"
                            onClick={() => onSetOpen(!expanded)}
                            data-testid="terminal-toggle"
                            title={
                                expanded
                                    ? t(lang, "embeddedTerminalHide")
                                    : t(lang, "embeddedTerminalShow")
                            }
                        >
                            {expanded ? "▾" : "▸"}
                        </button>
                    </div>

                    {phase === "failed" && error ? (
                        <div className="inline-error" style={{ padding: "0 8px 8px" }}>
                            {error}
                        </div>
                    ) : null}
                </>
            ) : null}

            {/* The host stays mounted across collapse/expand so the xterm
                instance and its socket survive; only its visibility toggles. */}
            <div className="terminal-drawer-body" style={{ display: expanded ? "flex" : "none" }}>
                <div ref={containerRef} className="terminal-host" data-testid="terminal-host" />
            </div>
        </div>
    );
}
