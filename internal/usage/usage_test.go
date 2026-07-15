package usage

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

const sampleResponse = `{
  "five_hour":        {"utilization": 33.0, "resets_at": "2026-04-11T07:00:00.5+00:00"},
  "seven_day":        {"utilization": 13.0, "resets_at": "2026-04-17T00:59:59.9+00:00"},
  "seven_day_opus":   null,
  "seven_day_sonnet": {"utilization": 1.0, "resets_at": "2026-04-16T03:00:00.9+00:00"},
  "extra_usage":      {"is_enabled": true, "monthly_limit": 3000, "used_credits": 1234, "utilization": 41.1}
}`

func withTestServer(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	original := endpointURL
	endpointURL = server.URL
	t.Cleanup(func() { endpointURL = original })
}

func TestFetch_ParsesFullResponse(t *testing.T) {
	var gotAuth, gotBeta, gotUA, gotContentType string
	withTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotBeta = r.Header.Get("anthropic-beta")
		gotUA = r.Header.Get("User-Agent")
		gotContentType = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(sampleResponse))
	})

	report, status, err := Fetch(context.Background(), nil, "test-token", "2.1.210")
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}

	if gotAuth != "Bearer test-token" {
		t.Errorf("Authorization header = %q", gotAuth)
	}
	if gotBeta != "oauth-2025-04-20" {
		t.Errorf("anthropic-beta header = %q", gotBeta)
	}
	if gotUA != "claude-code/2.1.210" {
		t.Errorf("User-Agent header = %q", gotUA)
	}
	if gotContentType != "application/json" {
		t.Errorf("Content-Type header = %q", gotContentType)
	}

	if report.FiveHour == nil {
		t.Fatal("FiveHour is nil")
	}
	if report.FiveHour.Utilization != 33.0 {
		t.Errorf("FiveHour.Utilization = %v, want 33.0", report.FiveHour.Utilization)
	}
	wantFiveHourReset := time.Date(2026, 4, 11, 7, 0, 0, 500000000, time.UTC)
	if !report.FiveHour.ResetsAt.Equal(wantFiveHourReset) {
		t.Errorf("FiveHour.ResetsAt = %v, want %v", report.FiveHour.ResetsAt, wantFiveHourReset)
	}

	if report.SevenDay == nil {
		t.Fatal("SevenDay is nil")
	}
	if report.SevenDay.Utilization != 13.0 {
		t.Errorf("SevenDay.Utilization = %v, want 13.0", report.SevenDay.Utilization)
	}

	if report.SevenDayOpus != nil {
		t.Errorf("SevenDayOpus = %+v, want nil", report.SevenDayOpus)
	}

	if report.SevenDaySonnet == nil {
		t.Fatal("SevenDaySonnet is nil")
	}
	if report.SevenDaySonnet.Utilization != 1.0 {
		t.Errorf("SevenDaySonnet.Utilization = %v, want 1.0", report.SevenDaySonnet.Utilization)
	}

	if report.Extra == nil {
		t.Fatal("Extra is nil")
	}
	if !report.Extra.IsEnabled {
		t.Error("Extra.IsEnabled = false, want true")
	}
	// Monetary wire values are in USD cents and must be converted to dollars.
	if report.Extra.MonthlyLimit == nil {
		t.Fatal("Extra.MonthlyLimit is nil")
	}
	if *report.Extra.MonthlyLimit != 30.0 {
		t.Errorf("Extra.MonthlyLimit = %v, want 30.0 (3000 cents)", *report.Extra.MonthlyLimit)
	}
	if report.Extra.UsedCredits == nil {
		t.Fatal("Extra.UsedCredits is nil")
	}
	if *report.Extra.UsedCredits != 12.34 {
		t.Errorf("Extra.UsedCredits = %v, want 12.34 (1234 cents)", *report.Extra.UsedCredits)
	}
	// Utilization is a percentage and must NOT be scaled.
	if report.Extra.Utilization == nil {
		t.Fatal("Extra.Utilization is nil")
	}
	if *report.Extra.Utilization != 41.1 {
		t.Errorf("Extra.Utilization = %v, want 41.1 (unscaled)", *report.Extra.Utilization)
	}
}

func TestFetch_NullMonetaryFieldsStayNil(t *testing.T) {
	const body = `{
  "five_hour": null,
  "seven_day": null,
  "seven_day_opus": null,
  "seven_day_sonnet": null,
  "extra_usage": {"is_enabled": false, "monthly_limit": null, "used_credits": null, "utilization": null}
}`
	withTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	})

	report, _, err := Fetch(context.Background(), nil, "token", "1.0")
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
	if report.Extra == nil {
		t.Fatal("Extra is nil")
	}
	if report.Extra.MonthlyLimit != nil {
		t.Errorf("Extra.MonthlyLimit = %v, want nil", report.Extra.MonthlyLimit)
	}
	if report.Extra.UsedCredits != nil {
		t.Errorf("Extra.UsedCredits = %v, want nil", report.Extra.UsedCredits)
	}
	if report.Extra.Utilization != nil {
		t.Errorf("Extra.Utilization = %v, want nil", report.Extra.Utilization)
	}
}

func TestFetch_EmptyVersionUsesUnknownUserAgent(t *testing.T) {
	var gotUA string
	withTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(sampleResponse))
	})

	_, _, err := Fetch(context.Background(), nil, "test-token", "")
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
	if gotUA != "claude-code/unknown" {
		t.Errorf("User-Agent header = %q, want claude-code/unknown", gotUA)
	}
}

func TestFetch_Unauthorized(t *testing.T) {
	withTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})

	report, status, err := Fetch(context.Background(), nil, "bad-token", "1.0")
	if report != nil {
		t.Errorf("report = %+v, want nil", report)
	}
	if status != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", status)
	}
	if !errors.Is(err, ErrUnauthorized) {
		t.Errorf("err = %v, want ErrUnauthorized", err)
	}
}

func TestFetch_RateLimited(t *testing.T) {
	withTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	})

	report, status, err := Fetch(context.Background(), nil, "token", "1.0")
	if report != nil {
		t.Errorf("report = %+v, want nil", report)
	}
	if status != http.StatusTooManyRequests {
		t.Errorf("status = %d, want 429", status)
	}
	if !errors.Is(err, ErrRateLimited) {
		t.Errorf("err = %v, want ErrRateLimited", err)
	}
}

func TestFetch_ServerError(t *testing.T) {
	withTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal server error"))
	})

	report, status, err := Fetch(context.Background(), nil, "token", "1.0")
	if report != nil {
		t.Errorf("report = %+v, want nil", report)
	}
	if status != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", status)
	}
	if err == nil {
		t.Fatal("err is nil, want error mentioning status 500")
	}
	if errors.Is(err, ErrUnauthorized) || errors.Is(err, ErrRateLimited) {
		t.Errorf("err = %v, want plain status error", err)
	}
}

func TestFetch_NetworkError(t *testing.T) {
	original := endpointURL
	// Point at a URL that nothing is listening on.
	endpointURL = "http://127.0.0.1:1/unreachable"
	t.Cleanup(func() { endpointURL = original })

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	report, status, err := Fetch(ctx, nil, "token", "1.0")
	if report != nil {
		t.Errorf("report = %+v, want nil", report)
	}
	if status != 0 {
		t.Errorf("status = %d, want 0", status)
	}
	if err == nil {
		t.Fatal("err is nil, want network error")
	}
}
