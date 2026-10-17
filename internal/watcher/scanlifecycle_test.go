package watcher

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// slowWalkAdapter is an orderAdapter whose WALK (not its parse) stalls on one
// file: IsSessionFile for gateName blocks until gate is closed. It models a
// sparse tree on a slow filesystem (DrvFs), where the walker spends a long time
// between two matching files.
type slowWalkAdapter struct {
	orderAdapter
	gateName string
	gate     chan struct{}
	reached  chan struct{}
	once     sync.Once
}

func (s *slowWalkAdapter) IsSessionFile(p string) bool {
	if !strings.HasSuffix(p, s.ext) {
		return false
	}
	if filepath.Base(p) == s.gateName {
		s.once.Do(func() { close(s.reached) })
		<-s.gate
	}
	return true
}

// TestScanDoesNotBlockOnSlowWalkingLane pins P3-40: the full scan's
// round-robin must not park on a lane whose walker has not produced its next
// file yet. Pre-fix, scan() received from each lane in turn with a blocking
// receive, so a lane stuck walking held every other lane's ready files.
func TestScanDoesNotBlockOnSlowWalkingLane(t *testing.T) {
	t.Parallel()
	j := &parseJournal{}
	slow := &slowWalkAdapter{
		orderAdapter: orderAdapter{name: "aaa-slowwalk", root: t.TempDir(), ext: ".sw", journal: j},
		gateName:     "f000.sw", gate: make(chan struct{}), reached: make(chan struct{}),
	}
	other := &orderAdapter{name: "bbb-other", root: t.TempDir(), ext: ".ot", journal: j}
	writeFiles(t, slow.root, slow.ext, 2)
	otherFiles := writeFiles(t, other.root, other.ext, 3)
	w, s := fairScanFixture(t, 0, nil...)
	w.registry.Register(slow)
	w.registry.Register(other)

	ctx := context.Background()
	done := make(chan error, 1)
	go func() {
		_, err := w.Scan(ctx)
		done <- err
	}()
	select {
	case <-slow.reached:
	case <-time.After(5 * time.Second):
		close(slow.gate)
		t.Fatal("slow walker never reached its gated file")
	}
	want := int64(len("line\n"))
	deadline := time.Now().Add(5 * time.Second)
	for _, p := range otherFiles {
		for {
			off, _ := s.GetCursor(ctx, p)
			if off == want {
				break
			}
			if time.Now().After(deadline) {
				close(slow.gate)
				t.Fatalf("other lane's %s stuck at cursor %d while the slow lane is still walking", filepath.Base(p), off)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	close(slow.gate)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("scan did not finish after the slow walker was released")
	}
	if got := len(j.snapshot()); got != 5 {
		t.Fatalf("parsed %d files, want 5: %v", got, j.snapshot())
	}
}

// TestWatchReturnsPromptlyWhenFSNotifyCloses pins P3-35: when fsnotify closes
// its channels, Watch returns, and its join must not wait for a whole
// background scan running on the caller's (uncancelled) context. Pre-fix the
// initial scan ran on ctx, so Watch blocked until the scan finished.
func TestWatchReturnsPromptlyWhenFSNotifyCloses(t *testing.T) {
	t.Parallel()
	j := &parseJournal{}
	slow := &orderAdapter{
		name: "aaa-slow", root: t.TempDir(), ext: ".slow", journal: j,
		blockName: "f000.slow", block: make(chan struct{}), started: make(chan struct{}),
	}
	writeFiles(t, slow.root, slow.ext, 1)
	w, _ := fairScanFixture(t, 0, slow)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	t.Cleanup(func() {
		select {
		case <-slow.block:
		default:
			close(slow.block)
		}
	})

	done := make(chan error, 1)
	go func() { done <- w.Watch(ctx) }()
	select {
	case <-slow.started:
	case <-time.After(5 * time.Second):
		t.Fatal("initial scan never reached the slow file")
	}
	// Close the live fsnotify watcher: its Events / Errors channels close and
	// the event loop returns, with ctx still live.
	deadline := time.Now().Add(5 * time.Second)
	for {
		w.liveMu.Lock()
		fsw := w.fsw
		w.liveMu.Unlock()
		if fsw != nil {
			_ = fsw.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("fsnotify watcher never went live")
		}
		time.Sleep(5 * time.Millisecond)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Watch returned %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Watch did not return after fsnotify closed: it is waiting for the background scan on an uncancelled context")
	}
}
