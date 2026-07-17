// Import DSL source (paste or file), or a *.sloom.md Markdown exchange
// document (plans/config-store-and-format.md §6). Which one applies is
// decided from the content itself (never a file extension or a mode toggle):
// text whose first non-whitespace is a frontmatter "---" is treated as
// .sloom.md and goes through the store's validation boundary server-side
// (POST /api/exchange/import, wired by onImportMarkdown); anything else is
// DSL, offered as either of two modes:
//   * Add layouts — the pasted document's <layout> elements are appended to
//     the current document (never active; names disambiguated).
//   * Replace document — the pasted source replaces the whole document (it
//     may be invalid; the DSL editor then shows its diagnostics).
// DSL validation happens in App.tsx via POST /api/dsl/parse; a rejected
// append reports its message back here. Markdown import's diagnostics come
// from the exchange endpoint's response instead.

import { useState } from "react";
import { t, type Lang } from "../i18n.ts";
import { Modal } from "./Modal.tsx";

export type ImportMode = "append" | "replace";

interface Props {
    lang: Lang;
    // Resolves to an error message to display, or null on success (the modal
    // closes itself on success via onClose from the parent).
    onImport: (source: string, mode: ImportMode) => Promise<string | null>;
    // Resolves to an error message to display, or null on success. Called
    // instead of onImport when the pasted/dropped content is detected as a
    // .sloom.md Markdown document.
    onImportMarkdown: (markdown: string) => Promise<string | null>;
    onClose: () => void;
}

export function ImportModal({ lang, onImport, onImportMarkdown, onClose }: Props) {
    const [text, setText] = useState("");
    const [error, setError] = useState<string | null>(null);
    const [busy, setBusy] = useState(false);

    // A .sloom.md document always opens with a frontmatter block; DSL never
    // starts with "---" (its first non-whitespace is always "<").
    const isMarkdown = text.trimStart().startsWith("---");

    const apply = async (mode: ImportMode) => {
        setBusy(true);
        setError(null);
        try {
            const err = await onImport(text, mode);
            if (err !== null) {
                setError(err);
            }
        } finally {
            setBusy(false);
        }
    };

    const applyMarkdown = async () => {
        setBusy(true);
        setError(null);
        try {
            const err = await onImportMarkdown(text);
            if (err !== null) {
                setError(err);
            }
        } finally {
            setBusy(false);
        }
    };

    const onFile = (file: File) => {
        const reader = new FileReader();
        reader.onload = () => {
            const content = String(reader.result ?? "");
            setText(content);
            setError(null);
        };
        reader.readAsText(file);
    };

    return (
        <Modal title="Import" onClose={onClose}>
            <p className="hint">
                Choose a file or paste content below. "Add layouts" appends the
                pasted document's layouts to your current ones; "Replace document"
                swaps in the whole source.
            </p>
            <p className="hint">{t(lang, "importFormatHint")}</p>
            <input
                type="file"
                accept=".xml,.md,text/xml,application/xml,text/markdown"
                onChange={(e) => {
                    const file = e.target.files?.[0];
                    if (file) {
                        onFile(file);
                    }
                }}
            />
            <textarea
                value={text}
                placeholder="Paste DSL source or a .sloom.md document here…"
                data-testid="import-text"
                spellCheck={false}
                onChange={(e) => {
                    setText(e.target.value);
                    setError(null);
                }}
            />
            {error ? <div className="inline-error">{error}</div> : null}
            <div className="modal-actions">
                {isMarkdown ? (
                    <button
                        className="primary"
                        data-testid="import-markdown"
                        disabled={busy || text.trim().length === 0}
                        onClick={applyMarkdown}
                    >
                        Import
                    </button>
                ) : (
                    <>
                        <button
                            data-testid="import-replace"
                            disabled={busy || text.trim().length === 0}
                            onClick={() => apply("replace")}
                        >
                            Replace document
                        </button>
                        <button
                            className="primary"
                            data-testid="import-append"
                            disabled={busy || text.trim().length === 0}
                            onClick={() => apply("append")}
                        >
                            Add layouts
                        </button>
                    </>
                )}
            </div>
        </Modal>
    );
}
