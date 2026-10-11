package orgclient

import (
	"testing"
	"time"
)

// TestNotFoundBackoffTable walks the 404 backoff rule row by row: the first
// 404 waits one base interval (today's cadence), each further 404 doubles the
// wait up to the 1 h cap, any non-404 answer resets, and Reset re-arms an
// immediate fetch.
func TestNotFoundBackoffTable(t *testing.T) {
	base := 2 * time.Minute
	t0 := time.Date(2026, 10, 2, 5, 0, 0, 0, time.UTC)
	steps := []struct {
		name     string
		notFound bool
		wantWait time.Duration // 0 = no skip afterwards
	}{
		{"first 404 waits one interval", true, 2 * time.Minute},
		{"second doubles", true, 4 * time.Minute},
		{"third doubles", true, 8 * time.Minute},
		{"fourth", true, 16 * time.Minute},
		{"fifth", true, 32 * time.Minute},
		{"sixth hits the cap", true, time.Hour},
		{"seventh stays capped", true, time.Hour},
		{"a 200 resets", false, 0},
		{"a fresh 404 starts over", true, 2 * time.Minute},
	}
	var b notFoundBackoff
	now := t0
	for _, s := range steps {
		b.Observe(s.notFound, now, base)
		if s.wantWait == 0 {
			if b.Skip(now) {
				t.Fatalf("%s: still skipping after a reset", s.name)
			}
			continue
		}
		if !b.Skip(now.Add(s.wantWait - time.Second)) {
			t.Fatalf("%s: not skipping just before %v", s.name, s.wantWait)
		}
		if b.Skip(now.Add(s.wantWait)) {
			t.Fatalf("%s: still skipping at %v", s.name, s.wantWait)
		}
		now = now.Add(s.wantWait)
	}
	b.Observe(true, now, base)
	if !b.Skip(now) {
		t.Fatal("expected a skip after a 404")
	}
	b.Reset()
	if b.Skip(now) {
		t.Fatal("Reset must re-arm an immediate fetch")
	}
	var nilB *notFoundBackoff
	if nilB.Skip(now) {
		t.Fatal("a nil backoff never skips")
	}
}
