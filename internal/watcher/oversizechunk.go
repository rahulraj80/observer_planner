package watcher

import (
	"github.com/marmutapp/superbased-observer/internal/adapter"
)

// OversizeChunked reports whether the MaxFileBytes guard reads a file whose
// unread tail is over the cap in bounded windows instead of skipping it
// (T-413): the adapter declared that its parse honours the per-call read
// limit (adapter.FileCursorSemantics.TailChunkable).
//
// It sits beside OversizeSkipped as the second half of the one rule the
// watcher's gate and `observer doctor` both apply: a file that
// OversizeSkipped would skip and OversizeChunked accepts is being read, one
// window per pass, not stalled.
func OversizeChunked(sem adapter.FileCursorSemantics) bool {
	return sem.TailChunkable()
}

// chunkStalledAt reports whether a bounded window of path already made no
// progress from cursor off. Such a file falls back to the oversize skip at
// that offset: re-reading a full window every pass for a record that never
// fits would be the very cost the guard exists to bound.
func (w *Watcher) chunkStalledAt(path string, off int64) bool {
	v, ok := w.chunkStalls.Load(path)
	return ok && v.(int64) == off
}

// noteChunkProgress records the outcome of one bounded window. A window that
// advanced the cursor clears any stall mark; one that did not (and did not
// ask to be retried) marks the path stalled at its start offset, so the next
// pass skips it until the cursor moves.
func (w *Watcher) noteChunkProgress(a adapter.Adapter, path string, off int64, res adapter.ParseResult) {
	if res.NewOffset > off || res.RetrySuggested {
		w.chunkStalls.Delete(path)
		return
	}
	w.chunkStalls.Store(path, off)
	if w.warningDedup.Allow(a.Name() + "|" + path + "|<oversize-chunk-stalled>") {
		w.logger.Warn("watcher.processFile: oversize tail window made no progress; one record is longer than the window, skipping the file at this cursor",
			"path", path, "cursor", off, "window", w.maxFileBytes)
	}
}
