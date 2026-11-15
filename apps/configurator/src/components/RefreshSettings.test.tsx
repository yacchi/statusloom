// RefreshSettings: the Refresh panel edits the optional <refresh/> element
// through ast.updateRefreshAttrs. Every test drives the real component against
// a stateful root so the assertions are on the resulting AST (what the
// serializer writes), not on callback arguments alone.

import { describe, expect, it } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { useState } from "react";
import { updateRefreshAttrs } from "../ast.ts";
import type { RefreshItems, RefreshNode, StatusloomNode } from "../types.ts";
import type { SourceEntry } from "../sourcesApi.ts";
import { RefreshSettings, offeredTokens, parseTimeout, toggleWaitToken } from "./RefreshSettings.tsx";

function src(id: string, kind: SourceEntry["kind"]): SourceEntry {
    return {
        id,
        kind,
        path: "",
        argv: [],
        url: "",
        allowRemote: false,
        ttlSeconds: 0,
        staleAfterSeconds: 0,
        timeoutSeconds: 0,
        warnings: [],
    };
}

const SOURCES = [src("cmd", "command"), src("web", "http"), src("file1", "file")];

function setup(opts: {
    refresh?: Omit<RefreshNode, "id" | "kind">;
    items?: RefreshItems | null;
    readOnly?: boolean;
}) {
    const initial: StatusloomNode = {
        id: "root",
        kind: "statusloom",
        layouts: [],
        ...(opts.refresh ? { refresh: { id: "refresh", kind: "refresh", ...opts.refresh } } : {}),
    };
    const state = { root: initial, patches: 0 };
    function Harness() {
        const [root, setRoot] = useState(initial);
        return (
            <RefreshSettings
                root={root}
                readOnly={opts.readOnly ?? false}
                refreshItems={opts.items === undefined ? { git: true, transcript: false, sources: [] } : opts.items}
                sources={SOURCES}
                onPatchRefresh={(patch) => {
                    state.patches += 1;
                    setRoot((r) => {
                        const next = updateRefreshAttrs(r, patch);
                        state.root = next;
                        return next;
                    });
                }}
            />
        );
    }
    render(<Harness />);
    return state;
}

const chip = (tok: string) => screen.getByTestId(`refresh-chip-${tok}`) as HTMLButtonElement;
const timeoutInput = () => screen.getByTestId("setting-refresh-timeout") as HTMLInputElement;

describe("RefreshSettings helpers", () => {
    it("offers only used items plus usage, and only command/http sources", () => {
        expect(
            offeredTokens({ git: true, transcript: true, sources: ["cmd", "file1", "undef", "web"] }, SOURCES),
        ).toEqual(["git", "transcript", "usage", "source:cmd", "source:web"]);
        expect(offeredTokens({ git: false, transcript: false, sources: [] }, SOURCES)).toEqual(["usage"]);
        expect(offeredTokens(null, SOURCES)).toEqual(["usage"]);
    });

    it("toggles one token without touching the others", () => {
        expect(toggleWaitToken("x git y", "git")).toBe("x y");
        expect(toggleWaitToken("x  y", "usage")).toBe("x y usage");
        expect(toggleWaitToken("git", "git")).toBeUndefined();
        expect(toggleWaitToken(undefined, "git")).toBe("git");
    });

    it("validates the timeout range 1–3000", () => {
        expect(parseTimeout("")).toBeUndefined();
        expect(parseTimeout("1")).toBe(1);
        expect(parseTimeout("3000")).toBe(3000);
        expect(parseTimeout("0")).toBeNull();
        expect(parseTimeout("3001")).toBeNull();
        expect(parseTimeout("1.5")).toBeNull();
        expect(parseTimeout("-5")).toBeNull();
    });
});

