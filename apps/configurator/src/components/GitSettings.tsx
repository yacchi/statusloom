// Git data-collection settings: the optional <git/> element of the document.
// Every change is an attribute patch (undefined removes the attribute; a
// fully-cleared git element is dropped entirely).
//
// This renders only the form body (no panel chrome / heading); it is hosted
// inside SettingsModal, which supplies the modal chrome, title, and Close.

import type { AttrPatch } from "../ast.ts";
import type { GitNode, StatusloomNode } from "../types.ts";

interface Props {
    root: StatusloomNode;
    readOnly: boolean;
    onPatchGit: (patch: AttrPatch) => void;
}

function numberOrUnset(v: string): number | undefined {
    return v === "" ? undefined : Number(v);
}

export function GitSettings({ root, readOnly, onPatchGit }: Props) {
    const git: GitNode = root.git ?? { id: "git", kind: "git" };

    return (
        <fieldset className="props-body" disabled={readOnly}>
            <div className="field">
                <label>Cache TTL (ms)</label>
                <input
                    type="number"
                    min={0}
                    data-testid="setting-git-cache-ttl"
                    value={git["cache-ttl-ms"] ?? ""}
                    onChange={(e) =>
                        onPatchGit({ "cache-ttl-ms": numberOrUnset(e.target.value) })
                    }
                />
            </div>
            <div className="field">
                <label>Timeout (ms)</label>
                <input
                    type="number"
                    min={0}
                    data-testid="setting-git-timeout"
                    value={git["timeout-ms"] ?? ""}
                    onChange={(e) =>
                        onPatchGit({ "timeout-ms": numberOrUnset(e.target.value) })
                    }
                />
            </div>
            <div className="field">
                <label>Include untracked</label>
                <select
                    data-testid="setting-git-untracked"
                    value={
                        git["include-untracked"] === undefined
                            ? ""
                            : String(git["include-untracked"])
                    }
                    onChange={(e) =>
                        onPatchGit({
                            "include-untracked":
                                e.target.value === "" ? undefined : e.target.value === "true",
                        })
                    }
                >
                    <option value="">(default)</option>
                    <option value="true">true</option>
                    <option value="false">false</option>
                </select>
            </div>
            <div className="field">
                <label>Collect numstat</label>
                <select
                    data-testid="setting-git-numstat"
                    value={
                        git["collect-numstat"] === undefined
                            ? ""
                            : String(git["collect-numstat"])
                    }
                    onChange={(e) =>
                        onPatchGit({
                            "collect-numstat":
                                e.target.value === "" ? undefined : e.target.value === "true",
                        })
                    }
                >
                    <option value="">(default)</option>
                    <option value="true">true</option>
                    <option value="false">false</option>
                </select>
            </div>
        </fieldset>
    );
}
