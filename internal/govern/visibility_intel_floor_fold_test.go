package govern

import (
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/policyfam/nodegov"
)

// TestIntelConsentSectionIsFloorOnIndividualNodes pins T-379 / review M4-07:
// on an INDIVIDUAL node an org body can neither hide nor lock the `intel`
// Settings section (it carries the consent banner and Accept / Withdraw, the
// developer's own decision), while on a managed node the same body still
// applies and the managed default still locks it read-only.
func TestIntelConsentSectionIsFloorOnIndividualNodes(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	cases := []struct {
		name  string
		grant *Grant
		in    nodegov.PolicyInput
		want  nodegov.Visibility
	}{
		{
			"individual, org hides intel", visGrant(ConsentInteractive, AuthorityDashboardVisibility),
			nodegov.PolicyInput{HiddenSettings: []string{"intel"}},
			nodegov.VisibilityVisible,
		},
		{
			"individual, org locks intel", visGrant(ConsentInteractive, AuthorityDashboardVisibility),
			nodegov.PolicyInput{ReadOnlySettings: []string{"intel"}},
			nodegov.VisibilityVisible,
		},
		{
			"managed, org hides intel", visGrant(ConsentManaged, AuthorityDashboardVisibility),
			nodegov.PolicyInput{HiddenSettings: []string{"intel"}},
			nodegov.VisibilityHidden,
		},
		{
			"managed, nothing published: the default locks it", visGrant(ConsentManaged, AuthoritySettingsPin),
			nodegov.PolicyInput{},
			nodegov.VisibilityReadOnly,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var d Delivered
			if len(tc.in.HiddenSettings)+len(tc.in.ReadOnlySettings) > 0 {
				d = visBody(t, tc.in)
			}
			e := Resolve(d, tc.grant, visLive, now)
			if lvl, _ := e.SettingsVisibility("intel"); lvl != tc.want {
				t.Fatalf("intel = %s, want %s", lvl, tc.want)
			}
			v := e.DashboardVisibility()
			listed := contains(v.HiddenSettings, "intel") || contains(v.ReadOnlySettings, "intel")
			if listed != (tc.want != nodegov.VisibilityVisible) {
				t.Fatalf("resolved lists hidden=%v read_only=%v disagree with %s", v.HiddenSettings, v.ReadOnlySettings, tc.want)
			}
			if tc.want == nodegov.VisibilityVisible && (e.IsSettingsSectionHidden("intel") || e.IsSettingsSectionReadOnly("intel")) {
				t.Fatal("the dashboard guard would still refuse the intel consent routes")
			}
		})
	}
}
