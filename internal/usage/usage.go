// Package usage implements a client for Claude Code's undocumented OAuth
// usage endpoint. It is intended to run only inside a background refresh
// subprocess (never the statusline render hot path): the calls in this
// package perform blocking network I/O.
package usage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// endpointURL is the base URL for the usage endpoint. It is an unexported
// var so tests can point it at an httptest.Server.
var endpointURL = "https://api.anthropic.com/api/oauth/usage"

// Sentinel errors returned by Fetch and Token.
var (
	ErrNoToken      = errors.New("usage: no OAuth token available")
	ErrUnauthorized = errors.New("usage: unauthorized (401)")
	ErrRateLimited  = errors.New("usage: rate limited (429)")
)

// Window represents a single rate-limit window (e.g. five-hour or
// seven-day) reported by the usage endpoint.
type Window struct {
	Utilization float64
	ResetsAt    time.Time
}

// Extra represents the "extra usage" (pay-as-you-go credits) section of the
// usage response.
//
// MonthlyLimit and UsedCredits are USD dollars; the wire format returns them
// in cents, converted here. Utilization is a percentage and is left as-is.
type Extra struct {
	IsEnabled    bool
	MonthlyLimit *float64
	UsedCredits  *float64
	Utilization  *float64
}

// Report is the parsed usage response.
type Report struct {
	FiveHour       *Window
	SevenDay       *Window
	SevenDayOpus   *Window
	SevenDaySonnet *Window
	Extra          *Extra
}

// wireWindow mirrors the JSON shape of a single window in the response.
type wireWindow struct {
	Utilization float64 `json:"utilization"`
	ResetsAt    string  `json:"resets_at"`
}

// wireExtra mirrors the JSON shape of the extra_usage section.
type wireExtra struct {
	IsEnabled    bool     `json:"is_enabled"`
	MonthlyLimit *float64 `json:"monthly_limit"`
	UsedCredits  *float64 `json:"used_credits"`
	Utilization  *float64 `json:"utilization"`
}

// wireReport mirrors the JSON shape of the full usage response.
type wireReport struct {
	FiveHour       *wireWindow `json:"five_hour"`
	SevenDay       *wireWindow `json:"seven_day"`
	SevenDayOpus   *wireWindow `json:"seven_day_opus"`
	SevenDaySonnet *wireWindow `json:"seven_day_sonnet"`
	ExtraUsage     *wireExtra  `json:"extra_usage"`
}

// parseResetsAt parses the resets_at timestamp, trying RFC3339Nano first
// (ISO8601 with fractional seconds) and falling back to plain RFC3339.
func parseResetsAt(s string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t, nil
	}
	return time.Parse(time.RFC3339, s)
}

func toWindow(w *wireWindow) (*Window, error) {
	if w == nil {
		return nil, nil
	}
	resetsAt, err := parseResetsAt(w.ResetsAt)
	if err != nil {
		return nil, fmt.Errorf("usage: parse resets_at %q: %w", w.ResetsAt, err)
	}
	return &Window{
		Utilization: w.Utilization,
		ResetsAt:    resetsAt,
	}, nil
}

// centsToDollars converts a monetary wire value expressed in USD cents to
// USD dollars. A nil input (field absent) stays nil.
func centsToDollars(cents *float64) *float64 {
	if cents == nil {
		return nil
	}
	dollars := *cents / 100.0
	return &dollars
}

func toExtra(e *wireExtra) *Extra {
	if e == nil {
		return nil
	}
	return &Extra{
		IsEnabled:    e.IsEnabled,
		MonthlyLimit: centsToDollars(e.MonthlyLimit),
		UsedCredits:  centsToDollars(e.UsedCredits),
		Utilization:  e.Utilization,
	}
}

func (w *wireReport) toReport() (*Report, error) {
	fiveHour, err := toWindow(w.FiveHour)
	if err != nil {
		return nil, err
	}
	sevenDay, err := toWindow(w.SevenDay)
	if err != nil {
		return nil, err
	}
	sevenDayOpus, err := toWindow(w.SevenDayOpus)
	if err != nil {
		return nil, err
	}
	sevenDaySonnet, err := toWindow(w.SevenDaySonnet)
	if err != nil {
		return nil, err
	}
	return &Report{
		FiveHour:       fiveHour,
		SevenDay:       sevenDay,
		SevenDayOpus:   sevenDayOpus,
		SevenDaySonnet: sevenDaySonnet,
		Extra:          toExtra(w.ExtraUsage),
	}, nil
}

// maxErrorBodyBytes bounds how much of a non-2xx response body is included
// in diagnostic error messages.
const maxErrorBodyBytes = 512

// Fetch calls Claude Code's OAuth usage endpoint and returns the parsed
// report along with the HTTP status code observed (0 if the request never
// received a response, e.g. on a network error).
//
// This performs a real network call; it must only be invoked from a
// background refresh subprocess, never from the statusline render path.
func Fetch(ctx context.Context, hc *http.Client, token, version string) (*Report, int, error) {
	if hc == nil {
		hc = &http.Client{Timeout: 10 * time.Second}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpointURL, nil)
	if err != nil {
		return nil, 0, err
	}

	ua := "claude-code/unknown"
	if version != "" {
		ua = "claude-code/" + version
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Content-Type", "application/json")

	resp, err := hc.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, resp.StatusCode, ErrUnauthorized
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, resp.StatusCode, ErrRateLimited
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
		return nil, resp.StatusCode, fmt.Errorf("usage: unexpected status %d: %s", resp.StatusCode, body)
	}

	var wire wireReport
	if err := json.NewDecoder(resp.Body).Decode(&wire); err != nil {
		return nil, resp.StatusCode, fmt.Errorf("usage: decode response: %w", err)
	}

	report, err := wire.toReport()
	if err != nil {
		return nil, resp.StatusCode, err
	}

	return report, resp.StatusCode, nil
}
