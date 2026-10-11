package cloudcontract

import "testing"

// TestEvidenceLevelOrdering pins the closed, ordered vocabulary: each level
// covers itself and every level below it, never above; the zero value and an
// unknown value cover nothing and are never covered.
func TestEvidenceLevelOrdering(t *testing.T) {
	cases := []struct {
		have, want EvidenceLevel
		covers     bool
	}{
		{EvidenceLevelStructural, EvidenceLevelStructural, true},
		{EvidenceLevelStructural, EvidenceLevelTitles, false},
		{EvidenceLevelTitles, EvidenceLevelStructural, true},
		{EvidenceLevelTitles, EvidenceLevelExcerpts, false},
		{EvidenceLevelExcerpts, EvidenceLevelTitles, true},
		{EvidenceLevelExcerpts, EvidenceLevelExcerpts, true},
		{"", EvidenceLevelStructural, false},
		{"everything", EvidenceLevelStructural, false},
		{EvidenceLevelExcerpts, "", false},
	}
	for _, c := range cases {
		if got := c.have.Covers(c.want); got != c.covers {
			t.Errorf("%q.Covers(%q) = %t, want %t", c.have, c.want, got, c.covers)
		}
	}
}

// TestEvidenceLevelPurposeAndClasses pins the level -> purpose / field-class
// mapping the node builder and the org gate both read.
func TestEvidenceLevelPurposeAndClasses(t *testing.T) {
	if EvidenceLevelStructural.Purpose() != PurposeStructuralInsights || len(EvidenceLevelStructural.FieldClasses()) != 0 {
		t.Fatal("structural must be the structural purpose with no field class")
	}
	if EvidenceLevelTitles.Purpose() != PurposeStructuralInsights {
		t.Fatal("titles is built under the structural purpose")
	}
	if fc := EvidenceLevelTitles.FieldClasses(); len(fc) != 1 || fc[0] != FieldClassFirstUserPrompt {
		t.Fatalf("titles field classes = %v", fc)
	}
	if EvidenceLevelExcerpts.Purpose() != PurposeContextEnrichment {
		t.Fatal("excerpts is built under the bounded context purpose")
	}
	if EvidenceLevel("bogus").Purpose() != PurposeStructuralInsights {
		t.Fatal("an unknown level must fall back to the narrowest purpose")
	}
	if got := MinEvidenceLevel(EvidenceLevelExcerpts, EvidenceLevelTitles); got != EvidenceLevelTitles {
		t.Fatalf("min = %q", got)
	}
	if got := MinEvidenceLevel("bogus", EvidenceLevelExcerpts); got != EvidenceLevelStructural {
		t.Fatalf("min with an unknown = %q, want structural", got)
	}
	if got := NormalizeEvidenceLevel("nope", DefaultOrgEvidenceLevel); got != EvidenceLevelTitles {
		t.Fatalf("normalize = %q", got)
	}
}
