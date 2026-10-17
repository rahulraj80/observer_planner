package govern

import (
	"strings"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/policyfam/nodegov"
)

// Per-tab visibility (backlog item 4, T-132): a tab is hidden by an explicit
// org hide of "<page>/<tab>" or by its page being hidden; a page whose every
// tab is hidden is hidden itself.
func TestTabVisibilityResolve(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	allSecurityTabs := nodegov.NavTabsOf("security")
	cases := []struct {
		name       string
		grant      *Grant
		in         nodegov.PolicyInput
		tabs       map[string]nodegov.Visibility
		pages      map[string]nodegov.Visibility
		hiddenTabs string
	}{
		{
			name:  "managed: expose Security, hide its Prompt guard tab",
			grant: visGrant(ConsentManaged),
			in:    nodegov.PolicyInput{VisibleSections: []string{"security"}, HiddenSections: []string{"security/prompt-guard"}},
			tabs: map[string]nodegov.Visibility{
				"security/prompt-guard": nodegov.VisibilityHidden,
				"security/rules":        nodegov.VisibilityVisible,
			},
			pages:      map[string]nodegov.Visibility{"security": nodegov.VisibilityVisible},
			hiddenTabs: "security/prompt-guard",
		},
		{
			name:  "a tab of a page the managed default hides is hidden with it, but not listed",
			grant: visGrant(ConsentManaged),
			in:    nodegov.PolicyInput{},
			tabs: map[string]nodegov.Visibility{
				"security/rules": nodegov.VisibilityHidden,
				"cache/events":   nodegov.VisibilityVisible,
			},
			pages: map[string]nodegov.Visibility{"security": nodegov.VisibilityHidden},
		},
		{
			name:  "a read-only page's visible tab is read-only too",
			grant: visGrant(ConsentManaged),
			in:    nodegov.PolicyInput{HiddenSections: []string{"compression/experiments"}},
			tabs: map[string]nodegov.Visibility{
				"compression/experiments": nodegov.VisibilityHidden,
				"compression/savings":     nodegov.VisibilityReadOnly,
			},
			pages:      map[string]nodegov.Visibility{"compression": nodegov.VisibilityReadOnly},
			hiddenTabs: "compression/experiments",
		},
		{
			name:  "every tab hidden hides the page",
			grant: visGrant(ConsentManaged),
			in:    nodegov.PolicyInput{VisibleSections: []string{"security"}, HiddenSections: allSecurityTabs},
			pages: map[string]nodegov.Visibility{"security": nodegov.VisibilityHidden},
		},
		{
			name:  "individual node with dashboard.visibility: a tab hide applies",
			grant: visGrant(ConsentInteractive, AuthorityDashboardVisibility),
			in:    nodegov.PolicyInput{HiddenSections: []string{"cache/events"}},
			tabs: map[string]nodegov.Visibility{
				"cache/events":   nodegov.VisibilityHidden,
				"cache/overview": nodegov.VisibilityVisible,
			},
			pages:      map[string]nodegov.Visibility{"cache": nodegov.VisibilityVisible},
			hiddenTabs: "cache/events",
		},
		{
			name:  "individual node without dashboard.visibility: nothing hidden",
			grant: visGrant(ConsentInteractive, AuthoritySettingsPin),
			in:    nodegov.PolicyInput{HiddenSections: []string{"cache/events"}},
			tabs:  map[string]nodegov.Visibility{"cache/events": nodegov.VisibilityVisible},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := Resolve(visBody(t, tc.in), tc.grant, visLive, now)
			for id, want := range tc.tabs {
				if got, _ := e.TabVisibility(id); got != want {
					t.Errorf("tab %q = %s, want %s (state %s)", id, got, want, e.State)
				}
				if e.IsNavTabHidden(id) != (want == nodegov.VisibilityHidden) {
					t.Errorf("IsNavTabHidden(%q) disagrees with TabVisibility", id)
				}
			}
			for id, want := range tc.pages {
				if got, _ := e.NavVisibility(id); got != want {
					t.Errorf("page %q = %s, want %s", id, got, want)
				}
			}
			vis := e.DashboardVisibility()
			if got := strings.Join(vis.HiddenTabs, ","); got != tc.hiddenTabs {
				t.Errorf("HiddenTabs = %q, want %q", got, tc.hiddenTabs)
			}
			for id, want := range tc.tabs {
				if vis.Tabs[id].Level != want {
					t.Errorf("DashboardVisibility.Tabs[%q] = %s, want %s", id, vis.Tabs[id].Level, want)
				}
			}
			if len(vis.Tabs) != len(nodegov.NavTabIDs()) {
				t.Errorf("Tabs has %d entries, want one per tab (%d)", len(vis.Tabs), len(nodegov.NavTabIDs()))
			}
		})
	}
}

// TestTabVisibilityOfAPage: asking for a page id (not a tab) is visible and
// never panics; an unknown tab id too.
func TestTabVisibilityOfANonTab(t *testing.T) {
	e := Resolve(Delivered{}, nil, LiveIdentity{}, time.Unix(1_700_000_000, 0))
	for _, id := range []string{"security", "security/no-such-tab", ""} {
		if lvl, _ := e.TabVisibility(id); lvl != nodegov.VisibilityVisible {
			t.Errorf("TabVisibility(%q) = %s", id, lvl)
		}
	}
}
