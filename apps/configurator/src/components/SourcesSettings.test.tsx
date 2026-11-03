import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { ApiError } from "../api.ts";
import { t } from "../i18n.ts";
import type {
    SourceDefinition,
    SourceEntry,
    SourceTestRequest,
    SourceTestResult,
    SourceWarning,
    SourcesApi,
} from "../sourcesApi.ts";
import { SourcesSettings } from "./SourcesSettings.tsx";
import { SettingsModal } from "./SettingsModal.tsx";
import type { StatusloomNode } from "../types.ts";

// Mirrors the server's computed fields closely enough for the form tests:
// absent numbers come back as 0, and warnings follow the same two rules.
function toEntry(id: string, def: SourceDefinition): SourceEntry {
    const warnings: SourceWarning[] = [];
    const ttl = def.ttlSeconds ?? 0;
    if (def.kind === "http" && def.allowRemote) {
        warnings.push("allow-remote");
    }
    if ((def.kind === "command" || def.kind === "http") && ttl > 0 && ttl < 10) {
        warnings.push("short-ttl");
    }
    return {
        id,
        kind: def.kind,
        path: def.path ?? "",
        argv: def.argv ?? [],
        url: def.url ?? "",
        allowRemote: def.allowRemote ?? false,
        ttlSeconds: ttl,
        staleAfterSeconds: def.staleAfterSeconds ?? 0,
        timeoutSeconds: def.timeoutSeconds ?? 0,
        warnings,
    };
}

interface FakeOverrides {
    initial?: SourceEntry[];
    put?: (id: string, def: SourceDefinition) => Promise<SourceEntry>;
    test?: (id: string, req: SourceTestRequest) => Promise<SourceTestResult>;
}

function fakeApi(overrides: FakeOverrides = {}) {
    const store = new Map<string, SourceEntry>(
        (overrides.initial ?? []).map((e) => [e.id, e]),
    );
    const api = {
        listSources: vi.fn(async () => ({
            path: "/cfg/sources.json",
            sources: [...store.values()],
        })),
        putSource: vi.fn(
            overrides.put ??
                (async (id: string, def: SourceDefinition) => {
                    const entry = toEntry(id, def);
                    store.set(id, entry);
                    return entry;
                }),
        ),
        deleteSource: vi.fn(async (id: string) => {
            store.delete(id);
        }),
        testSource: vi.fn(
            overrides.test ??
                (async (_id: string, _req: SourceTestRequest) => ({
                    ok: true,
                    error: "",
                    matched: true,
                    value: { a: 1 },
                    display: "",
                    documentPreview: "",
                })),
        ),
    };
    return api;
}

function setup(api: SourcesApi, onChanged = vi.fn()) {
    render(<SourcesSettings sourcesApi={api} onChanged={onChanged} />);
    return { onChanged };
}

function field(testId: string, value: string) {
    fireEvent.change(screen.getByTestId(testId), { target: { value } });
}

async function waitLoaded() {
    await waitFor(() => expect(screen.getByTestId("sources-path")).toBeTruthy());
}

describe("SourcesSettings form", () => {
    it("sends argv one per line, dropping blank lines and CRLF endings, and omits unset keys", async () => {
        const api = fakeApi();
        setup(api);
        await waitLoaded();

        field("source-form-id", "ext");
        field("source-form-kind", "command");
        field("source-form-argv", "sh\n-c\n\necho '{\"a\": 1}'\r\n");
        field("source-form-timeout", "3");
        fireEvent.click(screen.getByTestId("source-save"));

        await waitFor(() => expect(api.putSource).toHaveBeenCalledTimes(1));
        const [id, body] = api.putSource.mock.calls[0];
        expect(id).toBe("ext");
        expect(body).toEqual({
            kind: "command",
            argv: ["sh", "-c", "echo '{\"a\": 1}'"],
            timeoutSeconds: 3,
        });
        expect(body).not.toHaveProperty("ttlSeconds");
        expect(body).not.toHaveProperty("staleAfterSeconds");
    });

    it("drops keys belonging to the previous kind when the kind is switched", async () => {
        const api = fakeApi();
        setup(api);
        await waitLoaded();

        field("source-form-id", "cc");
        field("source-form-path", "~/x.json");
        field("source-form-kind", "http");
        field("source-form-url", "http://127.0.0.1:1/s.json");
        fireEvent.click(screen.getByTestId("source-save"));

        await waitFor(() => expect(api.putSource).toHaveBeenCalledTimes(1));
        expect(api.putSource.mock.calls[0][1]).toEqual({
            kind: "http",
            url: "http://127.0.0.1:1/s.json",
        });
    });

    it("shows the short-ttl warning while editing and sends allowRemote and ttl for http", async () => {
        const api = fakeApi();
        setup(api);
        await waitLoaded();

        field("source-form-id", "cc");
        field("source-form-kind", "http");
        field("source-form-url", "http://127.0.0.1:1/s.json");
        fireEvent.click(screen.getByTestId("source-form-allow-remote"));
        field("source-form-ttl", "7");
        expect(screen.getByTestId("source-form-warning-short-ttl").textContent).toBe(
            t("en", "sourceWarnShortTtl"),
        );

        field("source-form-ttl", "10");
        expect(screen.queryByTestId("source-form-warning-short-ttl")).toBeNull();

        field("source-form-ttl", "7");
        fireEvent.click(screen.getByTestId("source-save"));
        await waitFor(() => expect(api.putSource).toHaveBeenCalledTimes(1));
        expect(api.putSource.mock.calls[0][1]).toEqual({
            kind: "http",
            url: "http://127.0.0.1:1/s.json",
            allowRemote: true,
            ttlSeconds: 7,
        });
    });

    it("renders each row's warnings inside that row only", async () => {
        const api = fakeApi({
            initial: [
                toEntry("cc", { kind: "http", url: "http://x", allowRemote: true, ttlSeconds: 7 }),
                toEntry("plain", { kind: "file", path: "/a.json" }),
            ],
        });
        setup(api);
        await waitLoaded();

        const cc = await screen.findByTestId("source-row-cc");
        expect(within(cc).getByTestId("source-warning-cc-allow-remote")).toBeTruthy();
        expect(within(cc).getByTestId("source-warning-cc-short-ttl")).toBeTruthy();

        const plain = screen.getByTestId("source-row-plain");
        expect(within(plain).queryByTestId(/^source-warning-/)).toBeNull();
    });

    it("shows an API rejection inline without reloading or notifying", async () => {
        const api = fakeApi({
            put: async () => {
                throw new ApiError(400, 'source "x": file sources need a path');
            },
        });
        const { onChanged } = setup(api);
        await waitLoaded();

        field("source-form-id", "x");
        fireEvent.click(screen.getByTestId("source-save"));

        const error = await screen.findByTestId("source-form-error");
        expect(error.textContent).toBe('source "x": file sources need a path');
        expect(api.listSources).toHaveBeenCalledTimes(1);
        expect(onChanged).not.toHaveBeenCalled();
    });
});

