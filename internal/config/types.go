// Package config defines the on-disk configuration schema for statusloom.
//
// This file contains type definitions only. The loader, defaulting, and
// validation logic are implemented separately.
package config

// Config is the root of the statusloom configuration file.
type Config struct {
	SchemaVersion int                   `json:"schemaVersion"`
	Shared        SharedConfig          `json:"shared"`
	Tools         map[string]ToolConfig `json:"tools"`
}

// SharedConfig holds settings shared across all tools.
type SharedConfig struct {
	Git GitConfig `json:"git"`
}

// GitConfig controls how git repository status is collected.
type GitConfig struct {
	CacheTTLMs       int  `json:"cacheTtlMs"`       // default 3000
	TimeoutMs        int  `json:"timeoutMs"`        // default 200
	IncludeUntracked bool `json:"includeUntracked"` // default true
	CollectNumstat   bool `json:"collectNumstat"`   // default true
}

// ToolConfig holds per-tool status-line configuration.
//
// A tool owns one or more named Layouts; ActiveLayout selects which one is
// rendered. See UnmarshalJSON (load.go) for the lenient handling of older
// documents that stored a single top-level "lines" array instead.
type ToolConfig struct {
	CompactThreshold int           `json:"compactThreshold,omitempty"` // terminal width; 0 = disabled
	ColorLevel       string        `json:"colorLevel,omitempty"`       // "none"|"ansi16"|"ansi256"|"truecolor"
	Context          ContextConfig `json:"context,omitempty"`
	Layouts          []Layout      `json:"layouts"`
	ActiveLayout     int           `json:"activeLayout"`
}

// Layout is a named arrangement of status lines. Lines is 1:1 with the
// terminal rows the layout would print (each inner slice is one line).
type Layout struct {
	Name  string         `json:"name"`
	Lines [][]WidgetSpec `json:"lines"`
}

// ContextConfig controls context-percentage computation and display.
type ContextConfig struct {
	PercentageMode string `json:"percentageMode,omitempty"` // "raw"|"usable"|"both"
	ReserveTokens  int    `json:"reserveTokens,omitempty"`  // 0 = auto by window size
}

// WidgetSpec configures a single widget instance within a status line.
type WidgetSpec struct {
	Type       string            `json:"type"`
	Text       string            `json:"text,omitempty"`
	Template   string            `json:"template,omitempty"` // e.g. "Model: {value}", "({value})"
	Flex       string            `json:"flex,omitempty"`     // flex-separator only: ""(=full) | "full" | "full-minus-<N>"
	Color      string            `json:"color,omitempty"`
	Bold       bool              `json:"bold,omitempty"`
	RawValue   bool              `json:"rawValue,omitempty"`
	Hyperlink  bool              `json:"hyperlink,omitempty"`  // OSC 8 hyperlink (linkable widgets only)
	ShowWhen   *Condition        `json:"showWhen,omitempty"`   // conditional visibility
	ColorRules []ColorRule       `json:"colorRules,omitempty"` // threshold-based color override
	Metadata   map[string]string `json:"metadata,omitempty"`
}

// Condition gates a content widget's visibility on a numeric metric.
type Condition struct {
	Source string  `json:"source,omitempty"` // ""|"self" = the widget's own metric; otherwise a named metric
	Op     string  `json:"op"`               // "lt"|"lte"|"gt"|"gte"|"eq"|"neq"
	Value  float64 `json:"value"`
}

// ColorRule overrides a widget's color when its self metric matches.
type ColorRule struct {
	Op    string  `json:"op"`
	Value float64 `json:"value"`
	Color string  `json:"color"`
}
