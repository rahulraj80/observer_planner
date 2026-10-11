package integration

import (
	"strings"
	"testing"
)

// TestEveryUpLaneRouteDeclaresItsLane (Sol A5-02 follow-up): a route whose
// base URL addresses a proxy `/up/<id>/` lane must declare that lane, so the
// launcher preflight can check it and the daemon can build any built-in
// default. Since A5-02 an undeclared, unconfigured lane is a 502 at runtime.
func TestEveryUpLaneRouteDeclaresItsLane(t *testing.T) {
	for _, c := range Capabilities() {
		if c.Proxy == nil || !strings.HasPrefix(c.Proxy.Suffix, "/up/") {
			if c.Proxy != nil && c.Proxy.Lane != nil {
				t.Errorf("%s declares lane %q but its route %q addresses no /up/ lane", c.Tool, c.Proxy.Lane.ID, c.Proxy.Suffix)
			}
			continue
		}
		if c.Proxy.Lane == nil {
			t.Errorf("%s routes at %q but declares no ProxyRoute.Lane", c.Tool, c.Proxy.Suffix)
			continue
		}
		if !strings.HasPrefix(c.Proxy.Suffix, "/up/"+c.Proxy.Lane.ID+"/") {
			t.Errorf("%s: lane %q does not match its route %q", c.Tool, c.Proxy.Lane.ID, c.Proxy.Suffix)
		}
		if u := c.Proxy.Lane.DefaultUpstream; u != "" &&
			(!strings.HasPrefix(u, "https://") || strings.HasSuffix(u, "/") || strings.HasSuffix(u, "/v1")) {
			t.Errorf("%s: default upstream %q must be an https host root with no version suffix", c.Tool, u)
		}
	}
}

// TestDefaultLaneUpstreams pins the grounded built-in lanes: the live-verified
// grok and openrouter hosts, and one upstream per lane id.
func TestDefaultLaneUpstreams(t *testing.T) {
	got := DefaultLaneUpstreams()
	want := map[string]string{
		"grok":       "https://cli-chat-proxy.grok.com",
		"openrouter": "https://openrouter.ai",
	}
	for lane, u := range want {
		if got[lane] != u {
			t.Errorf("DefaultLaneUpstreams()[%q] = %q, want %q", lane, got[lane], u)
		}
	}
	seen := map[string]string{}
	for _, c := range Capabilities() {
		if c.Proxy == nil || c.Proxy.Lane == nil || c.Proxy.Lane.DefaultUpstream == "" {
			continue
		}
		if prev, ok := seen[c.Proxy.Lane.ID]; ok && prev != c.Proxy.Lane.DefaultUpstream {
			t.Errorf("lane %q has two built-in upstreams: %q and %q", c.Proxy.Lane.ID, prev, c.Proxy.Lane.DefaultUpstream)
		}
		seen[c.Proxy.Lane.ID] = c.Proxy.Lane.DefaultUpstream
	}
}
