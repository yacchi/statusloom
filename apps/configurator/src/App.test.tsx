import { afterEach, beforeEach, describe, expect, it, vi, type MockInstance } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { App } from "./App.tsx";
import { defaultTestDoc, installFakeDslServer, type FakeServer } from "./test/fakeDsl.ts";

describe("App", () => {
    it("renders the full-page error when the URL has no token", () => {
        // jsdom's default location has an empty hash, so no token is present.
        render(<App />);
        expect(screen.getByText(/statusloom config/i)).toBeTruthy();
        expect(screen.getByText(/Cannot open configurator/i)).toBeTruthy();
    });
});

// Re-fetching the field catalog after a successful usage-API probe: the
// backend's GET /api/usage/probe, when it succeeds (reason "ok"), persists
// the user's real extra-usage / weekly-usage values to the shared
// account-usage cache (usageprobe.go's persistAccountUsage), and GET
// /api/dsl/fields' preview then overlays those real values
// (dsl.go's overlayRealAccountUsage) instead of the synthetic sample. The
// palette only shows this if the frontend re-fetches the field catalog once
// the probe resolves.
describe("App usage-probe field refresh", () => {
    const TOKEN = "a".repeat(32);
    let server: FakeServer;

    beforeEach(() => {
        window.location.hash = `#token=${TOKEN}`;
        server = installFakeDslServer(defaultTestDoc());
    });

    afterEach(() => {
        vi.unstubAllGlobals();
        window.location.hash = "";
        window.localStorage.clear();
    });

    function fieldsRequestCount(): number {
        return server.fetchMock.mock.calls.filter((call: unknown[]) =>
            String(call[0]).startsWith("/api/dsl/fields"),
        ).length;
    }

    it("re-fetches fields after a successful probe (reason ok)", async () => {
        server.usageProbe = { available: true, reason: "ok", extraUsageEnabled: true };
        render(<App />);

        await waitFor(() => expect(screen.getByTestId("palette-field:model")).toBeTruthy(), {
            timeout: 3000,
        });
        // Initial load fetches fields once; the probe resolving with "ok"
        // should trigger exactly one more fetch.
        await waitFor(() => expect(fieldsRequestCount()).toBe(2), { timeout: 3000 });
    });

    it("does not re-fetch fields when the probe is unavailable", async () => {
        server.usageProbe = { available: false, reason: "no-token" };
        render(<App />);

        await waitFor(() => expect(screen.getByTestId("palette-field:model")).toBeTruthy(), {
            timeout: 3000,
        });
        // Give the (fail-closed) probe effect a moment to settle, then make
        // sure it never asked for the fields a second time.
        await waitFor(() => expect(server.fetchMock).toHaveBeenCalled());
        expect(fieldsRequestCount()).toBe(1);
    });

    it("does not re-fetch fields when the probe is available but rate-limited", async () => {
        // "rate-limited" is available=true but the backend did not fetch (and
        // therefore did not persist) a report; a refetch here would be wasted.
        server.usageProbe = { available: true, reason: "rate-limited" };
        render(<App />);

        await waitFor(() => expect(screen.getByTestId("palette-field:model")).toBeTruthy(), {
            timeout: 3000,
        });
        await waitFor(() => expect(server.fetchMock).toHaveBeenCalled());
        expect(fieldsRequestCount()).toBe(1);
    });
});

