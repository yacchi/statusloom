// History panel: lists a tool's revisions (internal store's revision DAG,
// DSL_API.md "History API" / plans/config-store-and-format.md §4), shows a
// line diff of a selected revision against the current document, and lets
// the user restore an older revision (with a confirmation step — restore
// discards the tool's draft working node, §4.2).
//
// The diff base is always the *server's* current revision — fetched fresh
// by this component itself (never taken from a caller-supplied prop or the
// editor's own in-memory content) both when the panel opens and whenever its
// list is refreshed (e.g. after a restore). This matches `statusloom history
// diff`'s semantics and keeps the diff correct even if an external process
// changed the store after the editor last loaded (§4.5).
//
// Follows the same modal chrome as SettingsModal/ImportModal. This component
// owns its own data fetching (getHistory/getHistoryRevision/restoreRevision)
// and refreshes its list after a successful restore; the caller (App.tsx) is
// only responsible for reloading the editor's own state via onRestored.

import { useCallback, useEffect, useMemo, useState } from "react";
import type { Api } from "../api.ts";
import { diffLines } from "../diff.ts";
import { t, useLang } from "../i18n.ts";
import type { HistoryRefs, HistoryRevisionDetail, HistoryRevisionEntry } from "../types.ts";
import { Modal } from "./Modal.tsx";

interface Props {
    api: Api;
    tool: string;
    // Unsaved *local* editor edits (App's `dirty`: present !== savedSource)
    // that a restore-triggered reload would also discard, on top of whatever
    // server-side draft working node getHistory's refs.draft reports.
    localDirty: boolean;
    onClose: () => void;
    // Called once a restore has actually happened server-side (before the
    // panel refreshes its own list). The caller reloads the editor's state
    // for `tool` from the server.
    onRestored: (id: string) => void;
    // Called immediately before the restore API call, so the caller can
    // cancel its own pending draft autosave (debounce timer) and await any
    // write already in flight — otherwise that stale write could land after
    // this restore discards the server-side draft, resurrecting it.
    beforeRestore: () => Promise<void>;
}

// RFC3339 (always UTC, "Z"-suffixed) -> a short string in the *browser's*
// local timezone, since that's what a user actually reads at a glance. The
// exact instant is never lost: callers also set the row's `title` to the raw
// RFC3339/UTC string (formatSavedAtTitle) so it's one hover away.
function formatSavedAt(savedAt: string): string {
    const d = new Date(savedAt);
    if (Number.isNaN(d.getTime())) {
        return savedAt;
    }
    return d.toLocaleString(undefined, {
        year: "numeric",
        month: "2-digit",
        day: "2-digit",
        hour: "2-digit",
        minute: "2-digit",
        hour12: false,
    });
}

// The exact RFC3339/UTC instant, for the `title` attribute — a tooltip
// fallback for anyone comparing revisions across timezones.
function formatSavedAtTitle(savedAt: string): string {
    return savedAt;
}

