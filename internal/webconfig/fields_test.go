package webconfig

import (
	"net/http"
	"testing"
	"time"

	"github.com/yacchi/statusloom/internal/cache"
)

// fieldsResponse mirrors the GET /api/dsl/fields response shape for
// decoding in tests (only the parts this test cares about).
type fieldsResponse struct {
	Fields []struct {
		Name       string `json:"name"`
		Capability string `json:"capability"`
		Preview    struct {
			Text string `json:"text"`
		} `json:"preview"`
	} `json:"fields"`
}

// previewTextFor returns the preview text of the named field in body, or ""
// if the field is not present.
func previewTextFor(body fieldsResponse, name string) string {
	for _, f := range body.Fields {
		if f.Name == name {
			return f.Preview.Text
		}
	}
	return ""
}

func TestDSLFields_CapabilityMarksOAuthUsageFields(t *testing.T) {
	ts := startTestServer(t, time.Hour)

	resp := authedGet(t, ts, "/api/dsl/fields?tool=claude-code")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var body fieldsResponse
	if err := decodeJSON(resp.Body, &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Fields) == 0 {
		t.Fatal("fields = [], want at least one field")
	}

	// Fields known to require the oauth-usage capability.
	wantCapability := map[string]bool{
		"extra-usage-cost":    true,
		"extra-usage-limit":   true,
		"extra-usage-percent": true,
		"weekly-usage-opus":   true,
		"weekly-usage-sonnet": true,
		"weekly-reset-opus":   true,
		"weekly-reset-sonnet": true,
	}

	sawOAuthUsage := false
	sawPlain := false
	for _, f := range body.Fields {
		if wantCapability[f.Name] {
			sawOAuthUsage = true
			if f.Capability != "oauth-usage" {
				t.Errorf("field %q capability = %q, want oauth-usage", f.Name, f.Capability)
			}
			continue
		}
		sawPlain = true
		if f.Capability != "" {
			t.Errorf("field %q capability = %q, want empty", f.Name, f.Capability)
		}
	}
	if !sawOAuthUsage {
		t.Fatal("no oauth-usage-capability field found in the response; catalog may have changed")
	}
	if !sawPlain {
		t.Fatal("no capability-less field found in the response; catalog may have changed")
	}
}

// TestDSLFields_ExtraUsageOverlay_Real verifies that once the usage-API probe
// has persisted the user's real values to the shared account-usage cache
// (handleUsageProbe / persistAccountUsage in usageprobe.go), GET
// /api/dsl/fields' preview for the extra-usage fields reflects those real
// values (overlayRealAccountUsage in dsl.go) rather than the synthetic
// fullSample ones.
func TestDSLFields_ExtraUsageOverlay_Real(t *testing.T) {
	t.Setenv("STATUSLOOM_CACHE_DIR", t.TempDir())

	now := time.Now()
	env := cache.NewAccountUsageEnvelope(now)
	env.ExtraUsage = &cache.ExtraUsageState{
		Enabled:      true,
		MonthlyLimit: fptr(30),
		UsedCredits:  fptr(9.5),
		Utilization:  fptr(31),
	}
	env.SevenDayOpus = &cache.RateWindowState{UsedPercentage: 77, ResetsAt: now.Add(2 * time.Hour)}
	env.SevenDaySonnet = &cache.RateWindowState{UsedPercentage: 5, ResetsAt: now.Add(6 * time.Hour)}
	if err := cache.StoreAccountUsage(accountUsageKey, env); err != nil {
		t.Fatalf("StoreAccountUsage() error = %v", err)
	}

	ts := startTestServer(t, time.Hour)
	resp := authedGet(t, ts, "/api/dsl/fields?tool=claude-code")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body fieldsResponse
	if err := decodeJSON(resp.Body, &body); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if got := previewTextFor(body, "extra-usage-limit"); got != "$30.00" {
		t.Errorf("extra-usage-limit preview = %q, want $30.00 (from cached real value)", got)
	}
	if got := previewTextFor(body, "extra-usage-cost"); got != "$9.50" {
		t.Errorf("extra-usage-cost preview = %q, want $9.50 (from cached real value)", got)
	}
	if got := previewTextFor(body, "weekly-usage-opus"); got != "77%" {
		t.Errorf("weekly-usage-opus preview = %q, want 77%% (from cached real value)", got)
	}
	if got := previewTextFor(body, "weekly-usage-sonnet"); got != "5%" {
		t.Errorf("weekly-usage-sonnet preview = %q, want 5%% (from cached real value)", got)
	}
}

// TestDSLFields_ExtraUsageOverlay_FallsBackToSample verifies that with no
// cached account usage present, GET /api/dsl/fields' extra-usage previews
// come from the synthetic fullSample data (samples.go) rather than being
// empty or the "(no sample)" fallback.
func TestDSLFields_ExtraUsageOverlay_FallsBackToSample(t *testing.T) {
	t.Setenv("STATUSLOOM_CACHE_DIR", t.TempDir())
	ts := startTestServer(t, time.Hour)

	resp := authedGet(t, ts, "/api/dsl/fields?tool=claude-code")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body fieldsResponse
	if err := decodeJSON(resp.Body, &body); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if got := previewTextFor(body, "extra-usage-limit"); got != "$30.00" {
		t.Errorf("extra-usage-limit preview = %q, want $30.00 (synthetic sample)", got)
	}
	if got := previewTextFor(body, "weekly-usage-opus"); got == "" || got == previewFallback {
		t.Errorf("weekly-usage-opus preview = %q, want a non-fallback synthetic sample", got)
	}
}
