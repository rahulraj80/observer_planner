package emit

import (
	"context"

	"github.com/marmutapp/superbased-observer/internal/selfobs/run"
)

// Sink emits self-observability decision runs. Emit is fire-and-forget; callers
// flush with ForceFlush and release with Shutdown.
type Sink interface {
	Emit(ctx context.Context, r run.DecisionRun)
	ForceFlush(ctx context.Context) error
	Shutdown(ctx context.Context) error
}

// Nop returns a real no-op Sink (never nil).
func Nop() Sink { return nopSink{} }

type nopSink struct{}

func (nopSink) Emit(context.Context, run.DecisionRun) {}
func (nopSink) ForceFlush(context.Context) error      { return nil }
func (nopSink) Shutdown(context.Context) error        { return nil }
