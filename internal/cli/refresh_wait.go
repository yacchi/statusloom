package cli

import (
	"time"

	"github.com/yacchi/statusloom/internal/cache"
	"github.com/yacchi/statusloom/internal/dsl"
)

// The render-side refresh wait (markup.md "refresh", plans/refresh-wait.md).
// A live render whose document has <refresh wait="..."> may wait, up to its
// timeout, for named background work items to finish before drawing. The
// render still only reads caches, asks the refresh worker for work, and waits:
// the completion signal is each item's schedule entry (LastAttempt advancing
// past the value seen when the wait began), polled from schedule.json, and the
// worker it waits for is noticed gone through lease.json. No new file, lease or
// daemon; a document without <refresh> never reaches this code.

// waitPollInterval is how often the wait re-reads schedule.json and lease.json.
// Tests shorten it.
var waitPollInterval = 20 * time.Millisecond

// waitSleep is the wait's sleep; tests may replace it.
var waitSleep = time.Sleep

// refreshWaitDisabled reports the STATUSLOOM_NO_WAIT kill switch: any non-empty
// value makes renders ignore <refresh> and never wait.
func refreshWaitDisabled(getenv func(string) string) bool {
	return getenv("STATUSLOOM_NO_WAIT") != ""
}

// refreshWaitResult is what a refresh wait did.
type refreshWaitResult struct {
	// Waited is true when there was something to wait for (the render must
	// re-apply the caches before drawing).
	Waited bool
	// Spawned is true when the wait tried to start a worker itself (successful
	// or not), so the render must not spawn again after drawing.
	Spawned bool
}

// waitCandidates returns the due items of plan that policy waits for in s:
// cold ones (never attempted) by default, or with wait-on="due" every due one
// not in failure backoff (a cold item is always a candidate).
func waitCandidates(plan refreshPlan, s cache.RefreshSchedule, policy dsl.RefreshPolicy) []cache.WorkItem {
	var out []cache.WorkItem
	for _, it := range plan.Items {
		if !policy.Wants(string(it.Kind), it.Key) {
			continue
		}
		if s.Cold(it) {
			out = append(out, it)
			continue
		}
		if w, _ := s.Entry(it); policy.WaitOnDue && w.Failures == 0 {
			out = append(out, it)
		}
	}
	return out
}

// waitForRefresh requests the due work and waits until every candidate item
// (waitCandidates) has been attempted once, the deadline passes, or the
// workers that could attempt them are gone:
//
//   - lease free: spawn a worker for all due items (not only the candidates)
//     and watch it; a failed spawn ends the wait at once;
//   - lease held by another worker: watch that worker instead of spawning;
//   - the watched worker ended (lease gone or replaced) with candidates still
//     unattempted (it never had them, ran out of budget, or is an older
//     version that does not know the work): plan again and start one more
//     worker, at most once per render; when that one also ends, stop.
//
// An item is complete when its LastAttempt is after the value seen at the
// start (not after the start time: another process's worker records its own
// start time). Success is not required, so a failure ends the wait as well.
// A worker writes an item's data before its schedule entry, so a completed
// item's data is visible when the wait sees it.
func waitForRefresh(req refreshRequest, policy dsl.RefreshPolicy, deadline time.Time) refreshWaitResult {
	var res refreshWaitResult
	s := cache.LoadRefreshSchedule()
	plan, ok := planRefresh(req, s)
	if !ok {
		return res
	}
	cands := waitCandidates(plan, s, policy)
	if len(cands) == 0 {
		return res
	}
	res.Waited = true
	baseline := make([]time.Time, len(cands))
	for i, it := range cands {
		w, _ := s.Entry(it)
		baseline[i] = w.LastAttempt
	}

	retried := false
	watched, outcome := startPlannedRefresh(plan, req.Now)
	switch outcome {
	case spawnStarted:
		res.Spawned = true
	case spawnFailed:
		// Do not wait for a worker that never started (and do not try again
		// after drawing).
		res.Spawned = true
		return res
	default:
		id, held := cache.CurrentRefreshLeaseID()
		if !held {
			// The holder finished in between: one more try, below.
			id = ""
		}
		watched = id
	}

	for {
		now := time.Now()
		if !now.Before(deadline) {
			return res
		}
		waitSleep(min(waitPollInterval, deadline.Sub(now)))

		s = cache.LoadRefreshSchedule()
		if allAttempted(s, cands, baseline) {
			return res
		}
		if id, held := cache.CurrentRefreshLeaseID(); held && id == watched {
			continue
		}
		// The watched worker is gone but candidates remain.
		if retried {
			return res
		}
		retried = true
		next := req
		next.Now = time.Now()
		plan, ok = planRefresh(next, s)
		if !ok || len(plan.Items) == 0 {
			return res
		}
		res.Spawned = true
		watched, outcome = startPlannedRefresh(plan, next.Now)
		switch outcome {
		case spawnStarted:
			continue
		case spawnFailed:
			return res
		}
		// Someone else took the lease first: watch that worker instead.
		id, held := cache.CurrentRefreshLeaseID()
		if !held {
			return res
		}
		watched = id
	}
}

// allAttempted reports whether every item's LastAttempt is after its baseline.
func allAttempted(s cache.RefreshSchedule, items []cache.WorkItem, baseline []time.Time) bool {
	for i, it := range items {
		w, _ := s.Entry(it)
		if !w.LastAttempt.After(baseline[i]) {
			return false
		}
	}
	return true
}
