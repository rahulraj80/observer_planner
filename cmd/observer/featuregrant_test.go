package main

import (
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/policyfam/nodefeatures"
)

// TestNodeFeatureGrantLiftsADisabledFeature (request-grants G8): a feature
// the org's node.features policy disables is allowed while a verified
// feature access grant names it. The grant lifts "disabled" and nothing
// else: a terminals policy that also requires a sandbox still requires it.
// An enabled feature never consults grants; a refusal names the request
// path.
func TestNodeFeatureGrantLiftsADisabledFeature(t *testing.T) {
	nf := newNodeFeaturesHandle()
	nf.recordApplied(4, "bh", nodefeatures.PolicySpec{
		Remote:        nodefeatures.FeatureRule{Governed: true, Enabled: false},
		PatternsWrite: nodefeatures.FeatureRule{Governed: true, Enabled: false},
		RoutingApply:  nodefeatures.FeatureRule{Governed: true, Enabled: true},
		Terminals:     nodefeatures.TerminalsRule{FeatureRule: nodefeatures.FeatureRule{Governed: true}, SandboxRequired: true},
	})
	var asked []string
	nf.SetFeatureGrant(func(feature string) (int64, bool) {
		asked = append(asked, feature)
		switch feature {
		case nodefeatures.FeatureRemote:
			return 51, true
		case nodefeatures.FeatureTerminals:
			return 52, true
		}
		return 0, false
	})
	if ok, why := nf.Allowed(nodefeatures.FeatureRemote); !ok || !strings.Contains(why, "#51") {
		t.Fatalf("granted remote: %v %q", ok, why)
	}
	ok, why := nf.Allowed(nodefeatures.FeaturePatternsWrite)
	if ok || !strings.Contains(why, "observer request feature patterns_write") {
		t.Fatalf("ungranted patterns_write: %v %q", ok, why)
	}
	if ok, _ := nf.Allowed(nodefeatures.FeatureRoutingApply); !ok {
		t.Fatal("an enabled feature was refused")
	}
	if ok, why := nf.TerminalAllowed(false); ok || !strings.Contains(why, "sandbox") {
		t.Fatalf("the grant lifted the sandbox requirement: %v %q", ok, why)
	}
	if ok, _ := nf.TerminalAllowed(true); !ok {
		t.Fatal("a granted, sandboxed terminal was refused")
	}
	for _, f := range asked {
		if f == nodefeatures.FeatureRoutingApply {
			t.Fatalf("an enabled feature consulted grants: %v", asked)
		}
	}
}
