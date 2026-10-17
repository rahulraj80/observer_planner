package orgclient

import (
	"context"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/accessgrant"
	"github.com/marmutapp/superbased-observer/internal/orgcontract"
)

func targetGrant(id int64, typ, target string, now time.Time, d time.Duration) orgcontract.AccessGrantWire {
	return orgcontract.AccessGrantWire{
		ID: id, Type: typ, Target: target, SubjectKind: orgcontract.AccessGrantSubjectMember,
		StartsAtNs: now.Add(-time.Minute).UnixNano(), ExpiresAtNs: now.Add(d).UnixNano(),
	}
}

// TestClaimTargetGrant (request-grants G8): a tool grant lifts exactly its
// target, counts one use per allowed launch through the same conditional
// UPDATE the guard uses, and stops past max_uses; a peek counts nothing; a
// cache that no longer verifies grants nothing.
func TestClaimTargetGrant(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	g := targetGrant(21, accessgrant.TypeToolAllow, "crush", now, time.Hour)
	g.MaxUses = 2
	f := newClaimFixture(t, g, targetGrant(22, accessgrant.TypeFeatureEnable, "terminal.enabled", now, time.Hour))
	q := accessgrant.TargetQuery{Type: accessgrant.TypeToolAllow, Target: "crush"}

	if cl := PeekTargetGrant(ctx, f.s, q, time.Now()); !cl.OK || cl.Hit.GrantID != 21 {
		t.Fatalf("peek: %+v", cl)
	}
	for i, wantLeft := range []int64{1, 0} {
		cl := ClaimTargetGrant(ctx, f.s, q, time.Now())
		if !cl.OK || cl.Hit.GrantID != 21 || cl.Hit.UsesLeft != wantLeft {
			t.Fatalf("claim %d: %+v", i, cl)
		}
	}
	if cl := ClaimTargetGrant(ctx, f.s, q, time.Now()); cl.OK {
		t.Fatalf("claimed past max_uses: %+v", cl)
	}
	var used int64
	if err := f.raw.QueryRow(`SELECT uses_used FROM org_access_grants WHERE grant_id = 21`).Scan(&used); err != nil || used != 2 {
		t.Fatalf("uses_used = %d (%v), want 2 (the peek counted nothing)", used, err)
	}
	if cl := ClaimTargetGrant(ctx, f.s, accessgrant.TargetQuery{Type: accessgrant.TypeToolAllow, Target: "codex"}, time.Now()); cl.OK {
		t.Fatalf("another tool claimed: %+v", cl)
	}

	// Feature exceptions: the honoured feature grants, with the instant each
	// stops being honoured, for the governance sidecar writer.
	ex, err := FeatureGrantExceptions(ctx, f.s, []string{"terminal.enabled", "guard.mode"}, time.Now())
	if err != nil || len(ex) != 1 || ex["terminal.enabled"].GrantID != 22 || ex["terminal.enabled"].UntilNs <= time.Now().UnixNano() {
		t.Fatalf("feature exceptions: %+v %v", ex, err)
	}

	// The signed body edited after the fetch: nothing is granted (fail
	// closed), and no feature exception survives.
	if _, err := f.raw.Exec(`UPDATE org_access_grant_state SET body = replace(body, 'crush', 'codex')`); err != nil {
		t.Fatal(err)
	}
	if cl := ClaimTargetGrant(ctx, f.s, accessgrant.TargetQuery{Type: accessgrant.TypeToolAllow, Target: "codex"}, time.Now()); cl.OK || !cl.Unverified {
		t.Fatalf("tampered cache: %+v", cl)
	}
	if ex, _ := FeatureGrantExceptions(ctx, f.s, []string{"terminal.enabled"}, time.Now()); len(ex) != 0 {
		t.Fatalf("tampered cache kept a feature exception: %+v", ex)
	}
}

// TestPageGrants (T-435): the honoured page-access grants are the pages the
// dashboard shows; a cache that no longer verifies shows none.
func TestPageGrants(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	f := newClaimFixture(t,
		targetGrant(41, accessgrant.TypePageAccess, "security/rules", now, time.Hour),
		targetGrant(42, accessgrant.TypeFeatureEnable, "terminals", now, time.Hour),
		targetGrant(43, accessgrant.TypeConfigEdit, "codex/model", now, time.Hour),
	)
	if got, err := ConfigEditGrants(ctx, f.s, time.Now()); err != nil || len(got) != 1 || got[0] != "codex/model" {
		t.Fatalf("edit grants = %v (%v)", got, err)
	}
	got, err := PageGrants(ctx, f.s, time.Now())
	if err != nil || len(got) != 1 || got[0] != "security/rules" {
		t.Fatalf("page grants = %v (%v)", got, err)
	}
	if _, err := f.raw.Exec(`UPDATE org_access_grant_state SET body = replace(body, 'security/rules', 'cost')`); err != nil {
		t.Fatal(err)
	}
	if got, _ := PageGrants(ctx, f.s, time.Now()); len(got) != 0 {
		t.Fatalf("tampered cache showed pages: %v", got)
	}
}
