package detect

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/yacchi/statusloom/internal/schema"
)

func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	path := filepath.Join("..", "..", "fixtures", "claude", name)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading fixture %s: %v", name, err)
	}
	return b
}

func TestDetect_ExplicitToolWins(t *testing.T) {
	// Even with garbage stdin, an explicit tool takes priority.
	id, err := Detect("claude-code", []byte("not json at all"))
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if id != schema.ToolClaudeCode {
		t.Errorf("id = %v, want %v", id, schema.ToolClaudeCode)
	}
}

func TestDetect_ExplicitToolUnknown(t *testing.T) {
	_, err := Detect("some-unknown-tool", nil)
	if err == nil {
		t.Fatalf("expected error for unknown explicit tool")
	}
}

func TestDetect_ClaudeFixtureStructure(t *testing.T) {
	raw := loadFixture(t, "full.json")
	id, err := Detect("", raw)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if id != schema.ToolClaudeCode {
		t.Errorf("id = %v, want %v", id, schema.ToolClaudeCode)
	}
}

func TestDetect_SingleFieldNotEnough(t *testing.T) {
	_, err := Detect("", []byte(`{"session_id":"x"}`))
	if err == nil {
		t.Fatalf("expected error: session_id alone must not be detected as claude-code")
	}
}

func TestDetect_Garbage(t *testing.T) {
	_, err := Detect("", []byte("not json at all"))
	if err == nil {
		t.Fatalf("expected error for undetectable input")
	}
}
