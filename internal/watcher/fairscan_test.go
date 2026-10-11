package watcher

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/adapter"
	"github.com/marmutapp/superbased-observer/internal/db"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// orderAdapter is a stub adapter that records every parse in a shared
// journal and can hold a parse of one chosen file until released. Parses
// emit no events; they only report the file size as the new offset, so
// the watcher advances the cursor exactly as for a real transcript.
type orderAdapter struct {
	name    string
	root    string
	ext     string
	journal *parseJournal
	// block, when non-nil, holds the parse of any file whose base name is
	// blockName until the channel is closed (or ctx ends). started is
	// closed when that parse begins.
	blockName string
	block     chan struct{}
	started   chan struct{}
	startOnce sync.Once
}

type parseJournal struct {
	mu    sync.Mutex
	order []string
}

func (j *parseJournal) add(s string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.order = append(j.order, s)
}

func (j *parseJournal) snapshot() []string {
	j.mu.Lock()
	defer j.mu.Unlock()
	return append([]string(nil), j.order...)
}

func (o *orderAdapter) Name() string                { return o.name }
func (o *orderAdapter) WatchPaths() []string        { return []string{o.root} }
func (o *orderAdapter) IsSessionFile(p string) bool { return strings.HasSuffix(p, o.ext) }
func (o *orderAdapter) ParseSessionFile(ctx context.Context, p string, off int64) (adapter.ParseResult, error) {
	if o.block != nil && filepath.Base(p) == o.blockName {
		o.startOnce.Do(func() { close(o.started) })
		select {
		case <-o.block:
		case <-ctx.Done():
			return adapter.ParseResult{NewOffset: off}, ctx.Err()
		}
	}
	o.journal.add(o.name + "/" + filepath.Base(p))
	info, err := os.Stat(p)
	if err != nil {
		return adapter.ParseResult{}, err
	}
	return adapter.ParseResult{NewOffset: info.Size()}, nil
}

