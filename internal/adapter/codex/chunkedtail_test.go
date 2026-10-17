package codex

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/adapter"
)

// eventIDs is the multiset of every emitted tool and token event id.
func eventIDs(res adapter.ParseResult) map[string]int {
	out := map[string]int{}
	for _, ev := range res.ToolEvents {
		out["tool|"+ev.SourceEventID]++
	}
	for _, ev := range res.TokenEvents {
		out["token|"+ev.SourceEventID]++
	}
	return out
}

// TestParseHonoursChunkedTailLimit pins T-413 for Codex: a parse with
// a read limit on its context reads at most that many bytes past fromOffset,
// stops on a line boundary, and a sequence of bounded windows emits exactly
// the events one unbounded parse does. That is what lets the watcher read an
// over-cap tail in windows instead of skipping the rollout forever.
func TestParseHonoursChunkedTailLimit(t *testing.T) {
	t.Parallel()
	const window = 2000
	info, err := os.Stat(genRealFixture)
	if err != nil {
		t.Fatal(err)
	}
	a := New()
	if roots := a.WatchPaths(); len(roots) == 0 || !a.CursorSemanticsFor(filepath.Join(roots[0], "p", "rollout-2026-10-05T10-00-00-0199aaaa-bbbb-7ccc-8ddd-eeeeeeeeeeee.jsonl")).TailChunkable() {
		t.Fatal("rollout semantics do not declare a chunkable tail")
	}
	whole, err := a.ParseSessionFile(context.Background(), genRealFixture, 0)
	if err != nil {
		t.Fatal(err)
	}
	if whole.NewOffset != info.Size() {
		t.Fatalf("unbounded parse ended at %d, want %d", whole.NewOffset, info.Size())
	}

	got := map[string]int{}
	var off int64
	windows := 0
	for off < info.Size() {
		res, err := a.ParseSessionFile(adapter.WithParseByteLimit(context.Background(), window), genRealFixture, off)
		if err != nil {
			t.Fatal(err)
		}
		if res.NewOffset <= off {
			t.Fatalf("window %d made no progress from %d", windows, off)
		}
		if res.NewOffset-off > window {
			t.Fatalf("window %d read %d bytes, over the %d limit", windows, res.NewOffset-off, window)
		}
		for k, n := range eventIDs(res) {
			got[k] += n
		}
		off = res.NewOffset
		windows++
	}
	if windows < 10 {
		t.Fatalf("only %d windows; the limit was not applied", windows)
	}
	// The SET of event ids must match. Multiplicity may not: a record
	// family split across a window boundary (one assistant message written
	// as several lines) is re-emitted by the next window under the same
	// SourceEventID, exactly as when a live tail is read mid-message, and
	// the store's (source_file, source_event_id) uniqueness folds it.
	want := eventIDs(whole)
	if len(got) != len(want) {
		t.Fatalf("windowed parse emitted %d distinct events, unbounded %d", len(got), len(want))
	}
	for k := range want {
		if got[k] == 0 {
			t.Fatalf("event %s: emitted by the unbounded parse, missing from the windows", k)
		}
	}
}
