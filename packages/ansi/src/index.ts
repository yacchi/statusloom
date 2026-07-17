// Public entry point for @statusloom/ansi. The implementation lives in
// ansi.ts (moved from apps/configurator without changes); this module only
// re-exports its public surface.
export {
    ANSI_COLOR_NAMES,
    paletteFor,
    xterm256,
    parseAnsiLine,
    parseAnsiLines,
    type Theme,
    type Span,
    type AnsiColorName,
} from "./ansi.ts";
