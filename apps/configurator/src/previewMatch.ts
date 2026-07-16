// Maps a flat POST /api/dsl/preview `lines` array back onto the specific AST
// line nodes that produced it, and determines which <variant> a <responsive>
// selected at the previewed width.
//
// A responsive-free layout renders exactly one PreviewLine per <line>, but a
// <responsive> contributes lines from its variant(s), so the flat array
// cannot be zipped against the AST by position alone. Instead every segment
// carries the AST id of its owning node (DSL_API.md "Node IDs"), so a
// PreviewLine's own line-id is recoverable from any one of its segments —
// that is the only signal byLineId relies on, matching how the canvas
// already resolves a chip's segments (Canvas.tsx's segsFor).
//
// Which variant is width-selected is a separate signal: with `allVariants`
// requests (see DSL_API.md), the response's `selectedVariants` map is the
// only source of truth for that — every variant's lines are present in the
// flat array, so "which lines are present" no longer implies "which variant
// is selected" the way it used to for the (now-unused) selected-only preview.

import type { PreviewLine } from "./types.ts";

// The line-owning id a segment's nodeId belongs to: "L{i}.{p}" for a
// top-level line's own decoration/children, or "L{i}.{p}.v{v}.{j}" for a
// line nested inside a responsive's variant. Null for a nodeId that carries
// no such prefix (e.g. "" for a decoration-less/fallback segment).
function lineOwnerOf(nodeId: string): string | null {
    const m = /^(L\d+\.\d+(?:\.v\d+\.\d+)?)/.exec(nodeId);
    return m ? m[1] : null;
}

// The line-owner id of the first segment (own or descendant) that carries
// one, or null when a PreviewLine's segments carry no resolvable id at all
// (a fully decoration-less/empty line — rare).
function ownerOf(line: PreviewLine): string | null {
    for (const seg of line.segments) {
        if (seg.nodeId === "") {
            continue;
        }
        const owner = lineOwnerOf(seg.nodeId);
        if (owner) {
            return owner;
        }
    }
    return null;
}

// A responsive-nested line owner's responsive id + variant index, or null
// for a top-level (non-variant) line owner.
const VARIANT_LINE_OWNER = /^(L\d+\.\d+)\.v(\d+)\.\d+$/;

export interface PreviewMatch {
    // AST line id -> its PreviewLine, for every line the preview response
    // resolved to (top-level lines and variant-nested lines alike). A line
    // whose id is absent has no fresh preview data (loading or stale).
    byLineId: Map<string, PreviewLine>;
    // Responsive AST id ("L{i}.{p}") -> the variant index the preview
    // response selected at its width, taken verbatim from the response's
    // `selectedVariants` (absent when the response carries none yet).
    selectedVariant: Map<string, number>;
}

const EMPTY_MATCH: PreviewMatch = { byLineId: new Map(), selectedVariant: new Map() };

// Builds the id-keyed lookup a Canvas tree walk uses to attach preview
// segments to the right line, and to know which variant of each responsive
// is the one currently selected by width. `previewLines` is the entire flat
// response for the edited layout (every variant's lines, per `allVariants`);
// `selectedVariants` is the response's explicit selection map — it is the
// only source for `selectedVariant` now that every variant's lines are
// present, so "which lines exist" can no longer imply "which is selected".
export function matchPreview(
    previewLines: readonly PreviewLine[] | null,
    selectedVariants: Record<string, number> | null | undefined,
): PreviewMatch {
    if (!previewLines) {
        return EMPTY_MATCH;
    }
    const byLineId = new Map<string, PreviewLine>();
    for (const line of previewLines) {
        const owner = ownerOf(line);
        if (!owner) {
            continue;
        }
        byLineId.set(owner, line);
    }
    const selectedVariant = new Map<string, number>(
        selectedVariants ? Object.entries(selectedVariants) : [],
    );
    return { byLineId, selectedVariant };
}

// The lines forming the REAL statusline: a top-level line (owner
// "L{i}.{p}", no ".v{v}") always counts, and a variant-nested line
// ("L{i}.{p}.v{v}.{j}") counts only when v is the width-selected index for
// that responsive. Used by the canvas's "Pure output" view so it reflects
// the actual rendered statusline instead of every previewed variant.
// Falls back to every line when `selectedVariants` is unavailable (null or
// undefined) — e.g. while the preview response hasn't arrived yet, or for a
// selected-only (non-allVariants) response where this distinction cannot
// arise.
export function effectiveLines(
    previewLines: readonly PreviewLine[] | null,
    selectedVariants: Record<string, number> | null | undefined,
): readonly PreviewLine[] {
    if (!previewLines) {
        return [];
    }
    if (!selectedVariants) {
        return previewLines;
    }
    return previewLines.filter((line) => {
        const owner = ownerOf(line);
        if (!owner) {
            return true;
        }
        const vm = VARIANT_LINE_OWNER.exec(owner);
        if (!vm) {
            return true; // top-level line: always part of the real output.
        }
        const [, responsiveId, variantIndex] = vm;
        return selectedVariants[responsiveId] === Number(variantIndex);
    });
}
