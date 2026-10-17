package orgintel

import (
	"time"

	"github.com/marmutapp/superbased-observer/internal/cloudcontract"
	"github.com/marmutapp/superbased-observer/internal/orgcontract"
)

// Node classes (design section 1.1).
const (
	// ClassUnenrolled is a node with no enrolment (class U): the personal
	// plane only.
	ClassUnenrolled = "unenrolled"
	// ClassIndividual is an individual-tenancy enrolment (class I): the org
	// plane only with the developer's own opt-in.
	ClassIndividual = "individual"
	// ClassManaged is a managed-tenancy enrolment (class M): the org is the
	// controller; the developer is shown a notice, not asked for consent.
	ClassManaged = "managed"
)

// Pending reasons.
const (
	// PendingNotAccepted: the org requires consent and this node never
	// accepted (or withdrew, or left and re-enrolled).
	PendingNotAccepted = "not_accepted"
	// PendingTermsChanged: the org widened what the requirement covers since
	// the developer accepted; enrichment continues under the OLD coverage
	// until they accept again (design section 3.7 step 6).
	PendingTermsChanged = "terms_changed"
)

// Consent is the node's ACTIVE consent record (the latest org_intel_consent
// event, when it is an accept).
type Consent struct {
	RequirementVersion int64
	EvidenceLevel      string
	Purposes           []string
	DisclosureDigest   string
	AcceptedAt         time.Time
}

// Inputs is everything the decision reads, resolved at the boundary.
type Inputs struct {
	// Enrolled and Managed describe the live enrolment.
	Enrolled bool
	Managed  bool
	// Requirement is the cached, verified requirement body (nil when the
	// org server has not sent one: an older server, or not fetched yet).
	Requirement *orgcontract.IntelRequirementBody
	// ConfigOptIn is the node-authored [intelligence].org_enrichment key.
	ConfigOptIn bool
	// Active is the node's active consent (nil when none).
	Active *Consent
	// EverAccepted reports this node has accepted this org's terms at least
	// once, so an opt-out now is a withdrawal; a node that never accepted has
	// nothing to report and its member stays "outstanding" on the server.
	EverAccepted bool
	// Now is the clock for the deadline.
	Now time.Time
}

// Status is the derived node-side org-plane state.
type Status struct {
	Class string `json:"class"`
	// OrgEnabled reports the org turned session enrichment on.
	OrgEnabled bool `json:"org_enabled"`
	// Required reports the org made the individual-tenancy opt-in a
	// condition of membership.
	Required bool `json:"required"`
	// Pending is true when a compulsory requirement awaits this developer's
	// Accept (individual class only).
	Pending       bool   `json:"pending"`
	PendingReason string `json:"pending_reason,omitempty"`
	// Covered reports the active consent covers the org's current level and
	// purposes.
	Covered bool `json:"covered"`
	// EffectiveOptIn is the individual node's effective org-plane opt-in
	// (INV-13: config key AND a consent record). Always false on other
	// classes, whose org plane is not the developer's opt-in.
	EffectiveOptIn bool `json:"effective_opt_in"`
	// NoticeOnly is true on a managed node: the developer is shown what the
	// org runs, never asked.
	NoticeOnly bool `json:"notice_only"`
	// DeadlineAt is when the org will start refusing this node's pushes if
	// the requirement is still pending (zero when there is no deadline).
	DeadlineAt     time.Time `json:"deadline_at,omitempty"`
	DeadlinePassed bool      `json:"deadline_passed"`
}

// covers reports whether an accepted consent covers the requirement's level
// and every purpose it allows.
func covers(c *Consent, req *orgcontract.IntelRequirementBody) bool {
	if c == nil || req == nil {
		return false
	}
	if !cloudcontract.EvidenceLevel(c.EvidenceLevel).Covers(cloudcontract.EvidenceLevel(req.EvidenceLevel)) {
		return false
	}
	have := map[string]bool{}
	for _, p := range c.Purposes {
		have[p] = true
	}
	for _, p := range req.Purposes {
		if !have[p] {
			return false
		}
	}
	return true
}

