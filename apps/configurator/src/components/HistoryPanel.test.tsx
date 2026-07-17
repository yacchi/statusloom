import { afterEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { HistoryPanel } from "./HistoryPanel.tsx";
import type { Api } from "../api.ts";
import type {
    HistoryListResponse,
    HistoryRevisionDetail,
    HistoryRevisionEntry,
} from "../types.ts";

const REV_OLD: HistoryRevisionEntry = {
    id: "01958f00-0000-7000-8000-000000000001",
    parent: null,
    savedAt: "2026-07-15T09:00:00Z",
    origin: "ui",
    meta: {},
};

const REV_NEW: HistoryRevisionEntry = {
    id: "01958f29-0000-7000-8000-000000000002",
    parent: REV_OLD.id,
    savedAt: "2026-07-17T09:00:00Z",
    origin: "cli",
    meta: { name: "before refactor" },
};

function historyResponse(
    revisions: HistoryRevisionEntry[],
    current: string,
    draft = false,
): HistoryListResponse {
    return { revisions, refs: { current, draft } };
}

function detailOf(entry: HistoryRevisionEntry, source: string): HistoryRevisionDetail {
    return { ...entry, tool: "claude-code", source };
}

function makeApi(overrides: Partial<Api> = {}): Api {
    return {
        getTools: vi.fn(async () => [{ id: "claude-code", displayName: "Claude Code" }]),
        getDocument: vi.fn(async () => ({ source: "", version: "v0", exists: false })),
        putDocument: vi.fn(async () => ({ saved: true, version: "v0", diagnostics: [] })),
        parse: vi.fn(async () => ({ diagnostics: [], version: "v0" })),
        serialize: vi.fn(async () => ({ source: "", diagnostics: [] })),
        getDraft: vi.fn(async () => ({ source: "", version: "v0", exists: false })),
        putDraft: vi.fn(async () => ({ saved: true, version: "v0", diagnostics: [] })),
        preview: vi.fn(async () => ({ lines: [], diagnostics: [] })),
        getFields: vi.fn(async () => []),
        getMetrics: vi.fn(async () => []),
        probeUsage: vi.fn(async () => ({ available: false, reason: "no-token" })),
        getHistory: vi.fn(async () => historyResponse([REV_OLD, REV_NEW], REV_NEW.id)),
        getHistoryRevision: vi.fn(async (id: string) => {
            const entry = [REV_OLD, REV_NEW].find((r) => r.id === id);
            if (!entry) {
                throw new Error("unknown revision");
            }
            return detailOf(entry, entry === REV_OLD ? "line-a\nline-b" : "line-a\nline-b\nline-c");
        }),
        restoreRevision: vi.fn(async (id: string) => ({ ok: true, tool: "claude-code", current: id })),
        importExchange: vi.fn(async () => ({ saved: true, tool: "claude-code", revision: "", diagnostics: [] })),
        getExportMarkdown: vi.fn(async () => ""),
        getSessions: vi.fn(async () => []),
        shutdown: vi.fn(async () => {}),
        startLiveSession: vi.fn(async () => ({ launchCommand: "claude", tmpDir: "/tmp/x" })),
        startTerminalSession: vi.fn(async () => ({ terminalId: "deadbeef" })),
        ...overrides,
    };
}

afterEach(() => {
    vi.restoreAllMocks();
});

function renderPanel(overrides: {
    api?: Api;
    localDirty?: boolean;
    onClose?: () => void;
    onRestored?: (id: string) => void;
    beforeRestore?: () => Promise<void>;
} = {}) {
    const api = overrides.api ?? makeApi();
    const onClose = overrides.onClose ?? vi.fn();
    const onRestored = overrides.onRestored ?? vi.fn();
    const beforeRestore = overrides.beforeRestore ?? vi.fn(async () => {});
    const view = render(
        <HistoryPanel
            api={api}
            tool="claude-code"
            localDirty={overrides.localDirty ?? false}
            onClose={onClose}
            onRestored={onRestored}
            beforeRestore={beforeRestore}
        />,
    );
    return { api, onClose, onRestored, beforeRestore, ...view };
}

describe("HistoryPanel listing", () => {
    it("lists revisions newest-first and badges the current one", async () => {
        renderPanel();

        await waitFor(() => expect(screen.getByTestId("history-list")).toBeTruthy());

        const rows = screen.getAllByTestId(/^history-row-/);
        expect(rows.map((r) => r.getAttribute("data-testid"))).toEqual([
            `history-row-${REV_NEW.id}`,
            `history-row-${REV_OLD.id}`,
        ]);

        // Only the current (newest) revision shows the badge.
        expect(
            screen.getByTestId(`history-row-${REV_NEW.id}`).querySelector(
                '[data-testid="history-current-badge"]',
            ),
        ).toBeTruthy();
        expect(
            screen.getByTestId(`history-row-${REV_OLD.id}`).querySelector(
                '[data-testid="history-current-badge"]',
            ),
        ).toBeNull();
    });

    it("shows an empty-state message when there are no revisions", async () => {
        renderPanel({ api: makeApi({ getHistory: vi.fn(async () => historyResponse([], "")) }) });
        await waitFor(() => expect(screen.getByTestId("history-empty")).toBeTruthy());
    });

    it("surfaces a load error", async () => {
        renderPanel({
            api: makeApi({
                getHistory: vi.fn(async () => {
                    throw new Error("network down");
                }),
            }),
        });
        await waitFor(() => expect(screen.getByText("network down")).toBeTruthy());
    });
});

describe("HistoryPanel diff", () => {
    it("fetches the server's current revision as the diff base as soon as the panel opens", async () => {
        const { api } = renderPanel();
        // REV_NEW is `refs.current` in the default fixture — its source must
        // be fetched proactively (nothing has been clicked yet), since it's
        // the diff base for whatever gets selected, not something taken from
        // a prop or the editor's own in-memory content.
        await waitFor(() => expect(api.getHistoryRevision).toHaveBeenCalledWith(REV_NEW.id));
    });

    it("fetches and renders the diff against the current source on selection", async () => {
        const { api } = renderPanel();
        await waitFor(() => expect(screen.getByTestId("history-list")).toBeTruthy());
        await waitFor(() => expect(api.getHistoryRevision).toHaveBeenCalledWith(REV_NEW.id));

        fireEvent.click(screen.getByTestId(`history-row-${REV_OLD.id}`));

        await waitFor(() => expect(api.getHistoryRevision).toHaveBeenCalledWith(REV_OLD.id));
        await waitFor(() => expect(screen.getByTestId("history-diff")).toBeTruthy());
        // REV_OLD's source is "line-a\nline-b" — missing "line-c" relative to
        // REV_NEW's (the current revision's) source, so it must show up as
        // a removal.
        expect(screen.getByText("line-c")).toBeTruthy();

        // Re-selecting the same revision must not re-fetch its detail. Two
        // total calls: the proactive current-revision fetch (REV_NEW) plus
        // the one selection fetch (REV_OLD) above.
        fireEvent.click(screen.getByTestId(`history-row-${REV_OLD.id}`));
        expect(api.getHistoryRevision).toHaveBeenCalledTimes(2);
    });

    it("reports no differences when the selected revision matches the current source", async () => {
        renderPanel();
        await waitFor(() => expect(screen.getByTestId("history-list")).toBeTruthy());

        fireEvent.click(screen.getByTestId(`history-row-${REV_NEW.id}`));
        await waitFor(() => expect(screen.getByTestId("history-diff")).toBeTruthy());
        expect(screen.getByText(/No differences\./)).toBeTruthy();
    });

    it("compares against a freshly re-fetched current revision after a list refresh, never a stale one", async () => {
        // Simulates an external process (CLI, another tab) changing the
        // store: `refs.current` starts at REV_OLD, then flips to REV_NEW
        // once the restore below causes this panel to reload its list — the
        // moment plans/config-store-and-format.md §4.5 requires a fresh
        // server fetch of the (new) current revision's source.
        let currentId: string = REV_OLD.id;
        const api = makeApi({
            getHistory: vi.fn(async () => historyResponse([REV_OLD, REV_NEW], currentId)),
            restoreRevision: vi.fn(async (id: string) => {
                currentId = id;
                return { ok: true, tool: "claude-code", current: id };
            }),
        });
        renderPanel({ api });
        await waitFor(() => expect(screen.getByTestId("history-list")).toBeTruthy());
        await waitFor(() => expect(api.getHistoryRevision).toHaveBeenCalledWith(REV_OLD.id));

        // Base is REV_OLD ("line-a\nline-b") right now: REV_NEW has an extra
        // line relative to it, so selecting REV_NEW must show a real diff —
        // not "No differences", which a stale/cached base could wrongly
        // produce if REV_NEW's own source happened to match some old value.
        fireEvent.click(screen.getByTestId(`history-row-${REV_NEW.id}`));
        await waitFor(() => expect(screen.getByTestId("history-diff")).toBeTruthy());
        expect(screen.getByText("line-c")).toBeTruthy();

        // Restore REV_NEW: the server's `current` flips to it, and the
        // panel's post-restore reload must re-fetch REV_NEW's source as the
        // *new* base — not keep comparing against the REV_OLD base fetched
        // at open time.
        fireEvent.click(screen.getByTestId(`history-restore-${REV_NEW.id}`));
        await waitFor(() => expect(screen.getByTestId("history-confirm-restore")).toBeTruthy());
        fireEvent.click(screen.getByTestId("history-confirm-restore"));
        await waitFor(() => expect(api.restoreRevision).toHaveBeenCalledWith(REV_NEW.id));

        // REV_NEW is still selected and is now `current` too, so comparing
        // it against the re-fetched (new) base must report no differences.
        await waitFor(() => expect(screen.getByText(/No differences\./)).toBeTruthy());
    });
});

describe("HistoryPanel restore", () => {
    it("requires confirmation, then restores and reloads the list", async () => {
        const { api, onRestored } = renderPanel();
        await waitFor(() => expect(screen.getByTestId("history-list")).toBeTruthy());

        fireEvent.click(screen.getByTestId(`history-row-${REV_OLD.id}`));
        await waitFor(() => expect(screen.getByTestId(`history-restore-${REV_OLD.id}`)).toBeTruthy());

        // Restoring must not fire before the user confirms.
        fireEvent.click(screen.getByTestId(`history-restore-${REV_OLD.id}`));
        expect(api.restoreRevision).not.toHaveBeenCalled();
        await waitFor(() => expect(screen.getByTestId("history-confirm")).toBeTruthy());

        fireEvent.click(screen.getByTestId("history-confirm-restore"));

        await waitFor(() => expect(api.restoreRevision).toHaveBeenCalledWith(REV_OLD.id));
        await waitFor(() => expect(onRestored).toHaveBeenCalledWith(REV_OLD.id));
        // The list is refreshed after a successful restore.
        await waitFor(() => expect(api.getHistory).toHaveBeenCalledTimes(2));
    });

    it("cancel dismisses the confirmation without restoring", async () => {
        const { api } = renderPanel();
        await waitFor(() => expect(screen.getByTestId("history-list")).toBeTruthy());

        fireEvent.click(screen.getByTestId(`history-row-${REV_OLD.id}`));
        await waitFor(() => expect(screen.getByTestId(`history-restore-${REV_OLD.id}`)).toBeTruthy());
        fireEvent.click(screen.getByTestId(`history-restore-${REV_OLD.id}`));
        await waitFor(() => expect(screen.getByTestId("history-confirm")).toBeTruthy());

        fireEvent.click(screen.getByText("Cancel"));
        await waitFor(() => expect(screen.queryByTestId("history-confirm")).toBeNull());
        expect(api.restoreRevision).not.toHaveBeenCalled();
    });

    it("disables restoring the already-current revision", async () => {
        renderPanel();
        await waitFor(() => expect(screen.getByTestId("history-list")).toBeTruthy());

        fireEvent.click(screen.getByTestId(`history-row-${REV_NEW.id}`));
        await waitFor(() =>
            expect(screen.getByTestId(`history-restore-${REV_NEW.id}`)).toBeTruthy(),
        );
        expect(
            (screen.getByTestId(`history-restore-${REV_NEW.id}`) as HTMLButtonElement).disabled,
        ).toBe(true);
    });

    it("warns about discarding a server-side draft when one exists", async () => {
        renderPanel({
            api: makeApi({ getHistory: vi.fn(async () => historyResponse([REV_OLD, REV_NEW], REV_NEW.id, true)) }),
        });
        await waitFor(() => expect(screen.getByTestId("history-list")).toBeTruthy());

        fireEvent.click(screen.getByTestId(`history-row-${REV_OLD.id}`));
        await waitFor(() => expect(screen.getByTestId(`history-restore-${REV_OLD.id}`)).toBeTruthy());
        fireEvent.click(screen.getByTestId(`history-restore-${REV_OLD.id}`));

        await waitFor(() =>
            expect(screen.getByText(/discard your unsaved draft edits/)).toBeTruthy(),
        );
    });

    it("warns about discarding local edits when localDirty is set, even without a server draft", async () => {
        renderPanel({ localDirty: true });
        await waitFor(() => expect(screen.getByTestId("history-list")).toBeTruthy());

        fireEvent.click(screen.getByTestId(`history-row-${REV_OLD.id}`));
        await waitFor(() => expect(screen.getByTestId(`history-restore-${REV_OLD.id}`)).toBeTruthy());
        fireEvent.click(screen.getByTestId(`history-restore-${REV_OLD.id}`));

        await waitFor(() =>
            expect(screen.getByText(/discard your unsaved draft edits/)).toBeTruthy(),
        );
    });

    it("does not warn about discarding drafts when there is nothing unsaved", async () => {
        renderPanel();
        await waitFor(() => expect(screen.getByTestId("history-list")).toBeTruthy());

        fireEvent.click(screen.getByTestId(`history-row-${REV_OLD.id}`));
        await waitFor(() => expect(screen.getByTestId(`history-restore-${REV_OLD.id}`)).toBeTruthy());
        fireEvent.click(screen.getByTestId(`history-restore-${REV_OLD.id}`));

        await waitFor(() => expect(screen.getByTestId("history-confirm")).toBeTruthy());
        expect(screen.queryByText(/discard your unsaved draft edits/)).toBeNull();
    });

    it("awaits beforeRestore (settling any pending draft autosave) before calling restoreRevision", async () => {
        const order: string[] = [];
        const pending: { resolve: (() => void) | null } = { resolve: null };
        const beforeRestore = vi.fn(
            () =>
                new Promise<void>((resolve) => {
                    pending.resolve = () => {
                        order.push("beforeRestore-settled");
                        resolve();
                    };
                }),
        );
        const api = makeApi({
            restoreRevision: vi.fn(async (id: string) => {
                order.push("restoreRevision");
                return { ok: true, tool: "claude-code", current: id };
            }),
        });
        renderPanel({ api, beforeRestore });
        await waitFor(() => expect(screen.getByTestId("history-list")).toBeTruthy());

        fireEvent.click(screen.getByTestId(`history-row-${REV_OLD.id}`));
        await waitFor(() => expect(screen.getByTestId(`history-restore-${REV_OLD.id}`)).toBeTruthy());
        fireEvent.click(screen.getByTestId(`history-restore-${REV_OLD.id}`));
        await waitFor(() => expect(screen.getByTestId("history-confirm")).toBeTruthy());

        fireEvent.click(screen.getByTestId("history-confirm-restore"));
        await waitFor(() => expect(beforeRestore).toHaveBeenCalled());

        // restoreRevision must not fire while beforeRestore's promise (a
        // stand-in for "cancel the pending debounce timer, then await any
        // in-flight draft PUT") is still pending — that ordering is exactly
        // what prevents a stale draft write from landing after the restore.
        expect(api.restoreRevision).not.toHaveBeenCalled();

        pending.resolve?.();
        await waitFor(() => expect(api.restoreRevision).toHaveBeenCalledWith(REV_OLD.id));
        expect(order).toEqual(["beforeRestore-settled", "restoreRevision"]);
    });

    it("surfaces a restore error without closing the confirmation", async () => {
        const api = makeApi({
            restoreRevision: vi.fn(async () => {
                throw new Error("restore failed");
            }),
        });
        renderPanel({ api });
        await waitFor(() => expect(screen.getByTestId("history-list")).toBeTruthy());

        fireEvent.click(screen.getByTestId(`history-row-${REV_OLD.id}`));
        await waitFor(() => expect(screen.getByTestId(`history-restore-${REV_OLD.id}`)).toBeTruthy());
        fireEvent.click(screen.getByTestId(`history-restore-${REV_OLD.id}`));
        await waitFor(() => expect(screen.getByTestId("history-confirm")).toBeTruthy());

        fireEvent.click(screen.getByTestId("history-confirm-restore"));
        await waitFor(() => expect(screen.getByText("restore failed")).toBeTruthy());
        expect(screen.getByTestId("history-confirm")).toBeTruthy();
    });
});

describe("HistoryPanel chrome", () => {
    it("closes via the × button", async () => {
        const { onClose } = renderPanel();
        await waitFor(() => expect(screen.getByTestId("history-list")).toBeTruthy());
        fireEvent.click(screen.getByTestId("modal-close"));
        expect(onClose).toHaveBeenCalled();
    });

    it("closes on Escape", async () => {
        const { onClose } = renderPanel();
        await waitFor(() => expect(screen.getByTestId("history-list")).toBeTruthy());
        fireEvent.keyDown(window, { key: "Escape" });
        expect(onClose).toHaveBeenCalled();
    });
});