describe("RefreshSettings panel", () => {
    it("shows chips only for the items the document uses", () => {
        setup({ items: { git: true, transcript: false, sources: ["cmd", "file1"] } });
        expect(chip("git")).toBeTruthy();
        expect(chip("usage")).toBeTruthy();
        expect(chip("source:cmd")).toBeTruthy();
        expect(screen.queryByTestId("refresh-chip-transcript")).toBeNull();
        expect(screen.queryByTestId("refresh-chip-source:file1")).toBeNull();
    });

    it("toggling chips writes the wait list and removing the last one drops the element", () => {
        const s = setup({ items: { git: true, transcript: false, sources: ["cmd"] } });
        expect(s.root.refresh).toBeUndefined();
        fireEvent.click(chip("git"));
        expect(s.root.refresh?.wait).toBe("git");
        expect(chip("git").getAttribute("aria-pressed")).toBe("true");
        fireEvent.click(chip("source:cmd"));
        expect(s.root.refresh?.wait).toBe("git source:cmd");
        fireEvent.click(chip("git"));
        expect(s.root.refresh?.wait).toBe("source:cmd");
        expect(chip("git").getAttribute("aria-pressed")).toBe("false");
        fireEvent.click(chip("source:cmd"));
        // No attribute left: the <refresh> element is removed entirely.
        expect(s.root.refresh).toBeUndefined();
        expect(s.root.dirty).toBe(true);
    });

    it("preserves tokens it does not show, in order", () => {
        const s = setup({
            refresh: { wait: "transcript source:gone git", "timeout-ms": 1200 },
            items: { git: true, transcript: false, sources: [] },
        });
        expect(screen.getByTestId("setting-refresh-kept").textContent).toContain("transcript source:gone");
        fireEvent.click(chip("usage"));
        expect(s.root.refresh?.wait).toBe("transcript source:gone git usage");
        fireEvent.click(chip("git"));
        expect(s.root.refresh?.wait).toBe("transcript source:gone usage");
        fireEvent.click(chip("usage"));
        expect(s.root.refresh).toEqual(
            expect.objectContaining({ wait: "transcript source:gone", "timeout-ms": 1200 }),
        );
    });

    it("shows all / sources as read-only chips and marks covered items", () => {
        const s = setup({
            refresh: { wait: "sources" },
            items: { git: true, transcript: false, sources: ["cmd"] },
        });
        expect(chip("sources").disabled).toBe(true);
        expect(chip("source:cmd").disabled).toBe(true);
        expect(chip("source:cmd").getAttribute("aria-pressed")).toBe("true");
        expect(chip("git").disabled).toBe(false);
        fireEvent.click(chip("git"));
        expect(s.root.refresh?.wait).toBe("sources git");
    });

    it("all covers every item", () => {
        setup({ refresh: { wait: "all" }, items: { git: true, transcript: true, sources: [] } });
        for (const tok of ["git", "transcript", "usage", "all"]) {
            expect(chip(tok).disabled).toBe(true);
            expect(chip(tok).getAttribute("aria-pressed")).toBe("true");
        }
    });

    it("writes only in-range timeouts and shows the range", () => {
        const s = setup({ refresh: { wait: "git" } });
        expect(screen.getByTestId("setting-refresh-timeout-range").textContent).toContain("1–3000");
        expect(timeoutInput().placeholder).toBe("2000");

        fireEvent.change(timeoutInput(), { target: { value: "1500" } });
        expect(s.root.refresh?.["timeout-ms"]).toBe(1500);

        const before = s.patches;
        for (const bad of ["0", "3001", "1.5"]) {
            fireEvent.change(timeoutInput(), { target: { value: bad } });
            expect(screen.getByTestId("setting-refresh-timeout-error")).toBeTruthy();
            // The invalid text stays in the input but never reaches the document.
            expect(timeoutInput().value).toBe(bad);
            expect(s.root.refresh?.["timeout-ms"]).toBe(1500);
        }
        expect(s.patches).toBe(before);

        fireEvent.change(timeoutInput(), { target: { value: "3000" } });
        expect(s.root.refresh?.["timeout-ms"]).toBe(3000);
        expect(screen.queryByTestId("setting-refresh-timeout-error")).toBeNull();

        // Empty = default: the attribute is removed.
        fireEvent.change(timeoutInput(), { target: { value: "" } });
        expect(s.root.refresh).toBeDefined();
        expect(s.root.refresh?.["timeout-ms"]).toBeUndefined();
    });

    it("toggles wait-on, removing the attribute for the default cold", () => {
        const s = setup({});
        expect(chip("usage")).toBeTruthy();
        const cold = screen.getByTestId("refresh-wait-on-cold");
        const due = screen.getByTestId("refresh-wait-on-due");
        expect(cold.getAttribute("aria-pressed")).toBe("true");
        fireEvent.click(due);
        expect(s.root.refresh?.["wait-on"]).toBe("due");
        expect(due.getAttribute("aria-pressed")).toBe("true");
        fireEvent.click(cold);
        // wait-on was the only attribute; cold is the default, so it goes away.
        expect(s.root.refresh).toBeUndefined();
    });

    it("clearing the timeout of a timeout-only element removes the element", () => {
        const s = setup({ refresh: { "timeout-ms": 1000 } });
        fireEvent.change(timeoutInput(), { target: { value: "" } });
        expect(s.root.refresh).toBeUndefined();
    });

    it("follows the document when its timeout changes externally", () => {
        const s = setup({ refresh: { "timeout-ms": 800 } });
        expect(timeoutInput().value).toBe("800");
        fireEvent.change(timeoutInput(), { target: { value: "900" } });
        expect(timeoutInput().value).toBe("900");
        expect(s.root.refresh?.["timeout-ms"]).toBe(900);
    });

    it("is disabled when the document is read-only", () => {
        setup({ readOnly: true });
        expect((screen.getByTestId("setting-refresh-wait").closest("fieldset") as HTMLFieldSetElement).disabled).toBe(
            true,
        );
    });
});
