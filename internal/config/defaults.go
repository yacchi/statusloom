package config

import "fmt"

// ApplyDefaults fills zero values in place.
//
// Limitation: GitConfig.IncludeUntracked and GitConfig.CollectNumstat
// default to true, but once a JSON document has been unmarshaled into a
// GitConfig, a decoded `false` is indistinguishable from a field that was
// simply absent from the document (both come out as the Go zero value).
// ApplyDefaults therefore does NOT touch those two booleans — it cannot
// tell whether they should be defaulted. Detecting "was shared.git present
// at all" requires inspecting the raw JSON before it is unmarshaled, which
// Load/LoadFrom do via a raw-message pre-scan (see load.go). Callers that
// build a Config by hand and want the "true" defaults applied must set
// them explicitly, as Default() does below.
func ApplyDefaults(c *Config) {
	if c.Shared.Git.CacheTTLMs == 0 {
		c.Shared.Git.CacheTTLMs = 3000
	}
	if c.Shared.Git.TimeoutMs == 0 {
		c.Shared.Git.TimeoutMs = 200
	}

	for name, tool := range c.Tools {
		if tool.ColorLevel == "" {
			tool.ColorLevel = "ansi16"
		}
		if tool.Context.PercentageMode == "" {
			tool.Context.PercentageMode = "usable"
		}
		// Inject the built-in preset's layouts when a tool has none, so a
		// hand-built or partial config still renders something.
		if len(tool.Layouts) == 0 {
			tool.Layouts = Default().Tools["claude-code"].Layouts
		}
		// Clamp the active index into range; render falls back gracefully,
		// but keeping the stored value valid avoids surprises elsewhere.
		if tool.ActiveLayout < 0 || tool.ActiveLayout >= len(tool.Layouts) {
			tool.ActiveLayout = 0
		}
		// Name every layout so the UI always has a label to show.
		for i := range tool.Layouts {
			if tool.Layouts[i].Name == "" {
				tool.Layouts[i].Name = fmt.Sprintf("Layout %d", i+1)
			}
		}
		c.Tools[name] = tool
	}
}

// Default returns the built-in configuration (schemaVersion 1) with the
// default claude-code preset, as described in
// statusloom-local-development-plan.md section 10.
func Default() *Config {
	return &Config{
		SchemaVersion: 1,
		Shared: SharedConfig{
			Git: GitConfig{
				CacheTTLMs:       3000,
				TimeoutMs:        200,
				IncludeUntracked: true,
				CollectNumstat:   true,
			},
		},
		Tools: map[string]ToolConfig{
			"claude-code": {
				CompactThreshold: 60,
				ColorLevel:       "ansi16",
				Context: ContextConfig{
					PercentageMode: "usable",
				},
				ActiveLayout: 0,
				Layouts: []Layout{
					{
						Name: "Default",
						Lines: [][]WidgetSpec{
							{
								{Type: "model", Color: "cyan"},
								{Type: "separator", Text: " | "},
								{Type: "thinking-effort"},
								{Type: "separator", Text: " | "},
								{Type: "context-length"},
								{Type: "separator", Text: " | "},
								{Type: "context-percentage-usable"},
								{Type: "separator", Text: " | "},
								{Type: "session-cost"},
								{Type: "separator", Text: " | "},
								{Type: "git-branch", Color: "magenta"},
								{Type: "separator", Text: " | "},
								{Type: "git-changes", Color: "yellow"},
							},
							{
								// The usage widgets render bare values ("27%");
								// their labels live in these default templates.
								{Type: "five-hour-usage", Template: "5h: {value}"},
								{Type: "separator", Text: " | "},
								{Type: "five-hour-reset"},
								{Type: "separator", Text: " | "},
								{Type: "weekly-usage", Template: "7d: {value}"},
								{Type: "separator", Text: " | "},
								{Type: "weekly-reset"},
								{Type: "separator", Text: " | "},
								{Type: "tool-version"},
							},
						},
					},
				},
			},
		},
	}
}
