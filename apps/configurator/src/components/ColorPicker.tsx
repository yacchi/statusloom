import { useState } from "react";
import { ANSI_COLOR_NAMES, paletteFor, type Theme } from "../ansi.ts";
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
                    value={isHex ? current : ""}
                    onChange={(e) => onChange(e.target.value)}
                />
            </div>
        </div>
    );
}
