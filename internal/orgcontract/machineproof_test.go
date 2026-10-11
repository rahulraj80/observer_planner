package orgcontract

import (
	"crypto/ed25519"
	"crypto/rand"
	"strconv"
	"testing"
	"time"
)

// TestMachineProofBindsEveryField pins the T-656 per-request proof: it
// verifies for exactly the org, member, machine, method, path and timestamp
// it was signed for, and inside the freshness window only.
func TestMachineProofBindsEveryField(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	pub := priv.Public().(ed25519.PublicKey)
	now := time.Now()
	h := MachineProofHeaders(priv, "org-1", "alice", "laptop-7", "GET", AccessGrantsPath, now)
	if h[HeaderMachineProofCaps] != MachineProofCapability {
		t.Fatalf("caps header %q", h[HeaderMachineProofCaps])
	}
	ts, err := strconv.ParseInt(h[HeaderMachineProofTimestamp], 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	sig := h[HeaderMachineProofSignature]
	ok := func(org, sub, machine, method, path string, at time.Time) bool {
		return VerifyMachineMessage(pub, MachineProofMessage(org, sub, machine, method, path, ts), sig, ts, at) == nil
	}
	if !ok("org-1", "alice", "laptop-7", "GET", AccessGrantsPath, now) {
		t.Fatal("the proof does not verify for what it signed")
	}
	for name, bad := range map[string]bool{
		"another org":     ok("org-2", "alice", "laptop-7", "GET", AccessGrantsPath, now),
		"another member":  ok("org-1", "bob", "laptop-7", "GET", AccessGrantsPath, now),
		"another machine": ok("org-1", "alice", "laptop-8", "GET", AccessGrantsPath, now),
		"another method":  ok("org-1", "alice", "laptop-7", "POST", AccessGrantsPath, now),
		"another path":    ok("org-1", "alice", "laptop-7", "GET", AgentBudgetPath, now),
		"stale":           ok("org-1", "alice", "laptop-7", "GET", AccessGrantsPath, now.Add(MachineProofWindow+time.Minute)),
	} {
		if bad {
			t.Errorf("the proof verified for %s", name)
		}
	}
	// Registration and rotation are domain-separated from the proof.
	reg := MachineKeyRegistrationMessage("org-1", "alice", "laptop-7", "k", ts)
	rot := MachineKeyRotationMessage("org-1", "alice", "laptop-7", "k", ts)
	if string(reg) == string(rot) {
		t.Fatal("registration and rotation messages collide")
	}
}
