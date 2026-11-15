// Refresh-wait settings: the optional <refresh/> element of the document
// (markup.md "refresh"). Hosted in SettingsModal next to GitSettings and, like
// it, every change is an attribute patch (undefined removes the attribute; a
// fully-cleared refresh element is dropped entirely — ast.updateRefreshAttrs).
//
// The wait list is edited with toggle chips, one per work item the document
// actually uses (the parse response's refreshItems: git / transcript /
// referenced command|http sources) plus `usage`, which never depends on the
// document. Tokens the panel does not offer (an unused item, an unknown
// source, `all` / `sources`) are never rewritten: a toggle adds or removes only
// its own token, so they survive every edit (same principle as WhenBuilder).

import { useEffect, useState } from "react";
import type { AttrPatch } from "../ast.ts";
import type { RefreshItems, RefreshNode, StatusloomNode } from "../types.ts";
import type { SourceEntry } from "../sourcesApi.ts";
import { t, useLang } from "../i18n.ts";

export const REFRESH_TIMEOUT_MIN = 1;
export const REFRESH_TIMEOUT_MAX = 3000;
export const REFRESH_TIMEOUT_DEFAULT = 2000;

interface Props {
    root: StatusloomNode;
    readOnly: boolean;
    // null until a parse reported them; only `usage` is offered then.
    refreshItems: RefreshItems | null;
    // Defined sources (sources.json); used to offer only command/http ones.
    sources: readonly SourceEntry[];
    onPatchRefresh: (patch: AttrPatch) => void;
}

// Split a wait attribute into its tokens (whitespace-separated, duplicates
// collapsed keeping the first occurrence — the backend does the same).
export function waitTokens(wait: string | undefined): string[] {
    const out: string[] = [];
    for (const tok of (wait ?? "").split(/\s+/)) {
        if (tok !== "" && !out.includes(tok)) {
            out.push(tok);
        }
    }
    return out;
}

// The tokens the panel offers as toggles for this document, in display order.
export function offeredTokens(
    items: RefreshItems | null,
    sources: readonly SourceEntry[],
): string[] {
    const out: string[] = [];
    if (items?.git) {
        out.push("git");
    }
    if (items?.transcript) {
        out.push("transcript");
    }
    out.push("usage");
    for (const id of items?.sources ?? []) {
        const def = sources.find((s) => s.id === id);
        if (def && (def.kind === "command" || def.kind === "http")) {
            out.push(`source:${id}`);
        }
    }
    return out;
}

// Toggle one token in the wait list, leaving every other token (and their
// order) untouched. Returns the new attribute value (undefined = remove).
export function toggleWaitToken(wait: string | undefined, token: string): string | undefined {
    const cur = waitTokens(wait);
    const next = cur.includes(token) ? cur.filter((x) => x !== token) : [...cur, token];
    return next.length > 0 ? next.join(" ") : undefined;
}

// Parse the timeout input: "" = unset (default), an integer in range = that
// value, anything else = invalid (null; never written to the document).
export function parseTimeout(text: string): number | undefined | null {
    if (text.trim() === "") {
        return undefined;
    }
    if (!/^\d+$/.test(text.trim())) {
        return null;
    }
    const n = Number(text.trim());
    return n >= REFRESH_TIMEOUT_MIN && n <= REFRESH_TIMEOUT_MAX ? n : null;
}

