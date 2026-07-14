import { describe, expect, it } from "vitest";
import {
    canRedo,
    canUndo,
    HISTORY_LIMIT,
    initHistory,
    pushHistory,
    redo,
    undo,
} from "./history.ts";

describe("history", () => {
    it("starts with no undo/redo available", () => {
        const h = initHistory(0);
        expect(canUndo(h)).toBe(false);
        expect(canRedo(h)).toBe(false);
        expect(h.present).toBe(0);
    });

    it("round-trips edit -> undo -> redo", () => {
        let h = initHistory("a");
        h = pushHistory(h, "b");
        h = pushHistory(h, "c");
        expect(h.present).toBe("c");

        h = undo(h);
        expect(h.present).toBe("b");
        h = undo(h);
        expect(h.present).toBe("a");
        expect(canUndo(h)).toBe(false);

        h = redo(h);
        expect(h.present).toBe("b");
        h = redo(h);
        expect(h.present).toBe("c");
        expect(canRedo(h)).toBe(false);
    });

    it("clears the redo stack on a new edit", () => {
        let h = initHistory(1);
        h = pushHistory(h, 2);
        h = undo(h); // present = 1, future = [2]
        expect(canRedo(h)).toBe(true);
        h = pushHistory(h, 3); // new edit clears future
        expect(canRedo(h)).toBe(false);
        expect(h.present).toBe(3);
        h = undo(h);
        expect(h.present).toBe(1);
    });

    it("undo on empty history is a no-op", () => {
        const h = initHistory(5);
        expect(undo(h)).toEqual(h);
    });

    it("redo without prior undo is a no-op", () => {
        const h = initHistory(5);
        expect(redo(h)).toEqual(h);
    });

    it("is bounded at HISTORY_LIMIT past entries", () => {
        let h = initHistory(0);
        for (let i = 1; i <= HISTORY_LIMIT + 50; i += 1) {
            h = pushHistory(h, i);
        }
        expect(h.past.length).toBe(HISTORY_LIMIT);
        expect(h.present).toBe(HISTORY_LIMIT + 50);

        // We can only undo back HISTORY_LIMIT steps; the oldest states were
        // dropped, so the earliest reachable present is not 0.
        let steps = 0;
        while (canUndo(h)) {
            h = undo(h);
            steps += 1;
        }
        expect(steps).toBe(HISTORY_LIMIT);
        expect(h.present).toBe(50);
    });

    it("respects a custom limit", () => {
        let h = initHistory(0);
        for (let i = 1; i <= 10; i += 1) {
            h = pushHistory(h, i, 3);
        }
        expect(h.past.length).toBe(3);
    });
});
