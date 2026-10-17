package guardposture

import (
	"path/filepath"
	"testing"
	"time"
)

// TestAllowOnce pins the rate limit behind the unclassified tool-gate
// record (review M2-12): once per window per key, keys independent, an
// empty directory never allows, and a marker from the far future expires.
func TestAllowOnce(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "limits")
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	const w = time.Hour
	if !AllowOnce(dir, "a", w, now) {
		t.Fatal("first record for a key was refused")
	}
	if AllowOnce(dir, "a", w, now.Add(10*time.Minute)) {
		t.Fatal("second record inside the window was allowed")
	}
	if !AllowOnce(dir, "b", w, now.Add(10*time.Minute)) {
		t.Fatal("a different key shared the first key's window")
	}
	if !AllowOnce(dir, "a", w, now.Add(w+time.Second)) {
		t.Fatal("a record after the window was refused")
	}
	if !AllowOnce(dir, "a", w, now.Add(-3*w)) {
		t.Fatal("a marker dated far in the future did not expire")
	}
	if AllowOnce("", "a", w, now) {
		t.Fatal("an empty directory allowed a record")
	}
	if got := RateLimitDir("/x/guard-hookfail.jsonl"); got != "/x/guard-hookfail.jsonl.limits" {
		t.Errorf("RateLimitDir = %q", got)
	}
	if RateLimitDir("") != "" {
		t.Error("RateLimitDir of an empty spool path is not empty")
	}
	for _, k := range FailureKinds() {
		if k == KindUnclassified {
			t.Error("an unclassified call is not a hook failure")
		}
	}
}
