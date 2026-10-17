package govern

import (
	"strings"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/policyfam/nodegov"
)

// A page-access grant (T-435) shows ONE hidden page or tab read-only until it
// ends: it lifts hidden to read-only and never changes anything else.
func TestPageGrantLiftsHiddenToReadOnly(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	type lv struct {
		level  nodegov.Visibility
		source VisibilitySource
	}
	cases := []struct {
		name       string
		in         nodegov.PolicyInput
		granted    []string
		pages      map[string]lv
		tabs       map[string]lv
		hiddenTabs string
	}{
		{
			name:  "no grant: the managed default hides the page",
			pages: map[string]lv{"security": {nodegov.VisibilityHidden, VisibilitySourceDefault}},
		},
		{
			name:    "a page grant shows the page read-only, every tab with it",
			granted: []string{"security"},
			pages:   map[string]lv{"security": {nodegov.VisibilityReadOnly, VisibilitySourceGrant}},
			tabs: map[string]lv{
				"security/rules":        {nodegov.VisibilityReadOnly, VisibilitySourceGrant},
				"security/prompt-guard": {nodegov.VisibilityReadOnly, VisibilitySourceGrant},
			},
		},
		{
			name:    "a tab grant shows its page read-only but never a sibling tab",
			granted: []string{"security/rules"},
			pages:   map[string]lv{"security": {nodegov.VisibilityReadOnly, VisibilitySourceGrant}},
			tabs: map[string]lv{
				"security/rules":        {nodegov.VisibilityReadOnly, VisibilitySourceGrant},
				"security/prompt-guard": {nodegov.VisibilityHidden, VisibilitySourceDefault},
			},
			hiddenTabs: "!security/rules",
		},
		{
			name:    "a hidden tab of a visible page shows read-only",
			in:      nodegov.PolicyInput{VisibleSections: []string{"security"}, HiddenSections: []string{"security/prompt-guard"}},
			granted: []string{"security/prompt-guard"},
			pages:   map[string]lv{"security": {nodegov.VisibilityVisible, VisibilitySourceOrg}},
			tabs:    map[string]lv{"security/prompt-guard": {nodegov.VisibilityReadOnly, VisibilitySourceGrant}},
		},
		{
			name:    "a grant never raises a read-only page",
			in:      nodegov.PolicyInput{ReadOnlySections: []string{"cost"}},
			granted: []string{"cost"},
			pages:   map[string]lv{"cost": {nodegov.VisibilityReadOnly, VisibilitySourceOrg}},
		},
		{
			name:    "a grant for a page the org shows changes nothing",
			in:      nodegov.PolicyInput{VisibleSections: []string{"security"}},
			granted: []string{"security"},
			pages:   map[string]lv{"security": {nodegov.VisibilityVisible, VisibilitySourceOrg}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := Resolve(visBody(t, tc.in), visGrant(ConsentManaged), visLive, now)
			e.GrantedPages = tc.granted
			for id, want := range tc.pages {
				if lvl, src := e.NavVisibility(id); lvl != want.level || src != want.source {
					t.Errorf("page %q = %s/%s, want %s/%s", id, lvl, src, want.level, want.source)
				}
			}
			for id, want := range tc.tabs {
				if lvl, src := e.TabVisibility(id); lvl != want.level || src != want.source {
					t.Errorf("tab %q = %s/%s, want %s/%s", id, lvl, src, want.level, want.source)
				}
			}
			vis := e.DashboardVisibility()
			got := strings.Join(vis.HiddenTabs, ",")
			if strings.HasPrefix(tc.hiddenTabs, "!") {
				// Every tab but the granted one is listed hidden.
				granted := strings.TrimPrefix(tc.hiddenTabs, "!")
				for _, id := range nodegov.NavTabsOf("security") {
					listed := strings.Contains(","+got+",", ","+id+",")
					if listed == (id == granted) {
						t.Errorf("HiddenTabs = %q: tab %q listed=%v", got, id, listed)
					}
				}
			} else if tc.hiddenTabs == "" && strings.Contains(got, "security/") && len(tc.granted) > 0 && tc.in.HiddenSections == nil {
				t.Errorf("HiddenTabs = %q, want no security tab", got)
			}
		})
	}
}
