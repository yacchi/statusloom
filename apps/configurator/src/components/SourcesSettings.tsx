// External source definitions (<configDir>/sources.json): list, add/edit,
// delete, and a one-off test. These live on the local machine, never in the
// document, so they are not subject to the readOnly flag of the document.
//
// This renders only the body (no modal chrome); it is hosted inside
// SettingsModal, which supplies the title and Close button.

import { useEffect, useState } from "react";
import { t, useLang, type MessageKey } from "../i18n.ts";
import type {
    SourceDefinition,
    SourceEntry,
    SourceKind,
    SourceWarning,
    SourcesApi,
} from "../sourcesApi.ts";
import { HelpTip } from "./HelpTip.tsx";

interface Props {
    sourcesApi: SourcesApi;
    onChanged?: (sources: SourceEntry[]) => void;
}

interface SourceForm {
    id: string;
    kind: SourceKind;
    path: string;
    argv: string;
    url: string;
    allowRemote: boolean;
    ttl: string;
    staleAfter: string;
    timeout: string;
}

interface TestView {
    text: string;
    document: string;
}

const EMPTY_FORM: SourceForm = {
    id: "",
    kind: "file",
    path: "",
    argv: "",
    url: "",
    allowRemote: false,
    ttl: "",
    staleAfter: "",
    timeout: "",
};

const WARNING_KEYS: Record<SourceWarning, MessageKey> = {
    "allow-remote": "sourceWarnAllowRemote",
    "short-ttl": "sourceWarnShortTtl",
};

// The server reports absent numeric keys as 0; the form shows them as empty.
function numberText(n: number): string {
    return n === 0 ? "" : String(n);
}

function formFromEntry(entry: SourceEntry): SourceForm {
    return {
        id: entry.id,
        kind: entry.kind,
        path: entry.path,
        argv: entry.argv.join("\n"),
        url: entry.url,
        allowRemote: entry.allowRemote,
        ttl: numberText(entry.ttlSeconds),
        staleAfter: numberText(entry.staleAfterSeconds),
        timeout: numberText(entry.timeoutSeconds),
    };
}

// One argument per line. No shell parsing and no space splitting: an argument
// may contain spaces. CRLF line endings are tolerated; blank lines are dropped.
function parseArgv(text: string): string[] {
    return text
        .split("\n")
        .map((line) => (line.endsWith("\r") ? line.slice(0, -1) : line))
        .filter((line) => line !== "");
}

// Empty means "not set": the key is omitted so the server applies its default.
function optionalNumber(text: string): number | undefined {
    return text === "" ? undefined : Number(text);
}

// Build the PUT body with only the keys that apply to the chosen kind. Keys
// left over from a previously selected kind must not reach the server.
function buildDefinition(form: SourceForm): SourceDefinition {
    const def: SourceDefinition = { kind: form.kind };
    if (form.kind === "file") {
        def.path = form.path;
    }
    if (form.kind === "command") {
        def.argv = parseArgv(form.argv);
    }
    if (form.kind === "http") {
        def.url = form.url;
        if (form.allowRemote) {
            def.allowRemote = true;
        }
    }
    if (form.kind !== "file") {
        const ttl = optionalNumber(form.ttl);
        if (ttl !== undefined) {
            def.ttlSeconds = ttl;
        }
        const timeout = optionalNumber(form.timeout);
        if (timeout !== undefined) {
            def.timeoutSeconds = timeout;
        }
    }
    const staleAfter = optionalNumber(form.staleAfter);
    if (staleAfter !== undefined) {
        def.staleAfterSeconds = staleAfter;
    }
    return def;
}

function summaryOf(entry: SourceEntry): string {
    switch (entry.kind) {
        case "file":
            return entry.path;
        case "command":
            return entry.argv.join(" ");
        case "http":
            return entry.url;
    }
}

function messageOf(err: unknown): string {
    return err instanceof Error ? err.message : String(err);
}

