//go:build !sbo_omit_terminals_sandbox_sandhold && !sbo_omit_terminals_sandbox && !sbo_omit_isolation && !sbo_omit_terminals

package main

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/sandboxremote/sandhold"
)

// TestHostedBoxClassesFollowSandHold: the box note's class and tier tables
// (terminal_sandbox_hosted_boxwords.go) are SandHold's own mapping, word
// for word, so the stored class of a run is the one the driver plans with.
func TestHostedBoxClassesFollowSandHold(t *testing.T) {
	for word := range hostedBoxIsolations {
		want, ok := sandhold.ClassForTier(word)
		if got := hostedBoxClasses[word]; !ok || got != want {
			t.Errorf("tier %s: note class %q, SandHold %q (%v)", word, got, want, ok)
		}
	}
	if len(hostedBoxClasses) != len(hostedBoxIsolations) {
		t.Errorf("note classes %v, tier words %v", hostedBoxClasses, hostedBoxIsolations)
	}
	for word := range hostedBoxNetworks {
		want, ok := sandhold.TierForNetwork(word)
		if got := hostedBoxTiers[word]; !ok || got != want {
			t.Errorf("network %s: note tier %q, SandHold %q (%v)", word, got, want, ok)
		}
	}
	if len(hostedBoxTiers) != len(hostedBoxNetworks) {
		t.Errorf("note tiers %v, network words %v", hostedBoxTiers, hostedBoxNetworks)
	}
}
