// Shared subscription to the live-update WebSocket (GET /ws/live).
//
// Both the "run it yourself" Live monitor (LiveMonitor.tsx) and the embedded
// terminal drawer (TerminalDrawer.tsx) need the same thing: open /ws/live, and on
// each {"type":"live-update","sessionId",...} frame lock onto the first
// session seen and report subsequent renders for that same session. This hook
// owns the socket lifecycle, the reconnect/backoff policy, and the
// lock-on-first-session semantics so the two callers stay in sync.
//
// Robustness: a socket that never opens, or drops mid-session, must never
// wedge the UI. A handful of reconnect attempts with exponential backoff are
// made on unexpected close; once exhausted it settles back to "connecting"
// (i.e. waiting) without surfacing an error.

import { useCallback, useEffect, useRef, useState } from "react";
import { liveSocketUrl } from "./api.ts";
import type { LiveUpdate } from "./types.ts";

const MAX_RECONNECT_ATTEMPTS = 5;
const BASE_RECONNECT_DELAY_MS = 500;

export type LiveStatus = "idle" | "connecting" | "connected" | "active";

export interface UseLiveUpdates {
    status: LiveStatus;
    // The session id this subscription has locked onto, or null while waiting
    // for the first live-update.
    sessionId: string | null;
    start: () => void;
    stop: () => void;
}

// onUpdate fires for every live-update belonging to the locked-on session
// (the first session seen after start()). Updates for other sessions are
// ignored as noise. onUpdate must be referentially stable OR is captured via
// a ref internally, so callers may pass an inline function.
export function useLiveUpdates(token: string, onUpdate: (sessionId: string) => void): UseLiveUpdates {
    const [status, setStatus] = useState<LiveStatus>("idle");
    const [sessionId, setSessionId] = useState<string | null>(null);

    const wsRef = useRef<WebSocket | null>(null);
    const reconnectTimerRef = useRef<number | null>(null);
    const attemptsRef = useRef(0);
    const activeRef = useRef(false); // whether the subscription is meant to run
    const sessionIdRef = useRef<string | null>(null);
    const onUpdateRef = useRef(onUpdate);
    onUpdateRef.current = onUpdate;

    const clearReconnectTimer = () => {
        if (reconnectTimerRef.current !== null) {
            window.clearTimeout(reconnectTimerRef.current);
            reconnectTimerRef.current = null;
        }
    };

    const closeSocket = useCallback(() => {
        clearReconnectTimer();
        const ws = wsRef.current;
        if (ws) {
            wsRef.current = null;
            ws.onopen = null;
            ws.onmessage = null;
            ws.onclose = null;
            ws.onerror = null;
            ws.close();
        }
    }, []);

    const connect = useCallback(() => {
        if (!activeRef.current) {
            return;
        }
        setStatus("connecting");
        let ws: WebSocket;
        try {
            ws = new WebSocket(liveSocketUrl(token));
        } catch {
            // Constructor throw is rare (malformed URL); treat as a failed
            // attempt and stop rather than looping.
            activeRef.current = false;
            setStatus("idle");
            return;
        }
        wsRef.current = ws;

        ws.onopen = () => {
            if (!activeRef.current) {
                return;
            }
            attemptsRef.current = 0;
            setStatus(sessionIdRef.current ? "active" : "connected");
        };

        ws.onmessage = (ev) => {
            if (!activeRef.current) {
                return;
            }
            let msg: LiveUpdate;
            try {
                msg = JSON.parse(String(ev.data));
            } catch {
                return;
            }
            if (msg.type !== "live-update" || !msg.sessionId) {
                return;
            }
            // Ignore updates for any session other than the one we've already
            // locked onto (noise from other monitored sessions).
            if (sessionIdRef.current && msg.sessionId !== sessionIdRef.current) {
                return;
            }
            sessionIdRef.current = msg.sessionId;
            setSessionId(msg.sessionId);
            setStatus("active");
            onUpdateRef.current(msg.sessionId);
        };

        ws.onclose = () => {
            if (wsRef.current !== ws) {
                // Superseded or intentionally closed (closeSocket cleared the ref).
                return;
            }
            wsRef.current = null;
            if (!activeRef.current) {
                return;
            }
            if (attemptsRef.current >= MAX_RECONNECT_ATTEMPTS) {
                // Give up quietly: stay in "connecting" (waiting) with no
                // further attempts.
                setStatus("connecting");
                return;
            }
            const delay = BASE_RECONNECT_DELAY_MS * 2 ** attemptsRef.current;
            attemptsRef.current += 1;
            setStatus("connecting");
            reconnectTimerRef.current = window.setTimeout(() => {
                reconnectTimerRef.current = null;
                connect();
            }, delay);
        };

        ws.onerror = () => {
            // onclose always follows; reconnect logic lives there.
        };
    }, [token]);

    const start = useCallback(() => {
        if (activeRef.current) {
            return;
        }
        activeRef.current = true;
        attemptsRef.current = 0;
        sessionIdRef.current = null;
        setSessionId(null);
        connect();
    }, [connect]);

    const stop = useCallback(() => {
        activeRef.current = false;
        closeSocket();
        attemptsRef.current = 0;
        sessionIdRef.current = null;
        setSessionId(null);
        setStatus("idle");
    }, [closeSocket]);

    // Cancel the socket / pending reconnect on unmount.
    useEffect(() => {
        return () => {
            activeRef.current = false;
            closeSocket();
        };
    }, [closeSocket]);

    return { status, sessionId, start, stop };
}
