package orgclient

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/orgcontract"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// bindingResourceServer serves one admission.input envelope per poll; serve
// decides what (bound or not, to whom) and records the policy_caps it was
// asked with. The ETag is the server's own (orgcontract.PolicyResourceETag).
type bindingResourceServer struct {
	srv   *httptest.Server
	mu    sync.Mutex
	serve func() orgcontract.SignedPolicyResource
	caps  []string
}

func newBindingResourceServer(t *testing.T) *bindingResourceServer {
	t.Helper()
	bs := &bindingResourceServer{}
	bs.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bs.mu.Lock()
		bs.caps = append(bs.caps, r.URL.Query()["policy_caps"]...)
		res := bs.serve()
		bs.mu.Unlock()
		etag, err := orgcontract.PolicyResourceETag(res)
		if err != nil {
			t.Errorf("etag: %v", err)
		}
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", etag)
		writeTestJSON(w, http.StatusOK, res)
	}))
	t.Cleanup(bs.srv.Close)
	return bs
}

// boundTestResource signs a v2 envelope bound to b.
func boundTestResource(t *testing.T, priv ed25519.PrivateKey, pub ed25519.PublicKey, version int64, body string, b orgcontract.PolicyBinding) orgcontract.SignedPolicyResource {
	t.Helper()
	r := signedTestResource(t, priv, pub, policyfamAdmissionInput, version, body, nil)
	r.Binding = &b
	sig, err := orgcontract.SignPolicyResource(priv, r)
	if err != nil {
		t.Fatal(err)
	}
	r.Signature = sig
	return r
}

// TestPolicyResourceBindingGate pins the node half of C156-14: the node asks
// for a binding; a bound envelope for ITS org and member is applied, one bound
// to another member is refused although the org signed it, an unbound
// envelope is accepted from a server that does not bind yet, and once the node
// holds a bound envelope an unbound one is refused as a downgrade.
func TestPolicyResourceBindingGate(t *testing.T) {
	ctx := context.Background()
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	bs := newBindingResourceServer(t)
	c, _, cacheDir := enrolledPRClient(t, bs.srv.URL)
	opts := PolicyResourceOptions{CacheDir: cacheDir, AcceptFamilies: []string{policyfamAdmissionInput}}
	mine := orgcontract.PolicyBinding{OrgID: "org-1", Subject: "scim-42"}

	// 1. An older server: unbound v1 is applied (compat), and the node asked
	// for the binding.
	v1 := signedTestResource(t, priv, pub, policyfamAdmissionInput, 1, observeAdmissionBody, nil)
	bs.serve = func() orgcontract.SignedPolicyResource { return v1 }
	if res, err := c.FetchAndAcceptPolicyResource(ctx, policyfamAdmissionInput, opts); err != nil || res.Status != PRApplied {
		t.Fatalf("unbound from a non-binding server = %+v, %v; want applied", res, err)
	}
	if !strings.Contains(strings.Join(bs.caps, ","), orgcontract.PolicyBindingCapability) {
		t.Fatalf("the fetch did not ask for a binding: policy_caps=%v", bs.caps)
	}

	// 2. Another member's bound envelope at a newer version: refused.
	other := boundTestResource(t, priv, pub, 2, enforceAdmissionBody, orgcontract.PolicyBinding{OrgID: "org-1", Subject: "someone-else"})
	bs.serve = func() orgcontract.SignedPolicyResource { return other }
	if res, err := c.FetchAndAcceptPolicyResource(ctx, policyfamAdmissionInput, opts); err != nil || res.Status != PRRejected || res.RejectCode != PRRejectClosedEnvelope {
		t.Fatalf("another member's bound envelope = %+v, %v; want rejected closed_envelope_violation", res, err)
	}
	foreignOrg := boundTestResource(t, priv, pub, 2, enforceAdmissionBody, orgcontract.PolicyBinding{OrgID: "org-2", Subject: "scim-42"})
	bs.serve = func() orgcontract.SignedPolicyResource { return foreignOrg }
	if res, _ := c.FetchAndAcceptPolicyResource(ctx, policyfamAdmissionInput, opts); res.Status != PRRejected {
		t.Fatalf("another org's bound envelope = %+v; want rejected", res)
	}

	// 3. The same v1 content, now bound to this node: re-cached so the
	// binding sticks, and the next poll is a 304 (no re-download churn).
	v1b := boundTestResource(t, priv, pub, 1, observeAdmissionBody, mine)
	bs.serve = func() orgcontract.SignedPolicyResource { return v1b }
	if res, err := c.FetchAndAcceptPolicyResource(ctx, policyfamAdmissionInput, opts); err != nil || res.Status != PRUnchanged {
		t.Fatalf("same content bound = %+v, %v; want unchanged", res, err)
	}
	if res, err := c.FetchAndAcceptPolicyResource(ctx, policyfamAdmissionInput, opts); err != nil || res.Status != PRUnchanged {
		t.Fatalf("next poll = %+v, %v; want unchanged (304)", res, err)
	}

	// 4. An unbound envelope after the bound one: a downgrade, refused even at
	// a newer version.
	v2 := signedTestResource(t, priv, pub, policyfamAdmissionInput, 2, enforceAdmissionBody, nil)
	bs.serve = func() orgcontract.SignedPolicyResource { return v2 }
	if res, err := c.FetchAndAcceptPolicyResource(ctx, policyfamAdmissionInput, opts); err != nil || res.Status != PRRejected || res.RejectCode != PRRejectSigInvalid {
		t.Fatalf("unbound after bound = %+v, %v; want rejected sig_invalid", res, err)
	}

	// 5. A bound newer version for this node applies.
	v2b := boundTestResource(t, priv, pub, 2, enforceAdmissionBody, mine)
	bs.serve = func() orgcontract.SignedPolicyResource { return v2b }
	if res, err := c.FetchAndAcceptPolicyResource(ctx, policyfamAdmissionInput, PolicyResourceOptions{
		CacheDir: cacheDir, AcceptFamilies: []string{policyfamAdmissionInput}, PreauthorizeEnforce: []string{policyfamAdmissionInput},
	}); err != nil || res.Status != PRApplied || res.Version != 2 {
		t.Fatalf("bound v2 for this node = %+v, %v; want applied v2", res, err)
	}
}

