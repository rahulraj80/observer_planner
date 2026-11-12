//go:build linux && !sbo_omit_terminals_sandbox && !sbo_omit_isolation && !sbo_omit_terminals

package main

import (
	"testing"
)

// A hosted helper's wrap gets a SIGKILL backstop that covers its whole
// bounded teardown, so `terminals stop` on a Vault (Kata) box no longer
// kills the helper two seconds in, mid-delete (box left listed, exit -1).
// Every other wrap keeps the default.
func TestHostedHelperWrapGetsAKillGraceCoveringItsTeardown(t *testing.T) {
	hosted := []string{"/usr/bin/observer", hostedHostVerb, "--plan", "/p.json", "--", "/usr/bin/observer", "sandbox-host", "--", hostedInnerMarker}
	g := hostedKillGrace(hosted)
	if min := hostedTeardownPullTimeout + hostedTeardownExportTimeout + hostedTeardownDeleteTimeout; g <= min {
		t.Fatalf("hosted helper grace %s, want more than its teardown deadlines (%s)", g, min)
	}
	for _, w := range [][]string{
		nil,
		{"bwrap", "--die-with-parent", "--"},
		{"/usr/bin/observer", "sandbox-host", "--"},
		{"/usr/bin/observer", hostedHostVerb, "--plan", "/p.json", "--"},
	} {
		if g := hostedKillGrace(w); g != 0 {
			t.Fatalf("wrap %q got grace %s, want 0 (the default)", w, g)
		}
	}
}
