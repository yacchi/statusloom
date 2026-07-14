// Kind-specific property editors for the selected AST node. Every edit is an
// attribute patch (undefined removes the attribute, matching the wire
// contract where unset attributes are absent) that App.tsx turns into an AST
// update + serialize round trip. `when` expressions are free text; their
// diagnostics surface through the DSL editor's diagnostics list after the
// round trip.

import type { Theme } from "../ansi.ts";
import type { AttrPatch } from "../ast.ts";
import { pickDescription, t, useLang, type Lang } from "../i18n.ts";
import type {
    AstNode,
    ColorRuleNode,
    CommonAttrs,
    FieldCatalogEntry,
    FieldNode,
    FlexNode,
    Metric,
    TextNode,
} from "../types.ts";
import { ColorPicker } from "./ColorPicker.tsx";
import { HelpTip } from "./HelpTip.tsx";

interface Props {
    node: AstNode | null;
    fields: FieldCatalogEntry[];
    metrics: Metric[];
    theme: Theme;
    readOnly: boolean;
    onPatch: (patch: AttrPatch) => void;
    onRemove: () => void;
}

// ---- small shared editors ----

function TextAttr({
    label,
    value,
    helpKey,
    testid,
    placeholder,
    onChange,
}: {
    label: string;
    value: string | undefined;
    helpKey?: Parameters<typeof HelpTip>[0]["k"];
    testid?: string;
    placeholder?: string;
    onChange: (v: string | undefined) => void;
}) {
    return (
        <div className="field">
            <label>
                {label} {helpKey ? <HelpTip k={helpKey} /> : null}
            </label>
            <input
                type="text"
                className="grow"
                data-testid={testid}
                placeholder={placeholder}
                value={value ?? ""}
                onChange={(e) => onChange(e.target.value === "" ? undefined : e.target.value)}
            />
        </div>
    );
}

// Tri-state boolean attribute: unset (inherit) / on / off.
function TriBoolAttr({
    label,
    value,
    testid,
    onChange,
}: {
    label: string;
    value: boolean | undefined;
    testid?: string;
    onChange: (v: boolean | undefined) => void;
}) {
    const cur = value === undefined ? "" : value ? "true" : "false";
    return (
        <label className="tri-bool">
            <span>{label}</span>
            <select
                data-testid={testid}
                value={cur}
                onChange={(e) => {
                    const v = e.target.value;
                    onChange(v === "" ? undefined : v === "true");
                }}
            >
                <option value="">inherit</option>
                <option value="true">on</option>
                <option value="false">off</option>
            </select>
        </label>
    );
}

// ---- common attribute sections ----

function DecorationSection({
    node,
    theme,
    onPatch,
}: {
    node: CommonAttrs;
    theme: Theme;
    onPatch: (patch: AttrPatch) => void;
}) {
    return (
        <>
            <div className="field color-field">
                <label>
                    Color <HelpTip k="helpColor" />
                </label>
                <ColorPicker
                    color={node.color}
                    previewTheme={theme}
                    onChange={(color) => onPatch({ color: color === "" ? undefined : color })}
                />
            </div>
            <div className="field color-field">
                <label>
                    Background <HelpTip k="helpBackground" />
                </label>
                <ColorPicker
                    color={node.background}
                    previewTheme={theme}
                    onChange={(background) =>
                        onPatch({ background: background === "" ? undefined : background })
                    }
                />
            </div>
            <div className="field">
                <label>
                    Style <HelpTip k="helpDecorations" />
                </label>
                <div className="tri-bool-grid">
                    <TriBoolAttr
                        label="bold"
                        value={node.bold}
                        testid="attr-bold"
                        onChange={(bold) => onPatch({ bold })}
                    />
                    <TriBoolAttr
                        label="dim"
                        value={node.dim}
                        onChange={(dim) => onPatch({ dim })}
                    />
                    <TriBoolAttr
                        label="italic"
                        value={node.italic}
                        onChange={(italic) => onPatch({ italic })}
                    />
                    <TriBoolAttr
                        label="underline"
                        value={node.underline}
                        onChange={(underline) => onPatch({ underline })}
                    />
                    <TriBoolAttr
                        label="strikethrough"
                        value={node.strikethrough}
                        onChange={(strikethrough) => onPatch({ strikethrough })}
                    />
                </div>
            </div>
        </>
    );
}

