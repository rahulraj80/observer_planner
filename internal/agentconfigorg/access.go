package agentconfigorg

import (
	"strings"

	"github.com/marmutapp/superbased-observer/internal/agentconfig"
	"github.com/marmutapp/superbased-observer/internal/orgcontract"
)

// ManagedReadOnlyReason is the note on a managed node whose organization
// granted no editing (D-102).
const ManagedReadOnlyReason = "Your organization manages this machine. AI tool settings are read-only until your organization grants editing."

// AccessFor derives the developer's write posture on a MANAGED node from the
// org's verified policy body (plan section 6.8, D-102, D-122):
//
//   - every ENFORCED setting is locked, naming the policy that sets it, so
//     the form, the raw editor and the CLI all refuse it (org_locked);
//   - developer access "editable" opens everything else; "read_only" (the
//     default, and what an unknown value normalizes to) opens only what a
//     grant names, and with no grant at all the page is read-only;
//   - the plaintext-secret posture is the org's (D-104).
//
// A default the developer holds a grant for stays editable: the developer's
// own value then wins over the org default (D-122).
func AccessFor(body orgcontract.AgentConfigPolicyBody) agentconfig.Access {
	a := agentconfig.Access{SecretsPosture: secretsPosture(body.SecretsPosture)}
	for _, d := range body.Directives {
		if d.Mode != orgcontract.AgentConfigModeEnforced {
			continue
		}
		if a.Locked == nil {
			a.Locked = map[string]string{}
		}
		name := d.Policy
		if name == "" {
			name = "(unnamed)"
		}
		a.Locked[d.Setting] = name
	}
	// Org instruction blocks are always the org's (D-107): the developer
	// edits the text around them, never inside (T-426).
	var names []string
	for _, in := range body.Instructions {
		if in.Policy != "" && !containsString(names, in.Policy) {
			names = append(names, in.Policy)
		}
	}
	if len(body.Instructions) > 0 {
		a.OrgBlockLocked = strings.Join(names, ", ")
		if a.OrgBlockLocked == "" {
			a.OrgBlockLocked = "(unnamed)"
		}
	}
	if orgcontract.NormalizeAgentConfigAccess(body.DeveloperAccess) == orgcontract.AgentConfigAccessEditable {
		return a
	}
	if len(body.Grants) == 0 {
		a.ReadOnly, a.Reason = true, ManagedReadOnlyReason
		return a
	}
	a.GrantsOnly = true
	for _, g := range body.Grants {
		a.Grants = append(a.Grants, g.Setting)
	}
	return a
}

func secretsPosture(v string) agentconfig.SecretsPosture {
	switch orgcontract.NormalizeAgentConfigSecrets(v) {
	case orgcontract.AgentConfigSecretsRefuse:
		return agentconfig.SecretsRefuse
	case orgcontract.AgentConfigSecretsAllow:
		return agentconfig.SecretsAllow
	}
	return agentconfig.SecretsWarn
}

// PagePinnedReason is the note when the organization pinned the page
// read-only (D-130).
const PagePinnedReason = "This page is pinned by your organization and cannot be changed here."

// NodeAccessInput is what the dashboard and the CLI know about this node.
type NodeAccessInput struct {
	// Managed: a managed enrolment holding a live grant
	// (govern.Effective.ManagedDefaultsApply).
	Managed bool
	// Body is the cached VERIFIED policy body; nil when none arrived yet.
	Body *orgcontract.AgentConfigPolicyBody
	// PageReadOnly: the dashboard visibility resolves the page read-only;
	// PageFromDefault: that came from the managed-node DEFAULT table, not an
	// explicit org directive.
	PageReadOnly, PageFromDefault bool
	// EditGrants are the targets of the honoured AI tool setting edit grants
	// this node holds (T-436): setting ids or "<tool>/*".
	EditGrants []string
}

// PagePinned reports whether the page's read-only level still holds: an
// explicit org pin always does; the managed DEFAULT (D-102, "read-only
// until the organization grants editing") yields once a verified AI tool
// policy is held, because that policy IS the grant (its developer access
// and grants then decide, setting by setting). Every enforcement point (the
// dashboard API guard, the visibility block, both editors) asks this.
func PagePinned(readOnly, fromDefault, policyHeld bool) bool {
	return readOnly && !(fromDefault && policyHeld)
}

// NodeAccess is the one write-posture rule both editor surfaces apply: an
// individual node edits freely; a managed node with no policy yet is
// read-only (D-102); a managed node with a policy gets AccessFor(body); a
// page pinned by an explicit org directive is read-only on any node that
// granted the pin (D-130).
func NodeAccess(in NodeAccessInput) agentconfig.Access {
	a := agentconfig.Access{SecretsPosture: agentconfig.SecretsWarn}
	if in.Managed {
		if in.Body != nil {
			a = AccessFor(*in.Body)
		} else {
			a.ReadOnly, a.Reason = true, ManagedReadOnlyReason
		}
	}
	if in.Managed {
		a = WithEditGrants(a, in.EditGrants)
	}
	// An edit grant is the organization's permission too: like a held
	// policy it supersedes the managed DEFAULT read-only page.
	held := in.Managed && (in.Body != nil || len(in.EditGrants) > 0)
	if PagePinned(in.PageReadOnly, in.PageFromDefault, held) && !a.ReadOnly {
		a.ReadOnly, a.Reason = true, PagePinnedReason
	}
	return a
}

// EditGranted reports whether an edit grant names the setting or its tool
// ("<tool>/*"), the same match agentconfig.Access.Granted makes.
func EditGranted(grants []string, settingID string) bool {
	tool, _, _ := strings.Cut(settingID, "/")
	for _, g := range grants {
		if g == settingID || g == tool+"/*" {
			return true
		}
	}
	return false
}

// WithEditGrants opens what the developer's honoured AI tool setting edit
// grants name (T-436): an ENFORCED setting a grant names is no longer locked
// (the developer's value wins until the grant ends, decide.go's
// grant_lifts_lock row), and a setting the policy granted no editing of
// becomes editable. It never lifts any other read-only (an org-pinned page,
// an unreadable posture). No grant leaves a unchanged.
func WithEditGrants(a agentconfig.Access, grants []string) agentconfig.Access {
	if len(grants) == 0 {
		return a
	}
	if len(a.Locked) > 0 {
		locked := map[string]string{}
		for k, v := range a.Locked {
			if !EditGranted(grants, k) {
				locked[k] = v
			}
		}
		if len(locked) == 0 {
			locked = nil
		}
		a.Locked = locked
	}
	switch {
	case a.ReadOnly && a.Reason == ManagedReadOnlyReason:
		a.ReadOnly, a.Reason = false, ""
		a.GrantsOnly, a.Grants = true, append([]string(nil), grants...)
	case a.GrantsOnly:
		a.Grants = append(append([]string(nil), a.Grants...), grants...)
	}
	return a
}