// pendingRule is one row of the ordered pending table (CLAUDE.md #5).
type pendingRule struct {
	name   string
	match  func(Inputs) bool
	reason string
}

// pendingRules decides whether an individual node has a pending compulsory
// requirement. The first matching row decides; reason "" means not pending.
var pendingRules = []pendingRule{
	{"no_requirement_document", func(in Inputs) bool { return in.Requirement == nil }, ""},
	{"not_required", func(in Inputs) bool { return !in.Requirement.RequireIndividualConsent }, ""},
	{"never_accepted", func(in Inputs) bool { return in.Active == nil }, PendingNotAccepted},
	{"opt_in_switched_off", func(in Inputs) bool { return !in.ConfigOptIn }, PendingNotAccepted},
	{"older_version_accepted", func(in Inputs) bool {
		return in.Active.RequirementVersion < in.Requirement.RequirementVersion
	}, PendingTermsChanged},
	{"current_version_accepted", func(Inputs) bool { return true }, ""},
}

// PendingRuleNames lists the pending table's rows in order.
func PendingRuleNames() []string {
	out := make([]string, 0, len(pendingRules))
	for _, r := range pendingRules {
		out = append(out, r.name)
	}
	return out
}

// Derive computes the node's org-plane status.
func Derive(in Inputs) Status {
	switch {
	case !in.Enrolled:
		return Status{Class: ClassUnenrolled}
	case in.Managed:
		st := Status{Class: ClassManaged, NoticeOnly: true}
		if in.Requirement != nil {
			st.OrgEnabled = in.Requirement.Enabled
		}
		return st
	}
	st := Status{Class: ClassIndividual}
	if in.Requirement != nil {
		st.OrgEnabled = in.Requirement.Enabled
		st.Required = in.Requirement.RequireIndividualConsent
	}
	for _, r := range pendingRules {
		if r.match(in) {
			st.PendingReason = r.reason
			st.Pending = r.reason != ""
			break
		}
	}
	st.Covered = covers(in.Active, in.Requirement)
	// INV-13: the effective opt-in needs the node-authored key AND a
	// node-side consent record.
	st.EffectiveOptIn = in.ConfigOptIn && in.Active != nil
	if st.Pending && in.Requirement.DeadlineDays > 0 {
		if since, err := time.Parse(time.RFC3339, in.Requirement.RequiredSince); err == nil {
			st.DeadlineAt = since.Add(time.Duration(in.Requirement.DeadlineDays) * 24 * time.Hour).UTC()
			st.DeadlinePassed = !in.Now.Before(st.DeadlineAt)
		}
	}
	return st
}

// Report is the consent report an individual node sends its org server: its
// effective opt-in and the coverage it accepted. ok=false for every other
// class (a managed node's enrichment does not depend on a report; an
// unenrolled node has no org), and for an individual node that has never
// accepted: it has no answer yet, which the org counts as outstanding rather
// than as a withdrawal.
func Report(in Inputs, st Status) (orgcontract.IntelConsentReport, bool) {
	if st.Class != ClassIndividual {
		return orgcontract.IntelConsentReport{}, false
	}
	if !st.EffectiveOptIn || in.Active == nil {
		if in.Active == nil && !in.EverAccepted {
			return orgcontract.IntelConsentReport{}, false
		}
		return orgcontract.IntelConsentReport{OptIn: false, AcceptedPurposes: []string{}}, true
	}
	purposes := append([]string(nil), in.Active.Purposes...)
	if purposes == nil {
		purposes = []string{}
	}
	return orgcontract.IntelConsentReport{
		OptIn:                      true,
		AcceptedRequirementVersion: in.Active.RequirementVersion,
		AcceptedLevel:              in.Active.EvidenceLevel,
		AcceptedPurposes:           purposes,
		DisclosureDigest:           in.Active.DisclosureDigest,
	}, true
}

// SubmitPlan is whether, and at what level and purpose, this node may submit
// node-built org-plane evidence (the B1 rail, parity phase 2).
type SubmitPlan struct {
	OK      bool
	Level   cloudcontract.EvidenceLevel
	Purpose cloudcontract.Purpose
	// Reason names the submitRules row that refused (empty when OK).
	Reason string
}

