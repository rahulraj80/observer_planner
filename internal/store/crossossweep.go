package store

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/marmutapp/superbased-observer/internal/procmodel"
)

// crossOSFullEvery bounds how long the sweeper may skip a session's cross-OS
// pass on the strength of "no new process row touches its pool". A row that
// changes IN PLACE (an UPSERT filling a cwd, another pass re-homing a row)
// carries no new id, so a periodic full pass picks it up within this bound.
const crossOSFullEvery = 10 * time.Minute

// crossOSDeltaCap is the most new process rows one sweep inspects. A burst
// larger than this (a daemon that was down, a backfill) is treated as
// touching every session: running the full passes is cheaper than reasoning
// about a huge delta, and it is the pre-sweeper behaviour.
const crossOSDeltaCap = 50000

// CrossOSSweeper is the background cross-OS sweep's memory across ticks
// (T-413): per session, the pool its last converged full pass loaded, and the
// process_runs id watermark the next sweep reads new rows from. It is the ONE
// owner of that state; the daemon keeps one for its lifetime. The zero value
// is not usable; use NewCrossOSSweeper.
type CrossOSSweeper struct {
	mu        sync.Mutex
	watermark int64
	memos     map[string]crossOSMemo
	// now is the clock for the full-pass bound; tests inject one.
	now func() time.Time
}

type crossOSMemo struct {
	sess   procmodel.CrossOSSessionRef
	pool   procmodel.CrossOSPool
	fullAt time.Time
}

// NewCrossOSSweeper returns an empty sweeper: its first sweep runs a full
// pass for every session.
func NewCrossOSSweeper() *CrossOSSweeper {
	return &CrossOSSweeper{memos: map[string]crossOSMemo{}, now: time.Now}
}

// CrossOSSweepResult is what one sweep did.
type CrossOSSweepResult struct {
	// Swept is the number of sessions whose correlation was settled this
	// sweep, by a full pass or by a skip.
	Swept int
	// FullPasses is the number of full cross-OS passes that ran.
	FullPasses int
	// Skipped is the number of sessions whose pass was skipped because no
	// new process row could change its result.
	Skipped int
	// Attributed is the number of process rows newly attributed.
	Attributed int
	// Failed is the number of sessions whose pass returned an error; they
	// are retried on the next sweep.
	Failed int
}

// SweepCrossOS runs the background cross-OS correlation over sessionIDs,
// skipping a session's full pass when none of the process rows captured since
// the previous sweep can enter its candidate pool (procmodel.CrossOSPool).
// A nil sweeper runs a full pass for every session, exactly like calling
// CorrelateCrossOS on each.
//
// A session's pool is remembered only after a pass that attributed NOTHING:
// a pass that attributed rows can widen the next pass's pool (a newly
// attributed root pulls its whole subtree in through the session seed), so
// the next sweep must run it again until it converges, as before.
func (s *Store) SweepCrossOS(ctx context.Context, sw *CrossOSSweeper, sessionIDs []string) (CrossOSSweepResult, error) {
	var res CrossOSSweepResult
	if sw == nil {
		for _, id := range sessionIDs {
			n, err := s.CorrelateCrossOS(ctx, id)
			if err != nil {
				res.Failed++
				continue
			}
			res.Swept++
			res.FullPasses++
			res.Attributed += n
		}
		return res, nil
	}
	sw.mu.Lock()
	defer sw.mu.Unlock()

	// The high-water mark is read BEFORE any pool is loaded, so a row
	// inserted while this sweep runs is inspected again by the next one.
	var high int64
	if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(id), 0) FROM process_runs`).Scan(&high); err != nil {
		return res, fmt.Errorf("store.SweepCrossOS: watermark: %w", err)
	}
	delta, deltaOK := s.crossOSDelta(ctx, sw.watermark, high)

	now := sw.now()
	active := make(map[string]struct{}, len(sessionIDs))
	for _, id := range sessionIDs {
		if id == "" {
			continue
		}
		active[id] = struct{}{}
		sess, ok, err := s.loadCrossOSSession(ctx, id)
		if err != nil {
			res.Failed++
			continue
		}
		if !ok {
			delete(sw.memos, id)
			res.Swept++
			continue
		}
		if m, have := sw.memos[id]; have && deltaOK &&
			procmodel.SameCrossOSSession(m.sess, sess) &&
			now.Sub(m.fullAt) < crossOSFullEvery && !now.Before(m.fullAt) &&
			!m.pool.Touched(sess, delta, procmodel.CrossOSWindow) {
			res.Swept++
			res.Skipped++
			continue
		}
		n, runs, err := s.correlateCrossOSPass(ctx, sess)
		if err != nil {
			delete(sw.memos, id)
			res.Failed++
			continue
		}
		res.Swept++
		res.FullPasses++
		res.Attributed += n
		if n == 0 {
			sw.memos[id] = crossOSMemo{sess: sess, pool: procmodel.NewCrossOSPool(runs), fullAt: now}
		} else {
			delete(sw.memos, id)
		}
	}
	for id := range sw.memos {
		if _, ok := active[id]; !ok {
			delete(sw.memos, id)
		}
	}
	// Every row up to high is now accounted for: either it touched no pool,
	// or (delta unreadable or over the cap) every session ran a full pass.
	sw.watermark = high
	return res, nil
}

// crossOSDelta reads the process rows inserted in (after, upTo]. ok=false
// means the caller must not trust it (first sweep, read error, or a burst
// over crossOSDeltaCap) and should run full passes.
func (s *Store) crossOSDelta(ctx context.Context, after, upTo int64) ([]procmodel.CrossOSDelta, bool) {
	if after <= 0 || upTo < after {
		return nil, false
	}
	if upTo == after {
		return nil, true
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT process_key, COALESCE(parent_process_key, ''), COALESCE(session_id, ''), started_at
		  FROM process_runs
		 WHERE id > ? AND id <= ?
		 ORDER BY id
		 LIMIT ?`, after, upTo, crossOSDeltaCap+1)
	if err != nil {
		return nil, false
	}
	defer rows.Close()
	var out []procmodel.CrossOSDelta
	for rows.Next() {
		var d procmodel.CrossOSDelta
		var started string
		if err := rows.Scan(&d.ProcessKey, &d.ParentProcessKey, &d.SessionID, &started); err != nil {
			return nil, false
		}
		d.StartedAt = parseStamp(started)
		out = append(out, d)
	}
	if rows.Err() != nil || len(out) > crossOSDeltaCap {
		return nil, false
	}
	return out, true
}
