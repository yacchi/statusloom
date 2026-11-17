// Package modrec folds what the statusloom Claude Code mod records into the per-session record.
package modrec

import (
	"encoding/json"
	"sort"
	"time"

	"github.com/yacchi/statusloom/internal/schema"
)

// agentElem is the subset of an AgentInfo that ApplyAgents reads.
type agentElem struct {
	Kind   string `json:"kind"`
	Status string `json:"status"`
	Type   string `json:"type"`
	Cost   *struct {
		USD *float64 `json:"usd"`
	} `json:"cost"`
	Usage []json.RawMessage `json:"usage"`
}

// agentUsageElem is the subset of a TurnUsage/ModelUsage entry that ApplyAgents reads.
type agentUsageElem struct {
	InputTokens              float64 `json:"input_tokens"`
	OutputTokens             float64 `json:"output_tokens"`
	CacheReadInputTokens     float64 `json:"cache_read_input_tokens"`
	CacheCreationInputTokens float64 `json:"cache_creation_input_tokens"`
}

// ApplyAgents projects a verbatim $.agent.list() result (a JSON array of AgentInfo).
//
// When raw is a JSON array and (s.Agents == nil or !at.Before(s.Agents.At)), it
// replaces s.Agents with a fresh ModAgents stamped at and returns true; otherwise
// it leaves s untouched and returns false. Non-object elements are skipped.
func ApplyAgents(s *schema.ModSnapshot, at time.Time, raw json.RawMessage) bool {
	var elems []json.RawMessage
	if err := json.Unmarshal(raw, &elems); err != nil || elems == nil {
		return false
	}
	if s.Agents != nil && at.Before(s.Agents.At) {
		return false
	}

	agents := &schema.ModAgents{At: at}
	typeCounts := map[string]int{}
	var costSum float64
	costReported := false
	for _, raw := range elems {
		var a *agentElem
		if err := json.Unmarshal(raw, &a); err != nil || a == nil {
			continue
		}
		running := a.Status == "pending" || a.Status == "running" || a.Status == "waiting"
		switch {
		case a.Kind == "local_agent" && running:
			agents.Running++
			typeCounts[a.Type]++
			if a.Cost != nil && a.Cost.USD != nil {
				costSum += *a.Cost.USD
				costReported = true
			}
			for _, u := range a.Usage {
				var usage *agentUsageElem
				if err := json.Unmarshal(u, &usage); err != nil || usage == nil {
					continue
				}
				agents.Tokens += int(usage.InputTokens + usage.OutputTokens + usage.CacheReadInputTokens + usage.CacheCreationInputTokens)
			}
		case a.Kind == "in_process_teammate":
			if running {
				agents.TeammatesRunning++
			}
			if a.Status == "idle" {
				agents.TeammatesIdle++
			}
		}
	}

	if len(typeCounts) > 0 {
		agents.ByType = make([]schema.ModTypeCount, 0, len(typeCounts))
		for t, n := range typeCounts {
			agents.ByType = append(agents.ByType, schema.ModTypeCount{Type: t, Count: n})
		}
		sort.Slice(agents.ByType, func(i, j int) bool {
			if agents.ByType[i].Count != agents.ByType[j].Count {
				return agents.ByType[i].Count > agents.ByType[j].Count
			}
			return agents.ByType[i].Type < agents.ByType[j].Type
		})
	}
	if costReported {
		sum := costSum
		agents.CostUSD = &sum
	}

	s.Agents = agents
	return true
}

// contextCategoryElem is the subset of a ContextCategory that ApplyUsage reads.
type contextCategoryElem struct {
	Name   string  `json:"name"`
	Tokens float64 `json:"tokens"`
	Kind   string  `json:"kind"`
}

// ApplyUsage projects a verbatim $.session.usage({breakdown: 'summary'}) result (SessionUsage).
//
// When raw.context.breakdown is a JSON object and (s.Context == nil or
// !at.Before(s.Context.At)), it sets s.Context from the breakdown stamped at and
// returns true; otherwise it leaves s untouched and returns false. Only
// totalTokens, autoCompactThreshold (when auto-compact is enabled), and
// categories are stored; every other breakdown key is ignored.
func ApplyUsage(s *schema.ModSnapshot, at time.Time, raw json.RawMessage) bool {
	var usage struct {
		Context struct {
			Breakdown json.RawMessage `json:"breakdown"`
		} `json:"context"`
	}
	if err := json.Unmarshal(raw, &usage); err != nil {
		return false
	}
	var breakdown map[string]json.RawMessage
	if err := json.Unmarshal(usage.Context.Breakdown, &breakdown); err != nil || breakdown == nil {
		return false
	}
	if s.Context != nil && at.Before(s.Context.At) {
		return false
	}

	ctx := &schema.ModContext{At: at}

	var totalTokens float64
	if err := json.Unmarshal(breakdown["totalTokens"], &totalTokens); err == nil {
		ctx.TotalTokens = int(totalTokens)
	}

	var enabled bool
	var threshold *float64
	if err := json.Unmarshal(breakdown["isAutoCompactEnabled"], &enabled); err == nil && enabled {
		if err := json.Unmarshal(breakdown["autoCompactThreshold"], &threshold); err == nil && threshold != nil {
			t := int(*threshold)
			ctx.AutoCompactThreshold = &t
		}
	}

	var categories []json.RawMessage
	if err := json.Unmarshal(breakdown["categories"], &categories); err == nil {
		for _, c := range categories {
			var cat *contextCategoryElem
			if err := json.Unmarshal(c, &cat); err != nil || cat == nil {
				continue
			}
			ctx.Categories = append(ctx.Categories, schema.ModContextCategory{
				Name:   cat.Name,
				Tokens: int(cat.Tokens),
				Kind:   cat.Kind,
			})
		}
	}

	s.Context = ctx
	return true
}