// submitInputs is what the submit table reads.
type submitInputs struct {
	in     Inputs
	st     Status
	railOn bool
}

// submitRules is the ordered refusal table for org-plane submission; the first
// match refuses, no match allows (CLAUDE.md #5).
var submitRules = []struct {
	name  string
	match func(submitInputs) bool
}{
	{"not_enrolled", func(s submitInputs) bool { return s.st.Class == ClassUnenrolled }},
	{"no_terms", func(s submitInputs) bool { return s.in.Requirement == nil }},
	{"org_off", func(s submitInputs) bool { return !s.in.Requirement.Enabled }},
	{"managed_rail_off", func(s submitInputs) bool { return s.st.Class == ClassManaged && !s.railOn }},
	{"individual_not_opted_in", func(s submitInputs) bool { return s.st.Class == ClassIndividual && !s.st.EffectiveOptIn }},
}

// SubmitRuleNames lists the submit table's refusal rows in order.
func SubmitRuleNames() []string {
	out := make([]string, 0, len(submitRules))
	for _, r := range submitRules {
		out = append(out, r.name)
	}
	return out
}

// PlanSubmit decides org-plane submission. The level is the org's level on a
// managed node; on an individual node it is the LOWER of the org's level and
// the level the developer accepted (a widened requirement continues under the
// old coverage until re-accepted, design 3.7 step 6), and the purpose must be
// one the developer accepted. A level whose purpose the org does not allow
// falls back to structural when that is allowed. railOn is the daemon's result
// rail gate (the managed raise lives there).
func PlanSubmit(in Inputs, railOn bool) SubmitPlan {
	st := Derive(in)
	si := submitInputs{in: in, st: st, railOn: railOn}
	for _, r := range submitRules {
		if r.match(si) {
			return SubmitPlan{Reason: r.name}
		}
	}
	level := cloudcontract.NormalizeEvidenceLevel(in.Requirement.EvidenceLevel, cloudcontract.EvidenceLevelStructural)
	allowed := map[string]bool{}
	for _, p := range in.Requirement.Purposes {
		allowed[p] = true
	}
	if st.Class == ClassIndividual && in.Active != nil {
		accepted := cloudcontract.NormalizeEvidenceLevel(in.Active.EvidenceLevel, cloudcontract.EvidenceLevelStructural)
		level = cloudcontract.MinEvidenceLevel(level, accepted)
		mine := map[string]bool{}
		for _, p := range in.Active.Purposes {
			mine[p] = true
		}
		for p := range allowed {
			if !mine[p] {
				delete(allowed, p)
			}
		}
	}
	if !allowed[string(level.Purpose())] {
		level = cloudcontract.EvidenceLevelStructural
	}
	if !allowed[string(level.Purpose())] {
		return SubmitPlan{Reason: "purpose_not_allowed"}
	}
	return SubmitPlan{OK: true, Level: level, Purpose: level.Purpose()}
}

// AcceptPlan is what the interactive accept path records: exactly the
// coverage the disclosure it showed describes.
type AcceptPlan struct {
	RequirementVersion int64
	EvidenceLevel      string
	Purposes           []string
	Disclosure         string
	DisclosureDigest   string
}

// PlanAccept builds the consent record for a requirement body. The level and
// purposes are the org's current ones (what the disclosure describes); an
// unknown level is recorded as structural so a consent can never cover more
// than the text the developer read.
func PlanAccept(req orgcontract.IntelRequirementBody) AcceptPlan {
	text, digest := orgcontract.IntelDisclosure(req)
	level := cloudcontract.NormalizeEvidenceLevel(req.EvidenceLevel, cloudcontract.EvidenceLevelStructural)
	purposes := append([]string(nil), req.Purposes...)
	if len(purposes) == 0 {
		purposes = []string{string(cloudcontract.PurposeStructuralInsights)}
	}
	return AcceptPlan{
		RequirementVersion: req.RequirementVersion,
		EvidenceLevel:      string(level),
		Purposes:           purposes,
		Disclosure:         text,
		DisclosureDigest:   digest,
	}
}
