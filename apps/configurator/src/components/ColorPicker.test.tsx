import { describe, expect, it, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { ColorPicker, normalizeHex } from "./ColorPicker.tsx";

describe("normalizeHex", () => {
    it("accepts the canonical form", () => {
        expect(normalizeHex("#00ff00")).toBe("#00ff00");
        expect(normalizeHex("#00FF00")).toBe("#00ff00");
    });

    it("accepts what people actually paste", () => {
        expect(normalizeHex("00ff00")).toBe("#00ff00"); // no leading #
        expect(normalizeHex("  #00ff00\n")).toBe("#00ff00"); // stray whitespace
    });

    // Shorthand is rejected on purpose: "#00f" is a prefix of "#00ff00", so
    // accepting it would commit "#0000ff" while the user is still typing.
    it("rejects the 3-digit shorthand", () => {
        expect(normalizeHex("#abc")).toBeNull();
    });

    it("rejects every incomplete or non-hex value", () => {
        for (const raw of ["", "#", "#0", "#00f", "#00ff0", "#gggggg", "rgb(0,255,0)", "#0000000"]) {
            expect(normalizeHex(raw), raw).toBeNull();
        }
    });
});

describe("ColorPicker hex input", () => {
    const setup = (color?: string) => {
        const onChange = vi.fn();
        render(<ColorPicker color={color} previewTheme="dark" onChange={onChange} />);
        return { onChange, input: screen.getByTestId("color-hex-input") as HTMLInputElement };
    };

    // The regression this guards: committing every keystroke wrote an invalid
    // color ("#", "#00ff0") into the document, and the editor ignores all
    // further AST edits while the document is invalid — so the box froze on the
    // first character and the color never applied.
    it("does not emit while the value is still incomplete", () => {
        const { onChange, input } = setup("#ff0000");
        for (const partial of ["#", "#0", "#00", "#00f", "#00ff", "#00ff0"]) {
            fireEvent.change(input, { target: { value: partial } });
        }
        expect(onChange).not.toHaveBeenCalled();
    });

    it("keeps showing what was typed so the field stays editable", () => {
        const { input } = setup("#ff0000");
        fireEvent.change(input, { target: { value: "#00ff0" } });
        expect(input.value).toBe("#00ff0");
        expect(input.getAttribute("aria-invalid")).toBe("true");
    });

    it("emits once the value becomes a complete color", () => {
        const { onChange, input } = setup("#ff0000");
        fireEvent.change(input, { target: { value: "#00ff0" } });
        fireEvent.change(input, { target: { value: "#00ff00" } });
        expect(onChange).toHaveBeenCalledTimes(1);
        expect(onChange).toHaveBeenCalledWith("#00ff00");
    });

    it("accepts a pasted value with no leading # and normalizes it", () => {
        const { onChange, input } = setup("#ff0000");
        fireEvent.change(input, { target: { value: "00ff00" } });
        expect(onChange).toHaveBeenCalledWith("#00ff00");
    });

    it("clears the color when the field is emptied", () => {
        const { onChange, input } = setup("#ff0000");
        fireEvent.change(input, { target: { value: "" } });
        expect(onChange).toHaveBeenCalledWith("");
    });

    it("snaps back to the committed value on blur", () => {
        const { onChange, input } = setup("#ff0000");
        fireEvent.change(input, { target: { value: "#00ff0" } });
        fireEvent.blur(input);
        expect(input.value).toBe("#ff0000");
        expect(onChange).not.toHaveBeenCalled();
    });

    // An ANSI name is not a hex, so the hex box shows empty rather than
    // "bright-black" — and typing there must still not emit garbage.
    it("shows empty for a named color", () => {
        const { input } = setup("bright-black");
        expect(input.value).toBe("");
    });
});
