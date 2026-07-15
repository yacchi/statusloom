// Document-level display settings (attributes of the <statusloom> root that
// affect rendering). These live near the Canvas preview — rather than in the
// ⚙ modal (which is now Git-only) — because they change what the preview shows.
// Each change is an attribute patch (undefined removes the attribute).

import type { AttrPatch } from "../ast.ts";
import type { StatusloomNode } from "../types.ts";
import { t, useLang } from "../i18n.ts";
import { HelpTip } from "./HelpTip.tsx";

interface Props {
    root: StatusloomNode;
    readOnly: boolean;
    onPatchRoot: (patch: AttrPatch) => void;
}

const COLOR_LEVELS = [
    { value: "", label: "(default)" },
    { value: "none", label: "none" },
    { value: "ansi16", label: "ansi16" },
    { value: "ansi256", label: "ansi256" },
    { value: "truecolor", label: "truecolor" },
];

const CONTEXT_MODES = [
    { value: "", label: "(default)" },
    { value: "raw", label: "raw" },
    { value: "usable", label: "usable" },
    { value: "both", label: "both" },
];

function numberOrUnset(v: string): number | undefined {
    return v === "" ? undefined : Number(v);
}

export function DisplaySettings({ root, readOnly, onPatchRoot }: Props) {
    const lang = useLang();

    return (
        <fieldset className="display-settings" disabled={readOnly}>
            <span className="display-settings-label">Display</span>

            <label className="display-field">
                Compact threshold <HelpTip k="helpCompactThreshold" />
                <input
                    type="number"
                    min={0}
                    data-testid="setting-compact-threshold"
                    value={root["compact-threshold"] ?? ""}
                    onChange={(e) =>
                        onPatchRoot({ "compact-threshold": numberOrUnset(e.target.value) })
                    }
                />
            </label>

            <label className="display-field">
                Output style
                <select
                    data-testid="setting-output-style"
                    value={root["output-style"] ?? "standard"}
                    onChange={(e) =>
                        onPatchRoot({
                            "output-style":
                                e.target.value === "standard" ? undefined : e.target.value,
                        })
                    }
                >
                    <option value="standard">Standard</option>
                    <option value="powerline">Powerline</option>
                </select>
            </label>

            <label className="display-field">
                Color level <HelpTip k="helpColorLevel" />
                <select
                    data-testid="setting-color-level"
                    value={root["color-level"] ?? ""}
                    onChange={(e) =>
                        onPatchRoot({
                            "color-level": e.target.value === "" ? undefined : e.target.value,
                        })
                    }
                >
                    {COLOR_LEVELS.map((c) => (
                        <option key={c.value} value={c.value}>
                            {c.label}
                        </option>
                    ))}
                </select>
            </label>

            <label className="display-field">
                Context % mode <HelpTip k="helpContextMode" />
                <select
                    data-testid="setting-context-mode"
                    value={root["context-percentage-mode"] ?? ""}
                    onChange={(e) =>
                        onPatchRoot({
                            "context-percentage-mode":
                                e.target.value === "" ? undefined : e.target.value,
                        })
                    }
                >
                    {CONTEXT_MODES.map((c) => (
                        <option key={c.value} value={c.value}>
                            {c.label}
                        </option>
                    ))}
                </select>
            </label>

            <label className="display-field">
                Reserve tokens <HelpTip k="helpReserveTokens" />
                <input
                    type="number"
                    min={0}
                    data-testid="setting-reserve-tokens"
                    value={root["context-reserve-tokens"] ?? ""}
                    onChange={(e) =>
                        onPatchRoot({
                            "context-reserve-tokens": numberOrUnset(e.target.value),
                        })
                    }
                />
            </label>
            <span className="display-settings-hint hint">{t(lang, "zeroDisabledHint")}</span>
        </fieldset>
    );
}