export function RefreshSettings({ root, readOnly, refreshItems, sources, onPatchRefresh }: Props) {
    const lang = useLang();
    const node: RefreshNode = root.refresh ?? { id: "refresh", kind: "refresh" };
    const tokens = waitTokens(node.wait);
    const offered = offeredTokens(refreshItems, sources);
    const hasAll = tokens.includes("all");
    const hasSources = tokens.includes("sources");
    // Tokens present but not offered as toggles; kept verbatim.
    const kept = tokens.filter(
        (tok) => !offered.includes(tok) && tok !== "all" && tok !== "sources",
    );

    // The timeout input keeps its own text so an out-of-range value can be
    // typed (and flagged) without ever reaching the document. It follows the
    // document whenever the document's value differs from what the text means.
    const timeout = node["timeout-ms"];
    const [timeoutText, setTimeoutText] = useState(timeout === undefined ? "" : String(timeout));
    useEffect(() => {
        setTimeoutText((text) => {
            const p = parseTimeout(text);
            if (p !== null && p === timeout) {
                return text;
            }
            return timeout === undefined ? "" : String(timeout);
        });
    }, [timeout]);
    const timeoutInvalid = parseTimeout(timeoutText) === null;

    const waitOn = node["wait-on"] ?? "cold";

    return (
        <fieldset className="props-body refresh-settings" disabled={readOnly}>
            <div className="field">
                <label>Wait for</label>
                <div className="view-toggle refresh-chips" data-testid="setting-refresh-wait">
                    {offered.map((tok) => {
                        const covered = hasAll || (hasSources && tok.startsWith("source:"));
                        const on = covered || tokens.includes(tok);
                        return (
                            <button
                                key={tok}
                                type="button"
                                className={on ? "on" : ""}
                                aria-pressed={on}
                                disabled={covered}
                                title={covered ? t(lang, "refreshCovered") : undefined}
                                data-testid={`refresh-chip-${tok}`}
                                onClick={() => onPatchRefresh({ wait: toggleWaitToken(node.wait, tok) })}
                            >
                                {tok}
                            </button>
                        );
                    })}
                    {(["all", "sources"] as const)
                        .filter((tok) => tokens.includes(tok))
                        .map((tok) => (
                            <button
                                key={tok}
                                type="button"
                                className="on"
                                aria-pressed={true}
                                disabled
                                title={t(lang, "refreshReadOnlyToken")}
                                data-testid={`refresh-chip-${tok}`}
                            >
                                {tok}
                            </button>
                        ))}
                </div>
            </div>
            {kept.length > 0 && (
                <p className="hint" data-testid="setting-refresh-kept">
                    {t(lang, "refreshKeptTokens")} <code>{kept.join(" ")}</code>
                </p>
            )}
            <div className="field">
                <label htmlFor="setting-refresh-timeout">Timeout (ms)</label>
                <input
                    id="setting-refresh-timeout"
                    type="number"
                    min={REFRESH_TIMEOUT_MIN}
                    max={REFRESH_TIMEOUT_MAX}
                    step={1}
                    placeholder={String(REFRESH_TIMEOUT_DEFAULT)}
                    data-testid="setting-refresh-timeout"
                    aria-invalid={timeoutInvalid}
                    value={timeoutText}
                    onChange={(e) => {
                        const text = e.target.value;
                        setTimeoutText(text);
                        const p = parseTimeout(text);
                        if (p !== null && p !== timeout) {
                            onPatchRefresh({ "timeout-ms": p });
                        }
                    }}
                />
                <span className="hint" data-testid="setting-refresh-timeout-range">
                    {REFRESH_TIMEOUT_MIN}–{REFRESH_TIMEOUT_MAX} (default {REFRESH_TIMEOUT_DEFAULT})
                </span>
            </div>
            {timeoutInvalid && (
                <p className="inline-error" role="alert" data-testid="setting-refresh-timeout-error">
                    {t(lang, "refreshTimeoutInvalid")}
                </p>
            )}
            <div className="field">
                <label>Wait on</label>
                <div className="view-toggle" data-testid="setting-refresh-wait-on">
                    {(["cold", "due"] as const).map((v) => (
                        <button
                            key={v}
                            type="button"
                            className={waitOn === v ? "on" : ""}
                            aria-pressed={waitOn === v}
                            data-testid={`refresh-wait-on-${v}`}
                            onClick={() =>
                                waitOn !== v &&
                                onPatchRefresh({ "wait-on": v === "cold" ? undefined : v })
                            }
                        >
                            {v}
                        </button>
                    ))}
                </div>
            </div>
        </fieldset>
    );
}
