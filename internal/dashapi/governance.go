package dashapi

import (
	"context"
	"strings"

	"github.com/marmutapp/superbased-observer/internal/govern"
	"github.com/marmutapp/superbased-observer/internal/policyfam/nodegov"
)

// Section is the node-dashboard nav section a route belongs to - the unit an
// organization's governance body hides or locks. Its values are exactly the
// ids in web/src/lib/nav.ts, which internal/policyfam/nodegov pins against
// that file.
//
// SectionNone means "not part of any single page": status probes, the SPA
// shell, cross-cutting reads, and the governance endpoint itself. An
// unmapped/None route is NEVER hidden (the adversarial review's D-D
// default): fail-open here leaks at most one endpoint of a hidden page,
// while fail-closed would 404 every new route the moment any section is
// hidden.
type Section string

const (
	SectionNone Section = ""
)

const (
	SectionEgress Section = "egress"
)

const (
	SectionPolicies Section = "policies"
)

// settingsSubSep separates a route's nav section from the Settings subsection
// it also belongs to in a registered Section value ("settings#storage").
const settingsSubSep = "#"

// WithSettings tags a route as ALSO owned by one Settings subsection (Sol
// A3-01): the subsection is route REGISTRATION metadata, authored at the reg()
// call beside the nav section and the capability, rather than a secondary path
// table that drifts. The guard refuses the route when either owner hides it (or
// locks it, for an unsafe method). id is a nodegov.SettingsSectionIDs entry;
// TestEverySettingsRouteDeclaresItsSubsection pins that.
func (sec Section) WithSettings(id string) Section {
	return sec + Section(settingsSubSep+id)
}

// WithTab tags a route as owned by ONE tab of its page (per-tab visibility,
// T-132): the guard refuses it while the organization hides that tab, as it
// refuses a hidden page's routes. Only a route a single tab reads EXCLUSIVELY
// carries a tab; a route several tabs (or another page) read stays page-level,
// because hiding a tab is a view control that must not take data away from a
// tab still shown. tab is a nodegov tab id of the page
// (TestTabRoutesArePinned pins every tagged route).
func (sec Section) WithTab(tab string) Section {
	return sec + Section(nodegov.NavTabSep+tab)
}

// Split returns the nav section and the Settings subsection ("" when the
// route declares none) of a registered Section value. A tab tag is not part
// of the nav section (see Tab).
func (sec Section) Split() (Section, string) {
	nav, sub, _ := strings.Cut(string(sec), settingsSubSep)
	nav, _, _ = strings.Cut(nav, nodegov.NavTabSep)
	return Section(nav), sub
}

// Tab returns the page tab a registered Section value names, "" for none.
func (sec Section) Tab() string {
	nav, _, _ := strings.Cut(string(sec), settingsSubSep)
	_, tab, _ := strings.Cut(nav, nodegov.NavTabSep)
	return tab
}

// GovernanceProvider resolves the node's live governance posture. It is the
// ONE seam this package has onto the feature: nil (the default, and every
// solo node) means the guard is not installed at all and the handler chain
// is byte-identical to an ungoverned build.
type GovernanceProvider func(ctx context.Context) govern.Effective
