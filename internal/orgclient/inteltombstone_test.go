package orgclient

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/orgcontract"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// TestFetchIntelResultsAppliesVerifiedTombstones pins the node half of parity
// follow-up ruling 1: a purge on the org clears the node's cached result and
// org title for that session, but ONLY through a set that verifies against the
// key the node pinned, for its own org and subject. An unsigned, foreign-key or
// other-subject set clears nothing, a row a later Re-enrich produced (a higher
// job id) survives, and a node with no pinned key keeps its cache.
func TestFetchIntelResultsAppliesVerifiedTombstones(t *testing.T) {
	ctx := context.Background()
	is := newIntelServer(t)
	is.body = orgcontract.IntelResultsResponse{Results: []orgcontract.IntelResultRow{
		{SessionID: "s1", JobID: "10", Title: "purged title", GeneratedAt: "2026-10-04T10:00:00Z"},
		{SessionID: "s2", JobID: "11", Title: "kept title", GeneratedAt: "2026-10-04T10:00:01Z"},
	}}
	c, s := enrolledIntelClient(t, is.srv.URL)
	if out, err := c.FetchIntelResults(ctx); err != nil || out.Applied != 2 {
		t.Fatalf("seed fetch = %+v, %v", out, err)
	}

	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	_, otherPriv, _ := ed25519.GenerateKey(rand.Reader)
	body := orgcontract.IntelTombstoneBody{
		Subject: "scim-42", IssuedAt: "2026-10-04T11:00:00Z",
		Entries: []orgcontract.IntelTombstone{{SessionID: "s1", JobID: "10", PurgedAt: "2026-10-04T11:00:00Z"}},
	}
	sign := func(k ed25519.PrivateKey, org string, b orgcontract.IntelTombstoneBody) *orgcontract.IntelTombstoneDoc {
		d, err := orgcontract.SignIntelTombstones(k, org, b)
		if err != nil {
			t.Fatal(err)
		}
		return &d
	}
	titles := func() map[string]string {
		t.Helper()
		m, err := s.LoadOrgIntelTitles(ctx, []string{"s1", "s2"})
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	fetchWith := func(doc *orgcontract.IntelTombstoneDoc) IntelFetchOutcome {
		t.Helper()
		is.body = orgcontract.IntelResultsResponse{Tombstones: doc}
		out, err := c.FetchIntelResults(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}

	// No pinned key yet: nothing clears.
	if out := fetchWith(sign(priv, "org-1", body)); out.Cleared != 0 || titles()["s1"] != "purged title" {
		t.Fatalf("cleared without a pinned key: %+v %v", out, titles())
	}
	if err := s.UpsertOrgIntelRequirement(ctx, store.OrgIntelRequirementRow{
		OrgID: "org-1", Version: 1, Body: "{}", ServerPubkey: base64.StdEncoding.EncodeToString(pub), ReceivedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	other := body
	other.Subject = "scim-99"
	unsigned := sign(priv, "org-1", body)
	unsigned.Signature = ""
	for name, doc := range map[string]*orgcontract.IntelTombstoneDoc{
		"unsigned":        unsigned,
		"foreign key":     sign(otherPriv, "org-1", body),
		"another subject": sign(priv, "org-1", other),
		"another org":     sign(priv, "org-2", body),
	} {
		if out := fetchWith(doc); out.Cleared != 0 || titles()["s1"] != "purged title" {
			t.Fatalf("%s set cleared the cache: %+v %v", name, out, titles())
		}
	}

	// A later Re-enrich's result for s1 carries a higher job id and survives.
	if err := s.UpsertOrgIntelResult(ctx, store.OrgIntelResult{OrgID: "org-1", SessionID: "s1", JobID: "20", Title: "re-enriched"}); err != nil {
		t.Fatal(err)
	}
	out := fetchWith(sign(priv, "org-1", body))
	if out.Cleared != 1 {
		t.Fatalf("verified set cleared %d rows, want 1", out.Cleared)
	}
	rows, err := s.OrgIntelResultsForSession(ctx, "s1")
	if err != nil || len(rows) != 1 || rows[0].JobID != "20" {
		t.Fatalf("s1 rows after the purge = %+v, %v", rows, err)
	}
	if got := titles(); got["s1"] != "re-enriched" || got["s2"] != "kept title" {
		t.Fatalf("titles after the purge = %v", got)
	}
	// The same set again is not re-applied (and would clear nothing anyway).
	if again := fetchWith(sign(priv, "org-1", body)); again.Cleared != 0 {
		t.Fatalf("re-applied set cleared %d", again.Cleared)
	}
}