function BoxSection({
    node,
    onPatch,
}: {
    node: CommonAttrs;
    onPatch: (patch: AttrPatch) => void;
}) {
    // `padding` sets both sides; individual left/right override. Editing the
    // "both" input clears the individual keys and vice versa.
    const both = node.padding;
    const left = node["padding-left"] ?? both;
    const right = node["padding-right"] ?? both;
    return (
        <>
            <TextAttr
                label="Prefix"
                value={node.prefix}
                helpKey="helpPrefixSuffix"
                testid="attr-prefix"
                onChange={(prefix) => onPatch({ prefix })}
            />
            <TextAttr
                label="Suffix"
                value={node.suffix}
                testid="attr-suffix"
                onChange={(suffix) => onPatch({ suffix })}
            />
            <div className="field">
                <label>
                    Padding <HelpTip k="helpPadding" />
                </label>
                <span className="pad-label">L</span>
                <input
                    type="number"
                    min={0}
                    data-testid="attr-padding-left"
                    value={left ?? ""}
                    onChange={(e) => {
                        const v = e.target.value === "" ? undefined : Number(e.target.value);
                        applyPadding(onPatch, v, right);
                    }}
                />
                <span className="pad-label">R</span>
                <input
                    type="number"
                    min={0}
                    data-testid="attr-padding-right"
                    value={right ?? ""}
                    onChange={(e) => {
                        const v = e.target.value === "" ? undefined : Number(e.target.value);
                        applyPadding(onPatch, left, v);
                    }}
                />
            </div>
        </>
    );
}

// Normalize a left/right pair into the wire shape: equal values collapse to
// `padding`, otherwise the individual keys are used.
function applyPadding(
    onPatch: (patch: AttrPatch) => void,
    left: number | undefined,
    right: number | undefined,
) {
    if (left !== undefined && left === right) {
        onPatch({ padding: left, "padding-left": undefined, "padding-right": undefined });
    } else {
        onPatch({
            padding: undefined,
            "padding-left": left,
            "padding-right": right,
        });
    }
}

function VisibilitySection({
    node,
    fields,
    metrics,
    lang,
    onPatch,
}: {
    node: CommonAttrs;
    fields: FieldCatalogEntry[];
    metrics: Metric[];
    lang: Lang;
    onPatch: (patch: AttrPatch) => void;
}) {
    return (
        <div className="subsection">
            <div className="field">
                <label>
                    Optional <HelpTip k="helpOptional" />
                </label>
                <select
                    data-testid="attr-optional"
                    value={node.optional ?? ""}
                    onChange={(e) =>
                        onPatch({ optional: e.target.value === "" ? undefined : e.target.value })
                    }
                >
                    <option value="">(always)</option>
                    {fields.map((f) => (
                        <option key={f.name} value={f.name}>
                            {f.displayName}
                        </option>
                    ))}
                </select>
            </div>
            <TextAttr
                label="When"
                value={node.when}
                helpKey="helpWhen"
                testid="attr-when"
                placeholder="e.g. context-percent ge 80"
                onChange={(when) => onPatch({ when })}
            />
            {metrics.length > 0 ? (
                <p className="hint">
                    {metrics.map((m) => m.name).join(" · ")}
                </p>
            ) : null}
            {node.when ? <p className="hint">{t(lang, "helpWhen")}</p> : null}
        </div>
    );
}

// ---- color rules (fields only) ----

