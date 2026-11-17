package render

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/yacchi/statusloom/internal/config"
	"github.com/yacchi/statusloom/internal/dsl"
	"github.com/yacchi/statusloom/internal/schema"
)

// modFieldText renders a statusloom-mod field (snap.Mod). It returns "" for
// every other spec.Type and for every mod field when snap.Mod is nil. A field
// and its self metric share the helpers below, so both always agree.
func modFieldText(spec config.WidgetSpec, snap schema.StatusSnapshot, compact bool) string {
	m := snap.Mod
	if m == nil {
		return ""
	}
	raw := spec.RawValue
	switch spec.Type {
	case "subagents-running", "subagents-tokens", "teammates-running", "teammates-idle":
		n, ok := modAgentInt(spec.Type, snap)
		if !ok {
			return ""
		}
		return renderInteger(n, raw, compact)

	case "subagents-by-type":
		if m.Agents == nil || len(m.Agents.ByType) == 0 {
			return ""
		}
		parts := make([]string, 0, len(m.Agents.ByType))
		for _, tc := range m.Agents.ByType {
			if tc.Count > 1 {
				parts = append(parts, tc.Type+"×"+strconv.Itoa(tc.Count))
			} else {
				parts = append(parts, tc.Type)
			}
		}
		return strings.Join(parts, " ")

	case "subagents-cost":
		v, ok := modCostUSD(snap)
		if !ok {
			return ""
		}
		text := strconv.FormatFloat(v, 'f', 2, 64)
		if raw {
			return text
		}
		return "$" + text

	case "last-turn-duration":
		d, ok := modLastTurnDuration(snap)
		if !ok {
			return ""
		}
		if raw {
			return strconv.Itoa(int(d / time.Second))
		}
		return formatDuration(d)

	case "last-turn-reason", "permission-mode":
		s, ok := modText(spec.Type, snap)
		if !ok {
			return ""
		}
		return s

	case "model-switches":
		n, ok := modCount(spec.Type, snap)
		if !ok {
			return ""
		}
		return renderInteger(n, raw, compact)

	case "autocompact-remaining":
		n, ok := modAutocompactRemaining(snap)
		if !ok {
			return ""
		}
		return renderInteger(n, raw, compact)

	case "context-breakdown":
		return modContextBreakdown(m)

	case "model-fallback":
		if !modFallback(snap) {
			return ""
		}
		return snap.Mod.SessionModel + "→" + snap.Mod.TurnModel
	}
	return ""
}

// modMetricValue resolves a numeric metric of mod-fields from snap.Mod. ok is
// false for a name that is not one of its numeric metrics, for a nil snap.Mod,
// and whenever mod-fields says the metric is unresolved.
func modMetricValue(name string, snap schema.StatusSnapshot) (float64, bool) {
	m := snap.Mod
	if m == nil {
		return 0, false
	}
	switch name {
	case "subagents-running", "subagents-tokens", "teammates-running", "teammates-idle":
		n, ok := modAgentInt(name, snap)
		return float64(n), ok
	case "subagents-cost-usd":
		v, ok := modCostUSD(snap)
		return v, ok
	case "last-turn-seconds":
		d, ok := modLastTurnDuration(snap)
		return d.Seconds(), ok
	case "model-switches":
		n, ok := modCount(name, snap)
		return float64(n), ok
	case "autocompact-remaining-tokens":
		n, ok := modAutocompactRemaining(snap)
		return float64(n), ok
	}
	return 0, false
}

// modTypedMetric resolves the bool and text metrics of mod-fields. handled is
// false for every other name. When handled, ok is false exactly when mod-fields
// says the metric is unresolved; a resolved text metric is
// dsl.Value{Kind: dsl.ValueString, Str: ...}, a bool dsl.Value{Kind: dsl.ValueBool, Bool: ...}.
func modTypedMetric(name string, snap schema.StatusSnapshot) (v dsl.Value, ok, handled bool) {
	switch name {
	case "model-fallback":
		if snap.Mod == nil {
			return dsl.Value{}, false, true
		}
		return dsl.Value{Kind: dsl.ValueBool, Bool: modFallback(snap)}, true, true
	case "last-turn-reason", "permission-mode":
		s, resolved := modText(name, snap)
		if !resolved {
			return dsl.Value{}, false, true
		}
		return dsl.Value{Kind: dsl.ValueString, Str: s}, true, true
	}
	return dsl.Value{}, false, false
}

