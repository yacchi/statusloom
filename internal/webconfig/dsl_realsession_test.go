package webconfig

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/yacchi/statusloom/internal/cache"
	"github.com/yacchi/statusloom/internal/schema"
)

// TestDSL_Preview_RealSession_OverlaysCurrentAccountCache verifies
// previewSnapshot's account/extra-usage overlay (dsl.go): POST
// /api/dsl/preview with sessionId set must not just replay a cached session
// snapshot's own (possibly stale-empty) ExtraUsage as-is. The cached snapshot
// here is stored with Account.ExtraUsage nil - exactly what renderDocFromRaw
// (internal/cli/render.go) would have frozen if the shared account cache
// happened to be empty at the moment that session was rendered. The shared
// extra-usage cache is then populated afterward (as statusloom refresh
// --once does, asynchronously). The preview must reflect that freshly cached
// value, not the stored-empty snapshot.
func TestDSL_Preview_RealSession_OverlaysCurrentAccountCache(t *testing.T) {
	t.Setenv("STATUSLOOM_CACHE_DIR", t.TempDir())
	isolateClaudeConfig(t) // keep this test's account cache key at cache.AccountCacheKey regardless of the developer's real .claude.json
	ts := startTestServer(t, time.Hour)

	snap := schema.StatusSnapshot{
		Tool:    schema.ToolSnapshot{ID: schema.ToolClaudeCode, Version: "2.1.200"},
		Session: schema.SessionSnapshot{ID: "sess-extra-usage"},
		System:  schema.SystemSnapshot{Cwd: "/Users/dev/myapp"},
	}
	if err := cache.StoreSnapshot("sess-extra-usage", snap, time.Now()); err != nil {
		t.Fatalf("StoreSnapshot() error = %v", err)
	}

	// Populate the shared extra-usage cache after the snapshot above was
	// captured, simulating the async refresh worker catching up.
	env := cache.NewAccountUsageEnvelope(time.Now())
	env.ExtraUsage = &cache.ExtraUsageState{
		Enabled:      true,
		MonthlyLimit: fptr(100),
		UsedCredits:  fptr(12.5),
		Utilization:  fptr(12.5),
	}
	if err := cache.StoreAccountUsage(cache.AccountCacheKey, env); err != nil {
		t.Fatalf("StoreAccountUsage() error = %v", err)
	}

	source := `<statusloom version="1" tool="claude-code">
  <layout name="Default" active="true">
    <line>
      <span prefix="overage: " optional="extra-usage-cost">
        <field name="extra-usage-cost" format="currency"/>
      </span>
    </line>
  </layout>
</statusloom>`

	resp := putPOST(t, ts, "/api/dsl/preview", map[string]any{
		"tool": "claude-code", "source": source, "width": 120, "sessionId": "sess-extra-usage",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("preview status = %d, want 200", resp.StatusCode)
	}
	var pv struct {
		Lines []struct {
			Segments []struct {
				Text string `json:"text"`
			} `json:"segments"`
		} `json:"lines"`
	}
	if err := decodeJSON(resp.Body, &pv); err != nil {
		t.Fatalf("decode preview: %v", err)
	}
	resp.Body.Close()

	var all strings.Builder
	for _, ln := range pv.Lines {
		for _, seg := range ln.Segments {
			all.WriteString(seg.Text)
		}
	}
	got := all.String()
	if !strings.Contains(got, "$12.50") {
		t.Errorf("preview text = %q, want it to contain the freshly cached extra-usage value ($12.50) rather than the stored-empty snapshot's", got)
	}
}

// TestDSL_Preview_RealSession_DoesNotMutateSharedAccountCache verifies that
// previewSnapshot's account-cache overlay (dsl.go) is read-only against the
// shared account cache: it must call cache.FillAccountFromCache, never
// cache.ApplyAccountCache/cache.StoreAccountFromSnapshot. Previewing a
// cached session is a read-only look at a past moment and must not have a
// side effect on the render path's shared state - in particular, it must not
// overwrite the shared account cache with whatever five-hour/seven-day
// window that old session snapshot happened to carry, stamped as "observed
// now".
func TestDSL_Preview_RealSession_DoesNotMutateSharedAccountCache(t *testing.T) {
	t.Setenv("STATUSLOOM_CACHE_DIR", t.TempDir())
	isolateClaudeConfig(t) // keep this test's account cache key at cache.AccountCacheKey regardless of the developer's real .claude.json
	ts := startTestServer(t, time.Hour)

	// Seed the shared account cache with a known baseline, as the render
	// path would have written from a real, current render.
	baseline := cache.AccountUsage{
		Source:     "claude-code-stdin",
		ObservedAt: time.Now().Add(-time.Hour),
		ExpiresAt:  time.Now().Add(4 * time.Minute),
		FiveHour:   &cache.RateWindowState{UsedPercentage: 5, ResetsAt: time.Now().Add(2 * time.Hour)},
	}
	if err := cache.StoreAccount(cache.AccountCacheKey, baseline); err != nil {
		t.Fatalf("StoreAccount() error = %v", err)
	}
	before, err := cache.LoadAccount(cache.AccountCacheKey)
	if err != nil || before == nil {
		t.Fatalf("LoadAccount() before preview: err=%v before=%v", err, before)
	}

	// Store a cached session snapshot carrying its OWN, different
	// five-hour window. If previewSnapshot called StoreAccountFromSnapshot
	// (directly, or via ApplyAccountCache) instead of FillAccountFromCache,
	// this would get written to the shared cache, clobbering the baseline
	// above and re-stamping it as observed just now.
	snap := schema.StatusSnapshot{
		Tool:    schema.ToolSnapshot{ID: schema.ToolClaudeCode, Version: "2.1.200"},
		Session: schema.SessionSnapshot{ID: "sess-preview-readonly"},
		System:  schema.SystemSnapshot{Cwd: "/Users/dev/myapp"},
		Account: schema.AccountSnapshot{
			FiveHour: &schema.RateWindow{UsedPercentage: 99, ResetsAt: time.Now().Add(3 * time.Hour)},
		},
	}
	if err := cache.StoreSnapshot("sess-preview-readonly", snap, time.Now()); err != nil {
		t.Fatalf("StoreSnapshot() error = %v", err)
	}

	source := `<statusloom version="1" tool="claude-code">
  <layout name="Default" active="true">
    <line><field name="model"/></line>
  </layout>
</statusloom>`
	resp := putPOST(t, ts, "/api/dsl/preview", map[string]any{
		"tool": "claude-code", "source": source, "width": 120, "sessionId": "sess-preview-readonly",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("preview status = %d, want 200", resp.StatusCode)
	}
	resp.Body.Close()

	after, err := cache.LoadAccount(cache.AccountCacheKey)
	if err != nil || after == nil {
		t.Fatalf("LoadAccount() after preview: err=%v after=%v", err, after)
	}
	if !after.ObservedAt.Equal(before.ObservedAt) {
		t.Errorf("shared account cache ObservedAt changed by preview: before=%v after=%v (preview must not write to the shared cache)", before.ObservedAt, after.ObservedAt)
	}
	if after.FiveHour == nil || after.FiveHour.UsedPercentage != 5 {
		t.Errorf("shared account cache FiveHour = %v, want unchanged (UsedPercentage 5); preview must not overwrite it with the session's own window", after.FiveHour)
	}
}
