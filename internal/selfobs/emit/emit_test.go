package emit

import (
	"context"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/provenance"
	"github.com/marmutapp/superbased-observer/internal/selfobs/run"
)

func TestNopNeverPanics(t *testing.T) {
	t.Parallel()

	sink := Nop()
	if sink == nil {
		t.Fatal("Nop returned a nil interface")
	}
	ctx := context.Background()
	sink.Emit(ctx, run.DecisionRun{RunID: "run-1", Component: "test-component", InitiatedBy: provenance.ActorHuman})
	if err := sink.ForceFlush(ctx); err != nil {
		t.Errorf("Nop ForceFlush: %v", err)
	}
	if err := sink.Shutdown(ctx); err != nil {
		t.Errorf("Nop Shutdown: %v", err)
	}
}
