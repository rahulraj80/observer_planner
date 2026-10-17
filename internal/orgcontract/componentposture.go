package orgcontract

import (
	"sort"
	"strings"
	"time"
)

// Node -> server COMPONENT POSTURE self-report (modular packaging plan 6.4,
// T-682).
//
// It answers the fleet compliance questions (plan 7.4) with enums only: which
// distro and catalog this binary was built from, a digest of the compiled set
// and of the active set, which governance source decided, and per component
// that is NOT active its state and reason. A component id is a catalog id (a
// closed vocabulary compiled into every binary), never free text; the server
// drops an id its own catalog does not know.
//
// WHAT IT DELIBERATELY DOES NOT CARRY: no path, no hostname, no config text,
// no policy body, no org name, no ceiling source path, no "because" detail
// (the resolver's Because names another component, which the server can
// recompute from the catalog), and no selection deltas from the local
// [components] block. Those are node-local. Active components are not listed
// one by one: ActiveCount and ActiveDigest say the same thing in a fixed
// size, and the non-active list is what compliance reads (required_missing,
// blocked, available, absent).
//
// Like UpdatePostureRow and BudgetPostureRow it is envelope-level, composed by
// a store function seam (internal/store/componentposture.go) so orgpush.go
// names no component table (there is none), and it ships in every share mode:
// it is metadata about the binary, not content. Its field set is pinned by
// tests/invariant/component_posture_test.go.
type ComponentPostureRow struct {
	// Distro is the prebuilt distro whose compiled set this binary matches
	// (minimal, developer, managed, full) or "custom".
	Distro string `json:"distro,omitempty"`
	// CatalogVersion is components.CatalogVersion of the binary.
	CatalogVersion int `json:"catalog_version,omitempty"`
	// ComponentsDigest is components.Digest of the compiled set: the same
	// value the update manifest, the release index and the SBOM carry, so
	// the org can match it against an artifact it published.
	ComponentsDigest string `json:"components_digest,omitempty"`
	// ActiveDigest is components.Digest of the active set.
	ActiveDigest string `json:"active_digest,omitempty"`
	// CompiledCount and ActiveCount size the two sets.
	CompiledCount int `json:"compiled_count,omitempty"`
	ActiveCount   int `json:"active_count,omitempty"`
	// GovernanceSource is the nodecomponents source in force: one of the
	// ComponentSource* values ("" when no governance was resolved).
	GovernanceSource string `json:"governance_source,omitempty"`
	// OrgManaged reports that an organization governs this node's
	// components (a managed enrolment with a live grant).
	OrgManaged bool `json:"org_managed,omitempty"`
	// CeilingPinned reports that a local component ceiling is in force.
	CeilingPinned bool `json:"ceiling_pinned,omitempty"`
	// Inactive lists every component that is not active, sorted by id.
	Inactive []ComponentStateEntry `json:"inactive,omitempty"`
	// ScheduledPurges is the node's scheduled component purges (T-711): per
	// component, the state of its latest plan a node.components policy
	// scheduled, while that plan is open or reached its end within
	// ScheduledPurgeReportWindow. It is a POINTER so "this node does not
	// report purges" (nil: an older node, or the plan table could not be
	// read) differs from "no scheduled purge" (a non-nil empty list): the
	// server keeps its stored list on nil and records an audit row only when
	// a reported list differs from it. Enum-only: an id, a closed state and a
	// UTC date, never counts, plan ids, table names or policy text.
	ScheduledPurges *[]ComponentPurgeEntry `json:"scheduled_purges,omitempty"`
}

// ComponentPurgeEntry is one component's scheduled purge as the org sees it.
type ComponentPurgeEntry struct {
	// ID is a catalog component id (an adapter row today: only adapters have
	// a reviewed purge plan).
	ID string `json:"id"`
	// State is one of the ComponentPurge* values.
	State string `json:"state"`
	// DueOn is the UTC date (YYYY-MM-DD) the plan's grace period ends, ""
	// when the plan carries none.
	DueOn string `json:"due_on,omitempty"`
}

