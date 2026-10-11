package orgclient

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
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

// The node half of the Cloud Intelligence parity consent rails (design 3.7):
// the signed requirement fetch (display-policy state rules) and the signed
// consent report (body bound into the per-request proof).

type consentFake struct {
	srv       *httptest.Server
	mu        sync.Mutex
	priv      ed25519.PrivateKey
	orgID     string
	subject   string
	version   int64
	body      orgcontract.IntelRequirementBody
	reqStatus int
	conStatus int
	nodePub   ed25519.PublicKey
	reports   []orgcontract.IntelConsentReport
	badProof  int
	// corruptSig serves the document with its signature replaced (T-390).
	corruptSig bool
}

func newConsentFake(t *testing.T) *consentFake {
	t.Helper()
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	f := &consentFake{
		priv: priv, orgID: "org-1", subject: "scim-42", version: 1, reqStatus: http.StatusOK, conStatus: http.StatusOK,
		body: orgcontract.IntelRequirementBody{
			OrgName: "Acme", Enabled: true, EvidenceLevel: "titles",
			Purposes: []string{"structural_activity_insights"}, RequireIndividualConsent: true, RequirementVersion: 1,
		},
	}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		switch r.URL.Path {
		case orgcontract.IntelRequirementPath:
			if f.reqStatus != http.StatusOK {
				w.WriteHeader(f.reqStatus)
				return
			}
			doc, err := orgcontract.SignIntelRequirement(f.priv, f.orgID, f.subject, f.version, f.body)
			if err != nil {
				t.Errorf("sign: %v", err)
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			if f.corruptSig {
				doc.Signature = base64.StdEncoding.EncodeToString(make([]byte, ed25519.SignatureSize))
			}
			writeTestJSON(w, http.StatusOK, doc)
		case orgcontract.IntelConsentPath:
			raw, _ := io.ReadAll(r.Body)
			ts, _ := strconv.ParseInt(r.Header.Get(orgcontract.HeaderTimestamp), 10, 64)
			sig, _ := base64.RawURLEncoding.DecodeString(r.Header.Get(orgcontract.HeaderAgentSignature))
			msg := orgcontract.IntelRailSigningMessage(ts, http.MethodPost, r.URL.Path, orgcontract.IntelBodyCanonical(raw), "org-1")
			if f.nodePub != nil && !ed25519.Verify(f.nodePub, msg, sig) {
				f.badProof++
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			if f.conStatus != http.StatusOK {
				w.WriteHeader(f.conStatus)
				return
			}
			var rep orgcontract.IntelConsentReport
			_ = json.Unmarshal(raw, &rep)
			f.reports = append(f.reports, rep)
			writeTestJSON(w, http.StatusOK, orgcontract.IntelConsentReply{Recorded: true, OptIn: rep.OptIn})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func consentClient(t *testing.T, f *consentFake) (*Client, *store.Store) {
	t.Helper()
	s := newAgentStore(t)
	if err := s.WriteEnrolment(context.Background(), store.Enrolment{
		OrgID: "org-1", OrgName: "Acme", OrgServerURL: f.srv.URL, UserID: "scim-42",
		EnrolledAt: time.Now().UTC().Format(time.RFC3339), BearerKeyID: "test",
	}); err != nil {
		t.Fatalf("WriteEnrolment: %v", err)
	}
	pub, key, _ := ed25519.GenerateKey(rand.Reader)
	f.nodePub = pub
	return newTestClient(t, s, &memBearerStore{bearer: "bearer-xyz", key: key}), s
}

// TestFetchIntelRequirementStateRules: a verified document is cached, an equal
// version is a no-op, a newer one replaces it, a 404 and a document for
// another org leave the cache untouched, and the fetch never writes a consent.
func TestFetchIntelRequirementStateRules(t *testing.T) {
	ctx := context.Background()
	f := newConsentFake(t)
	c, s := consentClient(t, f)

	if got, err := c.RefreshIntelRequirement(ctx); err != nil || !got {
		t.Fatalf("first fetch = %v, %v", got, err)
	}
	row, ok, _ := c.IntelRequirement(ctx)
	if !ok || row.Version != 1 {
		t.Fatalf("cached = %+v ok=%v", row, ok)
	}
	if got, err := c.RefreshIntelRequirement(ctx); err != nil || got {
		t.Fatalf("same version = %v, %v", got, err)
	}
	f.mu.Lock()
	f.version, f.body.RequirementVersion, f.body.EvidenceLevel = 2, 2, "excerpts"
	f.mu.Unlock()
	if got, err := c.RefreshIntelRequirement(ctx); err != nil || !got {
		t.Fatalf("newer version = %v, %v", got, err)
	}
	f.mu.Lock()
	f.reqStatus = http.StatusNotFound
	f.mu.Unlock()
	if got, err := c.RefreshIntelRequirement(ctx); err != nil || got {
		t.Fatalf("404 = %v, %v", got, err)
	}
	if row, ok, _ := c.IntelRequirement(ctx); !ok || row.Version != 2 {
		t.Fatalf("404 cleared the cache: %+v", row)
	}
	f.mu.Lock()
	f.reqStatus, f.orgID, f.version = http.StatusOK, "org-other", 9
	f.mu.Unlock()
	if _, err := c.RefreshIntelRequirement(ctx); err == nil {
		t.Fatal("a document for another org was accepted")
	}
	if row, _, _ := c.IntelRequirement(ctx); row.Version != 2 {
		t.Fatalf("foreign doc replaced the cache: %+v", row)
	}
	// INV-13: the fetch path never writes a consent.
	if hist, _ := s.OrgIntelConsentHistory(ctx, "org-1", 0); len(hist) != 0 {
		t.Fatalf("fetch wrote consent rows: %+v", hist)
	}
}

// TestIntelConsentCycleReportsOnChangeOnly: the cycle reports the posture the
// source resolves, signed over the body, and re-sends only when it changes;
// a 404 (older server) is not an error and is retried on the next cycle.
func TestIntelConsentCycleReportsOnChangeOnly(t *testing.T) {
	ctx := context.Background()
	f := newConsentFake(t)
	c, _ := consentClient(t, f)
	posture := orgcontract.IntelConsentReport{
		OptIn: true, AcceptedRequirementVersion: 1, AcceptedLevel: "titles",
		AcceptedPurposes: []string{"structural_activity_insights"}, DisclosureDigest: "abc",
	}
	var mu sync.Mutex
	c.SetIntelConsentSource(func(context.Context) (orgcontract.IntelConsentReport, bool) {
		mu.Lock()
		defer mu.Unlock()
		return posture, true
	})

	c.runIntelConsentCycle(ctx)
	c.runIntelConsentCycle(ctx)
	f.mu.Lock()
	n, bad := len(f.reports), f.badProof
	f.mu.Unlock()
	if bad != 0 || n != 1 || !f.reports[0].OptIn {
		t.Fatalf("reports=%d bad=%d %+v", n, bad, f.reports)
	}
	mu.Lock()
	posture = orgcontract.IntelConsentReport{OptIn: false, AcceptedPurposes: []string{}}
	mu.Unlock()
	f.mu.Lock()
	f.conStatus = http.StatusNotFound
	f.mu.Unlock()
	c.runIntelConsentCycle(ctx)
	f.mu.Lock()
	f.conStatus = http.StatusOK
	f.mu.Unlock()
	c.runIntelConsentCycle(ctx)
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.reports) != 2 || f.reports[1].OptIn {
		t.Fatalf("withdraw not re-sent after the 404: %+v", f.reports)
	}
}

// TestReportIntelConsentNotSupported: a 404 maps to the typed sentinel.
func TestReportIntelConsentNotSupported(t *testing.T) {
	f := newConsentFake(t)
	f.conStatus = http.StatusNotFound
	c, _ := consentClient(t, f)
	if _, err := c.ReportIntelConsent(context.Background(), orgcontract.IntelConsentReport{}); !errors.Is(err, ErrIntelConsentNotSupported) {
		t.Fatalf("err = %v", err)
	}
}

// TestFetchIntelRequirementRefusesAnotherMembersDocument pins C156-16 on the
// node: a document signed by the org for a DIFFERENT member (a captured one,
// replayed) is refused and the cache is untouched.
func TestFetchIntelRequirementRefusesAnotherMembersDocument(t *testing.T) {
	ctx := context.Background()
	f := newConsentFake(t)
	f.subject = "someone-else"
	c, _ := consentClient(t, f)
	if got, err := c.RefreshIntelRequirement(ctx); err == nil || got {
		t.Fatalf("another member's document = %v, %v; want a refusal", got, err)
	}
	if _, ok, _ := c.IntelRequirement(ctx); ok {
		t.Fatal("another member's document was cached")
	}
	f.mu.Lock()
	f.subject = "scim-42"
	f.mu.Unlock()
	if got, err := c.RefreshIntelRequirement(ctx); err != nil || !got {
		t.Fatalf("own document = %v, %v", got, err)
	}
}

// TestFetchIntelRequirementVerifiesBeforeVersionCompare pins T-390: the
// cached version is compared only after the offered document verified. A
// document at or below the cached version that does not verify (a corrupted
// signature, or a captured document signed for another member) is REFUSED,
// never read as "unchanged", and the cache keeps its verified row.
func TestFetchIntelRequirementVerifiesBeforeVersionCompare(t *testing.T) {
	ctx := context.Background()
	f := newConsentFake(t)
	f.version, f.body.RequirementVersion = 2, 2
	c, _ := consentClient(t, f)
	if got, err := c.RefreshIntelRequirement(ctx); err != nil || !got {
		t.Fatalf("first fetch = %v, %v", got, err)
	}
	cases := []struct {
		name    string
		mutate  func(f *consentFake)
		version int64
	}{
		{"corrupt signature, same version", func(f *consentFake) { f.corruptSig = true }, 2},
		{"corrupt signature, older version", func(f *consentFake) { f.corruptSig = true }, 1},
		{"another member, same version", func(f *consentFake) { f.subject = "someone-else" }, 2},
		{"another member, older version", func(f *consentFake) { f.subject = "someone-else" }, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f.mu.Lock()
			f.corruptSig, f.subject = false, "scim-42"
			tc.mutate(f)
			f.version, f.body.RequirementVersion = tc.version, tc.version
			f.mu.Unlock()
			if got, err := c.RefreshIntelRequirement(ctx); err == nil || got {
				t.Fatalf("unverifiable document at v%d = %v, %v; want a refusal", tc.version, got, err)
			}
			if row, ok, _ := c.IntelRequirement(ctx); !ok || row.Version != 2 {
				t.Fatalf("cache changed: %+v ok=%v", row, ok)
			}
		})
	}
	// The verified document at the cached version is still the quiet no-op.
	f.mu.Lock()
	f.corruptSig, f.subject, f.version, f.body.RequirementVersion = false, "scim-42", 2, 2
	f.mu.Unlock()
	if got, err := c.RefreshIntelRequirement(ctx); err != nil || got {
		t.Fatalf("verified same version = %v, %v", got, err)
	}
}
