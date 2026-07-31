import { useEffect, useState } from "react";
import { ANSI_COLOR_NAMES, paletteFor, type Theme } from "@statusloom/ansi";
import {
    ANSI_THEME_ID,
    COLOR_THEMES,
    loadColorThemeId,
    saveColorThemeId,
} from "../themes.ts";

interface Props {
    // The node's current color: "", a kebab-case ANSI name, or "#rrggbb".
    color: string | undefined;
    // Preview background theme; determines the hex shown for ANSI names.
    previewTheme: Theme;
    onChange: (color: string) => void;
}

// The DSL uses kebab-case color names ("bright-black"); the internal ANSI
// palette keys are camelCase ("brightBlack").
function toKebab(name: string): string {
    return name.replace(/[A-Z]/g, (c) => "-" + c.toLowerCase());
}

interface Swatch {
    value: string; // what gets written into the widget's color field
    hex: string; // what the swatch cell looks like
    label: string;
}

const HEX_RE = /^#[0-9a-fA-F]{6}$/;

// normalizeHex turns user-typed or pasted text into the one hex form the DSL
// accepts ("#rrggbb"), or null when it is not (yet) a complete color.
//
// It tolerates surrounding whitespace and a missing leading "#", because a bare
// "00ff00" is what most pickers and CSS tools put on the clipboard. It
// deliberately does NOT expand the 3-digit shorthand "#abc": while someone
// types "#00ff00" the value passes through "#00f", so accepting shorthand would
// commit an unintended "#0000ff" (and a bogus undo entry) mid-word. Everything
// else - including every partially typed value - returns null, and the caller
// keeps it purely local instead of writing an invalid color into the document.
export function normalizeHex(raw: string): string | null {
    const s = raw.trim().toLowerCase().replace(/^#/, "");
    return /^[0-9a-f]{6}$/.test(s) ? "#" + s : null;
}

export function ColorPicker({ color, previewTheme, onChange }: Props) {
    // UI-side preference only; never part of the saved config.
    const [themeId, setThemeId] = useState<string>(loadColorThemeId);

    const pickTheme = (id: string) => {
        setThemeId(id);
        saveColorThemeId(id);
    };

    const palette = paletteFor(previewTheme);
    const swatches: Swatch[] =
        themeId === ANSI_THEME_ID
            ? ANSI_COLOR_NAMES.map((name) => ({
                  value: toKebab(name),
                  hex: palette[name],
                  label: toKebab(name),
              }))
            : (COLOR_THEMES.find((t) => t.id === themeId)?.colors ?? []).map((hex) => ({
                  value: hex,
                  hex,
                  label: hex,
              }));

    const current = color ?? "";
    const isHex = current.startsWith("#");
    const nativeValue = HEX_RE.test(current) ? current.toLowerCase() : "#ffffff";

    // hexDraft holds what the user is typing while it is not yet a complete
    // color. Committing every keystroke straight to the document instead would
    // push an invalid color ("#", "#00ff0") through validation, and the editor
    // refuses all further AST edits while the document is invalid - so the box
    // would freeze on the first character and the change would never apply.
    // null means "show the node's committed value".
    const [hexDraft, setHexDraft] = useState<string | null>(null);

    // Drop the draft whenever the committed color changes under us (our own
    // commit landed, or the selection moved to another node).
    useEffect(() => setHexDraft(null), [color]);

    const typeHex = (raw: string) => {
        if (raw.trim() === "") {
            setHexDraft(null);
            onChange("");
            return;
        }
        const norm = normalizeHex(raw);
        if (norm === null) {
            setHexDraft(raw); // incomplete: keep it local, document untouched
            return;
        }
        setHexDraft(null);
        onChange(norm);
    };

    const hexShown = hexDraft ?? (isHex ? current : "");
    const hexIncomplete = hexDraft !== null;

    return (
        <div className="color-picker">
            <select
                className="color-theme-select"
                data-testid="color-theme-select"
                value={themeId}
                onChange={(e) => pickTheme(e.target.value)}
            >
                <option value={ANSI_THEME_ID}>ANSI (terminal)</option>
                {COLOR_THEMES.map((t) => (
                    <option key={t.id} value={t.id}>
                        {t.name}
                    </option>
                ))}
            </select>

            <div className="swatch-grid">
                <button
                    className={"swatch-cell none" + (current === "" ? " selected" : "")}
                    title="none"
                    data-testid="swatch-none"
                    onClick={() => onChange("")}
                >
                    ×
                </button>
                {swatches.map((s) => (
                    <button
                        key={s.value}
                        className={
                            "swatch-cell" +
                            (current.toLowerCase() === s.value.toLowerCase()
                                ? " selected"
                                : "")
                        }
                        style={{ background: s.hex }}
                        title={s.label}
                        data-testid={`swatch-${s.value}`}
                        onClick={() => onChange(s.value)}
                    />
                ))}
            </div>

            <div className="custom-color">
                <input
                    type="color"
                    data-testid="color-native-input"
                    value={nativeValue}
                    onChange={(e) => onChange(e.target.value)}
                />
                <input
                    type="text"
                    data-testid="color-hex-input"
                    placeholder="#rrggbb"
                    spellCheck={false}
                    aria-invalid={hexIncomplete}
                    value={hexShown}
                    onChange={(e) => typeHex(e.target.value)}
                    // Snap back to the committed value on blur, so an
                    // incomplete entry never lingers as if it had applied.
                    onBlur={() => setHexDraft(null)}
                />
            </div>
        </div>
    );
}
