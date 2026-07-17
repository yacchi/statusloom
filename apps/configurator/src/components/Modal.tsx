// Shared modal chrome for HistoryPanel / ImportModal / SettingsModal (and any
// future modal): a backdrop + card with a header (title + × close button)
// and Escape-to-close. The Escape listener is added and removed with this
// component instance, so it is only live while *this* modal is mounted —
// nothing leaks past unmount, and two modals opened at once each own their
// own independent listener rather than sharing one.
//
// The shell deliberately does not render a footer: not every modal has a
// primary action (SettingsModal is a live-patch editor with none), and the
// ones that do (HistoryPanel's Restore, ImportModal's Import/Add layouts)
// render their own `.modal-actions` as children, right after their content.
// A dedicated Close/Cancel button is intentionally not provided here either
// — the × button and Escape are the only ways to dismiss a modal, so callers
// never need one.

import { useEffect, type ReactNode } from "react";

interface Props {
    title: ReactNode;
    onClose: () => void;
    // Extra class(es) appended to "modal", e.g. "history-modal" for
    // panel-specific sizing.
    className?: string;
    children: ReactNode;
}

export function Modal({ title, onClose, className, children }: Props) {
    useEffect(() => {
        const onKey = (e: KeyboardEvent) => {
            if (e.key === "Escape") {
                onClose();
            }
        };
        window.addEventListener("keydown", onKey);
        return () => window.removeEventListener("keydown", onKey);
    }, [onClose]);

    return (
        <div className="modal-backdrop" onClick={onClose}>
            <div
                className={className ? `modal ${className}` : "modal"}
                onClick={(e) => e.stopPropagation()}
            >
                <div className="modal-head">
                    <h2 style={{ margin: 0 }}>{title}</h2>
                    <button
                        type="button"
                        className="modal-close"
                        aria-label="Close"
                        data-testid="modal-close"
                        onClick={onClose}
                    >
                        ×
                    </button>
                </div>
                {children}
            </div>
        </div>
    );
}
