// Git settings hosted in a modal (opened from the header ⚙ button). The form
// body lives in GitSettings; this component only provides the modal chrome
// (backdrop, title, Close). Follows the same pattern as ImportModal.

import type { AttrPatch } from "../ast.ts";
import type { StatusloomNode } from "../types.ts";
import { t, useLang } from "../i18n.ts";
import { HelpTip } from "./HelpTip.tsx";
import { GitSettings } from "./GitSettings.tsx";

interface Props {
    root: StatusloomNode;
    readOnly: boolean;
    onPatchGit: (patch: AttrPatch) => void;
    onClose: () => void;
}

export function SettingsModal({ root, readOnly, onPatchGit, onClose }: Props) {
    const lang = useLang();
    return (
        <div className="modal-backdrop" onClick={onClose}>
            <div className="modal settings-modal" onClick={(e) => e.stopPropagation()}>
                <div className="modal-head">
                    <h2 style={{ margin: 0 }}>
                        {t(lang, "settingsTitle")} <HelpTip k="helpGit" />
                    </h2>
                </div>
                <GitSettings root={root} readOnly={readOnly} onPatchGit={onPatchGit} />
                <div className="modal-actions">
                    <button onClick={onClose}>Close</button>
                </div>
            </div>
        </div>
    );
}