// settleDraftBeforeRestore (plans/config-store-and-format.md §4.2): a
// history restore discards the tool's server-side draft working node
// unconditionally, so a debounced draft-autosave write that lands *after*
// the restore would resurrect content the restore just discarded. HistoryPanel
// awaits `beforeRestore` (App's settleDraftBeforeRestore) before calling the
// restore API; HistoryPanel.test.tsx checks that ordering against a bare
// `vi.fn()` stand-in, which cannot exercise settleDraftBeforeRestore's own
// body (clearing the pending setTimeout, awaiting the in-flight PUT). This
// drives the real App end-to-end instead, so a regression in
// settleDraftBeforeRestore itself (e.g. turning it into a no-op) is caught.
describe("App history restore vs draft autosave", () => {
    const TOKEN = "a".repeat(32);
    let server: FakeServer;

    beforeEach(() => {
        window.location.hash = `#token=${TOKEN}`;
        server = installFakeDslServer(defaultTestDoc());
    });

    afterEach(() => {
        vi.unstubAllGlobals();
        window.location.hash = "";
        window.localStorage.clear();
    });

    it("cancels a pending draft-autosave debounce before restoring, so no stale draft PUT follows", async () => {
        // Delay the restore response so the debounce's 250ms deadline falls
        // squarely inside the restore's in-flight window: if
        // settleDraftBeforeRestore failed to cancel the pending timer, the
        // debounced flushDraftNow would fire while the restore is still
        // pending and PUT a draft the restore is about to discard.
        server.restoreDelayMs = 400;
        render(<App />);

        await waitFor(() => expect(screen.getByTestId("palette-field:model")).toBeTruthy(), {
            timeout: 3000,
        });

        // Get all the way to the restore confirmation for the older
        // revision *before* touching the canvas, so the confirm click below
        // fires immediately after the palette click starts the debounce.
        fireEvent.click(screen.getByTestId("history-button"));
        await waitFor(() => expect(screen.getByTestId("history-row-rev-1")).toBeTruthy());
        fireEvent.click(screen.getByTestId("history-row-rev-1"));
        await waitFor(() => expect(screen.getByTestId("history-restore-rev-1")).toBeTruthy());
        fireEvent.click(screen.getByTestId("history-restore-rev-1"));
        await waitFor(() => expect(screen.getByTestId("history-confirm-restore")).toBeTruthy());

        // Start the 250ms draft-autosave debounce. The canvas chip
        // ("seg-0-1") appears the instant applyAstEdit's *optimistic* setAst
        // runs, well before its serialize/parse round-trip resolves and
        // calls setHistory — and it is only that later setHistory (updating
        // `present`) that the draft-publish effect actually depends on, so
        // waiting on the chip would race the debounce's own scheduling, not
        // just its firing. Undo enabling is driven by that same setHistory
        // call, so it is the right signal that `present` has actually
        // changed and the debounce has been scheduled.
        fireEvent.click(screen.getByTestId("palette-field:model"));
        await waitFor(() =>
            expect((screen.getByTitle(/Undo/i) as HTMLButtonElement).disabled).toBe(false),
        );
        // ...confirm the debounce truly hasn't fired yet (otherwise this
        // test would not be exercising the race it claims to)...
        expect(server.putDraftBodies.length).toBe(0);
        // ...then immediately restore.
        fireEvent.click(screen.getByTestId("history-confirm-restore"));

        await waitFor(() => expect(screen.getByText(/Restored revision/i)).toBeTruthy(), {
            timeout: 3000,
        });
        // The original 250ms deadline is long past by now; give it a further
        // margin before asserting no debounced write ever landed.
        await new Promise((r) => setTimeout(r, 300));
        expect(server.putDraftBodies.length).toBe(0);
    });
});