// modAgentInt returns the agent count behind a subagents-* or teammates-*
// field or metric. ok is false when snap.Mod or its Agents summary is nil.
func modAgentInt(name string, snap schema.StatusSnapshot) (int, bool) {
	if snap.Mod == nil || snap.Mod.Agents == nil {
		return 0, false
	}
	a := snap.Mod.Agents
	switch name {
	case "subagents-running":
		return a.Running, true
	case "subagents-tokens":
		return a.Tokens, true
	case "teammates-running":
		return a.TeammatesRunning, true
	case "teammates-idle":
		return a.TeammatesIdle, true
	}
	return 0, false
}

// modCostUSD returns the running agents' summed cost. ok is false when the
// agent summary or its cost is absent.
func modCostUSD(snap schema.StatusSnapshot) (float64, bool) {
	if snap.Mod == nil || snap.Mod.Agents == nil || snap.Mod.Agents.CostUSD == nil {
		return 0, false
	}
	return *snap.Mod.Agents.CostUSD, true
}

// modLastTurnDuration returns the latest main-loop turn duration. ok is false
// until a turn has completed.
func modLastTurnDuration(snap schema.StatusSnapshot) (time.Duration, bool) {
	if snap.Mod == nil || snap.Mod.LastTurn == nil {
		return 0, false
	}
	return snap.Mod.LastTurn.Duration, true
}

// modCount returns a session counter. A recorded zero is resolved; ok is
// false only when snap.Mod is nil.
func modCount(name string, snap schema.StatusSnapshot) (int, bool) {
	m := snap.Mod
	if m == nil {
		return 0, false
	}
	switch name {
	case "model-switches":
		return m.ModelSwitches, true
	}
	return 0, false
}

// modText returns a text field or metric value and whether it is resolved.
// last-turn-reason resolves once a turn has completed; permission-mode
// resolves when non-empty.
func modText(name string, snap schema.StatusSnapshot) (string, bool) {
	m := snap.Mod
	if m == nil {
		return "", false
	}
	switch name {
	case "last-turn-reason":
		if m.LastTurn == nil {
			return "", false
		}
		return m.LastTurn.Reason, true
	case "permission-mode":
		return m.PermissionMode, m.PermissionMode != ""
	}
	return "", false
}

// modFallback reports whether the model that answered the latest turn differs
// from the session model: Claude Code fell back to another model on its own.
// It is false while either model is unreported.
func modFallback(snap schema.StatusSnapshot) bool {
	m := snap.Mod
	return m != nil && m.SessionModel != "" && m.TurnModel != "" && m.SessionModel != m.TurnModel
}

// modAutocompactRemaining returns the tokens left before auto-compact fires,
// never below zero. The live count is the statusline's total input when the
// current API call is known, otherwise the breakdown estimate. ok is false
// when no breakdown or no threshold is known.
func modAutocompactRemaining(snap schema.StatusSnapshot) (int, bool) {
	if snap.Mod == nil || snap.Mod.Context == nil || snap.Mod.Context.AutoCompactThreshold == nil {
		return 0, false
	}
	live := snap.Mod.Context.TotalTokens
	if c := snap.Session.Context; c != nil && c.Current != nil {
		live = c.TotalInputTokens
	}
	remaining := *snap.Mod.Context.AutoCompactThreshold - live
	if remaining < 0 {
		remaining = 0
	}
	return remaining, true
}

// modContextBreakdown lists the used context categories largest first, e.g.
// "Tools 9.0k · System 3.0k". It returns "" when none qualify. The snapshot's
// own category order is never changed.
func modContextBreakdown(m *schema.ModSnapshot) string {
	if m == nil || m.Context == nil {
		return ""
	}
	used := make([]schema.ModContextCategory, 0, len(m.Context.Categories))
	for _, c := range m.Context.Categories {
		if c.Kind == "used" && c.Tokens > 0 {
			used = append(used, c)
		}
	}
	if len(used) == 0 {
		return ""
	}
	sort.SliceStable(used, func(i, j int) bool {
		return used[i].Tokens > used[j].Tokens
	})
	parts := make([]string, 0, len(used))
	for _, c := range used {
		parts = append(parts, c.Name+" "+formatCompactK(c.Tokens))
	}
	return strings.Join(parts, " · ")
}
