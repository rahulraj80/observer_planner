package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/marmutapp/superbased-observer/internal/components"
	"github.com/marmutapp/superbased-observer/internal/componentstate"
	"github.com/marmutapp/superbased-observer/internal/config"
)

// TestInactiveServiceVerbOffersToTurnItOn pins T-734 (D-547): `observer
// service install` on a node where integration.service is installed but off
// explains it and offers to turn it on (a terminal only), instead of only
// refusing; every other component and every non-interactive run keeps the
// refusal.
func TestInactiveServiceVerbOffersToTurnItOn(t *testing.T) {
	snap := componentstate.ResolveGoverned(config.ComponentsConfig{}, components.InDistro(components.DistroDeveloper), componentstate.Governed{})
	if snap.Active("integration.service") {
		t.Skip("integration.service is on in this snapshot")
	}
	managed := componentstate.ResolveGoverned(config.ComponentsConfig{}, components.InDistro(components.DistroManaged), componentstate.Governed{OrgManaged: true})
	cases := []struct {
		name        string
		snap        componentstate.Snapshot
		id          string
		tty         bool
		answer      string
		wantOn      bool
		wantAsked   bool
		wantApplied bool
	}{
		{"terminal, yes", snap, "integration.service", true, "y\n", true, true, true},
		{"terminal, no", snap, "integration.service", true, "n\n", false, true, false},
		{"pipe", snap, "integration.service", false, "y\n", false, false, false},
		{"not in the offer table", snap, "codeintel", true, "y\n", false, false, false},
		{"org-managed node", managed, "integration.service", true, "y\n", false, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			applied := ""
			oldA, oldI := componentActivator, interactiveVerb
			t.Cleanup(func() { componentActivator, interactiveVerb = oldA, oldI })
			componentActivator = func(_ context.Context, _ string, id string) error { applied = id; return nil }
			interactiveVerb = func(*cobra.Command) bool { return tc.tty }
			cmd := &cobra.Command{Use: "install"}
			var out bytes.Buffer
			cmd.SetIn(strings.NewReader(tc.answer))
			cmd.SetOut(&out)
			cmd.SetErr(&out)
			got := offerActivation(cmd, tc.snap, tc.id, "")
			if got != tc.wantOn {
				t.Fatalf("offerActivation = %v, want %v\n%s", got, tc.wantOn, out.String())
			}
			if asked := strings.Contains(out.String(), "Turn on "); asked != tc.wantAsked {
				t.Fatalf("asked = %v, want %v\n%s", asked, tc.wantAsked, out.String())
			}
			if (applied != "") != tc.wantApplied {
				t.Fatalf("applied %q, want applied=%v", applied, tc.wantApplied)
			}
		})
	}
}
