package main

import (
	"context"
	"reflect"
	"testing"
	"time"
)

// T-435: the daemon's governance handle carries the honoured page-access
// grants into Effective, re-reading them at most every
// pageGrantRefreshInterval (Effective runs on every dashboard request).
func TestNodeGovernanceCarriesPageGrants(t *testing.T) {
	h, _ := sidecarTestHandle(t, t.TempDir(), `{"schema":2,"pinned":{"guard.enabled":true}}`, startupSidecar{})
	ctx := context.Background()
	if got := h.Effective(ctx).GrantedPages; got != nil {
		t.Fatalf("no reader bound: %v", got)
	}
	clock := time.Now()
	h.now = func() time.Time { return clock }
	reads := 0
	answer := []string{"security/rules"}
	h.SetPageGrants(func(context.Context, time.Time) []string {
		reads++
		return answer
	})
	if got := h.Effective(ctx).GrantedPages; !reflect.DeepEqual(got, []string{"security/rules"}) {
		t.Fatalf("granted pages = %v", got)
	}
	answer = nil
	_ = h.Effective(ctx)
	if reads != 1 {
		t.Fatalf("reader called %d times within the refresh interval, want 1", reads)
	}
	clock = clock.Add(pageGrantRefreshInterval)
	if got := h.Effective(ctx).GrantedPages; got != nil || reads != 2 {
		t.Fatalf("after the interval: %v (reads %d), want the lapse seen", got, reads)
	}
}
