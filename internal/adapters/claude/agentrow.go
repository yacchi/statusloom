package claude

// This file decodes the AgentRow mod's ui.render request (agent-row-protocol):
// one agent-panel row plus the agent tree it sits in. The mod forwards the
// props verbatim, so all interpretation lives here. Each key is read on its
// own, so one wrong-typed key stays zero or nil instead of failing the decode.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/yacchi/statusloom/internal/schema"
)

// maxAgentDepth bounds the parentId walk so a cyclic agents tree terminates.
const maxAgentDepth = 16

// AgentRowRequest is one decoded `statusloom claude-agent-row` request (agent-row-protocol).
type AgentRowRequest struct {
	V         int                     // request "v"; 0 when absent or not an integer
	SessionID string                  // "sessionId"
	Columns   int                     // row.columns; 0 when absent
	HasBody   bool                    // row has a "body" key holding a JSON string ("" included)
	Depth     int                     // nesting depth of the row's agent, see DecodeAgentRow
	Task      schema.SubagentSnapshot // the row mapped onto the <subagent> task fields
}

// DecodeAgentRow decodes a claude-agent-row request. It fails only when raw is
// not a JSON object or when "row" is missing or not a JSON object. Unknown keys
// and unexpected value types of single fields never fail: that field stays zero
// or nil. Depth is 0 when row.parentId is absent or empty; otherwise it is 1
// plus the depth of the parent, following parentId through agents. The walk
// stops when an id is not in agents (that hop still counts) and after 16 hops.
func DecodeAgentRow(raw []byte) (AgentRowRequest, error) {
	top := objectOf(raw)
	if top == nil {
		if err := json.Unmarshal(raw, &map[string]json.RawMessage{}); err != nil {
			return AgentRowRequest{}, fmt.Errorf("claude: invalid agent-row JSON: %w", err)
		}
		return AgentRowRequest{}, errors.New("claude: invalid agent-row JSON: not an object")
	}

	row := objectOf(top["row"])
	if row == nil {
		return AgentRowRequest{}, errors.New("claude: agent-row request has no row object")
	}

	depth := agentDepth(row, top["agents"])
	inTokens, outTokens, cacheRead, cacheCreate := agentUsage(row["usage"])

	req := AgentRowRequest{
		V:         rawInt(top, "v"),
		SessionID: rawString(top, "sessionId"),
		Columns:   rawInt(row, "columns"),
		HasBody:   hasLeadingByte(row["body"], '"'),
		Depth:     depth,
	}

	status := rawString(row, "status")
	if status == "" {
		status = rawString(row, "taskStatus")
	}

	var startedAt time.Time
	if ms := rawInt64(row, "startTime"); ms > 0 {
		startedAt = time.UnixMilli(ms)
	}

	model := rawString(row, "model")
	var costUSD *float64
	if usd, ok := rawFloat(objectOf(row["cost"]), "usd"); ok {
		costUSD = &usd
	}

	req.Task = schema.SubagentSnapshot{
		ID:                  rawString(row, "id"),
		Type:                rawString(row, "kind"),
		Status:              status,
		Description:         rawString(row, "description"),
		Label:               rawString(row, "label"),
		StartedAt:           startedAt,
		ModelID:             model,
		ModelDisplay:        PrettyModelName(model),
		ContextWindowSize:   rawInt(row, "contextWindowSize"),
		TokenCount:          rawInt(row, "tokenCount"),
		Cwd:                 rawString(row, "cwd"),
		Effort:              agentEffort(row["effort"]),
		AgentType:           rawString(row, "type"),
		Name:                rawString(row, "name"),
		CostUSD:             costUSD,
		Depth:               &depth,
		InputTokens:         inTokens,
		OutputTokens:        outTokens,
		CacheReadTokens:     cacheRead,
		CacheCreationTokens: cacheCreate,
	}
	return req, nil
}

// agentEffort reads the mod's effort key. A non-empty string is kept as is; a
// number is kept as its literal JSON text so no precision is lost. Absent,
// null, empty, or any other JSON type yields nil.
func agentEffort(raw json.RawMessage) *string {
	s := strings.TrimSpace(string(raw))
	if s == "" {
		return nil
	}
	switch {
	case s[0] == '"':
		var str string
		if err := json.Unmarshal([]byte(s), &str); err != nil || str == "" {
			return nil
		}
		return &str
	case s[0] == '-' || (s[0] >= '0' && s[0] <= '9'):
		return &s
	}
	return nil
}

