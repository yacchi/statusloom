// The DSL Editor pane: a plain textarea over the DSL source plus a
// diagnostics list (no external editor library, per the UI constraints).
// The source text is the single source of truth; the parent owns it and the
// parse pipeline — this component only edits text and displays diagnostics.

import { useRef } from "react";
import { t, useLang } from "../i18n.ts";
import { byteToCharIndex, byteToLineCol } from "../sourcePos.ts";
import type { Diagnostic } from "../types.ts";

interface Props {
    source: string;
    diagnostics: Diagnostic[];
    // False while the current source has error diagnostics (save blocked,
    // visual editor read-only).
    valid: boolean;
    onChange: (source: string) => void;
}

export function DslEditor({ source, diagnostics, valid, onChange }: Props) {
    const lang = useLang();
    const textareaRef = useRef<HTMLTextAreaElement>(null);

    // Clicking a diagnostic moves the textarea caret/selection to its range.
    const jumpTo = (d: Diagnostic) => {
        const ta = textareaRef.current;
        if (!ta) {
            return;
        }
        const start = byteToCharIndex(source, d.range.start);
        const end = Math.max(start, byteToCharIndex(source, d.range.end));
        ta.focus();
        ta.setSelectionRange(start, end);
    };

    return (
        <div className="panel dsl-editor" data-testid="dsl-editor">
            <h2>
                DSL{" "}
                <span
                    className={"dsl-status" + (valid ? " ok" : " bad")}
                    data-testid="dsl-status"
                >
                    {valid ? "✓" : "✕"}
                </span>
            </h2>
            <p className="hint">{t(lang, "dslEditorHint")}</p>
            <textarea
                ref={textareaRef}
                className="dsl-textarea"
                data-testid="dsl-textarea"
                spellCheck={false}
                autoCapitalize="off"
                autoCorrect="off"
                value={source}
                onChange={(e) => onChange(e.target.value)}
            />
            <div className="dsl-diagnostics" data-testid="dsl-diagnostics">
                {diagnostics.length === 0 ? (
                    <p className="hint">{t(lang, "dslNoProblems")}</p>
                ) : (
                    <ul>
                        {diagnostics.map((d, i) => {
                            const pos = byteToLineCol(source, d.range.start);
                            return (
                                <li key={i}>
                                    <button
                                        className={"diag-row " + d.severity}
                                        data-testid={`diag-${i}`}
                                        onClick={() => jumpTo(d)}
                                    >
                                        <span className="diag-sev">{d.severity}</span>
                                        <span className="diag-pos">
                                            {pos.line}:{pos.col}
                                        </span>
                                        <span className="diag-msg">{d.message}</span>
                                    </button>
                                </li>
                            );
                        })}
                    </ul>
                )}
            </div>
        </div>
    );
}
