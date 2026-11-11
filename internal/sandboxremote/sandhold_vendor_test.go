package sandboxremote

import (
	"testing"

	vocab "github.com/marmutapp/superbased-observer/internal/sandboxvocab"
)

// TestSandHoldVendorRow: the SandHold row's endpoint is only the connected
// node (https, or http for this machine), its environment key is bound to
// the node the environment names, it runs every exec as the box user, and
// it never carries the model proxy on the none tier.
func TestSandHoldVendorRow(t *testing.T) {
	v, ok := VendorFor(VendorSandHold)
	if !ok || !v.NodeEndpoint || !v.SeveralIsolations || !v.NoRootExec || v.GuestPTY || v.Reattach || !v.RelayStdin {
		t.Fatalf("capability flags %+v", v)
	}
	if v.DefaultEndpoint != "" || len(v.Domains) != 0 || v.Price.USDPerHour != 0 {
		t.Errorf("a node row has no vendor endpoint, domain or price: %q %v %v", v.DefaultEndpoint, v.Domains, v.Price)
	}
	if v.Tiers[vocab.TierNone].Relay || !v.Tiers[vocab.TierProxyOnly].Relay {
		t.Error("tier relays")
	}
	for _, tc := range []struct {
		ep string
		ok bool
	}{
		{"https://node.example:48000", true},
		{"http://127.0.0.1:18100", true},
		{"http://localhost:8080", true},
		{"http://[::1]:8080", true},
		{"http://node.example", false},
		{"https://user@node.example", false},
		{"https://node.example/?x=1", false},
		{"ftp://node.example", false},
		{"", false},
	} {
		r, ep, ok := v.ResolveEndpoint("", tc.ep, false)
		if ok != tc.ok || (ok && (ep != tc.ep || r != RegionSandHoldNode)) {
			t.Errorf("%q: got %q %q %v, want ok=%v", tc.ep, r, ep, ok, tc.ok)
		}
	}
	env := Credential{Provider: v.ID, Fields: map[string]string{"SANDHOLD_API_KEY": "k"}, Endpoint: "https://node.example", Source: SourceEnv}
	if err := CheckBinding(env, v, "https://node.example"); err != nil {
		t.Errorf("an environment key for its own node: %v", err)
	}
	if err := CheckBinding(env, v, "https://other.example"); err == nil {
		t.Error("an environment key was allowed to another node")
	}
	l := v.Layout("/home/me/proj")
	if l.Workspace != "/workspace" || l.Home != "/tmp/observer-home" {
		t.Errorf("layout %+v", l)
	}
}
