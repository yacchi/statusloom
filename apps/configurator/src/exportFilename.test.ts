import { describe, expect, it } from "vitest";
import { exportFilename, slugify } from "./exportFilename.ts";

function doc(frontmatterLines: string[], notes = ""): string {
    return ["---", ...frontmatterLines, "---", "", notes, "```xml", "<statusloom/>", "```", ""].join(
        "\n",
    );
}

describe("slugify", () => {
    it("lowercases and hyphenates", () => {
        expect(slugify("Claude Code statusline")).toBe("claude-code-statusline");
    });

    it("collapses runs of non-alphanumeric characters into a single hyphen", () => {
        expect(slugify("  a__b -- c!!d  ")).toBe("a-b-c-d");
    });

    it("trims leading/trailing hyphens", () => {
        expect(slugify("-already-hyphenated-")).toBe("already-hyphenated");
    });

    it("returns empty for a name with no ASCII alphanumerics", () => {
        expect(slugify("日本語のみ")).toBe("");
    });
});

describe("exportFilename", () => {
    it("slugifies the frontmatter name field", () => {
        const md = doc(['name: "Claude Code statusline"']);
        expect(exportFilename(md, "claude-code")).toBe("claude-code-statusline");
    });

    it("falls back to the tool id when name is absent", () => {
        const md = doc(['format: "statusloom/config"', "formatVersion: 1"]);
        expect(exportFilename(md, "claude-code")).toBe("claude-code");
    });

    it("falls back to the tool id when name slugifies to nothing (non-ASCII only)", () => {
        const md = doc(['name: "日本語のみ"']);
        expect(exportFilename(md, "claude-code")).toBe("claude-code");
    });

    it("ignores a coincidental \"name:\" line outside the frontmatter block", () => {
        const md = doc(["formatVersion: 1"], "name: this is prose, not frontmatter");
        expect(exportFilename(md, "claude-code")).toBe("claude-code");
    });

    it("falls back to the tool id for content without a frontmatter block", () => {
        expect(exportFilename("<statusloom/>", "claude-code")).toBe("claude-code");
    });
});