describe("SourcesSettings delete and test", () => {
    it("deletes only after confirmation and reports the reloaded list", async () => {
        const api = fakeApi({
            initial: [toEntry("cc", { kind: "file", path: "/a.json" })],
        });
        const { onChanged } = setup(api);
        await waitLoaded();

        fireEvent.click(await screen.findByTestId("source-edit-cc"));
        fireEvent.click(screen.getByTestId("source-delete"));
        fireEvent.click(screen.getByText("Cancel"));
        expect(api.deleteSource).not.toHaveBeenCalled();
        expect(screen.queryByTestId("source-delete-confirm")).toBeNull();

        fireEvent.click(screen.getByTestId("source-delete"));
        fireEvent.click(screen.getByTestId("source-delete-confirm-yes"));

        await waitFor(() => expect(api.deleteSource).toHaveBeenCalledWith("cc"));
        await waitFor(() => expect(onChanged).toHaveBeenCalledWith([]));
    });

    it("reports a test result: no match, error, and JSON display of the value", async () => {
        const results: SourceTestResult[] = [
            { ok: true, error: "", matched: false, value: null, display: "", documentPreview: "" },
            { ok: false, error: "exit status 1", matched: false, value: null, display: "", documentPreview: "" },
            { ok: true, error: "", matched: true, value: { a: 1 }, display: "", documentPreview: "" },
        ];
        const api = fakeApi({
            initial: [toEntry("cc", { kind: "file", path: "/a.json" })],
            test: async () => results.shift() as SourceTestResult,
        });
        setup(api);
        await waitLoaded();

        fireEvent.click(await screen.findByTestId("source-edit-cc"));
        const result = () => screen.getByTestId("source-test-result").textContent;

        fireEvent.click(screen.getByTestId("source-test"));
        await waitFor(() => expect(result()).toBe(t("en", "sourceNoMatch")));

        fireEvent.click(screen.getByTestId("source-test"));
        await waitFor(() => expect(result()).toBe("exit status 1"));

        fireEvent.click(screen.getByTestId("source-test"));
        await waitFor(() => expect(result()).toBe('{"a":1}'));
    });
});

describe("SettingsModal sources section", () => {
    const root: StatusloomNode = { id: "root", kind: "statusloom", layouts: [] };
    const noop = () => {};

    it("omits the sources section when no sourcesApi is provided", () => {
        render(
            <SettingsModal
                root={root}
                readOnly={false}
                onPatchGit={noop}
                onChangeLang={noop}
                onClose={noop}
            />,
        );
        expect(screen.queryByTestId("sources-settings")).toBeNull();
        expect(screen.queryByText(t("en", "sourcesSection"))).toBeNull();
    });

    it("renders the sources section when a sourcesApi is provided", async () => {
        render(
            <SettingsModal
                root={root}
                readOnly={false}
                onPatchGit={noop}
                onChangeLang={noop}
                onClose={noop}
                sourcesApi={fakeApi()}
            />,
        );
        expect(screen.getByText(t("en", "sourcesSection"))).toBeTruthy();
        expect(await screen.findByTestId("sources-settings")).toBeTruthy();
    });

    it("keeps the test result and the full document inside the modal card", async () => {
        const api = fakeApi({
            initial: [toEntry("cc", { kind: "file", path: "/a.json" })],
            test: async () => ({
                ok: true,
                error: "",
                matched: true,
                value: 1793667322,
                display: "1793667322",
                documentPreview: '{\n  "sessions": []\n}',
            }),
        });
        render(
            <SettingsModal
                root={root}
                readOnly={false}
                onPatchGit={noop}
                onChangeLang={noop}
                onClose={noop}
                sourcesApi={api}
            />,
        );
        fireEvent.click(await screen.findByTestId("source-edit-cc"));
        fireEvent.click(screen.getByTestId("source-test"));

        const result = await screen.findByTestId("source-test-result");
        const details = screen.getByTestId("source-test-document");
        const card = result.closest(".modal") as HTMLElement;
        expect(card).toBeTruthy();
        expect(within(card).getByTestId("source-test-document")).toBe(details);
        expect(details.tagName).toBe("DETAILS");
        expect(details.querySelector("pre")?.textContent).toBe('{\n  "sessions": []\n}');
    });
});
