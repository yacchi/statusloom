package dsl

import (
	"sort"
	"strings"
)

// <refresh wait="..." timeout-ms="..." wait-on="..."/> (markup.md "refresh"):
// the background work items a live render waits for before drawing, bounded by
// a timeout. The render only reads caches, asks the refresh worker for work,
// and waits; every fetch stays in the worker. The tokens name work item kinds,
// not fields, so a new kind of background data joins the wait by adding one
// token (and `all` picks it up without touching the document).

// Wait tokens. A work item kind's token equals its name (cache.WorkKind);
// `source:<id>` names one external source and `sources` every referenced one.
const (
	WaitTokenGit          = "git"
	WaitTokenTranscript   = "transcript"
	WaitTokenUsage        = "usage"
	WaitTokenSources      = "sources"
	WaitTokenAll          = "all"
	WaitTokenSourcePrefix = "source:"

	// WaitKindSource is the work item kind matched by `source:<id>` and
	// `sources` (cache.WorkSource).
	WaitKindSource = "source"
)

// wait-on values.
const (
	WaitOnCold = "cold" // default: wait only for a work item that was never attempted
	WaitOnDue  = "due"  // wait for every due work item that is not backing off
)

// Bounds of timeout-ms. Out-of-range values are validation errors, not clamped.
const (
	DefaultRefreshTimeoutMS = 2000
	MinRefreshTimeoutMS     = 1
	MaxRefreshTimeoutMS     = 3000
)

// RefreshPolicy is a document's interpreted <refresh> element. The zero value
// waits for nothing.
type RefreshPolicy struct {
	tokens map[string]bool
	// TimeoutMS bounds one render's wait, measured from the render's start.
	TimeoutMS int
	// WaitOnDue is wait-on="due" (false = the default "cold").
	WaitOnDue bool
}

// DocumentRefreshPolicy interprets doc's <refresh> element. Invalid tokens are
// dropped (Validate reports them; a document with errors is never rendered).
func DocumentRefreshPolicy(doc *Document) RefreshPolicy {
	if doc == nil || doc.Root == nil || doc.Root.Refresh == nil {
		return RefreshPolicy{}
	}
	r := doc.Root.Refresh
	p := RefreshPolicy{TimeoutMS: DefaultRefreshTimeoutMS, WaitOnDue: r.WaitOn == WaitOnDue}
	if r.TimeoutMS != nil {
		p.TimeoutMS = *r.TimeoutMS
	}
	for _, tok := range strings.Fields(r.Wait) {
		if validWaitToken(tok) {
			if p.tokens == nil {
				p.tokens = map[string]bool{}
			}
			p.tokens[tok] = true
		}
	}
	return p
}

// Waits reports whether the policy names any work item at all.
func (p RefreshPolicy) Waits() bool { return len(p.tokens) > 0 }

// Wants reports whether the policy waits for the work item of kind (a
// cache.WorkKind value) and key (for sources: the source id).
func (p RefreshPolicy) Wants(kind, key string) bool {
	if p.tokens[WaitTokenAll] {
		return true
	}
	if kind == WaitKindSource {
		return p.tokens[WaitTokenSources] || p.tokens[WaitTokenSourcePrefix+key]
	}
	return p.tokens[kind]
}

// Tokens returns the policy's distinct tokens, sorted.
func (p RefreshPolicy) Tokens() []string {
	out := make([]string, 0, len(p.tokens))
	for t := range p.tokens {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// validWaitToken reports whether tok is a known wait token or `source:<id>`
// with a valid source id.
func validWaitToken(tok string) bool {
	switch tok {
	case WaitTokenGit, WaitTokenTranscript, WaitTokenUsage, WaitTokenSources, WaitTokenAll:
		return true
	}
	if id, ok := strings.CutPrefix(tok, WaitTokenSourcePrefix); ok {
		return ValidSourceID(id)
	}
	return false
}

// validateRefresh checks the <refresh> element's attributes (markup.md
// "refresh" validation). Duplicate tokens and an empty wait are not
// diagnosed, nor are tokens for work the document does not use (doctor warns
// about those instead).
func (v *validator) validateRefresh(r *RefreshSettings) {
	rng := r.Meta.SourceRange
	for _, tok := range strings.Fields(r.Wait) {
		if validWaitToken(tok) {
			continue
		}
		if id, ok := strings.CutPrefix(tok, WaitTokenSourcePrefix); ok {
			v.errInvalidSourceID(rng, id)
			continue
		}
		v.errf(rng, "unknown <refresh> wait token %q: expected git, transcript, usage, sources, all, or source:<id>", tok)
	}
	if t := r.TimeoutMS; t != nil && (*t < MinRefreshTimeoutMS || *t > MaxRefreshTimeoutMS) {
		v.errf(rng, "<refresh> timeout-ms must be between %d and %d, got %d", MinRefreshTimeoutMS, MaxRefreshTimeoutMS, *t)
	}
	if w := r.WaitOn; w != "" && w != WaitOnCold && w != WaitOnDue {
		v.errf(rng, "invalid <refresh> wait-on %q: expected cold or due", w)
	}
}
