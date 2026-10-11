package integration

import (
	"strings"
	"testing"
)

// TestProviderOverridesAreGrounded pins T-035's table: every covered row has a
// lane that matches its path (or rides a fixed upstream), every lane is an
// https host root that DefaultLaneUpstreams publishes, every uncovered row
// says why, and no provider appears twice for one tool.
func TestProviderOverridesAreGrounded(t *testing.T) {
	lanes := DefaultLaneUpstreams()
	for _, tool := range []string{"pi", "prime-agent"} {
		got := ProviderOverridesFor(tool)
		if len(got) == 0 {
			t.Fatalf("%s: no ProviderOverrides", tool)
		}
		seen := map[string]bool{}
		for _, o := range got {
			if seen[o.Provider] {
				t.Errorf("%s: provider %q listed twice", tool, o.Provider)
			}
			seen[o.Provider] = true
			if !o.Covered() {
				if o.Path != "" || o.Lane != nil {
					t.Errorf("%s/%s: uncovered row carries a route", tool, o.Provider)
				}
				continue
			}
			if o.Lane == nil {
				if strings.HasPrefix(o.Path, "/up/") {
					t.Errorf("%s/%s: path %q addresses a lane but declares none", tool, o.Provider, o.Path)
				}
				continue
			}
			if !strings.HasPrefix(o.Path, "/up/"+o.Lane.ID) {
				t.Errorf("%s/%s: lane %q does not match path %q", tool, o.Provider, o.Lane.ID, o.Path)
			}
			u := o.Lane.DefaultUpstream
			if !strings.HasPrefix(u, "https://") || strings.HasSuffix(u, "/") || strings.Count(u, "/") != 2 {
				t.Errorf("%s/%s: default upstream %q must be an https host root", tool, o.Provider, u)
			}
			if lanes[o.Lane.ID] != u {
				t.Errorf("%s/%s: DefaultLaneUpstreams()[%q] = %q, want %q", tool, o.Provider, o.Lane.ID, lanes[o.Lane.ID], u)
			}
		}
		for _, want := range []string{"anthropic", "openai", "google", "openrouter"} {
			if !seen[want] {
				t.Errorf("%s: built-in provider %q missing", tool, want)
			}
		}
	}
}

// TestProviderOverridesKeepExistingLanes: the new lanes never move a lane an
// existing row already declares.
func TestProviderOverridesKeepExistingLanes(t *testing.T) {
	got := DefaultLaneUpstreams()
	if got["openrouter"] != "https://openrouter.ai" || got["grok"] != "https://cli-chat-proxy.grok.com" {
		t.Fatalf("existing lanes moved: openrouter=%q grok=%q", got["openrouter"], got["grok"])
	}
	for _, c := range Capabilities() {
		if c.Proxy == nil {
			continue
		}
		for _, o := range c.Proxy.ProviderOverrides {
			if o.Lane != nil && got[o.Lane.ID] != o.Lane.DefaultUpstream {
				t.Errorf("%s/%s: lane %q has two built-in upstreams", c.Tool, o.Provider, o.Lane.ID)
			}
		}
	}
}

// TestProviderOverridesForCopies: callers cannot mutate the registry.
func TestProviderOverridesForCopies(t *testing.T) {
	a := ProviderOverridesFor("pi")
	a[0].Provider = "mutated"
	if ProviderOverridesFor("pi")[0].Provider == "mutated" {
		t.Fatal("ProviderOverridesFor returned the registry's backing array")
	}
	if ProviderOverridesFor("claude-code") != nil {
		t.Fatal("a tool without built-in provider overrides must return nil")
	}
}