// "Close without saving" (doDiscardClose): the escape hatch for closing the
// tab without promoting the in-progress edits to a new saved revision. On a
// dirty document it confirms first, then rewinds the shared draft back to
// the last saved source before shutting down.
describe("App discard-close", () => {
    const TOKEN = "a".repeat(32);
    let server: FakeServer;
    let confirmSpy: MockInstance<(message?: string) => boolean>;

    beforeEach(() => {
        window.location.hash = `#token=${TOKEN}`;
        server = installFakeDslServer(defaultTestDoc());
        confirmSpy = vi.spyOn(window, "confirm");
    });

    afterEach(() => {
        vi.restoreAllMocks();
        vi.unstubAllGlobals();
        window.location.hash = "";
        window.localStorage.clear();
    });

    async function makeDirty(): Promise<void> {
        fireEvent.click(screen.getByTestId("palette-field:model"));
        await waitFor(() =>
            expect((screen.getByTitle(/Undo/i) as HTMLButtonElement).disabled).toBe(false),
        );
        await waitFor(() => expect(server.putDraftBodies.length).toBe(1));
    }

    it("confirms, rewinds the draft to the saved source, shuts down and closes when confirmed", async () => {
        confirmSpy.mockReturnValue(true);
        render(<App />);
        await waitFor(() => expect(screen.getByTestId("palette-field:model")).toBeTruthy(), {
            timeout: 3000,
        });

        const savedDocument = server.document;
        await makeDirty();
        expect(server.draft).not.toBe(savedDocument);

        fireEvent.click(screen.getByTestId("discard-close-button"));

        expect(confirmSpy).toHaveBeenCalledWith("Discard unsaved changes?");
        await waitFor(() => expect(server.draft).toBe(savedDocument));
        await waitFor(() => expect(server.shutdownCalls).toBe(1));
        await waitFor(() => expect(screen.getByText(/Changes discarded/i)).toBeTruthy());
        // No new saved revision was created by discarding.
        expect(server.putDocumentBodies.length).toBe(0);
    });

    it("does nothing when the confirmation is cancelled", async () => {
        confirmSpy.mockReturnValue(false);
        render(<App />);
        await waitFor(() => expect(screen.getByTestId("palette-field:model")).toBeTruthy(), {
            timeout: 3000,
        });

        await makeDirty();
        const draftBeforeClick = server.draft;

        fireEvent.click(screen.getByTestId("discard-close-button"));

        expect(confirmSpy).toHaveBeenCalled();
        // Give any (wrongly fired) async work a chance to run, then confirm
        // nothing changed: draft untouched, no shutdown, still open.
        await new Promise((r) => setTimeout(r, 50));
        expect(server.draft).toBe(draftBeforeClick);
        expect(server.shutdownCalls).toBe(0);
        expect(screen.queryByText(/You can close this tab/i)).toBeNull();
        expect(screen.getByText(/unsaved/i)).toBeTruthy();
    });

    it("closes immediately without a confirmation dialog when there are no unsaved changes", async () => {
        render(<App />);
        await waitFor(() => expect(screen.getByTestId("palette-field:model")).toBeTruthy(), {
            timeout: 3000,
        });

        fireEvent.click(screen.getByTestId("discard-close-button"));

        expect(confirmSpy).not.toHaveBeenCalled();
        await waitFor(() => expect(server.shutdownCalls).toBe(1));
        await waitFor(() => expect(screen.getByText(/You can close this tab/i)).toBeTruthy());
    });

    it("discards edits that never reached the server draft (settled before the debounce fires)", async () => {
        confirmSpy.mockReturnValue(true);
        render(<App />);
        await waitFor(() => expect(screen.getByTestId("palette-field:model")).toBeTruthy(), {
            timeout: 3000,
        });

        // Start the 250ms draft-autosave debounce, then discard immediately
        // — well before it would have fired — proving doDiscardClose settles
        // the pending write instead of racing it.
        fireEvent.click(screen.getByTestId("palette-field:model"));
        await waitFor(() =>
            expect((screen.getByTitle(/Undo/i) as HTMLButtonElement).disabled).toBe(false),
        );
        expect(server.putDraftBodies.length).toBe(0);

        fireEvent.click(screen.getByTestId("discard-close-button"));

        await waitFor(() => expect(server.shutdownCalls).toBe(1));
        // The debounce's original 250ms deadline is long past by now; the
        // discarded edit must never have reached the server as a draft PUT.
        await new Promise((r) => setTimeout(r, 300));
        expect(server.putDraftBodies.length).toBe(0);
    });
});

// beforeunload: warns on tab/window close only while there are unsaved
// edits, and stops warning once those edits are resolved (saved, discarded,
// or the tab is already in its post-shutdown "closed" state).
describe("App beforeunload warning", () => {
    const TOKEN = "a".repeat(32);

    beforeEach(() => {
        window.location.hash = `#token=${TOKEN}`;
        installFakeDslServer(defaultTestDoc());
    });

    afterEach(() => {
        vi.restoreAllMocks();
        vi.unstubAllGlobals();
        window.location.hash = "";
        window.localStorage.clear();
    });

    function dispatchBeforeUnload(): Event {
        const event = new Event("beforeunload", { cancelable: true });
        window.dispatchEvent(event);
        return event;
    }

    it("does not prevent unload while there are no unsaved changes", async () => {
        render(<App />);
        await waitFor(() => expect(screen.getByTestId("palette-field:model")).toBeTruthy(), {
            timeout: 3000,
        });

        const event = dispatchBeforeUnload();
        expect(event.defaultPrevented).toBe(false);
    });

    it("prevents unload while there are unsaved changes", async () => {
        render(<App />);
        await waitFor(() => expect(screen.getByTestId("palette-field:model")).toBeTruthy(), {
            timeout: 3000,
        });

        fireEvent.click(screen.getByTestId("palette-field:model"));
        await waitFor(() =>
            expect((screen.getByTitle(/Undo/i) as HTMLButtonElement).disabled).toBe(false),
        );

        const event = dispatchBeforeUnload();
        expect(event.defaultPrevented).toBe(true);
    });

    it("stops warning once the tab is closed (discarded), even though present still differs from savedSource", async () => {
        vi.spyOn(window, "confirm").mockReturnValue(true);
        render(<App />);
        await waitFor(() => expect(screen.getByTestId("palette-field:model")).toBeTruthy(), {
            timeout: 3000,
        });

        fireEvent.click(screen.getByTestId("palette-field:model"));
        await waitFor(() =>
            expect((screen.getByTitle(/Undo/i) as HTMLButtonElement).disabled).toBe(false),
        );
        fireEvent.click(screen.getByTestId("discard-close-button"));
        await waitFor(() => expect(screen.getByText(/Changes discarded/i)).toBeTruthy());

        const event = dispatchBeforeUnload();
        expect(event.defaultPrevented).toBe(false);
    });
});
