package main

import (
	"context"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/components"
	"github.com/marmutapp/superbased-observer/internal/componentstate"
	"github.com/marmutapp/superbased-observer/internal/componentsvc"
	"github.com/marmutapp/superbased-observer/internal/config"
)

// TestUnconfiguredSelectionWordingMatchesTheCounts pins T-732 / T-739
// (D-548): with no [components] block the status and doctor lines said
// "every component this build contains is active" on distro and managed
// builds too, then listed the components that were not.
func TestUnconfiguredSelectionWordingMatchesTheCounts(t *testing.T) {
	cases := []struct {
		name   string
		v      componentstate.View
		want   string
		absent string
	}{
		{"all active", componentstate.View{ActiveCount: 87, CompiledCount: 87}, "every component this build contains is active", ""},
		{"distro defaults", componentstate.View{ActiveCount: 28, CompiledCount: 86}, "this build's defaults are active (28 of 86", "every component"},
		{"managed", componentstate.View{OrgManaged: true, OrgName: "demo.superbased.app", ActiveCount: 50, CompiledCount: 77}, "the components demo.superbased.app allows are active (50 of 77)", "every component"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := unconfiguredSelectionPhrase(tc.v)
			if !strings.Contains(got, tc.want) || (tc.absent != "" && strings.Contains(got, tc.absent)) {
				t.Fatalf("phrase = %q, want %q and not %q", got, tc.want, tc.absent)
			}
		})
	}
}

// TestManagedPolicySourceIsWorded pins T-739: a managed node's doctor line
// never prints a raw source token such as "(policy source unauthorized)".
func TestManagedPolicySourceIsWorded(t *testing.T) {
	for _, src := range []string{"policy", "last_known_good", "initial_grant", "legacy_pinned", "unauthorized", ""} {
		c := componentsPolicyCheck(componentstate.View{OrgManaged: true, OrgName: "acme", GovernanceSource: src})
		if strings.Contains(c.Message, "policy source") || (src != "" && strings.Contains(c.Message, "("+src+")")) {
			t.Errorf("source %q: %q", src, c.Message)
		}
	}
	c := componentsPolicyCheck(componentstate.View{OrgManaged: true, OrgName: "acme", GovernanceSource: "unauthorized"})
	if strings.Contains(c.Message, "the organization decides") {
		t.Errorf("unauthorized claims the organization decides: %q", c.Message)
	}
}

// TestLeftoversSkipComponentsNotInTheBuild pins T-732: the leftovers check
// probes only components this build contains (capture.browser warned on a
// minimal build).
func TestLeftoversSkipComponentsNotInTheBuild(t *testing.T) {
	snap := componentstate.ResolveGoverned(config.ComponentsConfig{Distro: "minimal"}, components.InDistro(components.DistroMinimal), componentstate.Governed{})
	var probed []string
	probe := func(_ context.Context, ids []string) ([]componentsvc.StepOutcome, error) {
		probed = ids
		return nil, nil
	}
	componentsDoctorChecks(context.Background(), snap, probe)
	for _, id := range probed {
		if d, ok := snap.Decision(id); ok && d.State == components.StateAbsent {
			t.Fatalf("probed %q, which this build lacks (probed %v)", id, probed)
		}
	}
}

// TestServiceRestartNoteFollowsTheProxy pins T-732: a node with no active
// proxy is not told "the service IS the proxy".
func TestServiceRestartNoteFollowsTheProxy(t *testing.T) {
	if strings.Contains(serviceRestartNote(false), "IS the proxy") {
		t.Fatal("no-proxy note claims the service is the proxy")
	}
	if !strings.Contains(serviceRestartNote(true), "IS the proxy") {
		t.Fatal("the proxy note lost its warning")
	}
}