// TestPolicyResourceBindingNodeGovernanceTarget pins the target half of
// C156-14: a node.governance envelope bound to a LOWER target than this node
// advertises (what a bearer holder gets by sending older nodegov tokens) is
// refused; this node's own expected binding names its advertised tokens.
func TestPolicyResourceBindingNodeGovernanceTarget(t *testing.T) {
	enr := testEnrolmentForBinding()
	want := expectedPolicyBinding(enr, "node.governance")
	if want.Target == "" || !strings.Contains(want.Target, "nodegov-vocab-") {
		t.Fatalf("node.governance expected target = %q, want this build's nodegov tokens", want.Target)
	}
	if got := expectedPolicyBinding(enr, policyfamAdmissionInput); got.Target != "" {
		t.Fatalf("admission.input expected target = %q, want none", got.Target)
	}
	lower := want
	lower.Target = "nodegov-keys-1,nodegov-schema-2,nodegov-vocab-1"
	res := orgcontract.SignedPolicyResource{Version: 3, Binding: &lower}
	if rej, refused := checkPolicyResourceBinding(enr, "node.governance", res, ""); !refused || rej.RejectCode != PRRejectClosedEnvelope {
		t.Fatalf("lower-target binding = %+v refused=%v; want closed_envelope_violation", rej, refused)
	}
	res.Binding = &want
	if _, refused := checkPolicyResourceBinding(enr, "node.governance", res, ""); refused {
		t.Fatal("this node's own binding was refused")
	}
}

func testEnrolmentForBinding() *store.Enrolment {
	return &store.Enrolment{OrgID: "org-1", UserID: "scim-42"}
}