function ColorRulesSection({
    node,
    theme,
    lang,
    selfMetric,
    onPatch,
}: {
    node: FieldNode;
    theme: Theme;
    lang: Lang;
    selfMetric: string | undefined;
    onPatch: (patch: AttrPatch) => void;
}) {
    const rules = node.colorRules ?? [];
    const setRules = (next: ColorRuleNode[]) =>
        onPatch({ colorRules: next.length > 0 ? next : undefined });

    const defaultWhen = selfMetric ? "self ge 80" : "";
    const addRule = () =>
        setRules([...rules, { id: "", kind: "color-rule", when: defaultWhen, color: "yellow" }]);
    const removeRule = (i: number) => setRules(rules.filter((_, j) => j !== i));
    const patchRule = (i: number, patch: Partial<ColorRuleNode>) =>
        setRules(rules.map((r, j) => (j === i ? { ...r, ...patch } : r)));
    const moveRule = (i: number, dir: -1 | 1) => {
        const j = i + dir;
        if (j < 0 || j >= rules.length) {
            return;
        }
        const next = [...rules];
        [next[i], next[j]] = [next[j], next[i]];
        setRules(next);
    };

    return (
        <div className="subsection">
            <div className="field">
                <label>
                    Color rules <HelpTip k="helpColorRules" />
                </label>
                <button className="link-button" data-testid="colorrule-add" onClick={addRule}>
                    {t(lang, "addRule")}
                </button>
            </div>

            {rules.length === 0 ? (
                <p className="hint">{t(lang, "colorRulesEmpty")}</p>
            ) : (
                <ul className="rules-list">
                    {rules.map((rule, i) => (
                        <li key={i} className="rule-row" data-testid={`colorrule-${i}`}>
                            <div className="rule-head">
                                <input
                                    type="text"
                                    className="grow"
                                    data-testid={`colorrule-when-${i}`}
                                    placeholder="self ge 80"
                                    value={rule.when ?? ""}
                                    onChange={(e) => patchRule(i, { when: e.target.value })}
                                />
                                <button
                                    className="link-button"
                                    data-testid={`colorrule-up-${i}`}
                                    disabled={i === 0}
                                    onClick={() => moveRule(i, -1)}
                                >
                                    ↑
                                </button>
                                <button
                                    className="link-button"
                                    data-testid={`colorrule-down-${i}`}
                                    disabled={i === rules.length - 1}
                                    onClick={() => moveRule(i, 1)}
                                >
                                    ↓
                                </button>
                                <button
                                    className="link-button danger-text"
                                    data-testid={`colorrule-remove-${i}`}
                                    onClick={() => removeRule(i)}
                                >
                                    ✕
                                </button>
                            </div>
                            <ColorPicker
                                color={rule.color}
                                previewTheme={theme}
                                onChange={(color) => patchRule(i, { color })}
                            />
                        </li>
                    ))}
                </ul>
            )}
        </div>
    );
}

// ---- kind-specific top sections ----

function FieldSection({
    node,
    entry,
    onPatch,
}: {
    node: FieldNode;
    entry: FieldCatalogEntry | undefined;
    onPatch: (patch: AttrPatch) => void;
}) {
    const formats = entry?.formats ?? [];
    return (
        <>
            {formats.length > 0 ? (
                <div className="field">
                    <label>
                        Format <HelpTip k="helpFormat" />
                    </label>
                    <select
                        data-testid="attr-format"
                        value={node.format ?? ""}
                        onChange={(e) =>
                            onPatch({
                                format: e.target.value === "" ? undefined : e.target.value,
                                // Formatter-specific attrs are dropped with it.
                                ...(e.target.value === "" && {
                                    precision: undefined,
                                    currency: undefined,
                                }),
                            })
                        }
                    >
                        <option value="">(default)</option>
                        {formats.map((f) => (
                            <option key={f} value={f}>
                                {f}
                            </option>
                        ))}
                    </select>
                </div>
            ) : null}
            {node.format ? (
                <TextAttr
                    label="Precision"
                    value={node.precision}
                    helpKey="helpPrecision"
                    testid="attr-precision"
                    onChange={(precision) => onPatch({ precision })}
                />
            ) : null}
            {node.format === "currency" ? (
                <TextAttr
                    label="Currency"
                    value={node.currency}
                    testid="attr-currency"
                    onChange={(currency) => onPatch({ currency })}
                />
            ) : null}
            <div className="field">
                <label>
                    Raw value <HelpTip k="helpRawValue" />
                </label>
                <input
                    type="checkbox"
                    data-testid="attr-raw"
                    checked={node.raw === true}
                    onChange={(e) => onPatch({ raw: e.target.checked ? true : undefined })}
                />
            </div>
            {entry?.linkable ? (
                <div className="field">
                    <label>
                        Hyperlink <HelpTip k="helpHyperlink" />
                    </label>
                    <input
                        type="checkbox"
                        data-testid="attr-hyperlink"
                        checked={node.hyperlink === true}
                        onChange={(e) =>
                            onPatch({ hyperlink: e.target.checked ? true : undefined })
                        }
                    />
                </div>
            ) : null}
        </>
    );
}

function TextSection({
    node,
    onPatch,
}: {
    node: TextNode;
    onPatch: (patch: AttrPatch) => void;
}) {
    const changeRole = (role: string) => {
        if (role === "separator") {
            onPatch({
                role,
                value: "",
                padding: undefined,
                "padding-left": undefined,
                "padding-right": undefined,
            });
            return;
        }
        onPatch({ role: undefined });
    };

    return (
        <>
            <div className="field">
                <label>Text</label>
                <input
                    type="text"
                    className="grow"
                    data-testid="attr-value"
                    value={node.value}
                    onChange={(e) => onPatch({ value: e.target.value })}
                />
            </div>
            <div className="field">
                <label>
                    Role <HelpTip k="helpRole" />
                </label>
                <select
                    data-testid="attr-role"
                    value={node.role ?? ""}
                    onChange={(e) => changeRole(e.target.value)}
                >
                    <option value="">(plain text)</option>
                    <option value="separator">separator</option>
                </select>
            </div>
        </>
    );
}

