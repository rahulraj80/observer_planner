package store

import (
	"context"
	"testing"
	"time"
)

// TestResolveDeveloperCostDisplay pins the ONE resolution every developer
// surface uses: the org's verified choice applies only to a node currently
// enrolled in the org the document was signed for; everything else is
// dollars.
func TestResolveDeveloperCostDisplay(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	empty, _ := newTestStore(t)
	tokens, _ := newTestStore(t)
	if err := tokens.UpsertOrgDisplayPolicy(ctx, OrgDisplayPolicyRow{
		OrgID: "org-a", Version: 3, Body: "{}", BodyHash: "h", Signature: "s", ServerPubkey: "k",
		DeveloperCostDisplay: "tokens", ReceivedAt: time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatal(err)
	}
	odd, _ := newTestStore(t)
	if err := odd.UpsertOrgDisplayPolicy(ctx, OrgDisplayPolicyRow{
		OrgID: "org-a", Version: 1, Body: "{}", BodyHash: "h", Signature: "s", ServerPubkey: "k",
		DeveloperCostDisplay: "bogus", ReceivedAt: time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name     string
		s        *Store
		enrolled bool
		orgID    string
		want     DeveloperCostDisplay
	}{
		{"solo install, no row", empty, false, "", DeveloperCostDisplay{Mode: "usd", Source: "default"}},
		{"enrolled, no row yet", empty, true, "org-a", DeveloperCostDisplay{Mode: "usd", Source: "default"}},
		{"enrolled in the signing org", tokens, true, "org-a", DeveloperCostDisplay{Mode: "tokens", Source: "org", Version: 3}},
		{"row outlived its enrolment", tokens, false, "", DeveloperCostDisplay{Mode: "usd", Source: "default"}},
		{"enrolled in a different org", tokens, true, "org-b", DeveloperCostDisplay{Mode: "usd", Source: "default"}},
		{"unknown stored value resolves to token usage", odd, true, "org-a", DeveloperCostDisplay{Mode: "tokens", Source: "org", Version: 1}},
	}
	for _, tc := range cases {
		if got := tc.s.ResolveDeveloperCostDisplay(ctx, tc.enrolled, tc.orgID); got != tc.want {
			t.Errorf("%s: got %+v, want %+v", tc.name, got, tc.want)
		}
	}
	var nilStore *Store
	if got := nilStore.ResolveDeveloperCostDisplay(ctx, true, "org-a"); got.Mode != "usd" {
		t.Errorf("nil store: got %+v", got)
	}
}

// TestResolveDeveloperCostDisplayUnreadable (Sol A4-03): a cached policy
// that cannot be READ is reported as unverified, so a surface that must not
// fall open to dollars (the node dashboard) can tell it from "the org never
// chose". The Mode stays the dollars fallback the other callers rely on.
func TestResolveDeveloperCostDisplayUnreadable(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, database := newTestStore(t)
	if _, err := database.ExecContext(ctx, `DROP TABLE org_display_policy`); err != nil {
		t.Fatal(err)
	}
	got := s.ResolveDeveloperCostDisplay(ctx, true, "org-a")
	if !got.Unverified || got.Source == "org" {
		t.Fatalf("unreadable policy resolved %+v, want Unverified", got)
	}
	if ok := s.ResolveDeveloperCostDisplay(ctx, false, ""); ok.Unverified {
		t.Fatalf("a solo install is authoritative dollars, not unverified: %+v", ok)
	}
}
