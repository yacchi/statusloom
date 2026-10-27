package cli

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/yacchi/statusloom/internal/dsl"
	"github.com/yacchi/statusloom/internal/schema"
)

// metricOverrides holds the `--set METRIC=VALUE` overrides of one `claude` /
// `render` invocation. It implements flag.Value, so a bad entry is rejected
// during flag parsing (exit 2, before stdin is read).
//
// Mechanism: an override is not a metric-lookup shim. It is written into the
// very data the renderer reads - the snapshot's Account block for the numeric
// metrics, and a copy of the .claude.json account profile for the text
// metrics - so the displayed field and every when / color-rule / optional
// evaluation resolve from one value and cannot disagree. The overrides are
// applied only after the account/extra-usage cache merge and after every
// cache write of the render pass, so they win over cached and stdin values
// and never reach any cache file.
type metricOverrides struct {
	entries []metricOverride
}

type metricOverride struct {
	name string
	num  float64
	text string
}

// numericOverrideSetters maps each numeric metric --set supports to the
// snapshot mutation that makes the metric (and its field) report v.
var numericOverrideSetters = map[string]func(a *schema.AccountSnapshot, v float64){
	"extra-usage-cost-usd": func(a *schema.AccountSnapshot, v float64) {
		extraUsageForOverride(a).UsedCreditsUSD = &v
	},
	"extra-usage-limit-usd": func(a *schema.AccountSnapshot, v float64) {
		extraUsageForOverride(a).MonthlyLimitUSD = &v
	},
	"extra-usage-percent": func(a *schema.AccountSnapshot, v float64) {
		extraUsageForOverride(a).Utilization = &v
	},
	"five-hour-percent":        func(a *schema.AccountSnapshot, v float64) { setWindowPercent(&a.FiveHour, v) },
	"seven-day-percent":        func(a *schema.AccountSnapshot, v float64) { setWindowPercent(&a.SevenDay, v) },
	"seven-day-opus-percent":   func(a *schema.AccountSnapshot, v float64) { setWindowPercent(&a.SevenDayOpus, v) },
	"seven-day-sonnet-percent": func(a *schema.AccountSnapshot, v float64) { setWindowPercent(&a.SevenDaySonnet, v) },
}

// textOverrideSetters maps each text (account identity) metric --set supports
// to the profile field the metric and its same-named field both read.
var textOverrideSetters = map[string]func(p *schema.AccountProfile, v string){
	"account-type":  func(p *schema.AccountProfile, v string) { p.Type = v },
	"account-plan":  func(p *schema.AccountProfile, v string) { p.Plan = v },
	"account-seat":  func(p *schema.AccountProfile, v string) { p.Seat = v },
	"account-role":  func(p *schema.AccountProfile, v string) { p.Role = v },
	"account-org":   func(p *schema.AccountProfile, v string) { p.Organization = v },
	"account-email": func(p *schema.AccountProfile, v string) { p.Email = v },
}

// supportedOverrideNames is the user-facing list of --set metric names, in
// the order the error message prints them.
var supportedOverrideNames = []string{
	"extra-usage-cost-usd", "extra-usage-limit-usd", "extra-usage-percent",
	"five-hour-percent", "seven-day-percent", "seven-day-opus-percent", "seven-day-sonnet-percent",
	"account-type", "account-plan", "account-seat", "account-role", "account-org", "account-email",
}

func (m *metricOverrides) String() string {
	if m == nil {
		return ""
	}
	parts := make([]string, 0, len(m.entries))
	for _, e := range m.entries {
		if _, ok := textOverrideSetters[e.name]; ok {
			parts = append(parts, e.name+"="+e.text)
		} else {
			parts = append(parts, e.name+"="+strconv.FormatFloat(e.num, 'g', -1, 64))
		}
	}
	return strings.Join(parts, ",")
}

// Set parses and validates one NAME=VALUE entry. A later entry for the same
// name wins (entries are applied in order).
func (m *metricOverrides) Set(s string) error {
	name, value, ok := strings.Cut(s, "=")
	if !ok {
		return fmt.Errorf("expected METRIC=VALUE, got %q", s)
	}
	if _, known := dsl.MetricByName(string(schema.ToolClaudeCode), name); !known {
		return fmt.Errorf("unknown metric %q", name)
	}
	if _, isText := textOverrideSetters[name]; isText {
		m.entries = append(m.entries, metricOverride{name: name, text: value})
		return nil
	}
	if _, isNum := numericOverrideSetters[name]; !isNum {
		return fmt.Errorf("metric %q cannot be overridden; supported: %s", name, strings.Join(supportedOverrideNames, ", "))
	}
	v, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return fmt.Errorf("metric %q needs a numeric value, got %q", name, value)
	}
	m.entries = append(m.entries, metricOverride{name: name, num: v})
	return nil
}

// active reports whether any override was given.
func (m *metricOverrides) active() bool {
	return m != nil && len(m.entries) > 0
}

// applySnapshot writes the numeric overrides into snap's account block.
func (m *metricOverrides) applySnapshot(snap *schema.StatusSnapshot) {
	if !m.active() {
		return
	}
	for _, e := range m.entries {
		if set, ok := numericOverrideSetters[e.name]; ok {
			set(&snap.Account, e.num)
		}
	}
}

// wrapProfile returns a profile provider that yields a copy of base()'s
// profile with the text overrides applied (an empty profile when base has
// none). Without text overrides base is returned unchanged.
func (m *metricOverrides) wrapProfile(base func() *schema.AccountProfile) func() *schema.AccountProfile {
	hasText := false
	if m.active() {
		for _, e := range m.entries {
			if _, ok := textOverrideSetters[e.name]; ok {
				hasText = true
				break
			}
		}
	}
	if !hasText {
		return base
	}
	var p schema.AccountProfile
	if orig := base(); orig != nil {
		p = *orig
	}
	for _, e := range m.entries {
		if set, ok := textOverrideSetters[e.name]; ok {
			set(&p, e.text)
		}
	}
	return func() *schema.AccountProfile { return &p }
}

// extraUsageForOverride returns a's ExtraUsage, creating it when absent, and
// marks it enabled and fresh: a preview override is by definition a live
// value. It works on a copy so a value shared with the cache layer is never
// mutated.
func extraUsageForOverride(a *schema.AccountSnapshot) *schema.ExtraUsage {
	var eu schema.ExtraUsage
	if a.ExtraUsage != nil {
		eu = *a.ExtraUsage
	}
	eu.Enabled = true
	eu.Stale = false
	a.ExtraUsage = &eu
	return a.ExtraUsage
}

// setWindowPercent sets a rate window's used percentage, creating the window
// when absent (on a copy, like extraUsageForOverride). A created window keeps
// a zero ResetsAt: --set never invents a reset time.
func setWindowPercent(w **schema.RateWindow, v float64) {
	var nw schema.RateWindow
	if *w != nil {
		nw = **w
	}
	nw.UsedPercentage = v
	*w = &nw
}
