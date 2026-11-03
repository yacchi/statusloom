package config

// This file implements the DSL-document side of the configuration layer: a
// thin layer above the internal store (internal/store) that resolves a
// tool's committed document for the render path and CLI/webconfig
// diagnostics (plans/config-store-and-format.md §7, §8.11). The store's
// single cross-tool file (<configDir>/statusloom.json) is the single source
// of truth; this package never touches it directly, only through
// store.Open().
//
// Dependency direction: config -> {dsl, store}. config does NOT depend on
// render.

import (
	"time"

	"github.com/yacchi/statusloom/internal/dsl"
	"github.com/yacchi/statusloom/internal/store"
)

// DocumentExists reports whether the store holds a committed current
// revision for tool, as opposed to LoadDocument falling back to the built-in
// DefaultDocument. A store read failure is treated as "absent" (the caller's
// subsequent LoadDocument call will surface the same error).
func DocumentExists(tool string) bool {
	st, err := store.Open()
	if err != nil {
		return false
	}
	_, _, ok, err := st.Current(tool)
	return err == nil && ok
}

// maxAncestorHops bounds the parent-chain walk in LoadDocument so a corrupted
// store cannot make the render path loop or read the whole history.
const maxAncestorHops = 50

// LoadDocument returns tool's current committed document, parsed but not
// re-validated: a committed revision already passed the store's Save
// validation boundary (dsl.ParseAndValidate), so rendering only needs to
// Parse it again (plans/config-store-and-format.md §7). A missing store or
// tool ref falls back to DefaultDocument(tool), parsed the same way. A store
// read/decode failure is returned as err (with a nil document).
//
// When the current revision does not parse (it was saved by a newer version
// that uses syntax this binary cannot read), the newest ancestor on its
// parent chain that does parse is returned instead, with a single warning
// diagnostic naming that revision. The current revision's parse errors are
// not returned in that case. If no ancestor parses, DefaultDocument(tool) is
// returned together with the current revision's parse diagnostics, so the
// caller sees the errors and falls back to the default as before.
func LoadDocument(tool string) (*dsl.Document, []dsl.Diagnostic, error) {
	st, err := store.Open()
	if err != nil {
		return nil, nil, err
	}
	rev, id, ok, err := st.Current(tool)
	if err != nil {
		return nil, nil, err
	}
	if !ok {
		doc, diags := dsl.Parse(DefaultDocument(tool))
		return doc, diags, nil
	}
	doc, diags := dsl.Parse(rev.Source)
	if parsesCleanly(doc, diags) {
		return doc, diags, nil
	}
	if ancestorID, ancestor, ancestorDoc, found := readableAncestor(st, tool, id, rev); found {
		warning := dsl.Warnf(dsl.SourceRange{},
			"current revision uses features this version cannot read; showing revision %s (saved %s) instead",
			ancestorID, ancestor.SavedAt.UTC().Format(time.RFC3339))
		return ancestorDoc, []dsl.Diagnostic{warning}, nil
	}
	fallback, _ := dsl.Parse(DefaultDocument(tool))
	return fallback, diags, nil
}

// parsesCleanly reports whether a Parse result has a root and no
// error-severity diagnostics.
func parsesCleanly(doc *dsl.Document, diags []dsl.Diagnostic) bool {
	return doc != nil && doc.Root != nil && !dsl.HasErrors(diags)
}

// readableAncestor walks rev's parent chain (rev is revision id) and returns
// the first ancestor that parses cleanly. It stops at a missing or
// foreign-tool parent, a cycle, or after maxAncestorHops.
func readableAncestor(st *store.Store, tool, id string, rev store.Revision) (string, store.Revision, *dsl.Document, bool) {
	seen := map[string]bool{id: true}
	cur := rev
	for hop := 0; hop < maxAncestorHops && cur.Parent != nil; hop++ {
		pid := *cur.Parent
		if seen[pid] {
			break
		}
		seen[pid] = true
		prev, ok, err := st.Revision(pid)
		if err != nil || !ok || prev.Tool != tool {
			break
		}
		doc, diags := dsl.Parse(prev.Source)
		if parsesCleanly(doc, diags) {
			return pid, prev, doc, true
		}
		cur = prev
	}
	return "", store.Revision{}, nil, false
}

