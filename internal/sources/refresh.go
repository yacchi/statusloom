package sources

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/yacchi/statusloom/internal/cache"
)

// RefreshBudget: Refresh starts no new fetch once this much time has passed since it
// began. This keeps one worker well inside the 2-minute refresh lease.
const RefreshBudget = 60 * time.Second

// DueIDs returns, sorted and deduplicated, the ids among refs that f defines as
// command/http sources and that are due in s: no s.Sources entry, entry.DefHash !=
// def.Hash() (the definition changed), or !now.Before(entry.NextDueAt). file and
// undefined ids are never due. Pure: no I/O.
func DueIDs(f File, refs []string, s cache.RefreshSchedule, now time.Time) []string {
	seen := make(map[string]bool, len(refs))
	var out []string
	for _, id := range refs {
		if seen[id] {
			continue
		}
		seen[id] = true

		def, ok := f.Get(id)
		if !ok || (def.Kind != KindCommand && def.Kind != KindHTTP) {
			continue
		}
		entry, ok := s.Sources[id]
		if !ok || entry.DefHash != def.Hash() || !now.Before(entry.NextDueAt) {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

// Refresh is the refresh worker's external-source step (`statusloom refresh --once
// --sources a,b`). It loads sources.json and the refresh schedule, then for each id in
// ids (deduplicated, order kept) that DueIDs still reports due, fetches once with fetch
// and records the outcome. Sequential; errors are recorded, never returned.
func Refresh(ctx context.Context, ids []string, now time.Time, refreshInterval time.Duration, fetch Fetcher) {
	f, err := Load()
	if err != nil {
		return
	}
	s := cache.LoadRefreshSchedule()
	if s.Sources == nil {
		s.Sources = map[string]cache.WorkSchedule{}
	}

	start := time.Now()
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true

		if ctx.Err() != nil || time.Since(start) >= RefreshBudget {
			return
		}
		// Re-check against the schedule just loaded: another worker may have fetched
		// this id since the spawn. File and undefined ids drop out here too.
		if len(DueIDs(f, []string{id}, s, now)) == 0 {
			continue
		}

		def, _ := f.Get(id)
		s.Sources[id] = refreshOne(ctx, def, s.Sources[id], now, refreshInterval, fetch)
		_ = cache.StoreRefreshSchedule(s)
	}
}

// refreshOne performs one fetch of def and returns the updated schedule entry. It
// stores the resulting envelope (best effort) and never returns an error.
func refreshOne(
	ctx context.Context,
	def Definition,
	entry cache.WorkSchedule,
	now time.Time,
	refreshInterval time.Duration,
	fetch Fetcher,
) cache.WorkSchedule {
	hash := def.Hash()
	prev, hasPrev := LoadEnvelope(def.ID)
	// A changed definition never reuses the old body or sends the old ETag.
	reuse := hasPrev && prev.OK && prev.DefHash == hash && len(prev.Body) > 0
	etag := ""
	if reuse {
		etag = prev.ETag
	}
	// Failures of an older definition never count toward the new one's backoff.
	if entry.DefHash != hash {
		entry = cache.WorkSchedule{}
	}

	base := def.TTL(refreshInterval)
	res, err := fetch(ctx, def, etag)
	if err == nil && res.NotModified && !reuse {
		err = errors.New("HTTP 304 without a cached body")
	}

	env := Envelope{
		SchemaVersion: envelopeSchemaVersion,
		ID:            def.ID,
		DefHash:       hash,
		AttemptedAt:   now,
	}
	if err != nil {
		env.Error = err.Error()
		if reuse {
			env.FetchedAt = prev.FetchedAt
		}
		entry = entry.Record(now, false, base, MaxBackoff)
	} else {
		env.OK = true
		env.FetchedAt = now
		if res.NotModified {
			env.Body = prev.Body
			env.ETag = etag
		} else {
			env.Body = res.Body
			env.ETag = res.ETag
		}
		entry = entry.Record(now, true, base, MaxBackoff)
	}
	_ = StoreEnvelope(env)

	entry.DefHash = hash
	return entry
}
