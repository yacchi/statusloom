// "Live monitor" sidebar panel. Two ways to feed the preview with a real
// session:
//   1. "Run it yourself" — provisions a monitored directory (POST
//      /api/live/session) and shows the shell command to run in another
//      terminal. The shared useLiveUpdates hook (GET /ws/live) then locks
//      onto the first session and switches the preview source to it.
//   2. "Embedded terminal" — a small trigger that opens the bottom terminal
//      drawer (TerminalDrawer) and spawns/controls a real Claude Code process
//      in the browser. The trigger lives here; the terminal body lives in the
//      drawer at the bottom of the window.
//
// Neither path calls api.preview() itself — the sessionId-based preview
// dispatch in App.tsx does the rendering.

import { useCallback, useEffect, useRef, useState } from "react";
import type { Api } from "../api.ts";
import { t, useLang } from "../i18n.ts";
import type { LiveSessionInfo, PreviewSource } from "../types.ts";
import { useLiveUpdates } from "../useLiveUpdates.ts";
import type { TerminalStatus } from "./TerminalDrawer.tsx";

function sourceEquals(a: PreviewSource, b: PreviewSource): boolean {
    if (a.kind === "session" && b.kind === "session") {
        return a.id === b.id;
    }
    if (a.kind === "sample" && b.kind === "sample") {
        return a.sample === b.sample;
    }
    return false;
}

export interface LiveMonitorProps {
    api: Api;
    token: string;
    previewSource: PreviewSource;
    onPreviewSourceChange: (source: PreviewSource) => void;
    // Opens the bottom terminal drawer and (re)starts/shows an embedded session.
    onOpenEmbedded: () => void;
    embeddedOpen: boolean;
    // Hides the (still-running) embedded drawer. Optional so the footer bar can
    // render without an active drawer.
    onHideEmbedded?: () => void;
    // The embedded drawer's status, lifted here so the footer stays a single bar
    // (no second bar above it). Null/undefined until a session is mounted.
    termStatus?: TerminalStatus | null;
}

export function LiveMonitor({
    api,
    token,
    previewSource,
    onPreviewSourceChange,
    onOpenEmbedded,
    embeddedOpen,
    onHideEmbedded,
    termStatus,
}: LiveMonitorProps) {
    const lang = useLang();

    return (
        <div className="live-monitor-bar">
            <span className="live-monitor-label">{t(lang, "liveMonitorTitle")}</span>

            <div className="live-monitor-section">
                <span className="live-section-title">{t(lang, "liveModeSelf")}</span>
                <SelfRunMonitor
                    api={api}
                    token={token}
                    previewSource={previewSource}
                    onPreviewSourceChange={onPreviewSourceChange}
                />
            </div>

            <div className="live-monitor-section live-monitor-embedded">
                <span className="live-section-title">{t(lang, "liveModeEmbedded")}</span>
                <EmbeddedControls
                    termStatus={termStatus ?? null}
                    embeddedOpen={embeddedOpen}
                    onOpenEmbedded={onOpenEmbedded}
                    onHideEmbedded={onHideEmbedded}
                />
            </div>
        </div>
    );
}

// The embedded terminal's status + actions, mirrored into the footer bar so the
// collapsed drawer needs no visible bar of its own. "Open/Show/Restart" all go
// through onOpenEmbedded (App bumps a nonce + opens, so it doubles as restart).
function EmbeddedControls({
    termStatus,
    embeddedOpen,
    onOpenEmbedded,
    onHideEmbedded,
}: {
    termStatus: TerminalStatus | null;
    embeddedOpen: boolean;
    onOpenEmbedded: () => void;
    onHideEmbedded?: () => void;
}) {
    const lang = useLang();
    const phase = termStatus?.phase;
    const connected = termStatus?.connected ?? false;

    // Not started yet.
    if (!phase || phase === "idle") {
        return (
            <button
                type="button"
                onClick={onOpenEmbedded}
                title={t(lang, "embeddedTerminalWarning")}
            >
                {t(lang, "embeddedTerminalStart")}
            </button>
        );
    }

    // Session ended (or failed): offer a restart.
    if (phase === "closed" || phase === "failed") {
        return (
            <>
                <span className="live-monitor-status">
                    <span className="live-status-dot" />
                    {t(lang, "embeddedTerminalClosed")}
                </span>
                <button type="button" onClick={onOpenEmbedded}>
                    {t(lang, "embeddedTerminalRestart")}
                </button>
            </>
        );
    }

    // starting | running.
    return (
        <>
            <span className="live-monitor-status">
                <span className={`live-status-dot ${connected ? "connected" : "connecting"}`} />
                {connected
                    ? t(lang, "embeddedTerminalConnected")
                    : t(lang, "embeddedTerminalConnecting")}
            </span>
            {embeddedOpen ? (
                <button type="button" onClick={() => onHideEmbedded?.()}>
                    {t(lang, "embeddedTerminalHide")}
                </button>
            ) : (
                <button type="button" onClick={onOpenEmbedded}>
                    {t(lang, "embeddedTerminalShow")}
                </button>
            )}
        </>
    );
}

