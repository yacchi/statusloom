// Import DSL source (paste or file). Two modes:
//   * Add layouts — the pasted document's <layout> elements are appended to
//     the current document (never active; names disambiguated).
//   * Replace document — the pasted source replaces the whole document (it
//     may be invalid; the DSL editor then shows its diagnostics).
// Validation happens in App.tsx via POST /api/dsl/parse; a rejected append
// reports its message back here.

import { useState } from "react";

export type ImportMode = "append" | "replace";

interface Props {
    // Resolves to an error message to display, or null on success (the modal
    // closes itself on success via onClose from the parent).
    onImport: (source: string, mode: ImportMode) => Promise<string | null>;
    onClose: () => void;
}

export function ImportModal({ onImport, onClose }: Props) {
    const [text, setText] = useState("");
    const [error, setError] = useState<string | null>(null);
    const [busy, setBusy] = useState(false);

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
        <div className="modal-backdrop" onClick={onClose}>
            <div className="modal" onClick={(e) => e.stopPropagation()}>
                <h2 style={{ margin: 0 }}>Import DSL</h2>
                <p className="hint">
                    Choose a file or paste Statusloom DSL source below. "Add layouts"
                    appends its layouts to your current ones; "Replace document" swaps
                    in the whole source.
                </p>
                <input
                    type="file"
                    accept=".xml,text/xml,application/xml"
                    onChange={(e) => {
                        const file = e.target.files?.[0];
                        if (file) {
                            onFile(file);
                        }
                    }}
                />
                <textarea
                    value={text}
                    placeholder="Paste DSL source here…"
                    data-testid="import-text"
                    spellCheck={false}
                    onChange={(e) => {
                        setText(e.target.value);
                        setError(null);
                    }}
                />
                {error ? <div className="inline-error">{error}</div> : null}
                <div className="modal-actions">
                    <button onClick={onClose}>Cancel</button>
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
                </div>
            </div>
        </div>
    );
}