// defaultDocuments holds the built-in DSL source for each tool. The
// claude-code document uses the same fields in the same order, the same
// colors, " | " separators (as role="separator" text that compacts to "|"),
// and the "5h:"/"7d:" usage labels expressed as optional spans so an empty
// usage value hides the label.
var defaultDocuments = map[string]string{
	"claude-code": claudeCodeDefaultDocument,
}

const claudeCodeDefaultDocument = `<statusloom version="1" tool="claude-code" color-level="ansi16" compact-threshold="60" context-percentage-mode="usable">
  <layout name="Default" active="true">
    <line>
      <field name="model" color="cyan"/>
      <text role="separator" padding="1">|</text>
      <field name="thinking-effort"/>
      <text role="separator" padding="1">|</text>
      <field name="context-length"/>
      <text role="separator" padding="1">|</text>
      <field name="context-percentage-usable"/>
      <text role="separator" padding="1">|</text>
      <field name="session-cost"/>
      <text role="separator" padding="1">|</text>
      <field name="git-branch" color="magenta"/>
      <text role="separator" padding="1">|</text>
      <field name="git-changes" color="yellow"/>
    </line>
    <line>
      <span optional="five-hour-usage" prefix="5h: "><field name="five-hour-usage"/></span>
      <text role="separator" padding="1">|</text>
      <field name="five-hour-reset"/>
      <text role="separator" padding="1">|</text>
      <span optional="weekly-usage" prefix="7d: "><field name="weekly-usage"/></span>
      <text role="separator" padding="1">|</text>
      <field name="weekly-reset"/>
      <text role="separator" padding="1">|</text>
      <field name="tool-version"/>
    </line>
    <subagent>
      <!-- One row per running subagent task (statusloom claude-subagent).
           Ignored by the main "claude" render pass. This layout has no
           <responsive>/<variant> breakpoints, so this single <subagent>
           region (directly under <layout>, below its lines) is the sole
           fallback container for every terminal width. It uses progressive
           width breakpoints (the tool-agnostic "width" metric) to reveal
           more usage stats as the agent panel widens. -->
      <line>
        <field name="task-description"/>
        <field name="task-model" prefix="  "/>
        <flex/>
        <field name="task-duration" format="duration" when="width ge 48" min-width="7" align="right"/>
        <field name="task-tokens" prefix=" · ↓ " format="compact-number" optional="task-tokens" when="width ge 64" min-width="6" align="right"/>
        <field name="task-context-percent" prefix=" (" suffix=")" format="percent" precision="0" optional="task-context-percent" when="width ge 80" min-width="4" align="right"/>
      </line>
    </subagent>
  </layout>
</statusloom>
`

// DefaultDocument returns the built-in DSL source for tool. Unknown tools
// yield "".
func DefaultDocument(tool string) string {
	return defaultDocuments[tool]
}

// DocumentGitConfig derives a GitConfig from a document's optional <git/>
// element, applying the built-in defaults (3000ms / 200ms / include-untracked
// / collect-numstat) for a missing element or missing attributes.
func DocumentGitConfig(doc *dsl.Document) GitConfig {
	gc := GitConfig{CacheTTLMs: 3000, TimeoutMs: 200, IncludeUntracked: true, CollectNumstat: true}
	if doc == nil || doc.Root == nil || doc.Root.Git == nil {
		return gc
	}
	g := doc.Root.Git
	if g.CacheTTLMS != nil {
		gc.CacheTTLMs = *g.CacheTTLMS
	}
	if g.TimeoutMS != nil {
		gc.TimeoutMs = *g.TimeoutMS
	}
	if g.IncludeUntracked != nil {
		gc.IncludeUntracked = *g.IncludeUntracked
	}
	if g.CollectNumstat != nil {
		gc.CollectNumstat = *g.CollectNumstat
	}
	return gc
}
