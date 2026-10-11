package watcher

import (
	"context"
	"os"

	"github.com/marmutapp/superbased-observer/internal/adapter"
)

// rescanRemainingWindows finishes a FORCED from-zero pass over a file whose
// size is over MaxFileBytes (T-568).
//
// The ordinary watcher reads such a file one bounded window per pass (T-413)
// and relies on the persisted cursor to carry it to the next window. A forced
// rescan cannot: it starts at byte 0, and the cursor it writes is MAX-merged
// with the one already stored, so on a node whose cursor had reached the end
// nothing ever re-read the bytes after the first window. `observer backfill
// --codex-rescan` re-parsed only the first 50 MB of a 50 to 110 MB rollout and
// logged "deferred unterminated trailing record" where the window cut a record.
//
// Here the rescan keeps going, one window at a time, from where the previous
// window's parse ended (the end of its last complete record), so memory stays
// bounded by the cap while the whole file is read. It stops at EOF, on a
// window that made no progress (a record longer than the window, or an
// unterminated record still being written), on a retry hint, or when ctx ends.
func (w *Watcher) rescanRemainingWindows(ctx context.Context, a adapter.Adapter, path string, out budgetCaptureFileOutcome) error {
	for out.windowed && out.parsed && !out.retrySuggested && out.windowEnd > out.startOffset {
		fi, err := os.Stat(path)
		if err != nil || out.windowEnd >= fi.Size() {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		next, err := w.processFileFrom(ctx, a, path, true, false, out.windowEnd)
		if err != nil {
			return err
		}
		out = next
	}
	return nil
}
