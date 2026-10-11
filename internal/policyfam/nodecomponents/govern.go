package nodecomponents

import (
	"github.com/marmutapp/superbased-observer/internal/components"
)

// Source names which component policy is in force on a node (plan 6.2 rows
// 9 to 11 and 5.6.1). It is a closed enum for posture and doctor.
type Source string

// The closed Source vocabulary.
const (
	// SourceLocal: an individual node (or no enrolment): the local
	// selection alone decides (plan 6.1), under the ceiling if one is pinned.
	SourceLocal Source = "local"
	// SourceDormant: a managed enrolment whose grant expired, whose
	// identity changed, or that was offboarded (row 11): component policy
	// goes dormant and the node returns to its local selection. The caller
	// audits and shows the transition.
	SourceDormant Source = "dormant"
	// SourceUnauthorized: a live managed grant that does not carry the
	// components.govern authority. Nothing the org publishes in this family
	// is honoured; the node keeps its compiled set (no widening, no
	// narrowing) exactly like SourceLegacyPinned.
	SourceUnauthorized Source = "unauthorized"
	// SourcePolicy: the current verified node.components policy (rows 4 to
	// 8).
	SourcePolicy Source = "policy"
	// SourceLastKnownGood: the current policy expired or did not verify
	// while the grant is live and the identity matches; the last policy
	// that verified stays in force (row 10).
	SourceLastKnownGood Source = "last_known_good"
	// SourceInitialGrant: no policy ever received; the enrolment grant's
	// signed initial_components set applies, so there is no fail-open
	// window (row 9).
	SourceInitialGrant Source = "initial_grant"
	// SourceLegacyPinned: a managed node enrolled before this family
	// existed (its grant carries no initial_components) and no policy yet:
	// pinned to the compiled set it was running, no widening, no narrowing,
	// no silent change (plan 5.6.1, fourth-pass M8).
	SourceLegacyPinned Source = "legacy_pinned"
)

// Facts are the booleans the source table reads. Govern derives them from
// Inputs, so a caller cannot state a fact the inputs contradict.
type Facts struct {
	// Managed: a managed-tenancy enrolment (govern ConsentMode managed).
	Managed bool
	// Live: the enrolment grant is in force for this identity
	// (internal/govern.Resolve says so: not expired, key pin and identity
	// match, not offboarded).
	Live bool
	// Authorized: the live grant carries Authority (components.govern),
	// including any managed-family backfill internal/govern applies.
	Authorized bool
	// HavePolicy: a current node.components policy verified and compiled.
	HavePolicy bool
	// HaveLastKnownGood: an earlier verified policy is stored.
	HaveLastKnownGood bool
	// HaveInitial: the grant carries an initial_components set.
	HaveInitial bool
}

// sourceRule is one row of the ordered source table: the first row whose
// match is true decides.
type sourceRule struct {
	name   string
	match  func(Facts) bool
	source Source
}

// sourceRules is the ordered table (CLAUDE.md #5), one test row each.
var sourceRules = []sourceRule{
	{name: "not a managed enrolment", source: SourceLocal, match: func(f Facts) bool { return !f.Managed }},
	{name: "grant not live (expired, identity changed, offboarded)", source: SourceDormant, match: func(f Facts) bool { return !f.Live }},
	{name: "grant lacks components.govern", source: SourceUnauthorized, match: func(f Facts) bool { return !f.Authorized }},
	{name: "current verified policy", source: SourcePolicy, match: func(f Facts) bool { return f.HavePolicy }},
	{name: "last known good policy", source: SourceLastKnownGood, match: func(f Facts) bool { return f.HaveLastKnownGood }},
	{name: "grant initial set, no policy yet", source: SourceInitialGrant, match: func(f Facts) bool { return f.HaveInitial }},
	{name: "legacy managed enrolment, no policy yet", source: SourceLegacyPinned, match: func(Facts) bool { return true }},
}

// SelectSource walks the source table.
func SelectSource(f Facts) (Source, string) {
	for _, r := range sourceRules {
		if r.match(f) {
			return r.source, r.name
		}
	}
	return SourceLegacyPinned, "" // unreachable: the last row always matches
}

