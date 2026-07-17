// Download filename derivation for the *.sloom.md export (App.tsx's
// doExportMarkdown, GET /api/exchange/export). Rather than a fixed
// "<tool>.sloom.md" name, the file is named after the exported document's
// own frontmatter `name` field (plans/config-store-and-format.md §6.5),
// slugified into a filesystem-friendly basename — e.g. "Claude Code
// statusline" -> "claude-code-statusline.sloom.md" — falling back to the
// tool id when the name is absent or its slug would be empty.

// Extracts the frontmatter `name` field from a *.sloom.md document's raw
// text, if present. Only scans the frontmatter block itself (before the
// closing "---"), so a coincidental "name:" line in the free-form Notes
// prose below is never picked up. The value is a single-line JSON scalar
// (internal/exchange.Encode), so it is decoded with JSON.parse.
function frontmatterName(markdown: string): string | null {
    const lines = markdown.replace(/\r\n/g, "\n").split("\n");
    if (lines[0] !== "---") {
        return null;
    }
    for (let i = 1; i < lines.length; i += 1) {
        const line = lines[i];
        if (line === "---") {
            break;
        }
        const idx = line.indexOf(":");
        if (idx === -1) {
            continue;
        }
        if (line.slice(0, idx).trim() !== "name") {
            continue;
        }
        try {
            const value: unknown = JSON.parse(line.slice(idx + 1).trim());
            return typeof value === "string" ? value : null;
        } catch {
            return null;
        }
    }
    return null;
}

// Lowercases, collapses runs of non-alphanumeric characters into a single
// "-", and trims leading/trailing "-". Non-ASCII letters (e.g. Japanese)
// are not alphanumeric here, so a name that is entirely non-ASCII slugifies
// to "" — callers fall back to the tool id in that case.
export function slugify(text: string): string {
    return text
        .toLowerCase()
        .replace(/[^a-z0-9]+/g, "-")
        .replace(/^-+|-+$/g, "");
}

// The download basename (without the ".sloom.md" extension) for `tool`'s
// exported document: the slugified frontmatter name, or `tool` itself when
// the name is absent, empty, or slugifies to nothing.
export function exportFilename(markdown: string, tool: string): string {
    const name = frontmatterName(markdown);
    const slug = name ? slugify(name) : "";
    return slug || tool;
}
