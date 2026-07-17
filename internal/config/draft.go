package config

// This file implements render-time draft resolution
// (plans/config-store-and-format.md §7): a tool's draft is a mutable working
// node living in the internal store (refs.<tool>.draft), written without
// parse/validate — a last-writer-wins text-sharing channel that tolerates
// in-progress, invalid input. Rendering it requires parsing and validating it
// here so an invalid draft never reaches the renderer; the caller falls back
// to LoadDocument(tool) when ok is false.

import (
	"github.com/yacchi/statusloom/internal/dsl"
	"github.com/yacchi/statusloom/internal/store"
)

// DraftExists reports whether the store holds a draft working node for tool.
// Callers use this to distinguish "no draft" (silent fallback to the saved
// document) from "draft present but invalid" (worth reporting), since
// LoadDraftDocument's ok=false covers both cases. A store read failure is
// treated as "absent".
func DraftExists(tool string) bool {
	st, err := store.Open()
	if err != nil {
		return false
	}
	_, ok, err := st.ReadDraft(tool)
	return err == nil && ok
}

// LoadDraftDocument returns tool's draft working node, parsed and validated.
// ok is false when the draft is absent, or present but invalid (a structural
// parse failure or error-severity diagnostics) — the caller falls back to
// LoadDocument(tool) in that case. diags carries the draft's parse+validate
// findings whenever a draft was present (even when ok is true, so warnings
// can still be reported). A store read failure is returned as err.
func LoadDraftDocument(tool string) (doc *dsl.Document, diags []dsl.Diagnostic, ok bool, err error) {
	st, err := store.Open()
	if err != nil {
		return nil, nil, false, err
	}
	src, exists, err := st.ReadDraft(tool)
	if err != nil {
		return nil, nil, false, err
	}
	if !exists {
		return nil, nil, false, nil
	}
	doc, diags = dsl.ParseAndValidate(src)
	ok = doc != nil && doc.Root != nil && !dsl.HasErrors(diags)
	return doc, diags, ok, nil
}
