package webconfig

import (
	"context"
	"errors"
	"net/http"
	"os"
	"time"

	"github.com/yacchi/statusloom/internal/cache"
	"github.com/yacchi/statusloom/internal/usage"
)

// usageProbeToken and usageProbeFetch are seams over the usage package
// (credentials + network) so tests can drive handleUsageProbe's branches
// without touching the real environment, credential files, or the network.
var (
	usageProbeToken = usage.Token
	usageProbeFetch = func(ctx context.Context, token, version string) (*usage.Report, int, error) {
		return usage.Fetch(ctx, nil, token, version)
	}
)

// accountUsageKey is the shared account-usage cache key the probe writes to
// and the render path (and the fields preview overlay in dsl.go) read from.
// It must match cli's accountCacheKey (internal/cli/render.go), which is also
// the fixed string "default" (statusloom uses a single, unkeyed account).
const accountUsageKey = "default"

// usageProbeResponse is the GET /api/usage/probe response body: whether the
// authenticated OAuth usage API is reachable (gating oauth-usage-capability
// DSL fields in the config UI's palette), why not when it isn't, and
// (only meaningful when available) whether the account has extra usage
// enabled at all.
type usageProbeResponse struct {
	Available         bool   `json:"available"`
	Reason            string `json:"reason"`
	ExtraUsageEnabled bool   `json:"extraUsageEnabled"`
}

// recentToolVersion returns the Claude Code version recorded in the most
// recently observed cached session snapshot, or "" if none is cached or the
// lookup fails. It is used only to build a more specific
// "claude-code/<version>" User-Agent for the usage-API probe (usage.Fetch
// maps "" to "claude-code/unknown"); a miss here is not an error.
func recentToolVersion() string {
	entries, err := cache.ListSnapshots(time.Now())
	if err != nil || len(entries) == 0 {
		return ""
	}
	return entries[0].Snapshot.Tool.Version
}

// handleUsageProbe handles GET /api/usage/probe: a best-effort check of
// whether the authenticated OAuth usage API is reachable from this machine.
// The config UI uses this to decide whether to show oauth-usage-capability
// fields (extra-usage-*, weekly-usage-*, weekly-reset-*) in the palette at
// all ("駄目ならパレットに出さない").
//
// This always responds 200: the probe result is data describing
// availability, not an HTTP error condition.
func (s *server) handleUsageProbe(w http.ResponseWriter, r *http.Request) {
	token, err := usageProbeToken(os.Getenv)
	if errors.Is(err, usage.ErrNoToken) || token == "" {
		writeJSON(w, http.StatusOK, usageProbeResponse{Available: false, Reason: "no-token"})
		return
	}

	report, status, ferr := usageProbeFetch(r.Context(), token, recentToolVersion())
	switch {
	case status == http.StatusOK && ferr == nil:
		if report != nil {
			persistAccountUsage(report, time.Now())
		}
		writeJSON(w, http.StatusOK, usageProbeResponse{
			Available:         true,
			Reason:            "ok",
			ExtraUsageEnabled: report != nil && report.Extra != nil && report.Extra.IsEnabled,
		})
	case errors.Is(ferr, usage.ErrRateLimited):
		// The capability itself exists; the endpoint is just throttled.
		writeJSON(w, http.StatusOK, usageProbeResponse{Available: true, Reason: "rate-limited"})
	case errors.Is(ferr, usage.ErrUnauthorized):
		writeJSON(w, http.StatusOK, usageProbeResponse{Available: false, Reason: "unauthorized"})
	default:
		writeJSON(w, http.StatusOK, usageProbeResponse{Available: false, Reason: "error"})
	}
}

// persistAccountUsage maps a successful usage-API report onto the shared
// account-usage cache envelope and best-effort stores it (accountUsageKey),
// so the render path and the fields preview overlay (handleDSLFields in
// dsl.go) can pick up the user's real values instead of only synthetic
// samples. Errors are ignored: this is an opportunistic side effect of the
// probe, not something the probe response depends on.
func persistAccountUsage(report *usage.Report, now time.Time) {
	env := cache.NewAccountUsageEnvelope(now)
	if report.FiveHour != nil {
		env.FiveHour = &cache.RateWindowState{UsedPercentage: report.FiveHour.Utilization, ResetsAt: report.FiveHour.ResetsAt}
	}
	if report.SevenDay != nil {
		env.SevenDay = &cache.RateWindowState{UsedPercentage: report.SevenDay.Utilization, ResetsAt: report.SevenDay.ResetsAt}
	}
	if report.SevenDayOpus != nil {
		env.SevenDayOpus = &cache.RateWindowState{UsedPercentage: report.SevenDayOpus.Utilization, ResetsAt: report.SevenDayOpus.ResetsAt}
	}
	if report.SevenDaySonnet != nil {
		env.SevenDaySonnet = &cache.RateWindowState{UsedPercentage: report.SevenDaySonnet.Utilization, ResetsAt: report.SevenDaySonnet.ResetsAt}
	}
	if report.Extra != nil {
		env.ExtraUsage = &cache.ExtraUsageState{
			Enabled:      report.Extra.IsEnabled,
			MonthlyLimit: report.Extra.MonthlyLimit,
			UsedCredits:  report.Extra.UsedCredits,
			Utilization:  report.Extra.Utilization,
		}
	}
	_ = cache.StoreAccountUsage(accountUsageKey, env)
}
