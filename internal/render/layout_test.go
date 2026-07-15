package render

import (
	"testing"

	"github.com/yacchi/statusloom/internal/config"
	"github.com/yacchi/statusloom/internal/schema"
)

// TestRenderFallback_Degenerate verifies RenderFallback is non-empty even
// when neither model nor version is available, and uses the version alone
// when only it is present.
func TestRenderFallback_Degenerate(t *testing.T) {
	var snap schema.StatusSnapshot // no model, no version
	cfg := config.ToolConfig{ColorLevel: "none"}
	got := RenderFallback(snap, cfg, Options{Width: 120, Now: fixedNow})
	if got == "" {
		t.Fatal("RenderFallback must never be empty")
	}
	if got != "statusloom" {
		t.Errorf("degenerate fallback = %q, want %q", got, "statusloom")
	}

	// With only a version present, the fallback uses it.
	snap.Tool.Version = "9.9.9"
	got = RenderFallback(snap, cfg, Options{Width: 120, Now: fixedNow})
	if got != "v9.9.9" {
		t.Errorf("version-only fallback = %q, want %q", got, "v9.9.9")
	}
}
