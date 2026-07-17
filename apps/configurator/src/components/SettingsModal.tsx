// Git settings hosted in a modal (opened from the header ⚙ button). The form
// body lives in GitSettings; this component only provides the title and
// hosts it in the shared modal chrome. Every change here is a live attribute
// patch (GitSettings' doc comment) — there is no separate save step, so this
// modal has no footer/primary action; the × button and Escape are its only
// ways to close.

import type { AttrPatch } from "../ast.ts";
import type { StatusloomNode } from "../types.ts";
import { t, useLang } from "../i18n.ts";
import { HelpTip } from "./HelpTip.tsx";
import { GitSettings } from "./GitSettings.tsx";
import { Modal } from "./Modal.tsx";

interface Props {
    root: StatusloomNode;
    readOnly: boolean;
    onPatchGit: (patch: AttrPatch) => void;
    onClose: () => void;
}

export function SettingsModal({ root, readOnly, onPatchGit, onClose }: Props) {
    const lang = useLang();
    return (
        <Modal
            title={
                <>
                    {t(lang, "settingsTitle")} <HelpTip k="helpGit" />
                </>
            }
            onClose={onClose}
            className="settings-modal"
        >
            <GitSettings root={root} readOnly={readOnly} onPatchGit={onPatchGit} />
        </Modal>
    );
}
