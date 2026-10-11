//go:build !sbo_omit_proxy_routing && !sbo_omit_capture_proxy

package main

import (
	"context"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/routing"
	"github.com/marmutapp/superbased-observer/internal/selfobs/run"
)

func TestEmitSelfObs_SampleN(t *testing.T) {
	t.Parallel()
	rec := &recordingSink{}
	lr := &liveRouter{
		selfobs:        rec,
		selfobsSampleN: 3,
	}
	d := routing.Decision{RuleName: "r1"}
	for i := 0; i < 9; i++ {
		lr.emitSelfObs("sess-1", routing.TurnKind("readonly"), d, true)
	}
	if got := rec.n; got != 3 {
		t.Fatalf("emitted %d, want 3 (1-of-3 over 9)", got)
	}
}

func TestEmitSelfObs_SampleZeroOff(t *testing.T) {
	t.Parallel()
	rec := &recordingSink{}
	lr := &liveRouter{
		selfobs:        rec,
		selfobsSampleN: 0,
	}
	lr.emitSelfObs("sess-1", routing.TurnKind("readonly"), routing.Decision{}, true)
	if rec.n != 0 {
		t.Fatalf("emitted %d with sampleN=0, want 0", rec.n)
	}
}

type recordingSink struct {
	n int
}

func (r *recordingSink) Emit(_ context.Context, _ run.DecisionRun) { r.n++ }
func (r *recordingSink) ForceFlush(context.Context) error          { return nil }
func (r *recordingSink) Shutdown(context.Context) error            { return nil }
