package claude

// This file implements the statusloom adapter for Claude Code's
// subagentStatusLine stdin JSON payload (one row per agent-panel task),
// which is a separate, slimmer shape from the regular statusLine payload
// Decode handles: it carries no session-level fields (model/version/
// workspace/etc.) at its top level, only session_id/transcript_path/cwd/
// prompt_id/columns and a tasks[] array. See
// fixtures/claude/subagent-running.json and subagent-completed.json for
// real captured shapes.

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/yacchi/statusloom/internal/schema"
)

// subagentPayload mirrors the stdin JSON shape of a subagentStatusLine
// invocation. Unknown fields are ignored by encoding/json by default.
type subagentPayload struct {
	Columns int            `json:"columns"`
	Tasks   []subagentTask `json:"tasks"`
}

// subagentTask mirrors one entry of the payload's tasks[] array.
type subagentTask struct {
	ID                string `json:"id"`
	Type              string `json:"type"`
	Status            string `json:"status"`
	Description       string `json:"description"`
	Label             string `json:"label"`
	StartTime         int64  `json:"startTime"` // epoch milliseconds
	Model             string `json:"model"`
	ContextWindowSize int    `json:"contextWindowSize"`
	TokenCount        int    `json:"tokenCount"`
	Cwd               string `json:"cwd"`
}

// DecodeSubagent parses Claude Code's subagentStatusLine stdin JSON into one
// schema.SubagentSnapshot per task, in payload order. Unlike Decode (the
// session statusLine payload), there is no single mandatory field that
// distinguishes "not a subagent payload": a missing/empty tasks array
// decodes to an empty, non-nil slice rather than an error. A task whose
// "model" is empty or absent decodes with ModelID/ModelDisplay both empty
// (an unresolved-model task).
func DecodeSubagent(raw []byte) ([]schema.SubagentSnapshot, error) {
	var p subagentPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("claude: invalid subagent JSON: %w", err)
	}

	out := make([]schema.SubagentSnapshot, 0, len(p.Tasks))
	for _, t := range p.Tasks {
		snap := schema.SubagentSnapshot{
			ID:                t.ID,
			Type:              t.Type,
			Status:            t.Status,
			Description:       t.Description,
			Label:             t.Label,
			ModelID:           t.Model,
			ModelDisplay:      PrettyModelName(t.Model),
			ContextWindowSize: t.ContextWindowSize,
			TokenCount:        t.TokenCount,
			Cwd:               t.Cwd,
		}
		if t.StartTime > 0 {
			snap.StartedAt = time.UnixMilli(t.StartTime)
		}
		out = append(out, snap)
	}
	return out, nil
}

// DecodeSubagentColumns extracts just the top-level `columns` field of a
// subagentStatusLine stdin payload. It is best-effort: malformed JSON
// yields 0 ("unknown width", matching render.Options.Width's zero-means-
// unknown convention) instead of an error, since callers already surface
// DecodeSubagent's error for the same input.
func DecodeSubagentColumns(raw []byte) int {
	var p struct {
		Columns int `json:"columns"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return 0
	}
	return p.Columns
}

// knownModelFamilies are the "claude-<family>-..." prefixes PrettyModelName
// recognizes for its version-normalizing form. An id whose leading segment
// does not match one of these falls back to naive prettification instead.
var knownModelFamilies = map[string]bool{
	"opus":   true,
	"sonnet": true,
	"haiku":  true,
	"fable":  true,
}

// trailingDateSuffix matches a Claude model id's optional trailing release
// date segment, e.g. "-20251001" in "claude-haiku-4-5-20251001".
var trailingDateSuffix = regexp.MustCompile(`-\d{8}$`)

// contextWindowSuffixes are trailing context-window annotations
// PrettyModelName strips before parsing the family/version, e.g.
// "claude-sonnet-5[1m]" and "claude-opus-4-8 (1M context)".
var contextWindowSuffixes = []string{"[1m]", "(1M context)"}

// PrettyModelName best-effort formats a Claude Code model id into a short
// display name:
//
//	"claude-opus-4-8"             -> "Opus 4.8"
//	"claude-sonnet-5"             -> "Sonnet 5"
//	"claude-haiku-4-5-20251001"   -> "Haiku 4.5"
//
// The leading "claude-" prefix, a trailing "-YYYYMMDD" release date, and a
// trailing context-window annotation ("[1m]" / "(1M context)") are stripped
// first. When the remaining leading segment names a recognized family
// (opus/sonnet/haiku/fable), the family is Title-cased and any remaining
// "-"-separated version segments are dot-joined ("4-8" -> "4.8"). Otherwise
// the id is naively prettified: "-" becomes " " and each word is
// Title-cased. Empty input yields "".
func PrettyModelName(id string) string {
	s := strings.TrimSpace(id)
	if s == "" {
		return ""
	}
	s = strings.TrimPrefix(s, "claude-")
	s = stripContextWindowSuffix(s)
	s = trailingDateSuffix.ReplaceAllString(s, "")

	parts := strings.Split(s, "-")
	family := strings.ToLower(parts[0])
	if knownModelFamilies[family] {
		out := titleWord(family)
		if len(parts) > 1 {
			out += " " + strings.Join(parts[1:], ".")
		}
		return out
	}

	words := strings.Fields(strings.ReplaceAll(s, "-", " "))
	for i, w := range words {
		words[i] = titleWord(w)
	}
	return strings.Join(words, " ")
}

// stripContextWindowSuffix removes a trailing context-window annotation
// (and any separating whitespace) from a model id.
func stripContextWindowSuffix(s string) string {
	for _, suffix := range contextWindowSuffixes {
		if strings.HasSuffix(s, suffix) {
			return strings.TrimSpace(strings.TrimSuffix(s, suffix))
		}
	}
	return s
}

// titleWord upper-cases w's first rune and lower-cases the rest.
func titleWord(w string) string {
	if w == "" {
		return w
	}
	r := []rune(w)
	return strings.ToUpper(string(r[0])) + strings.ToLower(string(r[1:]))
}