func fairScanFixture(t *testing.T, pollInterval time.Duration, adapters ...*orderAdapter) (*Watcher, *store.Store) {
	t.Helper()
	ctx := context.Background()
	database, err := db.Open(ctx, db.Options{Path: filepath.Join(t.TempDir(), "w.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	s := store.New(database)
	reg := adapter.NewRegistry()
	for _, a := range adapters {
		reg.Register(a)
	}
	w := New(s, reg, Options{
		Logger:             slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
		Debounce:           20 * time.Millisecond,
		PollInterval:       pollInterval,
		RootDetectInterval: -1,
	})
	return w, s
}

func writeFiles(t *testing.T, root, ext string, n int) []string {
	t.Helper()
	var out []string
	for i := 0; i < n; i++ {
		p := filepath.Join(root, fmt.Sprintf("f%03d%s", i, ext))
		if err := os.WriteFile(p, []byte("line\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		out = append(out, p)
	}
	return out
}

// TestScanInterleavesAdaptersRoundRobin pins T-412's fairness rule: the
// full scan visits files of different adapters round-robin, so a tool
// with a large backlog cannot hold another tool's files until its whole
// tree is parsed. Pre-fix, adapters were walked one after another and the
// second adapter's only file was parsed 21st.
func TestScanInterleavesAdaptersRoundRobin(t *testing.T) {
	t.Parallel()
	j := &parseJournal{}
	big := &orderAdapter{name: "aaa-big", root: t.TempDir(), ext: ".big", journal: j}
	small := &orderAdapter{name: "bbb-small", root: t.TempDir(), ext: ".small", journal: j}
	writeFiles(t, big.root, big.ext, 20)
	writeFiles(t, small.root, small.ext, 1)
	w, s := fairScanFixture(t, 0, big, small)

	res, err := w.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.FilesProcessed != 21 || res.Errors != 0 {
		t.Fatalf("scan result = %+v, want 21 files / 0 errors", res)
	}
	order := j.snapshot()
	pos := -1
	for i, e := range order {
		if strings.HasPrefix(e, "bbb-small/") {
			pos = i
		}
	}
	if pos < 0 || pos > 1 {
		t.Fatalf("second adapter's file parsed at position %d of %d, want <= 1 (round-robin); order=%v", pos, len(order), order)
	}
	// Within one adapter the walk order is unchanged.
	var bigOrder []string
	for _, e := range order {
		if strings.HasPrefix(e, "aaa-big/") {
			bigOrder = append(bigOrder, e)
		}
	}
	for i := 1; i < len(bigOrder); i++ {
		if bigOrder[i-1] >= bigOrder[i] {
			t.Fatalf("per-adapter order changed: %v", bigOrder)
		}
	}
	// Every file reached its cursor.
	for _, root := range []string{big.root, small.root} {
		entries, _ := os.ReadDir(root)
		for _, e := range entries {
			off, err := s.GetCursor(context.Background(), filepath.Join(root, e.Name()))
			if err != nil || off != int64(len("line\n")) {
				t.Errorf("cursor for %s = %d (%v), want %d", e.Name(), off, err, len("line\n"))
			}
		}
	}
}

// TestWatchAdvancesOtherToolDuringInitialScan pins T-412's stall: while
// the initial scan is stuck inside one tool's file, another tool's file
// must still advance through the live path (the poller). Pre-fix, Watch
// ran the initial scan to completion before starting the poller and
// fsnotify, so nothing else moved until the slow tree finished.
//
// The stuck file also carries a stale cursor, so the poller meets it
// first in its pass: it must skip the busy file rather than queue behind
// the scan's per-file lock.
func TestWatchAdvancesOtherToolDuringInitialScan(t *testing.T) {
	t.Parallel()
	j := &parseJournal{}
	slow := &orderAdapter{
		name: "aaa-slow", root: t.TempDir(), ext: ".slow", journal: j,
		blockName: "f000.slow", block: make(chan struct{}), started: make(chan struct{}),
	}
	other := &orderAdapter{name: "bbb-other", root: t.TempDir(), ext: ".other", journal: j}
	slowFiles := writeFiles(t, slow.root, slow.ext, 3)
	otherFiles := writeFiles(t, other.root, other.ext, 1)
	w, s := fairScanFixture(t, 20*time.Millisecond, slow, other)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	// Prior daemon run: both files are known and behind.
	for _, p := range []string{slowFiles[0], otherFiles[0]} {
		if err := s.SetCursor(ctx, p, 1); err != nil {
			t.Fatal(err)
		}
	}

	done := make(chan error, 1)
	go func() { done <- w.Watch(ctx) }()
	select {
	case <-slow.started:
	case <-time.After(5 * time.Second):
		t.Fatal("initial scan never reached the slow file")
	}

	// The scan is now parked inside aaa-slow/f000.slow. The other tool's
	// file must still catch up.
	want := int64(len("line\n"))
	deadline := time.Now().Add(5 * time.Second)
	for {
		off, err := s.GetCursor(ctx, otherFiles[0])
		if err == nil && off == want {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("other tool's cursor stuck at %d (%v) while the initial scan is in progress", off, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	select {
	case <-slow.block:
		t.Fatal("slow parse released early; the test would not prove anything")
	default:
	}

	close(slow.block)
	// Once released, the scan finishes the slow tree too.
	deadline = time.Now().Add(5 * time.Second)
	for _, p := range slowFiles {
		for {
			off, _ := s.GetCursor(ctx, p)
			if off == want {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("slow file %s never caught up (cursor %d)", filepath.Base(p), off)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Watch returned %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Watch did not exit")
	}
}

// TestPollCursorsSkipsBusyFile pins the poller half on its own: a file
// whose per-file lock is held is skipped, and the pass still reaches the
// files after it instead of blocking.
func TestPollCursorsSkipsBusyFile(t *testing.T) {
	t.Parallel()
	j := &parseJournal{}
	a := &orderAdapter{name: "aaa", root: t.TempDir(), ext: ".x", journal: j}
	files := writeFiles(t, a.root, a.ext, 2)
	w, s := fairScanFixture(t, 0, a)
	ctx := context.Background()
	for _, p := range files {
		if err := s.SetCursor(ctx, p, 1); err != nil {
			t.Fatal(err)
		}
	}
	mu, err := w.lockProcessFile(ctx, files[0], false)
	if err != nil {
		t.Fatal(err)
	}
	pollDone := make(chan error, 1)
	go func() { pollDone <- w.pollCursors(ctx) }()
	select {
	case err := <-pollDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		mu.Unlock()
		t.Fatal("pollCursors blocked on a busy file")
	}
	mu.Unlock()
	if off, _ := s.GetCursor(ctx, files[1]); off != int64(len("line\n")) {
		t.Errorf("file after the busy one not processed: cursor %d", off)
	}
	if off, _ := s.GetCursor(ctx, files[0]); off != 1 {
		t.Errorf("busy file was processed: cursor %d, want 1", off)
	}
}
