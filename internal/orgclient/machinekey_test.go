package orgclient

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
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

// memMachineKeys is an in-memory MachineKeyStore.
type memMachineKeys struct {
	mu  sync.Mutex
	raw []byte
}

func (m *memMachineKeys) LoadMachineKey() ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.raw == nil {
		return nil, ErrNoSecret
	}
	return append([]byte(nil), m.raw...), nil
}

func (m *memMachineKeys) SaveMachineKey(raw []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.raw = append([]byte(nil), raw...)
	return nil
}

// TestMachineProofRegistersOnceAndSigns pins the node half of T-656: with a
// machine key slot wired, the first proved request registers the key (proof
// of possession over org, member, machine and key), later ones do not, and
// every proved request carries a signature the registered key verifies for
// exactly that method and path. Unwired, nothing is sent (a released node).
func TestMachineProofRegistersOnceAndSigns(t *testing.T) {
	var mu sync.Mutex
	var regs []orgcontract.MachineKeyRegistration
	status := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != orgcontract.MachineKeyPath {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var reg orgcontract.MachineKeyRegistration
		_ = json.Unmarshal(raw, &reg)
		mu.Lock()
		regs = append(regs, reg)
		st := status
		mu.Unlock()
		writeTestJSON(w, st, orgcontract.MachineKeyRegistrationResponse{Status: orgcontract.MachineKeyRegistered})
	}))
	t.Cleanup(srv.Close)
	ctx := context.Background()
	c, s, _ := enrolledClient(t, srv.URL)
	enr, err := s.LoadEnrolment(ctx)
	if err != nil || enr == nil {
		t.Fatalf("enrolment: %v", err)
	}

	// Unwired: no proof, no registration.
	req, _ := http.NewRequest(http.MethodGet, srv.URL+orgcontract.AccessGrantsPath, nil)
	c.attachMachineProof(ctx, req, enr, "laptop-7", http.MethodGet, orgcontract.AccessGrantsPath)
	if req.Header.Get(orgcontract.HeaderMachineProofSignature) != "" || len(regs) != 0 {
		t.Fatal("an unwired client sent a machine proof or registered")
	}

	keys := &memMachineKeys{}
	c.SetMachineKeyStore(keys)
	for i := 0; i < 2; i++ {
		req, _ := http.NewRequest(http.MethodGet, srv.URL+orgcontract.AccessGrantsPath, nil)
		c.attachMachineProof(ctx, req, enr, "laptop-7", http.MethodGet, orgcontract.AccessGrantsPath)
		mu.Lock()
		n := len(regs)
		reg := regs[0]
		mu.Unlock()
		if n != 1 {
			t.Fatalf("poll %d: %d registrations, want exactly 1", i, n)
		}
		pub, err := orgcontract.DecodeMachinePublicKey(reg.PublicKey)
		if err != nil {
			t.Fatal(err)
		}
		if err := orgcontract.VerifyMachineMessage(pub,
			orgcontract.MachineKeyRegistrationMessage(enr.OrgID, enr.UserID, "laptop-7", reg.PublicKey, reg.Timestamp),
			reg.Signature, reg.Timestamp, time.Now()); err != nil {
			t.Fatalf("registration proof of possession: %v", err)
		}
		ts, _ := strconv.ParseInt(req.Header.Get(orgcontract.HeaderMachineProofTimestamp), 10, 64)
		if err := orgcontract.VerifyMachineMessage(pub,
			orgcontract.MachineProofMessage(enr.OrgID, enr.UserID, "laptop-7", http.MethodGet, orgcontract.AccessGrantsPath, ts),
			req.Header.Get(orgcontract.HeaderMachineProofSignature), ts, time.Now()); err != nil {
			t.Fatalf("poll %d: request proof: %v", i, err)
		}
	}
	// The key is kept: a new client over the same slot reuses it.
	c2, _, _ := enrolledClient(t, srv.URL)
	c2.SetMachineKeyStore(keys)
	k1, err := c.loadOrCreateMachineKey(enr.OrgID)
	if err != nil {
		t.Fatal(err)
	}
	k2, err := c2.loadOrCreateMachineKey(enr.OrgID)
	if err != nil || !k1.Equal(k2) {
		t.Fatalf("the stored key was not reused: %v", err)
	}
	// Another org gets a fresh key.
	k3, err := c2.loadOrCreateMachineKey("org-other")
	if err != nil || k3.Equal(k1) {
		t.Fatalf("a key was reused across orgs: %v", err)
	}
	// ... without destroying the first org's key (REVIEW-4 P4-15): coming
	// back to the first org finds k1 again, so its server's registration
	// still matches instead of becoming a permanent 409. Not run in the lane
	// (D-507).
	k4, err := c2.loadOrCreateMachineKey(enr.OrgID)
	if err != nil || !k4.Equal(k1) {
		t.Fatalf("returning to the first org did not find its key: %v", err)
	}
	k5, err := c2.loadOrCreateMachineKey("org-other")
	if err != nil || !k5.Equal(k3) {
		t.Fatalf("the second org's key was not kept beside the first: %v", err)
	}
}

