package main

import (
	"reflect"
	"slices"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/components"
	"github.com/marmutapp/superbased-observer/internal/config"
)

// TestApplyProxyComponentGate pins the buildProxy component gate (T-659 /
// T-661): with no [components] block nothing changes; removing
// proxy.compression or proxy.routing turns exactly that feature's switch
// off in the proxy's own config load, and the daemon-wide overlay applies
// too (an inactive obs builds no admission service in the proxy). In a
// build that compiles obs out (sbo_omit_obs, its no_obs alias, or
// sbo_omit_judge_client) obs is never active, even with no [components]
// block (the legacy snapshot activates what is compiled), so the overlay
// turns observability off in every case; the engine it would gate is not
// in that binary anyway.
func TestApplyProxyComponentGate(t *testing.T) {
	requireComponents(t, "proxy.compression", "proxy.routing")
	base := func() config.Config {
		var c config.Config
		c.Compression.Conversation.Enabled = true
		c.Routing.Enabled = true
		c.Observability.Enabled = true
		return c
	}
	cases := []struct {
		name                     string
		remove                   []string
		wantComp, wantRoute, obs bool
	}{
		{"legacy block changes nothing", nil, true, true, true},
		{"compression removed", []string{"proxy.compression"}, false, true, true},
		{"routing removed", []string{"proxy.routing"}, true, false, true},
		{"obs removed", []string{"obs"}, true, true, false},
		{"both proxy features removed", []string{"proxy.compression", "proxy.routing"}, false, false, true},
	}
	obsCompiled := slices.Contains(components.Compiled(components.BinaryNode), "obs")
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wantObs := tc.obs && obsCompiled
			in := base()
			in.Components.Remove = tc.remove
			got := applyProxyComponentGate(in)
			if got.Compression.Conversation.Enabled != tc.wantComp {
				t.Errorf("compression enabled = %v, want %v", got.Compression.Conversation.Enabled, tc.wantComp)
			}
			if got.Routing.Enabled != tc.wantRoute {
				t.Errorf("routing enabled = %v, want %v", got.Routing.Enabled, tc.wantRoute)
			}
			if got.Observability.Enabled != wantObs {
				t.Errorf("observability enabled = %v, want %v", got.Observability.Enabled, wantObs)
			}
			if tc.remove == nil && obsCompiled && !reflect.DeepEqual(got, in) {
				t.Error("a config without a [components] block must come back unchanged")
			}
		})
	}
}

// TestProxyGateCoversEveryProxySeam is the lane MP-ADP completion of the
// proxy gate (T-598): every optional component buildProxy wires a seam for
// turns that seam's switch off in the proxy's config when it is inactive,
// and leaves it alone when active. One row per proxyInertOverlay entry.
func TestProxyGateCoversEveryProxySeam(t *testing.T) {
	on := func() config.Config {
		var c config.Config
		c.Compression.Conversation.Enabled = true
		c.Compression.Conversation.Compaction.InjectPostCompact = true
		c.Routing.Enabled = true
		c.MCPRelay.Enabled = true
		c.Observer.Process.Enabled = true
		c.Observer.Process.Network.Enabled = true
		c.SelfObs.Enabled = true
		c.Proxy.OrgRoute.Mode = "gateway"
		c.Guard.Enabled = true
		return c
	}
	off := map[string]func(config.Config) bool{
		"proxy.compression": func(c config.Config) bool {
			return !c.Compression.Conversation.Enabled && !c.Compression.Conversation.Compaction.InjectPostCompact
		},
		"proxy.routing":     func(c config.Config) bool { return !c.Routing.Enabled },
		"agentaccess.relay": func(c config.Config) bool { return !c.MCPRelay.Enabled },
		"processobs":        func(c config.Config) bool { return !c.Observer.Process.Network.Enabled },
		"export.otel":       func(c config.Config) bool { return !c.SelfObs.Enabled },
		"org.client":        func(c config.Config) bool { return c.Proxy.OrgRoute.Mode == "" },
	}
	if len(off) != len(proxyInertOverlay) {
		t.Fatalf("proxyInertOverlay has %d rows, this test pins %d", len(proxyInertOverlay), len(off))
	}
	compiled := components.Compiled(components.BinaryNode)
	for _, row := range proxyInertOverlay {
		check, ok := off[row.component]
		if !ok {
			t.Fatalf("no test row for %s", row.component)
		}
		if _, known := components.Get(row.component); !known {
			t.Fatalf("%s is not a catalog component", row.component)
		}
		if !slices.Contains(compiled, row.component) || !defaultActive(row.component) {
			continue // compiled out, or outside this distro's default set: never active here, nothing to toggle
		}
		in := on()
		in.Components.Remove = withDependants(row.component)
		got := applyProxyComponentGate(in)
		if !check(got) {
			t.Errorf("%s removed: its proxy seam is still on", row.component)
		}
		if !got.Guard.Enabled {
			t.Errorf("%s removed: guard's proxy decisions were switched off (MODPKG-11)", row.component)
		}
		if check(applyProxyComponentGate(on())) {
			t.Errorf("%s active: its proxy seam was switched off", row.component)
		}
	}
}

// withDependants returns id and every node component that requires it,
// directly or transitively: a removal another selected component requires
// stays active, so a test that wants id inactive removes them too.
func withDependants(id string) []string {
	out := []string{id}
	seen := map[string]bool{id: true}
	for changed := true; changed; {
		changed = false
		for _, c := range components.ForBinary(components.BinaryNode) {
			if seen[c.ID] {
				continue
			}
			for _, r := range c.Requires {
				if seen[r] {
					seen[c.ID] = true
					out = append(out, c.ID)
					changed = true
					break
				}
			}
		}
	}
	return out
}
