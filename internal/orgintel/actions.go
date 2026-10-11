package orgintel

import "github.com/marmutapp/superbased-observer/internal/dataauthority"

// actions.go decides what a developer may do with ONE session on the org plane
// (Cloud Intelligence parity phase 3, design section 3.2): preview the exact
// evidence, and start an enrichment. Pure, table-driven (CLAUDE.md #5); the
// dashboard routes and `observer intel preview|enrich` both read it, so the
// two surfaces can never offer different actions.

// SessionActionPlan is the table's answer for one session.
type SessionActionPlan struct {
	// Preview reports the developer may build and view the org-plane evidence
	// for this session (nothing leaves the machine).
	Preview bool `json:"preview"`
	// Enrich reports the developer may start an enrichment of this session.
	Enrich bool `json:"enrich"`
	// NeedsPreview reports an Enrich must name the upload digest of a preview
	// of the current bytes (individual tenancy: the developer confirms what
	// they saw). A managed developer submits directly (a notice, no veto).
	NeedsPreview bool `json:"needs_preview"`
	// Reason names the first refusal row (empty when both actions are open).
	Reason string `json:"reason,omitempty"`
	// Submit is the level and purpose a submission is built at.
	Submit SubmitPlan `json:"-"`
}

type actionInputs struct {
	plan      SubmitPlan
	authority dataauthority.Authority
	managed   bool
	devAction bool
}

// sessionActionRules is the ordered table: the first matching row decides.
var sessionActionRules = []struct {
	name   string
	match  func(actionInputs) bool
	decide func(actionInputs) SessionActionPlan
}{
	{"not_org_session", func(a actionInputs) bool { return a.authority != dataauthority.AuthorityOrg }, func(actionInputs) SessionActionPlan {
		return SessionActionPlan{}
	}},
	{"not_available", func(a actionInputs) bool { return !a.plan.OK }, func(actionInputs) SessionActionPlan {
		return SessionActionPlan{}
	}},
	{"developer_actions_off", func(a actionInputs) bool { return !a.devAction }, func(actionInputs) SessionActionPlan {
		return SessionActionPlan{Preview: true}
	}},
	{"managed", func(a actionInputs) bool { return a.managed }, func(actionInputs) SessionActionPlan {
		return SessionActionPlan{Preview: true, Enrich: true}
	}},
	{"individual", func(actionInputs) bool { return true }, func(actionInputs) SessionActionPlan {
		return SessionActionPlan{Preview: true, Enrich: true, NeedsPreview: true}
	}},
}

// PlanSessionAction resolves the developer's actions for one session from the
// node's inputs, the rail gate and the session's data-authority stamp.
func PlanSessionAction(in Inputs, railOn bool, authority dataauthority.Authority) SessionActionPlan {
	plan := PlanSubmit(in, railOn)
	a := actionInputs{
		plan: plan, authority: authority, managed: in.Managed,
		devAction: in.Requirement != nil && in.Requirement.DeveloperActions,
	}
	for _, r := range sessionActionRules {
		if r.match(a) {
			out := r.decide(a)
			out.Submit = plan
			switch r.name {
			case "not_available":
				out.Reason = plan.Reason
			case "managed", "individual":
			default:
				out.Reason = r.name
			}
			return out
		}
	}
	return SessionActionPlan{Submit: plan, Reason: "unreachable"}
}

// SessionActionRuleNames lists the table's rows in order.
func SessionActionRuleNames() []string {
	out := make([]string, 0, len(sessionActionRules))
	for _, r := range sessionActionRules {
		out = append(out, r.name)
	}
	return out
}

// actionReasonText is the one sentence each refusal reads as, shared by the
// CLI and the dashboard.
var actionReasonText = map[string]string{
	"not_org_session":         "This session was not captured for your organization, so it is not part of org session enrichment.",
	"not_enrolled":            "This machine is not enrolled with an organization.",
	"no_terms":                "Your organization's server has not sent its session enrichment terms yet.",
	"org_off":                 "Your organization has not turned on session enrichment.",
	"managed_rail_off":        "Your organization has not turned on session enrichment for this machine.",
	"individual_not_opted_in": "You have not accepted your organization's session enrichment terms.",
	"purpose_not_allowed":     "Your organization does not allow a purpose your consent covers.",
	"developer_actions_off":   "Your organization runs session enrichment in the background only.",
}

// ActionReasonText returns the sentence for a refusal reason ("" for none).
func ActionReasonText(reason string) string {
	if reason == "" {
		return ""
	}
	if t, ok := actionReasonText[reason]; ok {
		return t
	}
	return "Session enrichment is not available for this session (" + reason + ")."
}