interface SelfRunProps {
    api: Api;
    token: string;
    previewSource: PreviewSource;
    onPreviewSourceChange: (source: PreviewSource) => void;
}

// The original L2 flow: show a launch command and subscribe to live updates.
function SelfRunMonitor({ api, token, previewSource, onPreviewSourceChange }: SelfRunProps) {
    const lang = useLang();
    const [phase, setPhase] = useState<"idle" | "starting" | "running" | "failed">("idle");
    const [launchInfo, setLaunchInfo] = useState<LiveSessionInfo | null>(null);
    const [error, setError] = useState<string | null>(null);
    const [copied, setCopied] = useState(false);

    // The exact PreviewSource value we last pushed ourselves, so the manual
    // selection check can tell our own update apart from the user's.
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

    const stop = useCallback(() => {
        live.stop();
        expectedSourceRef.current = null;
        setLaunchInfo(null);
        setPhase("idle");
    }, [live]);

    const start = useCallback(async () => {
        setPhase("starting");
        setError(null);
        try {
            const info = await api.startLiveSession();
            setLaunchInfo(info);
            expectedSourceRef.current = null;
            live.start();
            setPhase("running");
        } catch (err) {
            setPhase("failed");
            setError((err as Error).message);
        }
    }, [api, live]);

    // Disengage when the user manually picks a different preview source via
    // the Canvas selector — but not when the change is the one we just pushed.
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
        stop();
    }, [previewSource, phase, stop]);

    const copyCommand = useCallback(async () => {
        if (!launchInfo) {
            return;
        }
        try {
            await navigator.clipboard.writeText(launchInfo.launchCommand);
            setCopied(true);
            window.setTimeout(() => setCopied(false), 1500);
        } catch {
            // Clipboard may be denied; the command is still shown/selectable.
        }
    }, [launchInfo]);

    if (phase === "idle" || phase === "starting") {
        return (
            <div className="live-monitor-self">
                <button
                    onClick={start}
                    disabled={phase === "starting"}
                    title={t(lang, "liveMonitorIntro")}
                >
                    {phase === "starting"
                        ? t(lang, "liveMonitorStarting")
                        : t(lang, "liveMonitorStart")}
                </button>
            </div>
        );
    }

    if (phase === "failed") {
        return (
            <div className="live-monitor-self">
                <span className="inline-error">{error}</span>
                <button onClick={start}>{t(lang, "liveMonitorRetry")}</button>
            </div>
        );
    }

    return (
        <div className="live-monitor-self live-monitor-active">
            <div className="live-monitor-cmd">
                <code data-testid="live-launch-command" title={t(lang, "liveMonitorRunHint")}>
                    {launchInfo?.launchCommand}
                </code>
                <button type="button" onClick={copyCommand}>
                    {copied ? t(lang, "liveMonitorCopied") : t(lang, "liveMonitorCopy")}
                </button>
            </div>
            <div className="live-monitor-status" data-testid="live-monitor-status">
                <span className={`live-status-dot ${live.status}`} />
                {live.status === "active"
                    ? t(lang, "liveMonitorStatusActive")
                    : live.status === "connected"
                      ? t(lang, "liveMonitorStatusConnected")
                      : t(lang, "liveMonitorStatusWaiting")}
            </div>
            <button type="button" onClick={stop}>
                {t(lang, "liveMonitorStop")}
            </button>
        </div>
    );
}
