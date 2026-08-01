import { describe, expect, it } from "vitest";
import { formatSimpleWhen, parseSimpleWhen } from "./whenExpr.ts";

describe("parseSimpleWhen", () => {
    it("parses a quoted string comparison", () => {
        expect(parseSimpleWhen('account-type eq "claude_team"')).toEqual({
            metric: "account-type",
            op: "eq",
            value: "claude_team",
            quoted: true,
        });
    });

    it("parses a bare numeric comparison", () => {
        expect(parseSimpleWhen("context-percent ge 80")).toEqual({
            metric: "context-percent",
            op: "ge",
            value: "80",
            quoted: false,
        });
    });

    it("parses a bare boolean comparison", () => {
        expect(parseSimpleWhen("git-dirty eq true")).toEqual({
            metric: "git-dirty",
            op: "eq",
            value: "true",
            quoted: false,
        });
    });

    it("tolerates single quotes and extra whitespace", () => {
        expect(parseSimpleWhen("  account-plan   ne 'default_claude_max_5x'  ")).toEqual({
            metric: "account-plan",
            op: "ne",
            value: "default_claude_max_5x",
            quoted: true,
        });
    });

    it("returns null for an empty or absent expression", () => {
        expect(parseSimpleWhen("")).toBeNull();
        expect(parseSimpleWhen(undefined)).toBeNull();
    });

    it("returns null for anything beyond one comparison", () => {
        // The builder must not claim to represent these — the caller keeps them
        // as raw text instead of silently rewriting them.
        expect(parseSimpleWhen('a eq 1 and b eq 2')).toBeNull();
        expect(parseSimpleWhen('not git-dirty')).toBeNull();
        expect(parseSimpleWhen('(context-percent ge 80)')).toBeNull();
        expect(parseSimpleWhen('account-type')).toBeNull();
        // An unknown word operator is not the DSL's grammar either.
        expect(parseSimpleWhen('context-percent >= 80')).toBeNull();
    });
});

describe("formatSimpleWhen", () => {
    it("quotes a string comparison and leaves a bare one alone", () => {
        expect(
            formatSimpleWhen({ metric: "account-type", op: "eq", value: "claude_max", quoted: true }),
        ).toBe('account-type eq "claude_max"');
        expect(
            formatSimpleWhen({ metric: "context-percent", op: "ge", value: "80", quoted: false }),
        ).toBe("context-percent ge 80");
    });

    it("round-trips every parse", () => {
        for (const src of [
            'account-type eq "claude_team"',
            "context-percent ge 80",
            "git-dirty eq true",
        ]) {
            const parsed = parseSimpleWhen(src);
            expect(parsed).not.toBeNull();
            expect(formatSimpleWhen(parsed!)).toBe(src);
        }
    });

    it("yields no condition while the value is still empty", () => {
        expect(
            formatSimpleWhen({ metric: "account-type", op: "eq", value: "", quoted: true }),
        ).toBe("");
    });
});
