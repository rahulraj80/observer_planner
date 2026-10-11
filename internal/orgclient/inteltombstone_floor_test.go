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

// TestIntelTombstoneFloorRefusesAnOlderSetAfterRestart pins reviews C156-28
// and C156-29 on the node: the cursor of an applied set is kept DURABLY (a
// fresh client over the same store, as after a daemon restart, still has
// it), it is sent back so the server pages past it, and a verified but OLDER
// set replayed later is refused and clears nothing.
func TestIntelTombstoneFloorRefusesAnOlderSetAfterRestart(t *testing.T) {
	ctx := context.Background()
	is := newIntelServer(t)
	c, s := enrolledIntelClient(t, is.srv.URL)
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	if err := s.UpsertOrgIntelRequirement(ctx, store.OrgIntelRequirementRow{
		OrgID: "org-1", Version: 1, Body: "{}", ServerPubkey: base64.StdEncoding.EncodeToString(pub), ReceivedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	set := func(cursor string, entries ...orgcontract.IntelTombstone) *orgcontract.IntelTombstoneDoc {
		t.Helper()
		d, err := orgcontract.SignIntelTombstones(priv, "org-1", orgcontract.IntelTombstoneBody{
			Subject: "scim-42", IssuedAt: entries[len(entries)-1].PurgedAt, Entries: entries, Cursor: cursor,
		})
		if err != nil {
			t.Fatal(err)
		}
		return &d
	}
	older := set(orgcontract.IntelTombstoneCursor("2026-10-04T10:00:00Z", "s1"),
		orgcontract.IntelTombstone{SessionID: "s1", JobID: "10", PurgedAt: "2026-10-04T10:00:00Z"})
	newer := set(orgcontract.IntelTombstoneCursor("2026-10-04T11:00:00Z", "s2"),
		orgcontract.IntelTombstone{SessionID: "s2", JobID: "11", PurgedAt: "2026-10-04T11:00:00Z"})

	// The newer set applies and becomes the floor.
	is.body = orgcontract.IntelResultsResponse{Tombstones: newer}
	if _, err := c.FetchIntelResults(ctx); err != nil {
		t.Fatal(err)
	}
	if is.gotAfter != "" {
		t.Fatalf("first fetch sent cursor %q, want none", is.gotAfter)
	}
	floor, err := s.OrgIntelTombstoneFloor(ctx, "org-1", "scim-42")
	if err != nil || floor != orgcontract.IntelTombstoneCursor("2026-10-04T11:00:00Z", "s2") {
		t.Fatalf("floor = %q, %v", floor, err)
	}

	// A restart: a fresh client over the same store (no in-memory dedup).
	// The node sends its floor, and the older set, replayed, clears nothing.
	if err := s.UpsertOrgIntelResult(ctx, store.OrgIntelResult{OrgID: "org-1", SessionID: "s1", JobID: "10", Title: "kept"}); err != nil {
		t.Fatal(err)
	}
	restarted := newTestClient(t, s, &memBearerStore{bearer: "bearer-xyz", key: mustKey(t)})
	restarted.SetIntelRail(func() bool { return true })
	is.body = orgcontract.IntelResultsResponse{Tombstones: older}
	out, err := restarted.FetchIntelResults(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if is.gotAfter != floor {
		t.Fatalf("restarted node sent cursor %q, want its floor %q", is.gotAfter, floor)
	}
	if out.Cleared != 0 {
		t.Fatalf("a replayed older set cleared %d rows", out.Cleared)
	}
	if rows, err := s.OrgIntelResultsForSession(ctx, "s1"); err != nil || len(rows) != 1 {
		t.Fatalf("s1 after the replay = %+v, %v", rows, err)
	}
	if again, _ := s.OrgIntelTombstoneFloor(ctx, "org-1", "scim-42"); again != floor {
		t.Fatalf("a refused set moved the floor to %q", again)
	}
}

func mustKey(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	_, k, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return k
}
