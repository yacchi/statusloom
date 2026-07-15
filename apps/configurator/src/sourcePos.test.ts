import { describe, expect, it } from "vitest";
import { byteToCharIndex, byteToLineCol, charIndexToLineCol } from "./sourcePos.ts";

describe("byteToCharIndex", () => {
    it("is the identity for ASCII", () => {
        expect(byteToCharIndex("hello", 0)).toBe(0);
        expect(byteToCharIndex("hello", 3)).toBe(3);
        expect(byteToCharIndex("hello", 99)).toBe(5);
    });

    it("accounts for multi-byte UTF-8 sequences", () => {
        // "あ" is 3 bytes / 1 UTF-16 code unit.
        const src = "あい<x/>";
        expect(byteToCharIndex(src, 3)).toBe(1);
        expect(byteToCharIndex(src, 6)).toBe(2);
        expect(byteToCharIndex(src, 7)).toBe(3);
    });

    it("clamps negative offsets", () => {
        expect(byteToCharIndex("abc", -1)).toBe(0);
    });
});

describe("charIndexToLineCol / byteToLineCol", () => {
    const src = "line one\nline two\nline three";

    it("computes 1-based line and column", () => {
        expect(charIndexToLineCol(src, 0)).toEqual({ line: 1, col: 1 });
        expect(charIndexToLineCol(src, 5)).toEqual({ line: 1, col: 6 });
        expect(charIndexToLineCol(src, 9)).toEqual({ line: 2, col: 1 });
        expect(charIndexToLineCol(src, 18)).toEqual({ line: 3, col: 1 });
    });

    it("byteToLineCol composes both conversions", () => {
        expect(byteToLineCol("あ\nb", 4)).toEqual({ line: 2, col: 1 });
    });
});
