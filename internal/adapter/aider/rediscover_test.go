package aider

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/platform/crossmount"
	"github.com/marmutapp/superbased-observer/internal/scrub"
)

// TestDiscoveryPicksUpATranscriptBornAfterStart pins T-706 (demo node 1,
// 2026-10-08): every transcript of the operator's dashboard aider runs was
// created AFTER the daemon started, and the old once-per-process snapshot
// never watched any of them. The discovery must return the startup set at
// once, and once it is older than the interval, start one background
// re-walk whose result the next WatchPaths call returns.
func TestDiscoveryPicksUpATranscriptBornAfterStart(t *testing.T) {
	home := t.TempDir()
	old := filepath.Join(home, "old-repo", historyFileName)
	fresh := filepath.Join(home, "work", "fresh-repo", historyFileName)
	for _, p := range []string{old} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	orig := allHomesFunc
	allHomesFunc = func() []crossmount.HomeRoot {
		return []crossmount.HomeRoot{{Path: home, OS: crossmount.OSLinux, Origin: "native"}}
	}
	defer func() { allHomesFunc = orig }()

	clock := time.Date(2026, 10, 8, 8, 15, 20, 0, time.UTC)
	var pending []func()
	d := &discovery{
		walk:     discoverRoots,
		now:      func() time.Time { return clock },
		spawn:    func(f func()) { pending = append(pending, f) },
		interval: rediscoverInterval,
	}
	a := &Adapter{scrubber: scrub.New(), disc: d}

	if got := a.WatchPaths(); !slices.Equal(got, []string{old}) {
		t.Fatalf("startup roots = %v, want [%s]", got, old)
	}
	// The operator's first aider run in a new repo, after the daemon start.
	if err := os.MkdirAll(filepath.Dir(fresh), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fresh, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if a.IsSessionFile(fresh) {
		t.Fatal("fresh transcript matched before any re-walk")
	}

	// Not yet stale: no re-walk is started.
	clock = clock.Add(rediscoverInterval - time.Second)
	_ = a.WatchPaths()
	if len(pending) != 0 {
		t.Fatalf("re-walk started before the interval: %d", len(pending))
	}

	// Stale: the call returns the current set at once and starts ONE walk,
	// however many calls arrive before it finishes.
	clock = clock.Add(2 * time.Second)
	if got := a.WatchPaths(); !slices.Equal(got, []string{old}) {
		t.Fatalf("stale call must not wait for the walk: %v", got)
	}
	_ = a.WatchPaths()
	if len(pending) != 1 {
		t.Fatalf("background walks started = %d, want 1", len(pending))
	}
	pending[0]()

	got := a.WatchPaths()
	if !slices.Contains(got, fresh) || !slices.Contains(got, old) {
		t.Fatalf("after re-walk roots = %v, want both %s and %s", got, old, fresh)
	}
	if !a.IsSessionFile(fresh) {
		t.Error("fresh transcript not matched after re-walk")
	}
	// The re-walk reset the clock: the next call starts nothing new.
	_ = a.WatchPaths()
	if len(pending) != 1 {
		t.Errorf("a fresh set started another walk: %d", len(pending))
	}
}

// TestInjectedRootsNeverRediscover keeps the injection seam static: tests
// and future root providers that pass roots get exactly those, no walk.
func TestInjectedRootsNeverRediscover(t *testing.T) {
	a := NewWithOptions(nil, []string{"/x/" + historyFileName})
	if a.disc != nil {
		t.Fatal("injected roots must not use discovery")
	}
	if got := a.WatchPaths(); !slices.Equal(got, []string{"/x/" + historyFileName}) {
		t.Errorf("WatchPaths = %v", got)
	}
}
