// App-level integration tests for the multi-layout UI over the AST: the
// layout tab strip (select edit target, set active, add, duplicate, delete,
// rename) and DSL import (append layouts / replace document).

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { App } from "./App.tsx";
import {
    doc,
    fld,
    installFakeDslServer,
    lay,
    ln,
    srcOf,
    type FakeServer,
} from "./test/fakeDsl.ts";
import type { StatusloomNode } from "./types.ts";

const TOKEN = "a".repeat(32);

function twoLayoutDoc(): StatusloomNode {
    return doc([
        lay("Wide", [ln([fld("model")]), ln([fld("git-branch")])], true),
        lay("Compact", [ln([fld("model")])]),
    ]);
}

let server: FakeServer;

beforeEach(() => {
    window.location.hash = `#token=${TOKEN}`;
    server = installFakeDslServer(twoLayoutDoc());
});

afterEach(() => {
    vi.unstubAllGlobals();
    window.location.hash = "";
    window.localStorage.clear();
});

async function renderApp() {
    const view = render(<App />);
    await waitFor(() => expect(screen.getByText("Wide")).toBeTruthy(), { timeout: 3000 });
    return view;
}

function rowCount(): number {
    return document.querySelectorAll(".canvas-row").length;
}

describe("layout tabs", () => {
    it("renders a tab per layout and edits the first by default", async () => {
        await renderApp();
        expect(screen.getByText("Wide")).toBeTruthy();
        expect(screen.getByText("Compact")).toBeTruthy();
        await waitFor(() => expect(rowCount()).toBe(2), { timeout: 3000 });
    });

    it("switches the edit target on tab click", async () => {
        await renderApp();
        fireEvent.click(screen.getByText("Compact"));
        await waitFor(() => expect(rowCount()).toBe(1), { timeout: 3000 });
    });

    it("marks the active layout and can activate another (exactly one active)", async () => {
        await renderApp();
        const badges = screen.getAllByTitle(/status line/);
        expect(badges.some((b) => b.textContent?.includes("● active"))).toBe(true);

        // Activate "Compact" via its badge.
        const setActive = screen
            .getAllByRole("button")
            .find((b) => b.textContent === "○ set active");
        expect(setActive).toBeTruthy();
        fireEvent.click(setActive!);
        await waitFor(
            () => {
                const last = server.putDraftBodies[server.putDraftBodies.length - 1] ?? "";
                const parsed = JSON.parse(last) as StatusloomNode;
                expect(parsed.layouts.map((l) => l.active === true)).toEqual([false, true]);
            },
            { timeout: 3000 },
        );
    });

    it("adds a new layout and switches to it", async () => {
        await renderApp();
        fireEvent.click(screen.getByTitle(/Add a new layout/));
        await waitFor(() => expect(screen.getByText("Layout 3")).toBeTruthy(), {
            timeout: 3000,
        });
        // The new layout starts with a single empty line.
        await waitFor(() => expect(rowCount()).toBe(1), { timeout: 3000 });
    });

    it("duplicates the edited layout", async () => {
        await renderApp();
        fireEvent.click(screen.getByText("Duplicate"));
        await waitFor(() => expect(screen.getByText("Wide copy")).toBeTruthy(), {
            timeout: 3000,
        });
    });

    it("deletes a layout but never the last one", async () => {
        await renderApp();
        const deletes = screen.getAllByTitle(/Delete this layout/);
        expect(deletes).toHaveLength(2);
        fireEvent.click(deletes[1]);
        await waitFor(() => expect(screen.queryByText("Compact")).toBeNull(), {
            timeout: 3000,
        });
        // A single remaining layout cannot be deleted.
        const remaining = screen.getAllByTitle(/Delete this layout/);
        expect((remaining[0] as HTMLButtonElement).disabled).toBe(true);
    });

    it("renames a layout via double-click", async () => {
        await renderApp();
        fireEvent.doubleClick(screen.getByText("Wide"));
        const input = document.querySelector(".layout-tab-input") as HTMLInputElement;
        expect(input).toBeTruthy();
        fireEvent.change(input, { target: { value: "Ultra" } });
        fireEvent.keyDown(input, { key: "Enter" });
        await waitFor(() => expect(screen.getByText("Ultra")).toBeTruthy(), { timeout: 3000 });
    });
});

describe("import", () => {
    it("appends the pasted document's layouts (never active, names disambiguated)", async () => {
        await renderApp();
        fireEvent.click(screen.getByText("Import"));
        const pasted = srcOf(doc([lay("Wide", [ln([fld("model")])], true)]));
        fireEvent.change(screen.getByTestId("import-text"), { target: { value: pasted } });
        fireEvent.click(screen.getByTestId("import-append"));

        await waitFor(() => expect(screen.getByText("Wide 2")).toBeTruthy(), {
            timeout: 3000,
        });
        await waitFor(
            () => {
                const last = server.putDraftBodies[server.putDraftBodies.length - 1] ?? "";
                const parsed = JSON.parse(last) as StatusloomNode;
                expect(parsed.layouts.map((l) => l.name)).toEqual([
                    "Wide",
                    "Compact",
                    "Wide 2",
                ]);
                // The original active layout is untouched.
                expect(parsed.layouts[0].active).toBe(true);
                expect(parsed.layouts[2].active).toBeUndefined();
            },
            { timeout: 3000 },
        );
    });

    it("rejects an append of invalid DSL with an error message", async () => {
        await renderApp();
        fireEvent.click(screen.getByText("Import"));
        fireEvent.change(screen.getByTestId("import-text"), {
            target: { value: "PARSE-ERROR <<<" },
        });
        fireEvent.click(screen.getByTestId("import-append"));
        await waitFor(() =>
            expect(document.querySelector(".modal .inline-error")).toBeTruthy(),
        );
    });

    it("replaces the whole document", async () => {
        await renderApp();
        fireEvent.click(screen.getByText("Import"));
        const pasted = srcOf(doc([lay("Solo", [ln([fld("model")])], true)]));
        fireEvent.change(screen.getByTestId("import-text"), { target: { value: pasted } });
        fireEvent.click(screen.getByTestId("import-replace"));

        await waitFor(() => expect(screen.getByText("Solo")).toBeTruthy(), { timeout: 3000 });
        expect(screen.queryByText("Wide")).toBeNull();
    });
});
