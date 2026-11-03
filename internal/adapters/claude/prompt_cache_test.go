package claude

import (
	"reflect"
	"testing"
	"time"

	"github.com/yacchi/statusloom/internal/schema"
)

// decodeWithPromptCache builds a minimal valid payload around the given
// prompt_cache JSON fragment ("" omits the key entirely) and returns the
// decoded session ID and prompt cache.
func decodeWithPromptCache(t *testing.T, promptCache string) (string, *schema.PromptCache) {
	t.Helper()
	body := `{"session_id":"s1","transcript_path":"/tmp/t.jsonl","context_window":{}`
	if promptCache != "" {
		body += `,"prompt_cache":` + promptCache
	}
	body += `}`
	snap, err := New().Decode([]byte(body))
	if err != nil {
		t.Fatalf("Decode returned error: %v", err)
	}
	return snap.Session.ID, snap.Session.PromptCache
}

func TestDecodePromptCacheFullPayload(t *testing.T) {
	const raw = `{"warm":true,"caching_observed":true,"ttl":"1h","expires_at":1760000000,` +
		`"requests":12,"misses":1,"expected_rebuilds":0,"hit_ratio":0.91,` +
		`"cache_write_tokens":5000,"miss_recache_tokens":800,"last_miss_at":1759999000,` +
		`"last_miss_cause":{"causes":["ttl_expired","model_switch"]},"recache_tokens_if_cold":42000}`

	snap, err := New().Decode([]byte(`{"session_id":"s1","transcript_path":"/tmp/t.jsonl","context_window":{},"prompt_cache":` + raw + `}`))
	if err != nil {
		t.Fatalf("Decode returned error: %v", err)
	}
	pc := snap.Session.PromptCache
	if pc == nil {
		t.Fatal("PromptCache = nil, want populated")
	}
	if !pc.Warm || !pc.CachingObserved || pc.TTL != "1h" {
		t.Errorf("flags/ttl = %v/%v/%q, want true/true/\"1h\"", pc.Warm, pc.CachingObserved, pc.TTL)
	}
	if want := time.Unix(1760000000, 0); pc.ExpiresAt == nil || !pc.ExpiresAt.Equal(want) {
		t.Errorf("ExpiresAt = %v, want %v", pc.ExpiresAt, want)
	}
	if want := time.Unix(1759999000, 0); pc.LastMissAt == nil || !pc.LastMissAt.Equal(want) {
		t.Errorf("LastMissAt = %v, want %v", pc.LastMissAt, want)
	}
	if pc.Requests != 12 || pc.Misses != 1 || pc.CacheWriteTokens != 5000 || pc.MissRecacheTokens != 800 {
		t.Errorf("counters = %d/%d/%d/%d, want 12/1/5000/800",
			pc.Requests, pc.Misses, pc.CacheWriteTokens, pc.MissRecacheTokens)
	}
	if pc.HitRatio == nil || *pc.HitRatio != 0.91 {
		t.Errorf("HitRatio = %v, want 0.91", pc.HitRatio)
	}
	if want := []string{"ttl_expired", "model_switch"}; !reflect.DeepEqual(pc.LastMissCauses, want) {
		t.Errorf("LastMissCauses = %v, want %v", pc.LastMissCauses, want)
	}
	if pc.RecacheTokensIfCold == nil || *pc.RecacheTokensIfCold != 42000 {
		t.Errorf("RecacheTokensIfCold = %v, want 42000", pc.RecacheTokensIfCold)
	}
}

func TestDecodePromptCacheNullOptionalsStayNil(t *testing.T) {
	const raw = `{"warm":false,"expires_at":null,"last_miss_at":null,"hit_ratio":null,` +
		`"last_miss_cause":null,"recache_tokens_if_cold":null}`

	snap, err := New().Decode([]byte(`{"session_id":"s1","transcript_path":"/tmp/t.jsonl","context_window":{},"prompt_cache":` + raw + `}`))
	if err != nil {
		t.Fatalf("Decode returned error: %v", err)
	}
	pc := snap.Session.PromptCache
	if pc == nil {
		t.Fatal("PromptCache = nil, want populated (object is present)")
	}
	// A null epoch must stay nil rather than becoming time.Unix(0, 0).
	if pc.ExpiresAt != nil || pc.LastMissAt != nil {
		t.Errorf("ExpiresAt/LastMissAt = %v/%v, want nil/nil", pc.ExpiresAt, pc.LastMissAt)
	}
	if pc.HitRatio != nil || pc.RecacheTokensIfCold != nil {
		t.Errorf("HitRatio/RecacheTokensIfCold = %v/%v, want nil/nil", pc.HitRatio, pc.RecacheTokensIfCold)
	}
	if pc.LastMissCauses != nil {
		t.Errorf("LastMissCauses = %v, want nil", pc.LastMissCauses)
	}
}

func TestDecodePromptCacheEmptyCausesStayNil(t *testing.T) {
	for _, cause := range []string{`{}`, `{"causes":[]}`} {
		raw := `{"last_miss_cause":` + cause + `}`
		snap, err := New().Decode([]byte(`{"session_id":"s1","transcript_path":"/tmp/t.jsonl","context_window":{},"prompt_cache":` + raw + `}`))
		if err != nil {
			t.Fatalf("Decode(%s) returned error: %v", cause, err)
		}
		if snap.Session.PromptCache == nil {
			t.Fatalf("Decode(%s): PromptCache = nil, want populated", cause)
		}
		if snap.Session.PromptCache.LastMissCauses != nil {
			t.Errorf("Decode(%s): LastMissCauses = %v, want nil", cause, snap.Session.PromptCache.LastMissCauses)
		}
	}
}

func TestDecodePromptCacheFractionalValuesTruncate(t *testing.T) {
	const raw = `{"expires_at":1760000000.7,"requests":3.0,"misses":2.5}`

	snap, err := New().Decode([]byte(`{"session_id":"s1","transcript_path":"/tmp/t.jsonl","context_window":{},"prompt_cache":` + raw + `}`))
	if err != nil {
		t.Fatalf("Decode returned error: %v", err)
	}
	pc := snap.Session.PromptCache
	if pc == nil {
		t.Fatal("PromptCache = nil, want populated")
	}
	if pc.ExpiresAt == nil || !pc.ExpiresAt.Equal(time.Unix(1760000000, 0)) {
		t.Errorf("ExpiresAt = %v, want Unix 1760000000", pc.ExpiresAt)
	}
	if pc.Requests != 3 || pc.Misses != 2 {
		t.Errorf("Requests/Misses = %d/%d, want 3/2", pc.Requests, pc.Misses)
	}
}

func TestDecodePromptCacheAbsentNullOrMalformedIsNil(t *testing.T) {
	cases := map[string]string{
		"absent":           "",
		"explicit null":    `null`,
		"string":           `"oops"`,
		"array":            `[1]`,
		"wrong-typed attr": `{"requests":"many"}`,
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			id, pc := decodeWithPromptCache(t, raw)
			if pc != nil {
				t.Errorf("PromptCache populated for %s input, want nil", name)
			}
			if id != "s1" {
				t.Errorf("Session.ID = %q, want \"s1\" (rest of snapshot must survive)", id)
			}
		})
	}
}
