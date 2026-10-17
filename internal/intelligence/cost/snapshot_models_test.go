package cost

import (
	"sort"
	"strings"
	"testing"
)

// TestSnapshotModels pins the catalogue accessor the Arena model picker
// reads: sorted, de-duplicated, and free of hand-literal family-prefix keys
// (every id it returns is a row the embedded snapshot actually prices).
func TestSnapshotModels(t *testing.T) {
	got := SnapshotModels()
	if len(got) == 0 {
		t.Skip("embedded snapshot is empty (committed zero state)")
	}
	if !sort.StringsAreSorted(got) {
		t.Fatalf("SnapshotModels not sorted: %v", got)
	}
	seen := map[string]bool{}
	for _, m := range got {
		if seen[m] {
			t.Fatalf("duplicate model %q", m)
		}
		seen[m] = true
		if strings.TrimSpace(m) == "" {
			t.Fatal("empty model id")
		}
		if _, ok := snapshotRows[m]; !ok {
			t.Fatalf("model %q is not a snapshot row", m)
		}
	}
	// A caller mutating the result must not reach the package state.
	got[0] = "mutated"
	if SnapshotModels()[0] == "mutated" {
		t.Fatal("SnapshotModels returned shared state")
	}
}