// agentDepth follows row.parentId through agents and returns the number of
// hops, capped at maxAgentDepth. A parent id missing from agents still counts
// as one hop. An absent or non-array agents value means an empty tree.
func agentDepth(row map[string]json.RawMessage, agents json.RawMessage) int {
	parent := agentParents(agents)
	depth := 0
	cur := rawString(row, "parentId")
	for cur != "" && depth < maxAgentDepth {
		depth++
		p, ok := parent[cur]
		if !ok {
			break
		}
		cur = p
	}
	return depth
}

// agentParents maps each agent id to its parentId. Non-object entries and
// entries without an id are skipped; the first entry for an id wins.
func agentParents(raw json.RawMessage) map[string]string {
	parent := map[string]string{}
	var list []json.RawMessage
	if !hasLeadingByte(raw, '[') || json.Unmarshal(raw, &list) != nil {
		return parent
	}
	for _, el := range list {
		m := objectOf(el)
		if m == nil {
			continue
		}
		id := rawString(m, "id")
		if id == "" {
			continue
		}
		if _, seen := parent[id]; seen {
			continue
		}
		parent[id] = rawString(m, "parentId")
	}
	return parent
}

// agentUsage sums the per-entry token counts of the usage array. The four
// results are nil when usage is not an array (absent, null, or another type)
// and non-nil zeros when it is an empty array.
func agentUsage(raw json.RawMessage) (in, out, cacheRead, cacheCreate *int) {
	if !hasLeadingByte(raw, '[') {
		return nil, nil, nil, nil
	}
	var list []json.RawMessage
	if json.Unmarshal(raw, &list) != nil {
		return nil, nil, nil, nil
	}
	var sumIn, sumOut, sumRead, sumCreate int
	for _, el := range list {
		m := objectOf(el)
		if m == nil {
			continue
		}
		sumIn += rawInt(m, "input_tokens")
		sumOut += rawInt(m, "output_tokens")
		sumRead += rawInt(m, "cache_read_input_tokens")
		sumCreate += rawInt(m, "cache_creation_input_tokens")
	}
	return &sumIn, &sumOut, &sumRead, &sumCreate
}

// objectOf decodes raw as a JSON object. It returns nil when raw is not an
// object, including JSON null, which Unmarshal would otherwise accept as a nil map.
func objectOf(raw []byte) map[string]json.RawMessage {
	if !hasLeadingByte(raw, '{') {
		return nil
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		return nil
	}
	return m
}

// hasLeadingByte reports whether the first non-whitespace byte of raw is c.
func hasLeadingByte(raw []byte, c byte) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && trimmed[0] == c
}

// rawString returns m[key] as a string, or "" when the key is absent or not a string.
func rawString(m map[string]json.RawMessage, key string) string {
	var s string
	if json.Unmarshal(m[key], &s) != nil {
		return ""
	}
	return s
}

// rawInt returns m[key] as an int, or 0 when the key is absent or not an integer.
func rawInt(m map[string]json.RawMessage, key string) int {
	var n int
	if json.Unmarshal(m[key], &n) != nil {
		return 0
	}
	return n
}

// rawInt64 returns m[key] as an int64, or 0 when the key is absent or not an integer.
func rawInt64(m map[string]json.RawMessage, key string) int64 {
	var n int64
	if json.Unmarshal(m[key], &n) != nil {
		return 0
	}
	return n
}

// rawFloat returns m[key] as a float64 and whether it is a JSON number. A nil
// map, a missing key, and null all report false.
func rawFloat(m map[string]json.RawMessage, key string) (float64, bool) {
	raw := bytes.TrimSpace(m[key])
	if len(raw) == 0 || !(raw[0] == '-' || (raw[0] >= '0' && raw[0] <= '9')) {
		return 0, false
	}
	var f float64
	if json.Unmarshal(raw, &f) != nil {
		return 0, false
	}
	return f, true
}