// Inputs is everything Govern needs, already verified by the caller (the
// signature, key-pin and generation checks of the policy-resource rail and
// of the grant live outside this pure package).
type Inputs struct {
	// Managed, Live and Authorized: see Facts.
	Managed    bool
	Live       bool
	Authorized bool
	// Current is the current verified policy, or nil.
	Current *PolicySpec
	// LastKnownGood is the last policy that verified, or nil.
	LastKnownGood *PolicySpec
	// Initial is the grant's signed initial_components ids, or nil when the
	// grant carries none (a legacy grant, or an old server). A non-nil empty
	// slice is a real set: core only.
	Initial []string
	// Pinned is the compiled set recorded for a legacy managed node (lane
	// MP-REL backfills it from the running binary at first start). Nil means
	// the running binary's compiled set.
	Pinned []string
	// Compiled is the running binary's compiled node set (nil means every
	// node row, as components.Resolve treats it).
	Compiled []string
	// CeilingPinned and Ceiling come from internal/managedconfig.
	CeilingPinned bool
	Ceiling       []string
	// Granted are ids of verified, unexpired component_enable grants.
	Granted []string
}

// Outcome is Govern's result.
type Outcome struct {
	Source Source
	// Rule names the source-table row that decided.
	Rule string
	// Governance is the resolver input.
	Governance components.Governance
	// Spec is the policy in force for SourcePolicy and SourceLastKnownGood
	// (its OnRemove and Distro apply), nil otherwise.
	Spec *PolicySpec
}

// Govern picks the policy in force and returns the components.Resolve
// input. The local ceiling applies in every source, including an
// individual node: it is the machine owner's MDM decision, not the org's.
// Component grants apply only while a policy (or the grant's initial set)
// governs.
func Govern(in Inputs) Outcome {
	f := Facts{
		Managed:           in.Managed,
		Live:              in.Live,
		Authorized:        in.Authorized,
		HavePolicy:        in.Current != nil,
		HaveLastKnownGood: in.LastKnownGood != nil,
		HaveInitial:       in.Initial != nil,
	}
	src, rule := SelectSource(f)
	out := Outcome{Source: src, Rule: rule}
	out.Governance.CeilingPinned = in.CeilingPinned
	if in.CeilingPinned {
		out.Governance.Ceiling = uniqSorted(in.Ceiling)
	}
	switch src {
	case SourcePolicy:
		out.Spec = in.Current
	case SourceLastKnownGood:
		out.Spec = in.LastKnownGood
	}
	switch {
	case out.Spec != nil:
		sets := out.Spec.Sets(string(src))
		out.Governance.Policy = &sets
		out.Governance.Granted = uniqSorted(in.Granted)
	case src == SourceInitialGrant:
		out.Governance.Policy = pinnedSets(src, in.Initial)
		out.Governance.Granted = uniqSorted(in.Granted)
	case src == SourceLegacyPinned || src == SourceUnauthorized:
		pinned := in.Pinned
		if pinned == nil {
			pinned = in.Compiled
		}
		if pinned == nil {
			pinned = components.Compiled(components.BinaryNode)
		}
		out.Governance.Policy = pinnedSets(src, pinned)
	}
	return out
}

// pinnedSets expresses an org-owned set (a legacy pin, an unauthorized
// grant's pin, the grant's initial set) as REQUIRE, never as allow (ledger
// MODPKG-7, T-677). Allow only lifts the unlisted block and leaves the
// choice to the local selection, so a local removal would still narrow the
// node; require makes each id active whatever the local block says (no
// narrowing) while every id outside the set stays unlisted, which is inert
// (no widening). Core needs no entry: it is always active.
func pinnedSets(src Source, ids []string) *components.PolicySets {
	return &components.PolicySets{Source: string(src), Require: uniqSorted(knownNonCore(ids))}
}

// knownNonCore is knownNodeIDs without core rows (core is never governed).
func knownNonCore(ids []string) []string {
	out := []string{}
	for _, id := range knownNodeIDs(ids) {
		if c, _ := components.Get(id); c.Class != components.ClassCore {
			out = append(out, id)
		}
	}
	return out
}

// knownNodeIDs drops ids that are not node components of this catalog.
func knownNodeIDs(ids []string) []string {
	var out []string
	for _, id := range ids {
		if c, ok := components.Get(id); ok && c.Binary == components.BinaryNode {
			out = append(out, id)
		}
	}
	if out == nil {
		out = []string{}
	}
	return out
}