export function HistoryPanel({
    api,
    tool,
    localDirty,
    onClose,
    onRestored,
    beforeRestore,
}: Props) {
    const lang = useLang();

    const [revisions, setRevisions] = useState<HistoryRevisionEntry[] | null>(null);
    const [refs, setRefs] = useState<HistoryRefs | null>(null);
    const [loadError, setLoadError] = useState<string | null>(null);

    const [selectedId, setSelectedId] = useState<string | null>(null);
    const [detailCache, setDetailCache] = useState<Record<string, HistoryRevisionDetail>>({});
    const [detailLoading, setDetailLoading] = useState(false);
    const [detailError, setDetailError] = useState<string | null>(null);

    const [confirmingId, setConfirmingId] = useState<string | null>(null);
    const [restoring, setRestoring] = useState(false);
    const [restoreError, setRestoreError] = useState<string | null>(null);

    // The diff base — always the *server's* current revision, never the
    // caller's in-memory/prop content. Fetched fresh right here on every
    // reload (the initial one when the panel opens, and again after a
    // restore refreshes the list — plans/config-store-and-format.md §4.5),
    // so an external process (CLI, another tab) moving `current` without
    // this UI having reloaded can never leave the diff comparing against
    // stale content.
    const [currentDetail, setCurrentDetail] = useState<HistoryRevisionDetail | null>(null);
    const [currentDetailLoading, setCurrentDetailLoading] = useState(false);
    const [currentDetailError, setCurrentDetailError] = useState<string | null>(null);

    const reload = useCallback(async () => {
        try {
            const res = await api.getHistory(tool);
            setRevisions(res.revisions);
            setRefs(res.refs);
            setLoadError(null);

            if (res.refs.current) {
                setCurrentDetailLoading(true);
                setCurrentDetailError(null);
                try {
                    const detail = await api.getHistoryRevision(res.refs.current);
                    setCurrentDetail(detail);
                    // Also seed the selection-detail cache with it, so
                    // selecting the current revision itself (to see "no
                    // differences") never re-fetches what we just fetched.
                    setDetailCache((c) => ({ ...c, [detail.id]: detail }));
                } catch (err) {
                    setCurrentDetail(null);
                    setCurrentDetailError((err as Error).message);
                } finally {
                    setCurrentDetailLoading(false);
                }
            } else {
                setCurrentDetail(null);
            }
        } catch (err) {
            setLoadError((err as Error).message);
        }
    }, [api, tool]);

    useEffect(() => {
        reload();
    }, [reload]);

    // Newest-first (the API lists oldest-first): savedAt descending, id
    // descending as a tiebreak (ids are UUIDv7, so lexicographic order
    // tracks creation time). A flat list — branches (two revisions sharing
    // a parent, from a past restore) just appear as separate rows; nothing
    // here assumes a single linear chain.
    const sorted = useMemo(() => {
        if (!revisions) {
            return [];
        }
        return [...revisions].sort((a, b) => {
            if (a.savedAt !== b.savedAt) {
                return a.savedAt < b.savedAt ? 1 : -1;
            }
            return a.id < b.id ? 1 : -1;
        });
    }, [revisions]);

    async function selectRevision(id: string) {
        setSelectedId(id);
        setConfirmingId(null);
        setRestoreError(null);
        if (detailCache[id]) {
            return;
        }
        setDetailLoading(true);
        setDetailError(null);
        try {
            const detail = await api.getHistoryRevision(id);
            setDetailCache((c) => ({ ...c, [id]: detail }));
        } catch (err) {
            setDetailError((err as Error).message);
        } finally {
            setDetailLoading(false);
        }
    }

    async function doRestore(id: string) {
        setRestoring(true);
        setRestoreError(null);
        try {
            // Settle any pending/in-flight draft autosave first so it cannot
            // land after this restore clears the server-side draft (see
            // beforeRestore's doc comment in App.tsx).
            await beforeRestore();
            await api.restoreRevision(id);
            setConfirmingId(null);
            onRestored(id);
            await reload();
        } catch (err) {
            setRestoreError((err as Error).message);
        } finally {
            setRestoring(false);
        }
    }

    const selectedDetail = selectedId ? (detailCache[selectedId] ?? null) : null;
    const diff = useMemo(
        () =>
            selectedDetail && currentDetail
                ? diffLines(currentDetail.source, selectedDetail.source)
                : null,
        [selectedDetail, currentDetail],
    );
    // A restore discards the tool's draft working node server-side
    // (refs.draft) and reloads the editor here, which also drops any local
    // edit not yet reflected in `savedSource` — warn when either is true.
    const hasUnsavedChanges = localDirty || (refs?.draft ?? false);

    return (
        <Modal title={t(lang, "historyTitle")} onClose={onClose} className="history-modal">
            <p className="hint">{t(lang, "historyHint")}</p>

            {loadError ? <div className="inline-error">{loadError}</div> : null}
            {revisions === null && !loadError ? (
                <div className="hint">{t(lang, "rendering")}</div>
            ) : null}
            {revisions !== null && sorted.length === 0 ? (
                <div className="hint" data-testid="history-empty">
                    {t(lang, "historyEmpty")}
                </div>
            ) : null}

            {sorted.length > 0 ? (
                <div className="history-body">
                    <ul className="history-list" data-testid="history-list">
                        {sorted.map((rev) => {
                            const isCurrent = refs?.current === rev.id;
                            return (
                                <li key={rev.id}>
                                    <button
                                        type="button"
                                        className={
                                            selectedId === rev.id
                                                ? "history-row on"
                                                : "history-row"
                                        }
                                        data-testid={`history-row-${rev.id}`}
                                        onClick={() => selectRevision(rev.id)}
                                    >
                                        <span className="history-row-meta">
                                            <span
                                                className="history-savedat"
                                                title={formatSavedAtTitle(rev.savedAt)}
                                            >
                                                {formatSavedAt(rev.savedAt)}
                                            </span>
                                            <span className="history-origin">{rev.origin}</span>
                                            <span className="history-name" title={rev.meta.name}>
                                                {rev.meta.name ?? ""}
                                            </span>
                                        </span>
                                        {isCurrent ? (
                                            <span
                                                className="history-current-badge"
                                                data-testid="history-current-badge"
                                            >
                                                {t(lang, "historyCurrentBadge")}
                                            </span>
                                        ) : null}
                                    </button>
                                </li>
                            );
                        })}
                    </ul>
                    {/* A single fixed-size box (history-diff) hosts every
                        state — placeholder, loading, error, and the diff
                        itself — so selecting a revision never resizes the
                        modal; content beyond its bounds scrolls internally
                        instead. The restore/confirm actions sit below it,
                        outside the scroll area, so they stay reachable
                        without scrolling. */}
                    <div className="history-detail">
                        <div className="history-diff" data-testid="history-diff">
                            {selectedId === null ? (
                                <div className="hint">{t(lang, "historyNoDiffSelected")}</div>
                            ) : detailLoading || currentDetailLoading ? (
                                <div className="hint">{t(lang, "rendering")}</div>
                            ) : detailError ? (
                                <div className="inline-error">{detailError}</div>
                            ) : currentDetailError ? (
                                <div className="inline-error">{currentDetailError}</div>
                            ) : selectedDetail && currentDetail ? (
                                diff && diff.some((d) => d.op !== "equal") ? (
                                    diff.map((d, i) => (
                                        <div key={i} className={`diff-line diff-${d.op}`}>
                                            <span className="diff-marker">
                                                {d.op === "add"
                                                    ? "+"
                                                    : d.op === "remove"
                                                      ? "-"
                                                      : " "}
                                            </span>
                                            <span className="diff-text">{d.text}</span>
                                        </div>
                                    ))
                                ) : (
                                    <div className="hint">{t(lang, "historyDiffEmpty")}</div>
                                )
                            ) : null}
                        </div>
                        {selectedId !== null && selectedDetail ? (
                            confirmingId === selectedId ? (
                                <div className="history-confirm" data-testid="history-confirm">
                                    <p>{t(lang, "historyRestoreConfirm")}</p>
                                    {hasUnsavedChanges ? (
                                        <p className="inline-error">
                                            {t(lang, "historyRestoreConfirmDraftWarning")}
                                        </p>
                                    ) : null}
                                    {restoreError ? (
                                        <div className="inline-error">{restoreError}</div>
                                    ) : null}
                                    <div className="modal-actions">
                                        <button
                                            onClick={() => setConfirmingId(null)}
                                            disabled={restoring}
                                        >
                                            Cancel
                                        </button>
                                        <button
                                            className="primary"
                                            data-testid="history-confirm-restore"
                                            disabled={restoring}
                                            onClick={() => doRestore(selectedId)}
                                        >
                                            {restoring ? "Restoring…" : "Restore"}
                                        </button>
                                    </div>
                                </div>
                            ) : (
                                <div className="modal-actions">
                                    <button
                                        data-testid={`history-restore-${selectedId}`}
                                        disabled={refs?.current === selectedId}
                                        onClick={() => setConfirmingId(selectedId)}
                                    >
                                        Restore
                                    </button>
                                </div>
                            )
                        ) : null}
                    </div>
                </div>
            ) : null}
        </Modal>
    );
}
