// Editor for a `when` condition, built out of pickers instead of a bare text
// box. Used by VariantCard for a <variant>'s candidacy gate.
//
// Three states, in the order a user meets them:
//
//   1. No condition (the common case — a variant is usually unconditional):
//      just an "add condition" button. Nothing takes up room until asked for.
//   2. A single comparison: metric / operator / value pickers. The value is a
//      <select> when the metric declares a closed set of values
//      (MetricDef.Values, e.g. account-type), a text box otherwise.
//   3. Anything the DSL accepts but the builder cannot represent (and/or, not,
//      parentheses, several comparisons): the raw expression stays editable as
//      text, with a note saying why. The builder NEVER rewrites an expression it
//      did not fully parse (see whenExpr.ts).
//
// Quoting is handled here: a string-valued metric compares against a quoted
// literal, a number or boolean against a bare one. Switching the metric between
// those two kinds therefore re-quotes the value.
//
// IMPORTANT — an INCOMPLETE condition is never written to the document. Two
// reasons, and both have already bitten this editor (see the fix for the color
// text box, commit cfa499e):
//
//   - A half-built condition has no valid DSL form. applyAstEdit ignores every
//     later edit while the document is invalid, so writing "account-type eq "
//     would latch the whole editor.
//   - Writing "" instead (no condition) would immediately erase the metric the
//     user just picked, since the pickers render from the document. That is
//     exactly what made the selects look unusable: choosing a metric with no
//     known values left the value empty, which round-tripped as "no condition".
//
// So a partial condition lives in local state until it is complete. The raw
// (advanced) box commits on blur/Enter for the same reason — its intermediate
// keystrokes are not valid expressions either.

import { useEffect, useRef, useState } from "react";
import { t, useLang } from "../i18n.ts";
import type { Metric } from "../types.ts";
import {
    formatSimpleWhen,
    parseSimpleWhen,
    TEXT_WHEN_OPS,
    WHEN_OPS,
    type SimpleWhen,
    type WhenOp,
} from "../whenExpr.ts";
import { HelpTip } from "./HelpTip.tsx";

interface Props {
    // The current expression, or undefined/"" when there is no condition.
    value: string | undefined;
    metrics: Metric[];
    readOnly: boolean;
    // Prefix for this instance's data-testid attributes, so several builders can
    // coexist (one per variant card).
    testidPrefix: string;
    // Called with the new expression, or "" to remove the condition entirely.
    onChange: (when: string) => void;
}

export function WhenBuilder({ value, metrics, readOnly, testidPrefix, onChange }: Props) {
    const lang = useLang();
    // Only meaningful while there is no expression yet: it keeps the freshly
    // opened (but still empty) builder on screen. Any real value makes the
    // builder visible regardless.
    const [opened, setOpened] = useState(false);
    // The condition being built while it is not yet complete enough to write
    // (see the doc comment). null = show whatever the document holds.
    const [draft, setDraft] = useState<SimpleWhen | null>(null);
    // The last expression this builder wrote, so an edit from ELSEWHERE (undo,
    // the DSL editor, an incoming shared draft) discards a stale local draft
    // instead of masking the new value.
    const emitted = useRef<string | null>(null);
    useEffect(() => {
        if (emitted.current !== null && (value ?? "") === emitted.current) {
            return; // our own write coming back
        }
        emitted.current = null;
        setDraft(null);
    }, [value]);

    const has = (value ?? "") !== "";

    if (!has && !opened && draft === null) {
        return (
            <button
                type="button"
                className="when-add"
                data-testid={`${testidPrefix}-add`}
                disabled={readOnly}
                onClick={() => setOpened(true)}
            >
                + {t(lang, "whenAdd")}
            </button>
        );
    }

    const simple = draft ?? parseSimpleWhen(value);
    const clear = () => {
        setOpened(false);
        setDraft(null);
        emitted.current = "";
        onChange("");
    };
    // Commit a complete condition, keep an incomplete one local.
    const commit = (next: SimpleWhen) => {
        const expr = formatSimpleWhen(next);
        if (expr === "") {
            setDraft(next);
            return;
        }
        setDraft(null);
        emitted.current = expr;
        onChange(expr);
    };

    return (
        <div className="when-builder">
            {/* helpWhenVariant, not the properties panel's helpWhen: that text
                advertises and/or/not and parentheses, which this builder has no
                input for. It says so instead of implying a control that is not
                here. */}
            <span className="when-label">
                {t(lang, "whenLabel")} <HelpTip k="helpWhenVariant" />
            </span>
            {simple !== null || !has ? (
                <SimpleEditor
                    current={simple}
                    metrics={metrics}
                    readOnly={readOnly}
                    testidPrefix={testidPrefix}
                    onCommit={commit}
                />
            ) : (
                <RawEditor
                    value={value ?? ""}
                    readOnly={readOnly}
                    testidPrefix={testidPrefix}
                    onCommit={(expr) => {
                        emitted.current = expr;
                        onChange(expr);
                    }}
                />
            )}
            <button
                type="button"
                className="when-clear"
                data-testid={`${testidPrefix}-clear`}
                title={t(lang, "whenClear")}
                aria-label={t(lang, "whenClear")}
                disabled={readOnly}
                onClick={clear}
            >
                ✕
            </button>
        </div>
    );
}

