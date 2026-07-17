import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { ImportModal } from "./ImportModal.tsx";

const XML_DOC = "<statusloom><layout/></statusloom>";
const MARKDOWN_DOC = [
    "---",
    'format: "statusloom/config"',
    "formatVersion: 1",
    "---",
    "",
    "```xml",
    XML_DOC,
    "```",
    "",
].join("\n");

function setup(overrides: { onImport?: () => Promise<string | null>; onImportMarkdown?: () => Promise<string | null> } = {}) {
    const onImport = vi.fn(overrides.onImport ?? (async () => null));
    const onImportMarkdown = vi.fn(overrides.onImportMarkdown ?? (async () => null));
    const onClose = vi.fn();
    render(
        <ImportModal
            lang="en"
            onImport={onImport}
            onImportMarkdown={onImportMarkdown}
            onClose={onClose}
        />,
    );
    return { onImport, onImportMarkdown, onClose };
}

function typeInto(text: string) {
    fireEvent.change(screen.getByTestId("import-text"), { target: { value: text } });
}

describe("ImportModal format detection", () => {
    it("shows the DSL append/replace buttons for XML content", () => {
        setup();
        typeInto(XML_DOC);
        expect(screen.getByTestId("import-append")).toBeTruthy();
        expect(screen.getByTestId("import-replace")).toBeTruthy();
        expect(screen.queryByTestId("import-markdown")).toBeNull();
    });

    it("shows a single Import button for content starting with a frontmatter ---", () => {
        setup();
        typeInto(MARKDOWN_DOC);
        expect(screen.getByTestId("import-markdown")).toBeTruthy();
        expect(screen.queryByTestId("import-append")).toBeNull();
        expect(screen.queryByTestId("import-replace")).toBeNull();
    });

    it("also detects Markdown content with leading whitespace before the frontmatter", () => {
        setup();
        typeInto("\n  " + MARKDOWN_DOC);
        expect(screen.getByTestId("import-markdown")).toBeTruthy();
    });

    it("disables every action button while the text area is empty", () => {
        setup();
        expect((screen.getByTestId("import-append") as HTMLButtonElement).disabled).toBe(true);
        expect((screen.getByTestId("import-replace") as HTMLButtonElement).disabled).toBe(true);
    });
});

describe("ImportModal markdown import", () => {
    it("calls onImportMarkdown (not onImport) with the pasted content", async () => {
        const { onImport, onImportMarkdown } = setup();
        typeInto(MARKDOWN_DOC);

        fireEvent.click(screen.getByTestId("import-markdown"));
        await waitFor(() => expect(onImportMarkdown).toHaveBeenCalledWith(MARKDOWN_DOC));

        expect(onImport).not.toHaveBeenCalled();
    });

    it("shows the returned error message inline and keeps the modal open", async () => {
        const { onClose } = setup({ onImportMarkdown: async () => "document rejected" });
        typeInto(MARKDOWN_DOC);

        fireEvent.click(screen.getByTestId("import-markdown"));
        await screen.findByText("document rejected");

        expect(onClose).not.toHaveBeenCalled();
    });
});

describe("ImportModal DSL import", () => {
    it("calls onImport with the append mode", async () => {
        const { onImport, onImportMarkdown } = setup();
        typeInto(XML_DOC);

        fireEvent.click(screen.getByTestId("import-append"));
        await waitFor(() => expect(onImport).toHaveBeenCalledWith(XML_DOC, "append"));

        expect(onImportMarkdown).not.toHaveBeenCalled();
    });

    it("calls onImport with the replace mode", async () => {
        const { onImport } = setup();
        typeInto(XML_DOC);

        fireEvent.click(screen.getByTestId("import-replace"));
        await waitFor(() => expect(onImport).toHaveBeenCalledWith(XML_DOC, "replace"));
    });
});
