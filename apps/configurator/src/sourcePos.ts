// Helpers translating the DSL API's byte-offset source ranges (UTF-8 byte
// offsets, per DSL_API.md "Diagnostics shape") into JS string indices and
// 1-based line:column positions for the DSL Editor's diagnostics list.

const encoder = new TextEncoder();
const decoder = new TextDecoder();

// Convert a UTF-8 byte offset into `source` to a JS string (UTF-16 code unit)
// index, clamping to the source bounds.
export function byteToCharIndex(source: string, byteOffset: number): number {
    if (byteOffset <= 0) {
        return 0;
    }
    const bytes = encoder.encode(source);
    if (byteOffset >= bytes.length) {
        return source.length;
    }
    // Decoding the prefix gives its UTF-16 length. A byte offset that lands
    // mid-codepoint decodes with a replacement char of length 1, which keeps
    // the result a usable (if approximate) caret position.
    return decoder.decode(bytes.subarray(0, byteOffset)).length;
}

export interface LineCol {
    line: number; // 1-based
    col: number; // 1-based, in characters
}

// 1-based line:column of a JS string index.
export function charIndexToLineCol(source: string, charIndex: number): LineCol {
    const upto = source.slice(0, Math.max(0, Math.min(charIndex, source.length)));
    let line = 1;
    let lastNl = -1;
    for (let i = 0; i < upto.length; i += 1) {
        if (upto.charCodeAt(i) === 10) {
            line += 1;
            lastNl = i;
        }
    }
    return { line, col: upto.length - lastNl };
}

export function byteToLineCol(source: string, byteOffset: number): LineCol {
    return charIndexToLineCol(source, byteToCharIndex(source, byteOffset));
}
