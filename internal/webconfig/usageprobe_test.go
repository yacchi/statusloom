package webconfig

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/yacchi/statusloom/internal/cache"
	"github.com/yacchi/statusloom/internal/usage"
)

// usageProbeBody mirrors the GET /api/usage/probe response shape for
// decoding in tests.
type usageProbeBody struct {
	Available         bool   `json:"available"`
	Reason            string `json:"reason"`
	ExtraUsageEnabled bool   `json:"extraUsageEnabled"`
}

// withUsageProbeSeams overrides the usageProbeToken/usageProbeFetch package
// vars for the duration of the test, restoring the originals on cleanup, so
// no test ever hits the real environment or network.
func withUsageProbeSeams(
	t *testing.T,
	token func(getenv func(string) string) (string, error),
	fetch func(ctx context.Context, token, version string) (*usage.Report, int, error),
) {
	t.Helper()
	origToken, origFetch := usageProbeToken, usageProbeFetch
	usageProbeToken = token
	usageProbeFetch = fetch
	t.Cleanup(func() {
		usageProbeToken = origToken
		usageProbeFetch = origFetch
	})
}

func getUsageProbe(t *testing.T, ts *testServer) usageProbeBody {
	t.Helper()
	resp := authedGet(t, ts, "/api/usage/probe")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	var body usageProbeBody
	if err := decodeJSON(resp.Body, &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return body
}

func TestUsageProbe_NoToken(t *testing.T) {
	withUsageProbeSeams(t,
		func(getenv func(string) string) (string, error) { return "", usage.ErrNoToken },
		func(ctx context.Context, token, version string) (*usage.Report, int, error) {
			t.Fatal("usageProbeFetch should not be called without a token")
			return nil, 0, nil
		},
	)
	ts := startTestServer(t, time.Hour)

	got := getUsageProbe(t, ts)
	want := usageProbeBody{Available: false, Reason: "no-token"}
	if got != want {
		t.Errorf("probe = %+v, want %+v", got, want)
	}
}

func TestUsageProbe_EmptyTokenTreatedAsNoToken(t *testing.T) {
	withUsageProbeSeams(t,
		func(getenv func(string) string) (string, error) { return "", nil },
		func(ctx context.Context, token, version string) (*usage.Report, int, error) {
			t.Fatal("usageProbeFetch should not be called with an empty token")
			return nil, 0, nil
		},
	)
	ts := startTestServer(t, time.Hour)

	got := getUsageProbe(t, ts)
	want := usageProbeBody{Available: false, Reason: "no-token"}
	if got != want {
		t.Errorf("probe = %+v, want %+v", got, want)
	}
}

func TestUsageProbe_Ok(t *testing.T) {
	withUsageProbeSeams(t,
		func(getenv func(string) string) (string, error) { return "tok", nil },
		func(ctx context.Context, token, version string) (*usage.Report, int, error) {
			if token != "tok" {
				t.Errorf("fetch token = %q, want tok", token)
			}
			return &usage.Report{Extra: &usage.Extra{IsEnabled: true}}, http.StatusOK, nil
		},
	)
	ts := startTestServer(t, time.Hour)

	got := getUsageProbe(t, ts)
	want := usageProbeBody{Available: true, Reason: "ok", ExtraUsageEnabled: true}
	if got != want {
		t.Errorf("probe = %+v, want %+v", got, want)
	}
}

func TestUsageProbe_OkExtraUsageDisabled(t *testing.T) {
	withUsageProbeSeams(t,
		func(getenv func(string) string) (string, error) { return "tok", nil },
		func(ctx context.Context, token, version string) (*usage.Report, int, error) {
			return &usage.Report{Extra: &usage.Extra{IsEnabled: false}}, http.StatusOK, nil
		},
	)
	ts := startTestServer(t, time.Hour)

	got := getUsageProbe(t, ts)
	want := usageProbeBody{Available: true, Reason: "ok", ExtraUsageEnabled: false}
	if got != want {
		t.Errorf("probe = %+v, want %+v", got, want)
	}
}

func TestUsageProbe_OkNilExtra(t *testing.T) {
	withUsageProbeSeams(t,
		func(getenv func(string) string) (string, error) { return "tok", nil },
		func(ctx context.Context, token, version string) (*usage.Report, int, error) {
			return &usage.Report{}, http.StatusOK, nil
		},
	)
	ts := startTestServer(t, time.Hour)

	got := getUsageProbe(t, ts)
	want := usageProbeBody{Available: true, Reason: "ok", ExtraUsageEnabled: false}
	if got != want {
		t.Errorf("probe = %+v, want %+v", got, want)
	}
}

func TestUsageProbe_RateLimited(t *testing.T) {
	withUsageProbeSeams(t,
		func(getenv func(string) string) (string, error) { return "tok", nil },
		func(ctx context.Context, token, version string) (*usage.Report, int, error) {
			return nil, http.StatusTooManyRequests, usage.ErrRateLimited
		},
	)
	ts := startTestServer(t, time.Hour)

	got := getUsageProbe(t, ts)
	want := usageProbeBody{Available: true, Reason: "rate-limited"}
	if got != want {
		t.Errorf("probe = %+v, want %+v", got, want)
	}
}

func TestUsageProbe_Unauthorized(t *testing.T) {
	withUsageProbeSeams(t,
		func(getenv func(string) string) (string, error) { return "tok", nil },
		func(ctx context.Context, token, version string) (*usage.Report, int, error) {
			return nil, http.StatusUnauthorized, usage.ErrUnauthorized
		},
	)
	ts := startTestServer(t, time.Hour)

	got := getUsageProbe(t, ts)
	want := usageProbeBody{Available: false, Reason: "unauthorized"}
	if got != want {
		t.Errorf("probe = %+v, want %+v", got, want)
	}
}

// TestUsageProbe_Ok_PersistsAccountUsage verifies that a successful probe
// (B1) stores the fetched report into the shared account-usage cache under
// accountUsageKey, mapping usage.Report's window/Extra fields onto
// cache.RateWindowState/ExtraUsageState so the render path and the fields
// preview overlay (handleDSLFields, B2) can read the user's real values.
func TestUsageProbe_Ok_PersistsAccountUsage(t *testing.T) {
	t.Setenv("STATUSLOOM_CACHE_DIR", t.TempDir())

	resetsAt := time.Now().Add(3 * time.Hour).Truncate(time.Second)
	withUsageProbeSeams(t,
		func(getenv func(string) string) (string, error) { return "tok", nil },
		func(ctx context.Context, token, version string) (*usage.Report, int, error) {
			return &usage.Report{
				FiveHour:       &usage.Window{Utilization: 11, ResetsAt: resetsAt},
				SevenDay:       &usage.Window{Utilization: 22, ResetsAt: resetsAt},
				SevenDayOpus:   &usage.Window{Utilization: 63, ResetsAt: resetsAt},
				SevenDaySonnet: &usage.Window{Utilization: 12, ResetsAt: resetsAt},
				Extra: &usage.Extra{
					IsEnabled:    true,
					MonthlyLimit: fptr(30),
					UsedCredits:  fptr(12.34),
					Utilization:  fptr(41),
				},
			}, http.StatusOK, nil
		},
	)
	ts := startTestServer(t, time.Hour)

	got := getUsageProbe(t, ts)
	want := usageProbeBody{Available: true, Reason: "ok", ExtraUsageEnabled: true}
	if got != want {
		t.Errorf("probe = %+v, want %+v", got, want)
	}

	env, _, ok := cache.LoadAccountUsage(accountUsageKey, time.Now())
	if !ok {
		t.Fatal("cache.LoadAccountUsage() ok = false, want true (probe should have persisted)")
	}
	if env.FiveHour == nil || env.FiveHour.UsedPercentage != 11 || !env.FiveHour.ResetsAt.Equal(resetsAt) {
		t.Errorf("FiveHour = %+v, want UsedPercentage 11, ResetsAt %v", env.FiveHour, resetsAt)
	}
	if env.SevenDay == nil || env.SevenDay.UsedPercentage != 22 {
		t.Errorf("SevenDay = %+v, want UsedPercentage 22", env.SevenDay)
	}
	if env.SevenDayOpus == nil || env.SevenDayOpus.UsedPercentage != 63 {
		t.Errorf("SevenDayOpus = %+v, want UsedPercentage 63", env.SevenDayOpus)
	}
	if env.SevenDaySonnet == nil || env.SevenDaySonnet.UsedPercentage != 12 {
		t.Errorf("SevenDaySonnet = %+v, want UsedPercentage 12", env.SevenDaySonnet)
	}
	if env.ExtraUsage == nil || !env.ExtraUsage.Enabled ||
		env.ExtraUsage.MonthlyLimit == nil || *env.ExtraUsage.MonthlyLimit != 30 ||
		env.ExtraUsage.UsedCredits == nil || *env.ExtraUsage.UsedCredits != 12.34 ||
		env.ExtraUsage.Utilization == nil || *env.ExtraUsage.Utilization != 41 {
		t.Errorf("ExtraUsage = %+v, want Enabled=true MonthlyLimit=30 UsedCredits=12.34 Utilization=41", env.ExtraUsage)
	}
}

// TestUsageProbe_NoToken_DoesNotPersist verifies that a no-token probe (which
// never fetches) never persists to the account-usage cache.
func TestUsageProbe_NoToken_DoesNotPersist(t *testing.T) {
	t.Setenv("STATUSLOOM_CACHE_DIR", t.TempDir())
	withUsageProbeSeams(t,
		func(getenv func(string) string) (string, error) { return "", usage.ErrNoToken },
		func(ctx context.Context, token, version string) (*usage.Report, int, error) {
			t.Fatal("usageProbeFetch should not be called without a token")
			return nil, 0, nil
		},
	)
	ts := startTestServer(t, time.Hour)
	_ = getUsageProbe(t, ts)

	if _, _, ok := cache.LoadAccountUsage(accountUsageKey, time.Now()); ok {
		t.Error("cache.LoadAccountUsage() ok = true, want false (no-token probe must not persist)")
	}
}

// TestUsageProbe_Unauthorized_DoesNotPersist verifies that a 401 probe result
// does not persist a report to the account-usage cache.
func TestUsageProbe_Unauthorized_DoesNotPersist(t *testing.T) {
	t.Setenv("STATUSLOOM_CACHE_DIR", t.TempDir())
	withUsageProbeSeams(t,
		func(getenv func(string) string) (string, error) { return "tok", nil },
		func(ctx context.Context, token, version string) (*usage.Report, int, error) {
			return nil, http.StatusUnauthorized, usage.ErrUnauthorized
		},
	)
	ts := startTestServer(t, time.Hour)
	_ = getUsageProbe(t, ts)

	if _, _, ok := cache.LoadAccountUsage(accountUsageKey, time.Now()); ok {
		t.Error("cache.LoadAccountUsage() ok = true, want false (401 probe must not persist)")
	}
}

func TestUsageProbe_Error(t *testing.T) {
	withUsageProbeSeams(t,
		func(getenv func(string) string) (string, error) { return "tok", nil },
		func(ctx context.Context, token, version string) (*usage.Report, int, error) {
			return nil, 0, errors.New("boom")
		},
	)
	ts := startTestServer(t, time.Hour)

	got := getUsageProbe(t, ts)
	want := usageProbeBody{Available: false, Reason: "error"}
	if got != want {
		t.Errorf("probe = %+v, want %+v", got, want)
	}
}
