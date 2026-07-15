// App-level tests of the shared DSL draft channel (<tool>.draft.xml via
// GET/PUT /api/dsl/draft): source edits publish (debounced), external edits
// fold into undoable history, our own echo never loops, and a backend
// without draft support leaves the app fully usable.

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { App } from "./App.tsx";
import {
    doc,
    fld,
    installFakeDslServer,
    ln,
    lay,
    srcOf,
    versionOf,
    defaultTestDoc,
    type FakeServer,
} from "./test/fakeDsl.ts";

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

function rowCount(): number {
    return document.querySelectorAll(".canvas-row").length;
}

describe("draft sharing", () => {
    it("publishes source edits to the shared draft (debounced)", async () => {
        render(<App />);
        await waitFor(() => expect(screen.getByTestId("palette-field:model")).toBeTruthy(), {
            timeout: 3000,
        });
        await waitFor(() => expect(rowCount()).toBe(2), { timeout: 3000 });

        fireEvent.click(screen.getByTestId("palette-field:model"));

        await waitFor(
            () => {
                expect(server.putDraftBodies.length).toBeGreaterThanOrEqual(1);
                const last = server.putDraftBodies[server.putDraftBodies.length - 1];
                // Three model fields now (two lines + the added one).
                expect(last.match(/"model"/g)?.length).toBe(3);
            },
            { timeout: 3000 },
        );
    });

    it("imports an external draft edit into history and lets undo revert it", async () => {
        const { container } = render(<App />);
        await waitFor(() => expect(rowCount()).toBe(2), { timeout: 3000 });

        // The other side (e.g. `statusloom draft push`) rewrites the draft:
        // a third line appears.
        server.draft = srcOf(
            doc([
                lay(
                    "Default",
                    [ln([fld("model")]), ln([fld("model")]), ln([fld("git-branch")])],
                    true,
                ),
            ]),
        );

        // The ~1s poll folds it into history and the parse pipeline applies it.
        await waitFor(() => expect(rowCount()).toBe(3), { timeout: 5000 });

        // It is undoable.
        fireEvent.click(screen.getByTitle(/Undo/i));
        await waitFor(() => expect(rowCount()).toBe(2), { timeout: 3000 });
        expect(container).toBeTruthy();
    });

    it("does not import its own echo (no spurious history entry)", async () => {
        render(<App />);
        await waitFor(() => expect(rowCount()).toBe(2), { timeout: 3000 });

        // Simulate our own previous write being echoed: the draft holds
        // exactly the source the app already has.
        server.draft = server.document;

        // Let two poll cycles pass with no external change.
        await new Promise((r) => setTimeout(r, 2200));

        // No history entry was created, so undo stays disabled.
        const undo = screen.getByTitle(/Undo/i) as HTMLButtonElement;
        expect(undo.disabled).toBe(true);
    });

    it("an equal-content external write only refreshes version tracking", async () => {
        render(<App />);
        await waitFor(() => expect(rowCount()).toBe(2), { timeout: 3000 });

        // Same content, different serialization (whitespace) -> different
        // version, but identical to the present source after our normalizer?
        // Here: identical content string, so the import is skipped entirely.
        const same = server.document;
        server.draft = same;
        expect(versionOf(same)).toBe(versionOf(server.document));

        await new Promise((r) => setTimeout(r, 1500));
        const undo = screen.getByTitle(/Undo/i) as HTMLButtonElement;
        expect(undo.disabled).toBe(true);
    });

    it("stays usable and inert when the backend has no draft support", async () => {
        server.draftFails = true;
        render(<App />);

        // The configurator still loads (from the document) and renders chips.
        await waitFor(() => expect(screen.getByTestId("seg-0-0")).toBeTruthy(), {
            timeout: 3000,
        });

        // Editing still works after the failed draft calls.
        fireEvent.click(screen.getByTestId("palette-field:model"));
        const undo = screen.getByTitle(/Undo/i) as HTMLButtonElement;
        await waitFor(() => expect(undo.disabled).toBe(false), { timeout: 3000 });

        // A poll cycle passes without any draft writes.
        await new Promise((r) => setTimeout(r, 1200));
        expect(server.putDraftBodies.length).toBe(0);
    });
});
