package orgclient

import (
	"sync"
	"time"
)

// notFoundBackoff spaces out a node-pulled rail that keeps answering 404
// (2026-10-02 budget-latency arc, Q7). The demo estate's org log showed
// `GET /api/agent/routing-policy 404` from every node on every push cycle -
// every 30 s from one node - for a rail the org simply never published. A 404
// on these rails means "nothing published" (or "server predates the rail"),
// which is a steady state, not an outage: re-asking every cycle buys nothing.
//
// The rule, one table row per state:
//
//   - a non-404 answer (200/304/any error) resets the streak: the rail is
//     live, or genuinely failing and owned by the caller's own error path;
//   - the first 404 allows the next attempt one base interval later (the
//     push ceiling, i.e. exactly today's cadence);
//   - each further 404 doubles the wait, capped at maxNotFoundBackoff;
//   - Reset (called when the org's policy-version map says the rail's
//     component moved) re-arms an immediate fetch.
//
// A skipped cycle CHANGES NOTHING on the node: these rails never clear on a
// 404 anyway (routing/announcement treat it as "none published", intel as
// "not supported"), so skipping a request that would have produced the same
// no-op answer is safe by construction.
type notFoundBackoff struct {
	mu     sync.Mutex
	streak int
	until  time.Time
}

// maxNotFoundBackoff bounds the longest a 404-answering rail goes unasked.
const maxNotFoundBackoff = time.Hour

// Skip reports whether the rail should NOT be fetched this cycle.
func (b *notFoundBackoff) Skip(now time.Time) bool {
	if b == nil {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.streak > 0 && now.Before(b.until)
}

// Observe records one fetch result. notFound is true for an HTTP 404; base is
// the caller's normal cadence (the push ceiling).
func (b *notFoundBackoff) Observe(notFound bool, now time.Time, base time.Duration) {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if !notFound {
		b.streak, b.until = 0, time.Time{}
		return
	}
	b.streak++
	wait := base
	for i := 1; i < b.streak && wait < maxNotFoundBackoff; i++ {
		wait *= 2
	}
	if wait > maxNotFoundBackoff {
		wait = maxNotFoundBackoff
	}
	if wait < 0 {
		wait = 0
	}
	b.until = now.Add(wait)
}

// Reset re-arms an immediate fetch (the org said this rail changed).
func (b *notFoundBackoff) Reset() {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.streak, b.until = 0, time.Time{}
}
