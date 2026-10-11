package govern

import (
	"sort"
	"testing"
)

// TestManagedEnrolmentAuthoritySet pins the ONE owner of what a managed
// enrolment token offers when its minter named nothing (T-311): the whole
// enterprise set plus the three governance tokens that let the org decide
// what the developer sees and changes, and nothing that is retired or that
// switches a subsystem's enforcement mode.
func TestManagedEnrolmentAuthoritySet(t *testing.T) {
	t.Parallel()
	set := ManagedEnrolmentAuthoritySet()
	if !sort.StringsAreSorted(set) {
		t.Fatalf("not sorted (the signing message must be stable): %v", set)
	}
	seen := map[string]bool{}
	for _, tok := range set {
		if seen[tok] {
			t.Errorf("duplicate token %q", tok)
		}
		seen[tok] = true
		if !KnownAuthority(tok) {
			t.Errorf("%q is not in the closed vocabulary", tok)
		}
		if RetiredAuthority(tok) {
			t.Errorf("%q is retired and grants nothing; a default must not offer it", tok)
		}
	}
	for _, tok := range EnterpriseAuthoritySet() {
		if !seen[tok] {
			t.Errorf("managed default omits enterprise token %q", tok)
		}
	}
	// The three tokens the 2026-10 integration test had to pass by hand, plus
	// the other two org-decides-what-the-developer-can-change tokens.
	for _, want := range []string{
		AuthorityDashboardVisibility, AuthoritySettingsPin, AuthorityExtractManaged,
		AuthorityFeatureLock, AuthorityCapturePin,
		AuthorityEnforceBudget, AuthorityEnforceMCPAccess,
	} {
		if !seen[want] {
			t.Errorf("managed default omits %q: %v", want, set)
		}
	}
	for _, absent := range []string{
		AuthorityEnforceRouting, AuthorityEnforceAdmission, AuthorityEnforceEgress,
		AuthorityCaptureRaise,
	} {
		if seen[absent] {
			t.Errorf("managed default carries %q, which stays an authored per-org act", absent)
		}
	}
}

// TestManagedEnrolmentAuthoritySetReturnsACopy: the result becomes a token
// row and then a signed grant, so a caller mutating it must not change the
// next mint's default.
func TestManagedEnrolmentAuthoritySetReturnsACopy(t *testing.T) {
	t.Parallel()
	a := ManagedEnrolmentAuthoritySet()
	a[0] = "tampered"
	if b := ManagedEnrolmentAuthoritySet(); b[0] == "tampered" {
		t.Fatal("ManagedEnrolmentAuthoritySet shares its backing array between calls")
	}
}

// TestManagedDefaultGrantResolvesOnAnOlderNode is the compat invariant for
// the wider default: an older node that receives a grant carrying the managed
// default PLUS a token from a newer build must still resolve it (applied,
// unknown token reported), never treat it as an enrolment failure.
func TestManagedDefaultGrantResolvesOnAnOlderNode(t *testing.T) {
	g := testGrant()
	g.ConsentMode = ConsentManaged
	g.Authority = append(ManagedEnrolmentAuthoritySet(), "extract.some_future_tier")
	e := Resolve(testDelivered(), g, testLive(), testNow)
	if e.State != StateApplied {
		t.Fatalf("State = %q, want applied", e.State)
	}
	if len(e.UnknownAuthority) != 1 || e.UnknownAuthority[0] != "extract.some_future_tier" {
		t.Fatalf("UnknownAuthority = %v, want only the future token reported", e.UnknownAuthority)
	}
	if !e.Managed {
		t.Fatal("a managed-consent grant carrying the managed default did not resolve as managed")
	}
	held := map[string]bool{}
	for _, a := range e.Authority {
		held[a] = true
	}
	for _, want := range []string{AuthorityDashboardVisibility, AuthoritySettingsPin, AuthorityExtractManaged} {
		if !held[want] {
			t.Errorf("resolved authority %v lacks %q", e.Authority, want)
		}
	}
}
