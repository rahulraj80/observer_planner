package watcher

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// slowAdder models a watch root on a slow filesystem (the WSL DrvFs mount,
// where the recursive fsnotify registration took about two minutes on the
// 2026-10-05 node): every directory Add costs a fixed delay before it reaches
// the real watcher.
type slowAdder struct {
	next  watchAdder
	delay time.Duration
}

func (s slowAdder) Add(name string) error {
	time.Sleep(s.delay)
	return s.next.Add(name)
}

// TestWatchReturnsPromptlyWhileRootRegistrationIsWalking pins T-553: a stop
// that arrives while the recursive watch registration is still walking a slow
// root must end Watch within a bound, not after the whole tree is walked. On
// 2026-10-06 daemons released their ports on SIGTERM and then kept running for
// minutes with the DB open; a walk that cannot be cancelled is one way that
// happens, because Watch only reaches its ctx.Done case after registration.
func TestWatchReturnsPromptlyWhileRootRegistrationIsWalking(t *testing.T) {
	j := &parseJournal{}
	a := &orderAdapter{name: "aaa-tool", root: t.TempDir(), ext: ".tool", journal: j}
	// 200 subdirectories at 25ms each: a 5s walk when nothing cancels it.
	for i := 0; i < 200; i++ {
		if err := os.MkdirAll(filepath.Join(a.root, fmt.Sprintf("d%03d", i)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	w, _ := fairScanFixture(t, 20*time.Millisecond, a)

	entered := make(chan struct{}, 1)
	w.addRoot = func(fw watchAdder, root string) addRecursiveResult {
		select {
		case entered <- struct{}{}:
		default:
		}
		return addRecursive(slowAdder{next: fw, delay: 25 * time.Millisecond}, root)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	done := make(chan error, 1)
	go func() { done <- w.Watch(ctx) }()

	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("root registration never started")
	}
	time.Sleep(100 * time.Millisecond) // let the walk get under way
	stopped := time.Now()
	cancel()
	const bound = 1500 * time.Millisecond
	select {
	case <-done:
		if took := time.Since(stopped); took > bound {
			t.Fatalf("Watch returned %v after the stop; want within %v", took, bound)
		}
	case <-time.After(bound):
		// Drain so the goroutine does not outlive the test's temp dirs.
		<-done
		t.Fatalf("Watch still walking the watch root %v after the stop (took %v in all); want it to return within %v",
			bound, time.Since(stopped), bound)
	}
}
