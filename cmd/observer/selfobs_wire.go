package main

// selfobs_wire.go is the core half of the self-observability wiring: the
// fire-and-forget emit helper every producer calls with an emit.Sink. The
// sink itself is built by buildSelfObsSink, which lives with the export.otel
// component (selfobs_sink.go, or selfobs_sink_omit.go in a
// `-tags sbo_omit_export_otel` build, which always returns emit.Nop),
// modular packaging ruling D-436.

import (
	"context"

	"github.com/marmutapp/superbased-observer/internal/provenance"
	"github.com/marmutapp/superbased-observer/internal/selfobs/emit"
	"github.com/marmutapp/superbased-observer/internal/selfobs/run"
)

// emitSelfObsRun fire-and-forgets one DecisionRun when a sink is present.
// Nil/Nop sinks are safe; never blocks callers beyond the sink's async path.
func emitSelfObsRun(sink emit.Sink, r run.DecisionRun) {
	if sink == nil {
		return
	}
	if r.InitiatedBy == "" {
		r.InitiatedBy = provenance.ActorHuman
	}
	sink.Emit(context.Background(), r)
}
