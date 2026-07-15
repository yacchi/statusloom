// Package adapters defines the interface implemented by each coding-agent
// specific adapter (Claude Code, Codex, GitHub Copilot, ...).
package adapters

import (
	"encoding/json"

	"github.com/yacchi/statusloom/internal/schema"
)

// Adapter decodes a specific tool's statusLine input into the normalized
// schema.StatusSnapshot model.
//
// WidgetCatalog() will be added in a later milestone.
type Adapter interface {
	// ID returns the tool identifier this adapter handles.
	ID() schema.ToolID

	// Detect reports whether raw looks like this tool's input structure.
	// raw is the top-level JSON object's fields, undecoded.
	Detect(raw map[string]json.RawMessage) bool

	// Decode parses raw stdin bytes into a normalized snapshot.
	Decode(raw []byte) (schema.StatusSnapshot, error)
}