// TestMachineKeyRingReadsTheLegacySingleKey pins that a slot written by an
// earlier build (one {"org_id", "seed"} record) keeps serving that org's key
// and gains a second org beside it (REVIEW-4 P4-15). Not run in the lane
// (D-507).
func TestMachineKeyRingReadsTheLegacySingleKey(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	legacy, _ := json.Marshal(map[string]string{"org_id": "org-a", "seed": base64.StdEncoding.EncodeToString(priv.Seed())})
	keys := &memMachineKeys{raw: legacy}
	c := &Client{}
	c.SetMachineKeyStore(keys)
	got, err := c.loadOrCreateMachineKey("org-a")
	if err != nil || !got.Equal(priv) {
		t.Fatalf("legacy key not served: %v", err)
	}
	if _, err := c.loadOrCreateMachineKey("org-b"); err != nil {
		t.Fatal(err)
	}
	again, err := c.loadOrCreateMachineKey("org-a")
	if err != nil || !again.Equal(priv) {
		t.Fatalf("legacy key lost after adding a second org: %v", err)
	}
}

// TestLeaveNoticeCarriesTheMachineProof pins the node half of T-747 (ruling
// D-559): the leave notice of a managed machine names the machine and carries
// a proof by the machine key the node already holds for the org, signed for
// POST /api/agent/leave, so the server can retire that key. With no key in the
// slot nothing is created and no proof is sent; an individual enrolment (no
// machine identity) sends none either.
func TestLeaveNoticeCarriesTheMachineProof(t *testing.T) {
	enr := &store.Enrolment{OrgID: "org-1", UserID: "scim-42"}
	keys := &memMachineKeys{}
	c := &Client{}
	c.SetMachineKeyStore(keys)

	req, _ := http.NewRequest(http.MethodPost, "https://org.example"+orgcontract.AgentLeavePath, nil)
	c.attachLeaveProof(req, enr, "laptop-7")
	if req.Header.Get(orgcontract.HeaderMachineProofSignature) != "" || keys.raw != nil {
		t.Fatal("a node with no machine key sent a proof or created a key on leave")
	}

	priv, err := c.loadOrCreateMachineKey("org-1")
	if err != nil {
		t.Fatal(err)
	}
	req, _ = http.NewRequest(http.MethodPost, "https://org.example"+orgcontract.AgentLeavePath, nil)
	c.attachLeaveProof(req, enr, "")
	if req.Header.Get(orgcontract.HeaderMachineIdentity) != "" || req.Header.Get(orgcontract.HeaderMachineProofSignature) != "" {
		t.Fatal("an unmanaged enrolment (no machine identity) sent a machine claim")
	}

	req, _ = http.NewRequest(http.MethodPost, "https://org.example"+orgcontract.AgentLeavePath, nil)
	c.attachLeaveProof(req, enr, "laptop-7")
	if got := req.Header.Get(orgcontract.HeaderMachineIdentity); got != "laptop-7" {
		t.Fatalf("machine header = %q, want laptop-7", got)
	}
	ts, _ := strconv.ParseInt(req.Header.Get(orgcontract.HeaderMachineProofTimestamp), 10, 64)
	if err := orgcontract.VerifyMachineMessage(priv.Public().(ed25519.PublicKey),
		orgcontract.MachineProofMessage("org-1", "scim-42", "laptop-7", http.MethodPost, orgcontract.AgentLeavePath, ts),
		req.Header.Get(orgcontract.HeaderMachineProofSignature), ts, time.Now()); err != nil {
		t.Fatalf("leave proof does not verify for POST %s: %v", orgcontract.AgentLeavePath, err)
	}
}
