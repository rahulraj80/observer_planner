package agentconfigorg

import (
	"github.com/marmutapp/superbased-observer/internal/agentconfig"
	"github.com/marmutapp/superbased-observer/internal/orgcontract"
)

// Placement is where an organization policy can set one catalogue setting:
// the tool's user-scope file, a project's personal (local-only) file, or a
// project's shared file. It answers "can the org lock this setting, and
// where" without a value, so a reference doc and the publish lint never
// disagree: both walk the same settingRules.
type Placement struct {
	User          bool `json:"user"`
	ProjectLocal  bool `json:"project_local"`
	ProjectShared bool `json:"project_shared"`
	// Refusal is the lint code that refuses the user-level placement
	// (secret_setting, not_writable, observer_span, no_user_file,
	// unknown_setting); "" when User is true.
	Refusal string `json:"refusal,omitempty"`
}

// Any reports whether some placement is open to the org policy.
func (p Placement) Any() bool { return p.User || p.ProjectLocal || p.ProjectShared }

// placementProbeProject stands in for a project identity: settingRules only
// look at whether a project is named, never at which one.
const placementProbeProject = "0000000000000000000000000000000000000000000000000000000000000000"

// PlacementFor reports where an org policy entry for settingID passes the
// per-setting publish rules. Value checks (type, range, secret-looking
// values) are not part of it: they depend on the value an admin types.
// Warnings (unverified, dangerous) never close a placement.
func PlacementFor(settingID string) Placement {
	r, ok := agentconfig.Setting(settingID)
	if !ok {
		return Placement{Refusal: "unknown_setting"}
	}
	refusal := func(e Entry) string {
		for _, rule := range settingRules {
			if code, sev, _ := rule(r, e); code != "" && sev == SevError {
				return code
			}
		}
		return ""
	}
	base := Entry{Setting: settingID, Mode: orgcontract.AgentConfigModeEnforced}
	local, shared := base, base
	local.Project, local.Placement = placementProbeProject, orgcontract.AgentConfigPlacementLocalOnly
	shared.Project, shared.Placement = placementProbeProject, orgcontract.AgentConfigPlacementShared

	user := refusal(base)
	return Placement{
		User:          user == "",
		ProjectLocal:  refusal(local) == "",
		ProjectShared: refusal(shared) == "",
		Refusal:       user,
	}
}
