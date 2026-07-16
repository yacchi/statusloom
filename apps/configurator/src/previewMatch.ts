// Maps a flat POST /api/dsl/preview `lines` array back onto the specific AST
// line nodes that produced it, and determines which <variant> a <responsive>
// selected at the previewed width.
//
// A responsive-free layout renders exactly one PreviewLine per <line>, but a
// <responsive> contributes a variable number of them (however many lines its
// width-selected variant has), so the flat array cannot be zipped against the
// AST by position alone. Instead every segment carries the AST id of its
// owning node (DSL_API.md "Node IDs"), so a PreviewLine's own line-id is
// recoverable from any one of its segments — that is the only signal this
// module relies on, matching how the canvas already resolves a chip's
// segments (Canvas.tsx's segsFor).

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
// (a fully decoration-less empty line — rare).
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

// A responsive-nested line owner's responsive id + selected variant index,
// or null for a top-level (non-variant) line owner.
const VARIANT_LINE_OWNER = /^(L\d+\.\d+)\.v(\d+)\.\d+$/;

export interface PreviewMatch {
    // AST line id -> its PreviewLine, for every line the preview response
    // resolved to (top-level lines and variant-nested lines alike). A line
    // whose id is absent has no fresh preview data (loading, stale, or not
    // the currently width-selected variant).
    byLineId: Map<string, PreviewLine>;
    // Responsive AST id ("L{i}.{p}") -> the variant index the preview
    // response selected at its width, for every responsive determinable
    // from the response (absent when no preview data covers it yet).
    selectedVariant: Map<string, number>;
}

const EMPTY_MATCH: PreviewMatch = { byLineId: new Map(), selectedVariant: new Map() };

// Builds the id-keyed lookup a Canvas tree walk uses to attach preview
// segments to the right line, and to know which variant of each responsive
// is the one currently selected by width. `previewLines` is the entire flat
// response for the edited layout.
export function matchPreview(previewLines: readonly PreviewLine[] | null): PreviewMatch {
    if (!previewLines) {
        return EMPTY_MATCH;
    }
    const byLineId = new Map<string, PreviewLine>();
    const selectedVariant = new Map<string, number>();
    for (const line of previewLines) {
        const owner = ownerOf(line);
        if (!owner) {
            continue;
        }
        byLineId.set(owner, line);
        const vm = VARIANT_LINE_OWNER.exec(owner);
        if (vm) {
            selectedVariant.set(vm[1], Number(vm[2]));
        }
    }
    return { byLineId, selectedVariant };
}
