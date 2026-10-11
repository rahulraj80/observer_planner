package govern

import (
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/policyfam/nodegov"
)

// TestVisibilityTableRows: one case per row of visibilityTable, in order.
func TestVisibilityTableRows(t *testing.T) {
	cases := []struct {
		name    string
		in      visibilityInput
		wantLvl nodegov.Visibility
		wantSrc VisibilitySource
	}{
		{
			"t8 floor beats everything",
			visibilityInput{floor: true, hidden: true, managedDefaults: true, managedDefault: nodegov.VisibilityHidden},
			nodegov.VisibilityVisible, VisibilitySourceFloor,
		},
		{
			"org hidden",
			visibilityInput{hidden: true, managedDefaults: true, managedDefault: nodegov.VisibilityVisible},
			nodegov.VisibilityHidden, VisibilitySourceOrg,
		},
		{
			"every tab of the page hidden hides the page",
			visibilityInput{allTabsHidden: true, visible: true, managedDefaults: true, managedDefault: nodegov.VisibilityVisible},
			nodegov.VisibilityHidden, VisibilitySourceOrg,
		},
		{
			"org read-only",
			visibilityInput{readOnly: true, managedDefaults: true, managedDefault: nodegov.VisibilityHidden},
			nodegov.VisibilityReadOnly, VisibilitySourceOrg,
		},
		{
			"org visible overrides a hidden default",
			visibilityInput{visible: true, managedDefaults: true, managedDefault: nodegov.VisibilityHidden},
			nodegov.VisibilityVisible, VisibilitySourceOrg,
		},
		{
			"managed default",
			visibilityInput{managedDefaults: true, managedDefault: nodegov.VisibilityReadOnly},
			nodegov.VisibilityReadOnly, VisibilitySourceDefault,
		},
		{
			"ungoverned",
			visibilityInput{managedDefault: nodegov.VisibilityHidden},
			nodegov.VisibilityVisible, VisibilitySourceNone,
		},
	}
	if len(cases) != len(visibilityTable()) {
		t.Fatalf("%d cases for %d rows: every row needs its own case", len(cases), len(visibilityTable()))
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lvl, src := resolveVisibility(tc.in)
			if lvl != tc.wantLvl || src != tc.wantSrc {
				t.Fatalf("got (%s, %s), want (%s, %s)", lvl, src, tc.wantLvl, tc.wantSrc)
			}
		})
	}
}

func visGrant(consent string, authority ...string) *Grant {
	return &Grant{OrgKey: "org-k", Generation: 1, OrgName: "Acme", Authority: authority, ConsentMode: consent}
}

var visLive = LiveIdentity{Enrolled: true, OrgKey: "org-k", Generation: 1}

func visBody(t *testing.T, in nodegov.PolicyInput) Delivered {
	t.Helper()
	spec, err := nodegov.Compile(in)
	if err != nil {
		t.Fatal(err)
	}
	return Delivered{Present: true, Version: 7, Spec: spec}
}

