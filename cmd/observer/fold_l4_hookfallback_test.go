package main

import (
	"os"
	"testing"
)

// TestFallbackWithoutStoreWithholdsOrgUserPolicyMode pins review M2-14: when
// the hook's fallback cannot open the database there is no key pin and no
// tenancy, so a bundle that verifies only on its own signature could hand
// an individual node the org's fail_closed choice for an invalid user
// policy. The families keep the conservative derivation; that MODE is
// withheld. With the store readable the org's choice still applies.
func TestFallbackWithoutStoreWithholdsOrgUserPolicyMode(t *testing.T) {
	const enforce = "[guard]\nenabled = true\nmode = \"enforce\""
	const failClosed = "[fallbacks]\nuser_policy_invalid = \"fail_closed\"\n"

	n := newPostureOrgNode(t, enforce)
	writeOrgBundle(t, n.bundle, failClosed)
	if err := os.Mkdir(n.dbPath, 0o700); err != nil { // a directory: the DB cannot open
		t.Fatal(err)
	}
	forgeOpenPosture(t, n.cp)
	plan := resolveHookFailurePlan("claude-code", "pre-tool", n.cp)
	if !plan.FailClosed {
		t.Fatal("an enforcing config no longer fails closed without the store")
	}
	if plan.UserPolicyFailClosed {
		t.Fatal("without the store (no pin, no tenancy) the org's fail_closed user-policy mode was applied")
	}

	// Store readable, bundle signed under the enrolment pin on a node whose
	// tenancy reads as managed or unresolved: the org's choice is honoured.
	m := newPostureOrgNode(t, enforce)
	pub := writeOrgBundle(t, m.bundle, failClosed)
	seedPin(t, m.dbPath, pinOf(pub))
	forgeOpenPosture(t, m.cp)
	if got := resolveHookFailurePlan("claude-code", "pre-tool", m.cp); !got.FailClosed {
		t.Fatalf("store readable: plan = %+v, want fail closed", got)
	}
}
