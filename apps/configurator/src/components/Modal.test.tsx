import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { Modal } from "./Modal.tsx";

describe("Modal chrome", () => {
    it("renders the title and children", () => {
        render(
            <Modal title="Widgets" onClose={vi.fn()}>
                <p>body content</p>
            </Modal>,
        );
        expect(screen.getByText("Widgets")).toBeTruthy();
        expect(screen.getByText("body content")).toBeTruthy();
    });

    it("closes via the × button", () => {
        const onClose = vi.fn();
        render(
            <Modal title="Widgets" onClose={onClose}>
                <p>body</p>
            </Modal>,
        );
        fireEvent.click(screen.getByTestId("modal-close"));
        expect(onClose).toHaveBeenCalledTimes(1);
    });

    it("closes on backdrop click but not on a click inside the card", () => {
        const onClose = vi.fn();
        render(
            <Modal title="Widgets" onClose={onClose}>
                <p>body</p>
            </Modal>,
        );
        fireEvent.click(screen.getByText("body"));
        expect(onClose).not.toHaveBeenCalled();

        // The backdrop is the outermost element, found via the title's
        // great-grandparent (title -> h2 -> .modal-head -> .modal -> backdrop).
        const backdrop = screen.getByText("Widgets").closest(".modal-backdrop");
        expect(backdrop).toBeTruthy();
        fireEvent.click(backdrop as Element);
        expect(onClose).toHaveBeenCalledTimes(1);
    });

    it("closes on Escape while mounted", () => {
        const onClose = vi.fn();
        render(
            <Modal title="Widgets" onClose={onClose}>
                <p>body</p>
            </Modal>,
        );
        fireEvent.keyDown(window, { key: "Escape" });
        expect(onClose).toHaveBeenCalledTimes(1);
    });

    it("stops listening for Escape once unmounted (no leaked handler)", () => {
        const onClose = vi.fn();
        const { unmount } = render(
            <Modal title="Widgets" onClose={onClose}>
                <p>body</p>
            </Modal>,
        );
        unmount();
        fireEvent.keyDown(window, { key: "Escape" });
        expect(onClose).not.toHaveBeenCalled();
    });
});
