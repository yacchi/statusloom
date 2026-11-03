package sessionexport

import (
	"encoding/json"
	"errors"
	"fmt"
)

// RowFromPayload derives the summary row from the raw statusline stdin payload.
// raw must be a JSON object; invalid JSON or any other JSON value -> (Row{}, error).
// String fields that are absent, null or not a JSON string become "". PromptCache is nil
// unless the payload carries a prompt_cache object. SeenAt is left 0 for Observe to set.
func RowFromPayload(raw []byte, pid int, startedAt int64, configDir string) (Row, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return Row{}, fmt.Errorf("sessionexport: decode statusline payload: %w", err)
	}
	if top == nil {
		return Row{}, errors.New("sessionexport: statusline payload is not a JSON object")
	}

	var model map[string]json.RawMessage
	_ = json.Unmarshal(top["model"], &model)

	dp, err := DetailPath(pid, startedAt)
	if err != nil {
		return Row{}, err
	}

	return Row{
		PID:            pid,
		StartedAt:      startedAt,
		SessionID:      rowString(top["session_id"]),
		TranscriptPath: rowString(top["transcript_path"]),
		ConfigDir:      configDir,
		Cwd:            rowString(top["cwd"]),
		Model:          rowString(model["id"]),
		PromptCache:    rowPromptCache(top["prompt_cache"]),
		SeenAt:         0,
		DetailPath:     dp,
	}, nil
}

// rowString returns v as a string, or "" when v is absent, null or not a JSON string.
func rowString(v json.RawMessage) string {
	var s string
	if err := json.Unmarshal(v, &s); err != nil {
		return ""
	}
	return s
}

// rowEpoch returns v as an int64 truncated toward zero. Absent, null, non-number and
// out-of-range values give nil.
func rowEpoch(v json.RawMessage) *int64 {
	var x any
	if err := json.Unmarshal(v, &x); err != nil {
		return nil
	}
	f, ok := x.(float64)
	if !ok {
		return nil
	}
	if !(f >= -0x1p63 && f < 0x1p63) {
		return nil
	}
	n := int64(f)
	return &n
}

// rowPromptCache returns the promptCache object for v, or nil when v is absent, null or not a
// JSON object.
func rowPromptCache(v json.RawMessage) *PromptCache {
	var pc map[string]json.RawMessage
	if err := json.Unmarshal(v, &pc); err != nil || pc == nil {
		return nil
	}
	return &PromptCache{
		TTL:       rowString(pc["ttl"]),
		ExpiresAt: rowEpoch(pc["expires_at"]),
	}
}
