package dashboard

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/accessgrant"
	"github.com/marmutapp/superbased-observer/internal/guard"
	"github.com/marmutapp/superbased-observer/internal/orgcontract"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// TestApprovalsPOSTSoftenGrantAllowsConfirmOnALockedRule: a local approval on
// an org-locked rule is refused (Track B) unless the organization itself
// issued a SOFTEN grant for that rule, which is exactly the org saying "this
// developer may confirm these calls" - the approval is then the developer's
// confirmation on a host that cannot ask (request-grants G3).
func TestApprovalsPOSTSoftenGrantAllowsConfirmOnALockedRule(t *testing.T) {
	s, _ := newTestServer(t)
	ctx := context.Background()
	// The org bundle locks every rule (a managed node).
	s.orgOverride.set = orgOverrideSet{at: s.now(), posture: guard.OrgOverridePosture{OrgApplies: true, LocksEveryRule: true}}
	post := func() int {
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, httptest.NewRequest("POST", "/api/guard/approvals",
			strings.NewReader(`{"rule_id":"R-101","scope":"session","session_id":"sA","ttl_hours":1}`)))
		return rec.Code
	}
	if code := post(); code != 403 {
		t.Fatalf("locked rule without a grant: %d, want 403", code)
	}
	st := store.New(s.opts.DB)
	if err := st.WriteEnrolment(ctx, store.Enrolment{
		OrgID: "org-1", OrgName: "Acme", OrgServerURL: "http://org.invalid", UserID: "u-1",
		UserEmail: "dev@acme.example", EnrolledAt: time.Now().UTC().Format(time.RFC3339), BearerKeyID: "k",
	}); err != nil {
		t.Fatal(err)
	}
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	now := time.Now()
	g := orgcontract.AccessGrantWire{
		ID: 9, Type: accessgrant.TypeGuardExemption, Effect: orgcontract.AccessGrantEffectSoften, LockOverride: true,
		SubjectKind: orgcontract.AccessGrantSubjectMember, RuleID: "R-101",
		StartsAtNs: now.Add(-time.Minute).UnixNano(), ExpiresAtNs: now.Add(time.Hour).UnixNano(),
	}
	doc, err := orgcontract.SignAccessGrants(priv, "org-1", "u-1|", 1, orgcontract.AccessGrantBody{
		Grants: []orgcontract.AccessGrantWire{g}, ServerNowNs: now.UnixNano(), MaxOfflineS: 86400, IssuedAt: now.UTC().Format(time.RFC3339),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SaveAccessGrants(ctx, store.AccessGrantState{
		OrgID: "org-1", Subject: "u-1|", Version: 1, Body: doc.Body, BodyHash: doc.BodyHash, Signature: doc.Signature,
		ServerPubkey: base64.StdEncoding.EncodeToString(pub), ServerNowNs: now.UnixNano(), ReceivedAtNs: now.UnixNano(),
		LastVerifiedNs: now.UnixNano(), HighWaterNs: now.UnixNano(), MaxOfflineS: 86400, IssuedAt: now.UTC().Format(time.RFC3339),
	}, []orgcontract.AccessGrantWire{g}); err != nil {
		t.Fatal(err)
	}
	if code := post(); code != 200 {
		t.Fatalf("locked rule under a soften grant: %d, want 200", code)
	}
}
