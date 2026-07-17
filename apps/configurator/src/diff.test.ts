import { describe, expect, it } from "vitest";
import { diffLines } from "./diff.ts";

describe("diffLines", () => {
    it("returns every line as equal for identical text", () => {
        const d = diffLines("a\nb\nc", "a\nb\nc");
        expect(d).toEqual([
            { op: "equal", text: "a" },
            { op: "equal", text: "b" },
            { op: "equal", text: "c" },
        ]);
    });

    it("marks appended lines as add", () => {
        const d = diffLines("a\nb", "a\nb\nc");
        expect(d).toEqual([
            { op: "equal", text: "a" },
            { op: "equal", text: "b" },
            { op: "add", text: "c" },
        ]);
    });

    it("marks removed lines as remove", () => {
        const d = diffLines("a\nb\nc", "a\nc");
        expect(d).toEqual([
            { op: "equal", text: "a" },
            { op: "remove", text: "b" },
            { op: "equal", text: "c" },
        ]);
    });

    it("handles a mixed change (replace the middle line)", () => {
        const d = diffLines("a\nb\nc", "a\nx\nc");
        expect(d).toEqual([
            { op: "equal", text: "a" },
            { op: "remove", text: "b" },
            { op: "add", text: "x" },
            { op: "equal", text: "c" },
        ]);
    });

    it("handles empty old text (everything added)", () => {
        const d = diffLines("", "a\nb");
        // "".split("\n") is [""], so the empty old text is one blank line
        // that gets removed before the two new lines are added.
        expect(d).toEqual([
            { op: "remove", text: "" },
            { op: "add", text: "a" },
            { op: "add", text: "b" },
        ]);
    });

    it("handles empty new text (everything removed)", () => {
        const d = diffLines("a\nb", "");
        expect(d).toEqual([
            { op: "remove", text: "a" },
            { op: "remove", text: "b" },
            { op: "add", text: "" },
        ]);
    });
});
