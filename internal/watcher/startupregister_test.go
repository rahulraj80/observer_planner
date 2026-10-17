package watcher

import (
	"context"
	"testing"
	"time"
)

// TestWatchIngestsWhileRootRegistrationIsSlow pins T-413's startup rule: the
// recursive fsnotify registration of the watch roots (about two minutes over
// DrvFs on the 2026-10-05 node) no longer runs before the initial scan and
// the poller start. A behind file is caught up while registration is still
// walking, and registration still completes afterwards.
func TestWatchIngestsWhileRootRegistrationIsSlow(t *testing.T) {
	j := &parseJournal{}
	a := &orderAdapter{name: "aaa-tool", root: t.TempDir(), ext: ".tool", journal: j}
	files := writeFiles(t, a.root, a.ext, 1)
	w, s := fairScanFixture(t, 20*time.Millisecond, a)

	release := make(chan struct{})
	entered := make(chan struct{}, 1)
	w.addRoot = func(fw watchAdder, root string) addRecursiveResult {
		select {
		case entered <- struct{}{}:
		default:
		}
		<-release
		return addRecursive(fw, root)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if err := s.SetCursor(ctx, files[0], 1); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- w.Watch(ctx) }()

	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("root registration never started")
	}
	want := int64(len("line\n"))
	deadline := time.Now().Add(5 * time.Second)
	for {
		off, err := s.GetCursor(ctx, files[0])
		if err == nil && off == want {
			break
		}
		if time.Now().After(deadline) {
			close(release)
			t.Fatalf("cursor stuck at %d (%v) while root registration is in progress", off, err)
		}
		time.Sleep(10 * time.Millisecond)
	}

	close(release)
	deadline = time.Now().Add(5 * time.Second)
	for len(w.snapshotByRoot()) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("root registration never completed")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Watch did not return")
	}
}
