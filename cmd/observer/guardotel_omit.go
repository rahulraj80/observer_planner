//go:build sbo_omit_export_otel

package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/marmutapp/superbased-observer/internal/store"
)

// guardotel_omit.go is the off side of guardotel.go: in a
// `-tags sbo_omit_export_otel` build there is no exporter, so start.go never
// starts the guard-event tail (it runs only under a non-nil exporter). The
// type keeps start.go's composite literal compiling.
type guardOTelTail struct {
	st       *store.Store
	sink     otelExporterHandle
	logger   *slog.Logger
	interval time.Duration
}

// run does nothing: guard events are not exported in this build.
func (*guardOTelTail) run(context.Context) {}
