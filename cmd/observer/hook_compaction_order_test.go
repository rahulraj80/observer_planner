package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/hookspool"
	"github.com/marmutapp/superbased-observer/internal/integration"
	"github.com/marmutapp/superbased-observer/internal/models"
)

// TestPostCompactWaitsBehindHeldPreCompact pins P3-37: a PreCompact held for
// a session the watcher had not ingested, followed by a PostCompact that fires
// once the session exists, must land pre then post. Pre-fix the post was
// applied at once (reconciling nothing) and the held pre was captured later,
// after the compaction it describes.
func TestPostCompactWaitsBehindHeldPreCompact(t *testing.T) {
	r := newHoldRig(t)
	ctx := context.Background()
	now := time.Now().UTC()
	pre := hookspool.Record{
		Kind: hookspool.KindCompaction, Phase: integration.HookPhasePre, Tool: models.ToolClaudeCode,
		SessionID: "s_p337", ProjectRoot: "/tmp/p_t320", Trigger: "auto", Timestamp: now,
	}
	if err := recordCompactionOrHold(ctx, r.database, r.spool, pre); err != nil {
		t.Fatal(err)
	}
	r.createSession(t, "s_p337")
	post := pre
	post.Phase = integration.HookPhasePost
	post.Timestamp = now.Add(time.Second)
	if err := recordCompactionOrHold(ctx, r.database, r.spool, post); err != nil {
		t.Fatal(err)
	}
	// Both records sit on the live spool, pre first.
	if _, err := hookspool.SetAside(r.spool, now); err != nil {
		t.Fatal(err)
	}
	ready, err := hookspool.Ready(r.spool, now.Add(hookspool.Grace+time.Second))
	if err != nil || len(ready) != 1 {
		t.Fatalf("ready = %v (%v), want one aside file", ready, err)
	}
	var phases []string
	for _, rec := range ready[0].Records {
		phases = append(phases, rec.Phase)
	}
	if len(phases) != 2 || phases[0] != integration.HookPhasePre || phases[1] != integration.HookPhasePost {
		t.Fatalf("spooled phases = %v, want [pre post]: the post must wait behind the held pre", phases)
	}
	// The drain lands both, in order.
	if n := drainHookLifecycleSpool(ctx, r.spool, r.deps(), now.Add(hookspool.Grace+time.Second), r.logger); n != 2 {
		t.Fatalf("drain landed %d, want 2", n)
	}
	if got := r.count(t, `SELECT COUNT(*) FROM compaction_events WHERE session_id = 's_p337'`); got != 1 {
		t.Fatalf("compaction_events = %d, want 1", got)
	}
	// Nothing is held any more.
	if left, _ := filepath.Glob(r.spool + "*"); len(left) != 0 {
		t.Fatalf("spool files after the drain = %v, want none", left)
	}
}