// The metric/operator/value pickers. `current` is null for a builder that was
// just opened and has nothing chosen yet.
function SimpleEditor({
    current,
    metrics,
    readOnly,
    testidPrefix,
    onCommit,
}: {
    current: SimpleWhen | null;
    metrics: Metric[];
    readOnly: boolean;
    testidPrefix: string;
    // Receives the whole condition; the parent decides whether it is complete
    // enough to write to the document.
    onCommit: (next: SimpleWhen) => void;
}) {
    const lang = useLang();
    const metricName = current?.metric ?? "";
    const def = metrics.find((m) => m.name === metricName);
    // An expression written in the DSL editor may name a metric this build's
    // catalog does not know; keep it selectable rather than silently swapping it.
    const unknownMetric = metricName !== "" && def === undefined;
    const isText = def?.text === true;
    const ops = isText ? TEXT_WHEN_OPS : WHEN_OPS;
    const op: WhenOp = current?.op ?? "eq";
    const val = current?.value ?? "";

    const emit = (next: Partial<SimpleWhen>) => {
        onCommit({
            metric: next.metric ?? metricName,
            op: next.op ?? op,
            value: next.value ?? val,
            quoted: next.quoted ?? current?.quoted ?? isText,
        });
    };

    return (
        <>
            <select
                className="when-metric"
                data-testid={`${testidPrefix}-metric`}
                value={metricName}
                disabled={readOnly}
                onChange={(e) => {
                    const nextDef = metrics.find((m) => m.name === e.target.value);
                    const nextIsText = nextDef?.text === true;
                    emit({
                        metric: e.target.value,
                        // Ordering operators are invalid for a string metric.
                        op: nextIsText && !TEXT_WHEN_OPS.includes(op) ? "eq" : op,
                        // A string metric's value is quoted, a numeric one's is not.
                        quoted: nextIsText,
                        // The old value belongs to the old metric; a closed-set
                        // metric starts at its first known value, others empty.
                        value: nextDef?.values?.[0] ?? "",
                    });
                }}
            >
                <option value="">{t(lang, "whenPickMetric")}</option>
                {metrics.map((m) => (
                    <option key={m.name} value={m.name}>
                        {m.displayName}
                    </option>
                ))}
                {unknownMetric ? <option value={metricName}>{metricName}</option> : null}
            </select>
            <select
                className="when-op"
                data-testid={`${testidPrefix}-op`}
                value={op}
                disabled={readOnly}
                onChange={(e) => emit({ op: e.target.value as WhenOp })}
            >
                {ops.map((o) => (
                    <option key={o} value={o}>
                        {o}
                    </option>
                ))}
            </select>
            {def?.values && def.values.length > 0 ? (
                <select
                    className="when-value"
                    data-testid={`${testidPrefix}-value`}
                    value={val}
                    disabled={readOnly}
                    onChange={(e) => emit({ value: e.target.value })}
                >
                    {/* An out-of-catalog value stays selected: Values is a list
                        of KNOWN values, not an exhaustive constraint. */}
                    {val !== "" && !def.values.includes(val) ? (
                        <option value={val}>{val}</option>
                    ) : null}
                    {def.values.map((v) => (
                        <option key={v} value={v}>
                            {v}
                        </option>
                    ))}
                </select>
            ) : (
                <input
                    type="text"
                    className="when-value"
                    data-testid={`${testidPrefix}-value`}
                    value={val}
                    placeholder={isText ? t(lang, "whenValueText") : t(lang, "whenValueNumber")}
                    disabled={readOnly}
                    onChange={(e) => emit({ value: e.target.value })}
                />
            )}
        </>
    );
}

// Fallback for an expression the builder cannot represent: edit it as text, and
// say why the pickers are absent.
//
// It commits on blur / Enter rather than per keystroke: the intermediate states
// of typing an expression are invalid DSL, and applyAstEdit latches on the first
// invalid document (see this file's header note).
function RawEditor({
    value,
    readOnly,
    testidPrefix,
    onCommit,
}: {
    value: string;
    readOnly: boolean;
    testidPrefix: string;
    onCommit: (when: string) => void;
}) {
    const lang = useLang();
    const [text, setText] = useState(value);
    // Follow the document when it changes from elsewhere (undo, DSL editor).
    useEffect(() => setText(value), [value]);
    const commit = () => {
        if (text !== value) {
            onCommit(text);
        }
    };
    return (
        <>
            <input
                type="text"
                className="when-raw"
                data-testid={`${testidPrefix}-raw`}
                value={text}
                disabled={readOnly}
                onChange={(e) => setText(e.target.value)}
                onBlur={commit}
                onKeyDown={(e) => {
                    if (e.key === "Enter") {
                        commit();
                    }
                }}
            />
            <span className="when-advanced-note">{t(lang, "whenAdvancedCommit")}</span>
        </>
    );
}
