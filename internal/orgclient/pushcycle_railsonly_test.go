package orgclient

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/orgcontract"
)

// TestPushCycleRailsOnlyRefreshesWithoutUploading (Sol A5-06): a ceiling that
// falls inside the org's X-SBO-Push-Min-Interval window refreshes the budget
// rail on time and uploads nothing; a full rail cycle does both.
func TestPushCycleRailsOnlyRefreshesWithoutUploading(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	doc, err := orgcontract.SignBudgetPolicy(priv, "org-1", "scim-42", budgetBody(7, 10_000_000))
	if err != nil {
		t.Fatalf("SignBudgetPolicy: %v", err)
	}
	cases := []struct {
		name       string
		kind       cycleKind
		wantPushes int
	}{
		{"rail-only: the rails, no upload", cycleKind{rails: true, railsOnly: true}, 0},
		{"full rail cycle: the rails and the upload", cycleKind{rails: true}, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sink := &postureSink{}
			ps := newPushAndBudgetServer(t, &doc, 0, sink)
			c, _ := wirePostureFixture(t, ps.srv.URL, pub, sink)
			if _, err := c.pushCycle(context.Background(), &pushCycleState{}, tc.kind); err != nil {
				t.Fatalf("pushCycle: %v", err)
			}
			sink.mu.Lock()
			outcomes := len(sink.outcomes)
			sink.mu.Unlock()
			if outcomes != 1 {
				t.Errorf("budget rail fetched %d times, want 1 - the ceiling must refresh the rails", outcomes)
			}
			ps.mu.Lock()
			pushes := ps.pushes
			ps.mu.Unlock()
			if pushes != tc.wantPushes {
				t.Errorf("uploads = %d, want %d", pushes, tc.wantPushes)
			}
		})
	}
}
