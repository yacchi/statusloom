// Package detect determines which coding-agent tool produced a given
// statusLine invocation.
package detect

import (
	"encoding/json"
	"fmt"

	"github.com/yacchi/statusloom/internal/adapters"
	"github.com/yacchi/statusloom/internal/adapters/claude"
	"github.com/yacchi/statusloom/internal/schema"
)

// registeredAdapters lists the adapters consulted for structural
// detection, in priority order. Only Claude Code is implemented so far;
// Codex and GitHub Copilot adapters will be added here once they exist.
var registeredAdapters = []adapters.Adapter{
	claude.New(),
}

// explicitToolIDs maps the accepted --tool / subcommand values to their
// schema.ToolID.
var explicitToolIDs = map[string]schema.ToolID{
	string(schema.ToolClaudeCode): schema.ToolClaudeCode,
	string(schema.ToolCodex):      schema.ToolCodex,
	string(schema.ToolCopilot):    schema.ToolCopilot,
}

// Detect determines which tool produced the given stdin payload.
//
// explicitTool is the value of an explicit --tool flag or dedicated
// subcommand ("" if none was given). raw is the raw stdin bytes, used
// for structural sniffing when explicitTool is not given.
//
// Priority:
//  1. explicitTool, if non-empty
//  2. stdin structure, checked against each registered adapter's Detect
//  3. otherwise, an error
//
// Env var / argv0 based detection is future work; not implemented here.
func Detect(explicitTool string, raw []byte) (schema.ToolID, error) {
	if explicitTool != "" {
		id, ok := explicitToolIDs[explicitTool]
		if !ok {
			return "", fmt.Errorf("detect: unknown tool %q", explicitTool)
		}
		return id, nil
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err == nil {
		for _, a := range registeredAdapters {
			if a.Detect(fields) {
				return a.ID(), nil
			}
		}
	}

	return "", fmt.Errorf("detect: could not determine tool from input; pass --tool explicitly")
}