export function SourcesSettings({ sourcesApi, onChanged }: Props) {
    const lang = useLang();
    const [sources, setSources] = useState<SourceEntry[]>([]);
    const [path, setPath] = useState<string | null>(null);
    const [loadError, setLoadError] = useState<string | null>(null);
    const [form, setForm] = useState<SourceForm>(EMPTY_FORM);
    const [formError, setFormError] = useState<string | null>(null);
    const [confirmingDelete, setConfirmingDelete] = useState(false);
    const [testSelect, setTestSelect] = useState("");
    const [testView, setTestView] = useState<TestView | null>(null);

    useEffect(() => {
        let cancelled = false;
        sourcesApi.listSources().then(
            (res) => {
                if (cancelled) {
                    return;
                }
                setSources(res.sources);
                setPath(res.path);
            },
            (err: unknown) => {
                if (!cancelled) {
                    setLoadError(messageOf(err));
                }
            },
        );
        return () => {
            cancelled = true;
        };
    }, [sourcesApi]);

    const id = form.id.trim();
    const isSaved = sources.some((s) => s.id === id);

    // Re-fetch after a successful write so the list reflects the stored state
    // (including server-computed warnings), then tell the parent.
    async function reload(): Promise<void> {
        const res = await sourcesApi.listSources();
        setSources(res.sources);
        setPath(res.path);
        setLoadError(null);
        onChanged?.(res.sources);
    }

    async function save(): Promise<void> {
        try {
            await sourcesApi.putSource(id, buildDefinition(form));
        } catch (err) {
            setFormError(messageOf(err));
            return;
        }
        setFormError(null);
        try {
            await reload();
        } catch (err) {
            setLoadError(messageOf(err));
        }
    }

    async function remove(): Promise<void> {
        try {
            await sourcesApi.deleteSource(id);
        } catch (err) {
            setFormError(messageOf(err));
            return;
        }
        setConfirmingDelete(false);
        setForm(EMPTY_FORM);
        setFormError(null);
        setTestView(null);
        try {
            await reload();
        } catch (err) {
            setLoadError(messageOf(err));
        }
    }

    async function runTest(): Promise<void> {
        try {
            const res = await sourcesApi.testSource(id, { select: testSelect });
            if (!res.ok) {
                setTestView({ text: res.error, document: "" });
                return;
            }
            if (!res.matched) {
                setTestView({ text: t(lang, "sourceNoMatch"), document: res.documentPreview });
                return;
            }
            setTestView({
                text: res.display !== "" ? res.display : JSON.stringify(res.value),
                document: res.documentPreview,
            });
        } catch (err) {
            setTestView({ text: messageOf(err), document: "" });
        }
    }

    function editEntry(entry: SourceEntry): void {
        setForm(formFromEntry(entry));
        setFormError(null);
        setConfirmingDelete(false);
        setTestView(null);
    }

    function newEntry(): void {
        setForm(EMPTY_FORM);
        setFormError(null);
        setConfirmingDelete(false);
        setTestView(null);
    }

    const showTtl = form.kind !== "file";
    const showTimeout = form.kind !== "file";
    const ttlSeconds = Number(form.ttl);
    const showShortTtl = showTtl && ttlSeconds > 0 && ttlSeconds < 10;

    return (
        <div data-testid="sources-settings">
            {path !== null && (
                <div className="hint" data-testid="sources-path">
                    {path}
                </div>
            )}
            {loadError !== null && (
                <div className="inline-error" data-testid="sources-error">
                    {loadError}
                </div>
            )}

            {sources.map((entry) => (
                <div key={entry.id} data-testid={`source-row-${entry.id}`}>
                    <strong>{entry.id}</strong> <span className="hint">{entry.kind}</span>{" "}
                    <span className="hint">{summaryOf(entry)}</span>{" "}
                    <button
                        type="button"
                        data-testid={`source-edit-${entry.id}`}
                        onClick={() => editEntry(entry)}
                    >
                        Edit
                    </button>
                    {entry.warnings.map((code) => (
                        <div
                            key={code}
                            className="inline-error"
                            data-testid={`source-warning-${entry.id}-${code}`}
                        >
                            {t(lang, WARNING_KEYS[code])}
                        </div>
                    ))}
                </div>
            ))}

            <div className="modal-actions">
                <button type="button" data-testid="source-new" onClick={newEntry}>
                    New
                </button>
            </div>

            <fieldset className="props-body">
                <div className="field">
                    <label htmlFor="source-form-id">Id</label>
                    <input
                        id="source-form-id"
                        type="text"
                        data-testid="source-form-id"
                        value={form.id}
                        onChange={(e) => setForm({ ...form, id: e.target.value })}
                    />
                </div>
                <div className="field">
                    <label htmlFor="source-form-kind">Kind</label>
                    <select
                        id="source-form-kind"
                        data-testid="source-form-kind"
                        value={form.kind}
                        onChange={(e) =>
                            setForm({ ...form, kind: e.target.value as SourceKind })
                        }
                    >
                        <option value="file">file</option>
                        <option value="command">command</option>
                        <option value="http">http</option>
                    </select>
                </div>

                {form.kind === "file" && (
                    <div className="field">
                        <label htmlFor="source-form-path">Path</label>
                        <input
                            id="source-form-path"
                            type="text"
                            data-testid="source-form-path"
                            value={form.path}
                            onChange={(e) => setForm({ ...form, path: e.target.value })}
                        />
                    </div>
                )}

                {form.kind === "command" && (
                    <div className="field">
                        <label htmlFor="source-form-argv">Argv (one argument per line)</label>
                        <textarea
                            id="source-form-argv"
                            data-testid="source-form-argv"
                            value={form.argv}
                            onChange={(e) => setForm({ ...form, argv: e.target.value })}
                        />
                    </div>
                )}

                {form.kind === "http" && (
                    <>
                        <div className="field">
                            <label htmlFor="source-form-url">URL</label>
                            <input
                                id="source-form-url"
                                type="text"
                                data-testid="source-form-url"
                                value={form.url}
                                onChange={(e) => setForm({ ...form, url: e.target.value })}
                            />
                        </div>
                        <div className="field">
                            <label htmlFor="source-form-allow-remote">Allow remote</label>
                            <input
                                id="source-form-allow-remote"
                                type="checkbox"
                                data-testid="source-form-allow-remote"
                                checked={form.allowRemote}
                                onChange={(e) =>
                                    setForm({ ...form, allowRemote: e.target.checked })
                                }
                            />
                        </div>
                    </>
                )}

                {showTtl && (
                    <div className="field">
                        <label htmlFor="source-form-ttl">
                            TTL (s) <HelpTip k="helpSourceTtl" />
                        </label>
                        <input
                            id="source-form-ttl"
                            type="number"
                            min={0}
                            data-testid="source-form-ttl"
                            value={form.ttl}
                            onChange={(e) => setForm({ ...form, ttl: e.target.value })}
                        />
                        {showShortTtl && (
                            <div className="inline-error" data-testid="source-form-warning-short-ttl">
                                {t(lang, "sourceWarnShortTtl")}
                            </div>
                        )}
                    </div>
                )}

                <div className="field">
                    <label htmlFor="source-form-stale-after">Stale after (s)</label>
                    <input
                        id="source-form-stale-after"
                        type="number"
                        min={0}
                        data-testid="source-form-stale-after"
                        value={form.staleAfter}
                        onChange={(e) => setForm({ ...form, staleAfter: e.target.value })}
                    />
                </div>

                {showTimeout && (
                    <div className="field">
                        <label htmlFor="source-form-timeout">Timeout (s)</label>
                        <input
                            id="source-form-timeout"
                            type="number"
                            min={0}
                            data-testid="source-form-timeout"
                            value={form.timeout}
                            onChange={(e) => setForm({ ...form, timeout: e.target.value })}
                        />
                    </div>
                )}
            </fieldset>

            {formError !== null && (
                <div className="inline-error" data-testid="source-form-error">
                    {formError}
                </div>
            )}

            <div className="modal-actions">
                <button
                    type="button"
                    className="primary"
                    data-testid="source-save"
                    disabled={id === ""}
                    onClick={() => void save()}
                >
                    Save
                </button>
                {isSaved && !confirmingDelete && (
                    <button
                        type="button"
                        data-testid="source-delete"
                        onClick={() => setConfirmingDelete(true)}
                    >
                        Delete
                    </button>
                )}
            </div>

            {confirmingDelete && (
                <div className="modal-actions" data-testid="source-delete-confirm">
                    <div className="hint">{t(lang, "sourceDeleteConfirm")}</div>
                    <button type="button" onClick={() => setConfirmingDelete(false)}>
                        Cancel
                    </button>
                    <button
                        type="button"
                        className="primary"
                        data-testid="source-delete-confirm-yes"
                        onClick={() => void remove()}
                    >
                        Delete
                    </button>
                </div>
            )}

            <fieldset className="props-body">
                <div className="field">
                    <label htmlFor="source-test-select">
                        Test select <HelpTip k="helpSourceTest" />
                    </label>
                    <input
                        id="source-test-select"
                        type="text"
                        data-testid="source-test-select"
                        placeholder="[id=${session_id}].keepalive.expiresAt"
                        value={testSelect}
                        onChange={(e) => setTestSelect(e.target.value)}
                    />
                </div>
                <div className="modal-actions">
                    <button
                        type="button"
                        data-testid="source-test"
                        disabled={!isSaved}
                        onClick={() => void runTest()}
                    >
                        Test
                    </button>
                </div>
            </fieldset>

            {testView !== null && (
                <div data-testid="source-test-result">{testView.text}</div>
            )}
            {testView !== null && testView.document !== "" && (
                <pre data-testid="source-test-document">{testView.document}</pre>
            )}
        </div>
    );
}
