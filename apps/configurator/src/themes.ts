// Well-known color theme palettes for the color picker swatch grid.
//
// The selected theme is a UI-side picker preference persisted to
// localStorage — it is NOT part of the saved statusloom config. Clicking a
// swatch simply writes that swatch's hex value into the widget's color.

export interface ColorTheme {
    id: string;
    name: string;
    // Accent colors as #rrggbb hex, in the theme's canonical order.
    colors: string[];
}

export const COLOR_THEME_STORAGE_KEY = "statusloom.colorTheme";

// Pseudo-theme id for the 16 ANSI named colors (rendered via the preview
// palette; clicking writes the ANSI color NAME, not a hex value).
export const ANSI_THEME_ID = "ansi";

export const COLOR_THEMES: ColorTheme[] = [
    {
        id: "dracula",
        name: "Dracula",
        // https://draculatheme.com/contribute#color-palette
        colors: [
            "#8be9fd", // cyan
            "#50fa7b", // green
            "#ffb86c", // orange
            "#ff79c6", // pink
            "#bd93f9", // purple
            "#ff5555", // red
            "#f1fa8c", // yellow
            "#6272a4", // comment
            "#f8f8f2", // foreground
            "#44475a", // current line
        ],
    },
    {
        id: "nord",
        name: "Nord",
        // https://www.nordtheme.com/docs/colors-and-palettes
        colors: [
            "#8fbcbb", // nord7 (frost)
            "#88c0d0", // nord8
            "#81a1c1", // nord9
            "#5e81ac", // nord10
            "#bf616a", // nord11 (aurora red)
            "#d08770", // nord12 (orange)
            "#ebcb8b", // nord13 (yellow)
            "#a3be8c", // nord14 (green)
            "#b48ead", // nord15 (purple)
            "#d8dee9", // nord4 (snow storm)
            "#e5e9f0", // nord5
            "#eceff4", // nord6
            "#4c566a", // nord3
        ],
    },
    {
        id: "solarized-dark",
        name: "Solarized Dark",
        // https://ethanschoonover.com/solarized/
        colors: [
            "#b58900", // yellow
            "#cb4b16", // orange
            "#dc322f", // red
            "#d33682", // magenta
            "#6c71c4", // violet
            "#268bd2", // blue
            "#2aa198", // cyan
            "#859900", // green
            "#93a1a1", // base1 (emphasized content on dark)
            "#839496", // base0 (body text on dark)
            "#586e75", // base01
            "#073642", // base02
        ],
    },
    {
        id: "solarized-light",
        name: "Solarized Light",
        // https://ethanschoonover.com/solarized/ (same accents, light bases)
        colors: [
            "#b58900", // yellow
            "#cb4b16", // orange
            "#dc322f", // red
            "#d33682", // magenta
            "#6c71c4", // violet
            "#268bd2", // blue
            "#2aa198", // cyan
            "#859900", // green
            "#657b83", // base00 (body text on light)
            "#586e75", // base01 (emphasized content on light)
            "#93a1a1", // base1
            "#eee8d5", // base2
        ],
    },
    {
        id: "gruvbox-dark",
        name: "Gruvbox Dark",
        // https://github.com/morhetz/gruvbox
        colors: [
            "#fb4934", // bright red
            "#b8bb26", // bright green
            "#fabd2f", // bright yellow
            "#83a598", // bright blue
            "#d3869b", // bright purple
            "#8ec07c", // bright aqua
            "#fe8019", // bright orange
            "#cc241d", // neutral red
            "#98971a", // neutral green
            "#d79921", // neutral yellow
            "#458588", // neutral blue
            "#b16286", // neutral purple
            "#689d6a", // neutral aqua
            "#d65d0e", // neutral orange
            "#928374", // gray
            "#ebdbb2", // fg
        ],
    },
    {
        id: "tokyo-night",
        name: "Tokyo Night",
        // https://github.com/enkia/tokyo-night-vscode-theme
        colors: [
            "#f7768e", // red
            "#ff9e64", // orange
            "#e0af68", // yellow
            "#9ece6a", // green
            "#73daca", // teal
            "#2ac3de", // cyan
            "#7dcfff", // light cyan
            "#7aa2f7", // blue
            "#bb9af7", // purple
            "#c0caf5", // foreground
            "#a9b1d6", // editor fg
            "#565f89", // comment
        ],
    },
];

export function loadColorThemeId(): string {
    try {
        const stored = window.localStorage.getItem(COLOR_THEME_STORAGE_KEY);
        if (stored === ANSI_THEME_ID || COLOR_THEMES.some((t) => t.id === stored)) {
            return stored as string;
        }
    } catch {
        // Storage unavailable: fall through.
    }
    return ANSI_THEME_ID;
}

export function saveColorThemeId(id: string): void {
    try {
        window.localStorage.setItem(COLOR_THEME_STORAGE_KEY, id);
    } catch {
        // Best effort only.
    }
}
