package planlimits

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestWebVocabCoversTheRegistry pins that the dashboard's vocabulary
// (web/src/lib/plans.ts) names every code this package can send: a provider,
// source, status, source kind or unit the page would otherwise render raw.
// The server sends codes; the page owns the words.
func TestWebVocabCoversTheRegistry(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "web", "src", "lib", "plans.ts"))
	if err != nil {
		t.Skipf("web source not present: %v", err)
	}
	src := string(raw)
	has := func(id string) bool {
		return strings.Contains(src, `"`+id+`"`) || strings.Contains(src, "\n  "+id+":")
	}
	var want []string
	for _, p := range Providers {
		want = append(want, p.ID)
	}
	for _, s := range Sources {
		want = append(want, s.ID)
	}
	for _, k := range AllKinds() {
		want = append(want, string(k))
	}
	want = append(want, StatusOK, StatusThrottled, StatusPlanOnly, StatusUnmetered, StatusBilledElse,
		StatusAuthNeeded, StatusError, StatusEndpointGone, StatusStale, StatusNotOptedIn, StatusSkipped,
		HealthWaiting,
		ScopeBound, ScopeNoSession, ScopeNoObservation, ScopeConflict, ScopeExplicit,
		string(UnitPercent), string(UnitCredits), string(UnitUSDMicros), string(UnitCNYMicros),
		string(UnitRequests), string(UnitTokens), string(UnitAICredits))
	for _, id := range want {
		if !has(id) {
			t.Errorf("web/src/lib/plans.ts has no words for %q", id)
		}
	}
}
