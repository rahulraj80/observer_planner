package dashapi

import (
	"net/http"
)

// OrgIntelDigestView is one digest as the page renders it.
type OrgIntelDigestView struct {
	ProjectID             string   `json:"project_id"`
	ProjectLabel          string   `json:"project_label,omitempty"`
	WeekStart             string   `json:"week_start"`
	PeriodEnd             string   `json:"period_end,omitempty"`
	SessionCount          int      `json:"session_count"`
	Headline              string   `json:"headline"`
	Themes                []string `json:"themes,omitempty"`
	RecurringErrorClasses []string `json:"recurring_error_classes,omitempty"`
	UnfinishedThreads     []string `json:"unfinished_threads,omitempty"`
	SuggestedNextSession  string   `json:"suggested_next_session,omitempty"`
	UpdatedAt             string   `json:"updated_at,omitempty"`
}

// OrgIntelDigests is the GET /api/intel/digests body. State is the pull's
// outcome in the result rail's vocabulary (ok, not_enrolled, not_supported,
// auth_failed, channel_off, unreachable, malformed).
type OrgIntelDigests struct {
	State    string               `json:"state"`
	OrgName  string               `json:"org_name,omitempty"`
	Projects int                  `json:"projects"`
	Withheld string               `json:"withheld,omitempty"`
	Digests  []OrgIntelDigestView `json:"digests"`
}

// OrgIntelDigestReader is the optional cmd-wired digest seam. It is asserted
// on Options.OrgIntel, so a manager without it answers 501 and no existing
// implementation has to change.
type OrgIntelDigestReader interface {
	ProjectDigests(r *http.Request) (OrgIntelDigests, error)
}