function FlexSection({
    node,
    onPatch,
}: {
    node: FlexNode;
    onPatch: (patch: AttrPatch) => void;
}) {
    // "" / absent = full; "full-minus-<N>" = leave N columns free.
    const m = node.size ? /^full-minus-(\d+)$/.exec(node.size) : null;
    const kind = m ? "full-minus" : "full";
    const n = m ? Number(m[1]) : 40;
    return (
        <div className="field">
            <label>
                Size <HelpTip k="helpFlex" />
            </label>
            <select
                data-testid="attr-flex-kind"
                value={kind}
                onChange={(e) =>
                    onPatch({
                        size: e.target.value === "full" ? undefined : `full-minus-${n}`,
                    })
                }
            >
                <option value="full">full</option>
                <option value="full-minus">full-minus-N</option>
            </select>
            {kind === "full-minus" ? (
                <input
                    type="number"
                    min={0}
                    data-testid="attr-flex-n"
                    value={n}
                    onChange={(e) =>
                        onPatch({ size: `full-minus-${Math.max(0, Number(e.target.value))}` })
                    }
                />
            ) : null}
        </div>
    );
}

// ---- the panel ----

const KIND_TITLES: Record<string, string> = {
    field: "Field",
    text: "Text",
    "raw-text": "Raw text",
    span: "Span",
    flex: "Flex",
    line: "Line",
    comment: "Comment",
};

export function PropertiesPanel({
    node,
    fields,
    metrics,
    theme,
    readOnly,
    onPatch,
    onRemove,
}: Props) {
    const lang = useLang();

    if (!node || !(node.kind in KIND_TITLES)) {
        return (
            <div className="panel">
                <h2>Properties</h2>
                <p className="hint">{t(lang, "propertiesEmptyHint")}</p>
            </div>
        );
    }

    const entry =
        node.kind === "field" ? fields.find((f) => f.name === node.name) : undefined;
    const title =
        node.kind === "field"
            ? (entry?.displayName ?? node.name ?? "field")
            : KIND_TITLES[node.kind];
    const description = entry ? pickDescription(entry, lang) : "";

    const hasCommon =
        node.kind === "field" ||
        node.kind === "text" ||
        node.kind === "span" ||
        node.kind === "line";
    const removable = node.kind !== "line";

    return (
        <div className={"panel" + (readOnly ? " readonly" : "")}>
            <h2>Properties</h2>
            <div className="field">
                <label>Node</label>
                <span data-testid="props-title">{title}</span>
                <span className="hint">{node.kind}</span>
            </div>
            {description ? <p className="hint">{description}</p> : null}

            <fieldset className="props-body" disabled={readOnly}>
                {node.kind === "field" ? (
                    <FieldSection node={node} entry={entry} onPatch={onPatch} />
                ) : null}
                {node.kind === "text" ? <TextSection node={node} onPatch={onPatch} /> : null}
                {node.kind === "raw-text" || node.kind === "comment" ? (
                    <div className="field">
                        <label>Text</label>
                        <input
                            type="text"
                            className="grow"
                            data-testid="attr-value"
                            value={node.value}
                            onChange={(e) => onPatch({ value: e.target.value })}
                        />
                    </div>
                ) : null}
                {node.kind === "flex" ? <FlexSection node={node} onPatch={onPatch} /> : null}

                {hasCommon ? (
                    <>
                        <DecorationSection
                            node={node as CommonAttrs}
                            theme={theme}
                            onPatch={onPatch}
                        />
                        <BoxSection node={node as CommonAttrs} onPatch={onPatch} />
                        <VisibilitySection
                            node={node as CommonAttrs}
                            fields={fields}
                            metrics={metrics}
                            lang={lang}
                            onPatch={onPatch}
                        />
                    </>
                ) : null}

                {node.kind === "field" ? (
                    <ColorRulesSection
                        node={node}
                        theme={theme}
                        lang={lang}
                        selfMetric={entry?.selfMetric}
                        onPatch={onPatch}
                    />
                ) : null}

                {removable ? (
                    <div className="panel-actions">
                        <button className="danger" data-testid="props-remove" onClick={onRemove}>
                            Remove
                        </button>
                        <span className="hint">{t(lang, "removeHint")}</span>
                    </div>
                ) : null}
            </fieldset>
        </div>
    );
}
