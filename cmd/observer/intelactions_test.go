//go:build !sbo_omit_cloud_intelligence

package main

import (
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/cloudcontract"
	"github.com/marmutapp/superbased-observer/internal/dashapi"
	"github.com/marmutapp/superbased-observer/internal/orgcontract"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// TestOrgIntelSessionActions walks one org session on an individual-tenancy
// machine through the phase-3 actions: nothing before consent; after Accept a
// preview whose bytes carry their digest, an enrich that needs that exact
// preview, a queued developer submission (the fixture's client has no bearer,
// so it stays queued and nothing reaches a network), a repeat that queues
// nothing new, and the ledger with the held bytes.
func TestOrgIntelSessionActions(t *testing.T) {
	f := newOrgIntelFixture(t, orgcontract.TenancyIndividual, false)
	seedOrgSession(t, f.st, "org-s1", time.Now().UTC().Add(-2*time.Hour), 4)
	r := httptest.NewRequest("GET", "/api/intel/session/org-s1", nil)

	v, err := f.d.Session(r, "org-s1")
	if err != nil {
		t.Fatal(err)
	}
	if v.Authority != "org" || v.Actions.Preview || v.Actions.Enrich || v.Actions.Reason != "individual_not_opted_in" || v.Actions.ReasonText == "" {
		t.Fatalf("before consent: %+v", v)
	}
	if _, err := f.d.Preview(r, "org-s1"); !errors.Is(err, dashapi.ErrOrgIntelConflict) {
		t.Fatalf("preview before consent err = %v, want a conflict", err)
	}

	st, _ := f.d.Status(r)
	if _, err := f.d.Accept(r, st.DisclosureDigest); err != nil {
		t.Fatal(err)
	}
	v, err = f.d.Session(r, "org-s1")
	if err != nil {
		t.Fatal(err)
	}
	if !v.Actions.Preview || !v.Actions.Enrich || !v.Actions.NeedsPreview || v.Actions.Reason != "" {
		t.Fatalf("after consent: %+v", v)
	}
	p, err := f.d.Preview(r, "org-s1")
	if err != nil {
		t.Fatal(err)
	}
	if p.Bytes == "" || cloudcontract.UploadDigest([]byte(p.Bytes)) != p.UploadDigest || p.NoticeOnly {
		t.Fatalf("preview = %+v", p)
	}
	if rows, _ := f.st.OrgIntelSubmissions(r.Context(), "org-1", "", 0); len(rows) != 0 {
		t.Fatalf("a preview queued something: %+v", rows)
	}

	if _, err := f.d.Enrich(r, "org-s1", ""); !errors.Is(err, dashapi.ErrOrgIntelPreviewRequired) {
		t.Fatalf("enrich without a preview err = %v", err)
	}
	if _, err := f.d.Enrich(r, "org-s1", "sha256:something-else"); !errors.Is(err, dashapi.ErrOrgIntelPreviewRequired) {
		t.Fatalf("enrich with a stale preview err = %v", err)
	}
	res, err := f.d.Enrich(r, "org-s1", p.UploadDigest)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Queued || res.Sent || res.Submission.Trigger != store.OrgIntelTriggerDeveloper ||
		res.Submission.UploadDigest != p.UploadDigest || res.Submission.State != store.OrgIntelOutboxFailedRetryable || res.Message == "" {
		t.Fatalf("enrich = %+v", res)
	}
	again, err := f.d.Enrich(r, "org-s1", p.UploadDigest)
	if err != nil {
		t.Fatal(err)
	}
	if again.Queued || again.Submission.ID != res.Submission.ID {
		t.Fatalf("repeat enrich = %+v", again)
	}

	l, err := f.d.Ledger(r, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Rows) != 1 || !l.Rows[0].BytesHeld || l.Rows[0].Bytes != "" {
		t.Fatalf("ledger = %+v", l)
	}
	one, err := f.d.Ledger(r, l.Rows[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(one.Rows) != 1 || one.Rows[0].Bytes != p.Bytes {
		t.Fatalf("ledger row = %+v", one)
	}
	if _, err := f.d.Ledger(r, 999); !errors.Is(err, dashapi.ErrOrgIntelNotFound) {
		t.Fatalf("unknown ledger row err = %v", err)
	}
	v, _ = f.d.Session(r, "org-s1")
	if v.Latest == nil || v.Latest.ID != res.Submission.ID || v.Submissions != 1 {
		t.Fatalf("session latest = %+v", v)
	}
}

// TestOrgIntelManagedRailOff: a managed machine whose organization has not
// raised the intel rail for it has nothing to preview or send.
func TestOrgIntelManagedRailOff(t *testing.T) {
	f := newOrgIntelFixture(t, orgcontract.TenancyManaged, false)
	seedOrgSession(t, f.st, "org-s1", time.Now().UTC().Add(-2*time.Hour), 4)
	r := httptest.NewRequest("GET", "/api/intel/session/org-s1", nil)
	v, err := f.d.Session(r, "org-s1")
	if err != nil {
		t.Fatal(err)
	}
	if v.Class != "managed" || v.Actions.Preview || v.Actions.Reason != "managed_rail_off" {
		t.Fatalf("managed, rail off: %+v", v)
	}
	if _, err := f.d.Enrich(r, "org-s1", ""); !errors.Is(err, dashapi.ErrOrgIntelConflict) {
		t.Fatalf("enrich err = %v, want a conflict", err)
	}
}
