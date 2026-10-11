package termsession

import (
	"sync/atomic"
	"testing"
	"time"
)

// TestExitAttributionForHandle pins the T-584 accessor: a running child
// reports nothing; once it exits, the handle yields its launcher verb and
// exit time, which a policy-stop lookup uses to correlate a wrapped launch
// whose vendor process was the launcher's child.
func TestExitAttributionForHandle(t *testing.T) {
	sp := &pidSpawner{pids: []int{4100}}
	launched := time.Date(2026, 10, 6, 21, 0, 0, 0, time.UTC)
	var clock atomic.Int64
	clock.Store(launched.UnixNano())
	m := newTestManager(t, sp, func() time.Time { return time.Unix(0, clock.Load()).UTC() })

	handle, err := m.Create(validSpec())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if sub, at, ok := m.ExitAttributionForHandle(handle); ok {
		t.Fatalf("ExitAttributionForHandle(running) = (%q,%v,true), want ok=false", sub, at)
	}

	clock.Store(launched.Add(5 * time.Minute).UnixNano())
	sp.lastPTY().exit(255)
	waitFor(t, "session marked exited", func() bool {
		_, _, ok := m.ExitAttributionForHandle(handle)
		return ok
	})
	sub, at, ok := m.ExitAttributionForHandle(handle)
	if !ok || sub != "claude" || !at.Equal(launched.Add(5*time.Minute)) {
		t.Fatalf("ExitAttributionForHandle(exited) = (%q,%v,%v), want (claude,%v,true)", sub, at, ok, launched.Add(5*time.Minute))
	}

	if _, _, ok := m.ExitAttributionForHandle("no-such-handle"); ok {
		t.Error("ExitAttributionForHandle(unknown) reported ok=true")
	}
}