// TestDashboardVisibilityResolve walks the end-to-end cases the operator
// ruling is about: a managed machine with nothing published starts from the
// defaults, the org exposes a page, an individual machine is unchanged, and a
// refused grant governs nothing.
func TestDashboardVisibilityResolve(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	cases := []struct {
		name      string
		delivered Delivered
		grant     *Grant
		nav       map[string]nodegov.Visibility
		settings  map[string]nodegov.Visibility
		active    bool
	}{
		{
			name:  "managed, nothing published: defaults",
			grant: visGrant(ConsentManaged, AuthoritySettingsPin),
			nav: map[string]nodegov.Visibility{
				"security": nodegov.VisibilityHidden, "routing": nodegov.VisibilityHidden,
				"policies": nodegov.VisibilityHidden, "egress": nodegov.VisibilityHidden,
				"compression": nodegov.VisibilityReadOnly, "remote": nodegov.VisibilityReadOnly,
				"sessions": nodegov.VisibilityVisible, "privacy": nodegov.VisibilityVisible,
			},
			settings: map[string]nodegov.Visibility{
				"guard": nodegov.VisibilityHidden, "pricing": nodegov.VisibilityReadOnly,
				"enrolment": nodegov.VisibilityVisible, "org": nodegov.VisibilityVisible,
			},
			active: true,
		},
		{
			name:      "managed via IdP, org exposes security and the guard section",
			grant:     visGrant(ConsentIdP),
			delivered: visBody(t, nodegov.PolicyInput{VisibleSections: []string{"security"}, VisibleSettings: []string{"guard"}}),
			nav: map[string]nodegov.Visibility{
				"security": nodegov.VisibilityVisible, "routing": nodegov.VisibilityHidden,
			},
			settings: map[string]nodegov.Visibility{"guard": nodegov.VisibilityVisible, "routing": nodegov.VisibilityHidden},
			active:   true,
		},
		{
			name:      "managed, org hides a default-visible page without dashboard.visibility",
			grant:     visGrant(ConsentManaged, AuthoritySettingsPin),
			delivered: visBody(t, nodegov.PolicyInput{HiddenSections: []string{"benchmarks"}}),
			nav: map[string]nodegov.Visibility{
				"benchmarks": nodegov.VisibilityHidden, "security": nodegov.VisibilityHidden,
			},
			active: true,
		},
		{
			name:  "individual, nothing published: everything visible",
			grant: visGrant(ConsentInteractive, AuthorityDashboardVisibility),
			nav: map[string]nodegov.Visibility{
				"security": nodegov.VisibilityVisible, "routing": nodegov.VisibilityVisible,
			},
			settings: map[string]nodegov.Visibility{"guard": nodegov.VisibilityVisible},
			active:   false,
		},
		{
			name:      "individual, expose list changes nothing, hide still hides",
			grant:     visGrant(ConsentInteractive, AuthorityDashboardVisibility),
			delivered: visBody(t, nodegov.PolicyInput{HiddenSections: []string{"remote"}, VisibleSections: []string{"security"}}),
			nav: map[string]nodegov.Visibility{
				"remote": nodegov.VisibilityHidden, "security": nodegov.VisibilityVisible, "routing": nodegov.VisibilityVisible,
			},
			active: true,
		},
		{
			name:      "individual without dashboard.visibility: the body is dropped",
			grant:     visGrant(ConsentInteractive, AuthoritySettingsPin),
			delivered: visBody(t, nodegov.PolicyInput{HiddenSections: []string{"remote"}}),
			nav:       map[string]nodegov.Visibility{"remote": nodegov.VisibilityVisible},
			// Active (a body is installed, state inert) but nothing hidden.
			active: true,
		},
		{
			name:  "managed but expired grant: no defaults",
			grant: &Grant{OrgKey: "org-k", Generation: 1, ConsentMode: ConsentManaged, ExpiresAt: now.Add(-time.Hour)},
			nav:   map[string]nodegov.Visibility{"security": nodegov.VisibilityVisible},
		},
		{
			name: "no grant at all",
			nav:  map[string]nodegov.Visibility{"security": nodegov.VisibilityVisible},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := Resolve(tc.delivered, tc.grant, visLive, now)
			if got := e.VisibilityActive(); got != tc.active {
				t.Errorf("VisibilityActive = %v, want %v (state %s)", got, tc.active, e.State)
			}
			for id, want := range tc.nav {
				if got, _ := e.NavVisibility(id); got != want {
					t.Errorf("nav %q = %s, want %s (state %s)", id, got, want, e.State)
				}
				if e.IsNavSectionHidden(id) != (want == nodegov.VisibilityHidden) {
					t.Errorf("IsNavSectionHidden(%q) disagrees with NavVisibility", id)
				}
				if e.IsNavSectionReadOnly(id) != (want == nodegov.VisibilityReadOnly) {
					t.Errorf("IsNavSectionReadOnly(%q) disagrees with NavVisibility", id)
				}
			}
			for id, want := range tc.settings {
				if got, _ := e.SettingsVisibility(id); got != want {
					t.Errorf("settings %q = %s, want %s", id, got, want)
				}
			}
			vis := e.DashboardVisibility()
			for id, want := range tc.nav {
				if vis.Sections[id].Level != want {
					t.Errorf("DashboardVisibility.Sections[%q] = %s, want %s", id, vis.Sections[id].Level, want)
				}
				inHidden := contains(vis.HiddenSections, id)
				if inHidden != (want == nodegov.VisibilityHidden) {
					t.Errorf("HiddenSections membership of %q = %v", id, inHidden)
				}
			}
		})
	}
}

// TestManagedSectionsClassApplied: on a managed grant the sections directive
// class is authorized by managed consent, so a managed node reports the body
// APPLIED rather than inert/not_preauthorized.
func TestManagedSectionsClassApplied(t *testing.T) {
	e := Resolve(visBody(t, nodegov.PolicyInput{VisibleSections: []string{"routing"}}),
		visGrant(ConsentManaged, AuthoritySettingsPin), visLive, time.Unix(1_700_000_000, 0))
	if e.State != StateApplied {
		t.Fatalf("state = %s, dropped = %+v", e.State, e.Dropped)
	}
	if len(e.VisibleSections) != 1 {
		t.Fatalf("visible = %v", e.VisibleSections)
	}
}

// TestExposeListsChangeTheHash: the running posture's hash covers the expose
// lists, so a body that only exposes a page cannot hash-match an empty one.
func TestExposeListsChangeTheHash(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	g := visGrant(ConsentManaged)
	a := Resolve(visBody(t, nodegov.PolicyInput{}), g, visLive, now)
	b := Resolve(visBody(t, nodegov.PolicyInput{VisibleSections: []string{"security"}}), g, visLive, now)
	if a.Hash == b.Hash {
		t.Fatal("expose list did not change the hash")
	}
}