// Scheduled purge state vocabulary (mirrors internal/store PurgeState for the
// scheduled kind; repeated here because orgcontract imports no store).
const (
	ComponentPurgeGracePending = "grace_pending"
	ComponentPurgeEligible     = "eligible"
	ComponentPurgeConsumed     = "consumed"
	ComponentPurgeExpired      = "expired"
	ComponentPurgeCancelled    = "cancelled"
)

// ScheduledPurgeReportWindowDays bounds how long a finished scheduled purge
// (consumed, expired, cancelled) stays in the node's report, so the org sees
// how it ended before it drops off.
const ScheduledPurgeReportWindowDays = 30

// ComponentStateEntry is one non-active component: three closed values.
type ComponentStateEntry struct {
	// ID is a catalog component id.
	ID string `json:"id"`
	// State is one of the ComponentState* values.
	State string `json:"state"`
	// Reason is one of the ComponentReason* values.
	Reason string `json:"reason,omitempty"`
}

// Component state vocabulary (mirrors internal/components State; repeated
// here because orgcontract is the wire's one owner and imports no catalog).
const (
	ComponentStateAvailable       = "available"
	ComponentStateAbsent          = "absent"
	ComponentStateBlocked         = "blocked"
	ComponentStateRequiredMissing = "required_missing"
)

// Component reason vocabulary (mirrors internal/components Reason).
const (
	ComponentReasonNotCompiled         = "not_compiled"
	ComponentReasonDenied              = "denied"
	ComponentReasonDeniedDependency    = "denied_dependency"
	ComponentReasonRequiresNotCompiled = "requires_not_compiled"
	ComponentReasonNotSelected         = "not_selected"
	ComponentReasonAboveCeiling        = "above_ceiling"
	ComponentReasonPolicyDenied        = "policy_denied"
	ComponentReasonPolicyUnlisted      = "policy_unlisted"
	ComponentReasonPolicyRequired      = "policy_required"
)

// Component governance source vocabulary (mirrors
// internal/policyfam/nodecomponents Source).
const (
	ComponentSourceLocal         = "local"
	ComponentSourceDormant       = "dormant"
	ComponentSourceUnauthorized  = "unauthorized"
	ComponentSourcePolicy        = "policy"
	ComponentSourceLastKnownGood = "last_known_good"
	ComponentSourceInitialGrant  = "initial_grant"
	ComponentSourceLegacyPinned  = "legacy_pinned"
)

var (
	componentStates = map[string]bool{
		ComponentStateAvailable: true, ComponentStateAbsent: true,
		ComponentStateBlocked: true, ComponentStateRequiredMissing: true,
	}
	componentReasons = map[string]bool{
		"": true, ComponentReasonNotCompiled: true, ComponentReasonDenied: true,
		ComponentReasonDeniedDependency: true, ComponentReasonRequiresNotCompiled: true,
		ComponentReasonNotSelected: true, ComponentReasonAboveCeiling: true,
		ComponentReasonPolicyDenied: true, ComponentReasonPolicyUnlisted: true,
		ComponentReasonPolicyRequired: true,
	}
	componentSources = map[string]bool{
		"": true, ComponentSourceLocal: true, ComponentSourceDormant: true,
		ComponentSourceUnauthorized: true, ComponentSourcePolicy: true,
		ComponentSourceLastKnownGood: true, ComponentSourceInitialGrant: true,
		ComponentSourceLegacyPinned: true,
	}
	componentDistros = map[string]bool{
		"": true, "minimal": true, "developer": true, "managed": true, "full": true, "custom": true,
	}
	componentPurgeStates = map[string]bool{
		ComponentPurgeGracePending: true, ComponentPurgeEligible: true, ComponentPurgeConsumed: true,
		ComponentPurgeExpired: true, ComponentPurgeCancelled: true,
	}
)

