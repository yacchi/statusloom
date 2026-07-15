// Package config defines the configuration types statusloom's renderer
// consumes.
//
// The on-disk configuration source is the DSL document (<tool>.xml, see
// document.go). The types in this file are the render-time primitives the
// renderer resolves a document into: ToolConfig carries the tool-level
// settings, WidgetSpec identifies a single field to render, and GitConfig /
// ContextConfig hold the git-collection and context-percentage knobs.
package config

// GitConfig controls how git repository status is collected.
type GitConfig struct {
	CacheTTLMs       int  `json:"cacheTtlMs"`       // default 3000
	TimeoutMs        int  `json:"timeoutMs"`        // default 200
	IncludeUntracked bool `json:"includeUntracked"` // default true
	CollectNumstat   bool `json:"collectNumstat"`   // default true
}

// ToolConfig holds per-tool status-line settings resolved from a DSL
// document's root element.
type ToolConfig struct {
	CompactThreshold int           `json:"compactThreshold,omitempty"` // terminal width; 0 = disabled
	ColorLevel       string        `json:"colorLevel,omitempty"`       // "none"|"ansi16"|"ansi256"|"truecolor"
	Context          ContextConfig `json:"context,omitempty"`
}

// ContextConfig controls context-percentage computation and display.
type ContextConfig struct {
	PercentageMode string `json:"percentageMode,omitempty"` // "raw"|"usable"|"both"
	ReserveTokens  int    `json:"reserveTokens,omitempty"`  // 0 = auto by window size
}

// WidgetSpec identifies a single field to render. The DSL render path
// resolves each <field> node into a WidgetSpec; renderContent (widgets.go)
// switches on Type and honors RawValue.
type WidgetSpec struct {
	Type     string `json:"type"`
	RawValue bool   `json:"rawValue,omitempty"`
}
