package orgclient

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/orgcontract"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// The node half of the B1 rail: queued rows are sent signed over the body,
// and every server answer maps onto the row's state (table rows below).

type evidenceFake struct {
	srv     *httptest.Server
	mu      sync.Mutex
	pub     ed25519.PublicKey
	status  int
	body    any
	got     []orgcontract.IntelEvidenceSubmit
	badSigs int
}

func newEvidenceFake(t *testing.T) *evidenceFake {
	t.Helper()
	f := &evidenceFake{status: http.StatusOK}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.URL.Path != orgcontract.IntelEvidencePath {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		ts, _ := strconv.ParseInt(r.Header.Get(orgcontract.HeaderTimestamp), 10, 64)
		sig, _ := base64.RawURLEncoding.DecodeString(r.Header.Get(orgcontract.HeaderAgentSignature))
		if !ed25519.Verify(f.pub, orgcontract.IntelRailSigningMessage(ts, http.MethodPost, r.URL.Path, orgcontract.IntelBodyCanonical(raw), "org-1"), sig) {
			f.badSigs++
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var req orgcontract.IntelEvidenceSubmit
		_ = json.Unmarshal(raw, &req)
		f.got = append(f.got, req)
		writeTestJSON(w, f.status, f.body)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func evidenceClient(t *testing.T, f *evidenceFake) (*Client, *store.Store) {
	t.Helper()
	s := newAgentStore(t)
	if err := s.WriteEnrolment(context.Background(), store.Enrolment{
		OrgID: "org-1", OrgName: "Acme", OrgServerURL: f.srv.URL, UserID: "scim-42",
		EnrolledAt: time.Now().UTC().Format(time.RFC3339), BearerKeyID: "test",
	}); err != nil {
		t.Fatal(err)
	}
	// An individual-tenancy developer sends evidence only under an active
	// accepted consent (review C156-20 re-checks it before every send).
	acceptOrgIntel(t, s)
	pub, key, _ := ed25519.GenerateKey(nil)
	f.pub = pub
	return newTestClient(t, s, &memBearerStore{bearer: "bearer-xyz", key: key}), s
}

func queue(t *testing.T, s *store.Store, session string) store.OrgIntelSubmission {
	t.Helper()
	row, _, err := s.EnqueueOrgIntelSubmission(context.Background(), store.OrgIntelSubmission{
		OrgID: "org-1", SessionID: session, Purpose: "structural_activity_insights", EvidenceLevel: "titles",
		Trigger: store.OrgIntelTriggerDeveloper, EvidenceDigest: "e", UploadDigest: "u-" + session, Envelope: []byte(`{"e":1}`),
	}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	return row
}

func TestSubmitIntelEvidenceOutcomes(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name      string
		status    int
		body      any
		state     string
		reason    string
		retryable bool
	}{
		{"admitted", 200, orgcontract.IntelEvidenceReceipt{JobID: 4, Outcome: "admitted"}, store.OrgIntelOutboxSubmitted, "", false},
		{"duplicate", 200, orgcontract.IntelEvidenceReceipt{JobID: 4, Outcome: "duplicate"}, store.OrgIntelOutboxDuplicate, "", false},
		{"already enriched", 200, orgcontract.IntelEvidenceReceipt{Outcome: "already_enriched"}, store.OrgIntelOutboxAlreadyEnriched, "", false},
		{"terminal refusal", 403, orgcontract.IntelEvidenceRefusal{Error: orgcontract.IntelEvidenceErrLevel, Message: "m"}, store.OrgIntelOutboxRefused, orgcontract.IntelEvidenceErrLevel, false},
		{"retryable refusal", 403, orgcontract.IntelEvidenceRefusal{Error: orgcontract.IntelEvidenceErrNotAdmitted, Retryable: true}, store.OrgIntelOutboxFailedRetryable, orgcontract.IntelEvidenceErrNotAdmitted, true},
		{"older server", 404, map[string]string{"error": "not_supported"}, store.OrgIntelOutboxFailedRetryable, "not_supported", true},
		{"server error", 500, map[string]string{"error": "internal"}, store.OrgIntelOutboxFailedRetryable, "http_500", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newEvidenceFake(t)
			f.status, f.body = c.status, c.body
			cl, s := evidenceClient(t, f)
			row := queue(t, s, "s1")
			res, err := cl.SubmitIntelEvidence(ctx, row)
			if err != nil {
				t.Fatal(err)
			}
			got, _, _ := s.GetOrgIntelSubmission(ctx, "org-1", row.ID)
			if got.State != c.state || (c.reason != "" && got.Reason != c.reason) || f.badSigs != 0 {
				t.Fatalf("row = %+v (res %+v, bad sigs %d)", got, res, f.badSigs)
			}
			if c.retryable && !got.NextAt.After(time.Now().UTC()) {
				t.Fatalf("retry not scheduled: %+v", got)
			}
			if !c.retryable && len(got.Envelope) != 0 {
				t.Fatal("terminal row kept its bytes")
			}
			if len(f.got) != 1 || f.got[0].SessionID != "s1" || f.got[0].Envelope != `{"e":1}` {
				t.Fatalf("server saw %+v", f.got)
			}
		})
	}
}

// TestIntelSubmitCycleSendsQueuedAndGivesUp: the cycle sends due rows, and a
// row that keeps failing is refused after the attempt ceiling.
func TestIntelSubmitCycleSendsQueuedAndGivesUp(t *testing.T) {
	ctx := context.Background()
	f := newEvidenceFake(t)
	f.status, f.body = 503, map[string]string{"error": "x"}
	cl, s := evidenceClient(t, f)
	row := queue(t, s, "s1")
	queued := 0
	cl.SetIntelSubmitSource(func(context.Context) error { queued++; return nil })
	cl.runIntelSubmitCycle(ctx)
	if queued != 1 || len(f.got) != 1 {
		t.Fatalf("queued=%d sent=%d", queued, len(f.got))
	}
	for i := 0; i < intelSubmitMaxAttempts; i++ {
		r, _, _ := s.GetOrgIntelSubmission(ctx, "org-1", row.ID)
		if r.State == store.OrgIntelOutboxRefused {
			break
		}
		if _, err := cl.SubmitIntelEvidence(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	got, _, _ := s.GetOrgIntelSubmission(ctx, "org-1", row.ID)
	if got.State != store.OrgIntelOutboxRefused || got.Attempts != intelSubmitMaxAttempts {
		t.Fatalf("after the ceiling: %+v", got)
	}
}