// maxComponentEntries bounds Inactive (the catalog has about 90 node rows).
const maxComponentEntries = 256

// NormalizeComponentPosture returns r with every field forced into its closed
// vocabulary: an unknown distro or source becomes "", a digest that is not 64
// lowercase hex becomes "", an entry whose id fails known (or whose state or
// reason is outside the vocabulary) is dropped, duplicates are dropped, and
// the list is sorted and bounded. known may be nil (no id check). Both the
// node composer and the server writer call it, so a value outside the
// vocabulary can never be stored.
func NormalizeComponentPosture(r ComponentPostureRow, known func(id string) bool) ComponentPostureRow {
	out := ComponentPostureRow{
		CatalogVersion: r.CatalogVersion,
		CompiledCount:  r.CompiledCount,
		ActiveCount:    r.ActiveCount,
		OrgManaged:     r.OrgManaged,
		CeilingPinned:  r.CeilingPinned,
	}
	if componentDistros[r.Distro] {
		out.Distro = r.Distro
	}
	if componentSources[r.GovernanceSource] {
		out.GovernanceSource = r.GovernanceSource
	}
	if isHex64(r.ComponentsDigest) {
		out.ComponentsDigest = r.ComponentsDigest
	}
	if isHex64(r.ActiveDigest) {
		out.ActiveDigest = r.ActiveDigest
	}
	if out.CatalogVersion < 0 {
		out.CatalogVersion = 0
	}
	if out.CompiledCount < 0 {
		out.CompiledCount = 0
	}
	if out.ActiveCount < 0 {
		out.ActiveCount = 0
	}
	seen := map[string]bool{}
	for _, e := range r.Inactive {
		id := strings.TrimSpace(e.ID)
		if id == "" || seen[id] || !componentStates[e.State] || !componentReasons[e.Reason] {
			continue
		}
		if known != nil && !known(id) {
			continue
		}
		seen[id] = true
		out.Inactive = append(out.Inactive, ComponentStateEntry{ID: id, State: e.State, Reason: e.Reason})
	}
	sort.Slice(out.Inactive, func(i, j int) bool { return out.Inactive[i].ID < out.Inactive[j].ID })
	if len(out.Inactive) > maxComponentEntries {
		out.Inactive = out.Inactive[:maxComponentEntries]
	}
	if r.ScheduledPurges != nil {
		purges := NormalizeComponentPurges(*r.ScheduledPurges, known)
		out.ScheduledPurges = &purges
	}
	return out
}

// NormalizeComponentPurges forces a scheduled purge list into its closed
// vocabulary: an entry with an unknown or failing id, a state outside the
// vocabulary, or a due date that is not YYYY-MM-DD is dropped, duplicate ids
// are dropped (first wins), and the list is sorted by id and bounded. The
// result is never nil, so a reported empty list stays reported.
func NormalizeComponentPurges(in []ComponentPurgeEntry, known func(id string) bool) []ComponentPurgeEntry {
	out := []ComponentPurgeEntry{}
	seen := map[string]bool{}
	for _, e := range in {
		id := strings.TrimSpace(e.ID)
		if id == "" || seen[id] || !componentPurgeStates[e.State] || (e.DueOn != "" && !isISODate(e.DueOn)) {
			continue
		}
		if known != nil && !known(id) {
			continue
		}
		seen[id] = true
		out = append(out, ComponentPurgeEntry{ID: id, State: e.State, DueOn: e.DueOn})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	if len(out) > maxComponentEntries {
		out = out[:maxComponentEntries]
	}
	return out
}

// isISODate reports whether s is a calendar date in YYYY-MM-DD form.
func isISODate(s string) bool {
	if len(s) != 10 || s[4] != '-' || s[7] != '-' {
		return false
	}
	if _, err := time.Parse("2006-01-02", s); err != nil {
		return false
	}
	return true
}

func isHex64(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
