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

// LoadDocument returns tool's current committed document, parsed but not
// re-validated: a committed revision already passed the store's Save
// validation boundary (dsl.ParseAndValidate), so rendering only needs to
// Parse it again (plans/config-store-and-format.md §7). A missing store or
// tool ref falls back to DefaultDocument(tool), parsed the same way. A store
// read/decode failure is returned as err (with a nil document).
func LoadDocument(tool string) (*dsl.Document, []dsl.Diagnostic, error) {
	st, err := store.Open()
	if err != nil {
		return nil, nil, err
	}
	rev, _, ok, err := st.Current(tool)
	if err != nil {
		return nil, nil, err
	}
	src := DefaultDocument(tool)
	if ok {
		src = rev.Source
	}
	doc, diags := dsl.Parse(src)
	return doc, diags, nil
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
