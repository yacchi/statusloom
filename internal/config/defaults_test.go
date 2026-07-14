package config

import "testing"

// TestApplyDefaults_InjectsLayoutsWhenEmpty verifies that a tool with no
// layouts is backfilled from the built-in preset.
func TestApplyDefaults_InjectsLayoutsWhenEmpty(t *testing.T) {
	c := &Config{
		SchemaVersion: 1,
		Tools: map[string]ToolConfig{
			"claude-code": {}, // no layouts
		},
	}
	ApplyDefaults(c)

	tool := c.Tools["claude-code"]
	want := Default().Tools["claude-code"].Layouts
	if len(tool.Layouts) != len(want) || len(want) == 0 {
		t.Fatalf("Layouts = %#v, want the built-in default layouts", tool.Layouts)
	}
	if tool.Layouts[0].Name != want[0].Name {
		t.Errorf("injected layout name = %q, want %q", tool.Layouts[0].Name, want[0].Name)
	}
}

// TestApplyDefaults_ClampsActiveLayout verifies that an out-of-range
// ActiveLayout is reset to 0.
func TestApplyDefaults_ClampsActiveLayout(t *testing.T) {
	for _, active := range []int{-1, 2, 99} {
		c := &Config{
			SchemaVersion: 1,
			Tools: map[string]ToolConfig{
				"claude-code": {
					ActiveLayout: active,
					Layouts: []Layout{
						{Name: "A", Lines: [][]WidgetSpec{{{Type: "model"}}}},
						{Name: "B", Lines: [][]WidgetSpec{{{Type: "tool-version"}}}},
					},
				},
			},
		}
		ApplyDefaults(c)
		if got := c.Tools["claude-code"].ActiveLayout; got != 0 {
			t.Errorf("ActiveLayout %d clamped to %d, want 0", active, got)
		}
	}
}

// TestApplyDefaults_KeepsValidActiveLayout verifies an in-range index is
// left untouched.
func TestApplyDefaults_KeepsValidActiveLayout(t *testing.T) {
	c := &Config{
		SchemaVersion: 1,
		Tools: map[string]ToolConfig{
			"claude-code": {
				ActiveLayout: 1,
				Layouts: []Layout{
					{Name: "A", Lines: [][]WidgetSpec{{{Type: "model"}}}},
					{Name: "B", Lines: [][]WidgetSpec{{{Type: "tool-version"}}}},
				},
			},
		},
	}
	ApplyDefaults(c)
	if got := c.Tools["claude-code"].ActiveLayout; got != 1 {
		t.Errorf("ActiveLayout = %d, want 1 (unchanged)", got)
	}
}

// TestApplyDefaults_NamesUnnamedLayouts verifies that empty layout names
// are filled with a 1-based "Layout N" default.
func TestApplyDefaults_NamesUnnamedLayouts(t *testing.T) {
	c := &Config{
		SchemaVersion: 1,
		Tools: map[string]ToolConfig{
			"claude-code": {
				Layouts: []Layout{
					{Lines: [][]WidgetSpec{{{Type: "model"}}}}, // unnamed -> "Layout 1"
					{Name: "Custom", Lines: [][]WidgetSpec{{{Type: "model"}}}},
					{Lines: [][]WidgetSpec{{{Type: "model"}}}}, // unnamed -> "Layout 3"
				},
			},
		},
	}
	ApplyDefaults(c)
	got := c.Tools["claude-code"].Layouts
	if got[0].Name != "Layout 1" {
		t.Errorf("Layouts[0].Name = %q, want %q", got[0].Name, "Layout 1")
	}
	if got[1].Name != "Custom" {
		t.Errorf("Layouts[1].Name = %q, want %q (unchanged)", got[1].Name, "Custom")
	}
	if got[2].Name != "Layout 3" {
		t.Errorf("Layouts[2].Name = %q, want %q", got[2].Name, "Layout 3")
	}
}
