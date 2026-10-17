package orgcontract

// IntelDigestsPath is the agent rail a developer's node reads the weekly org
// project digests from (T-490, D-241, D-328 item 3): the digests of the
// projects THIS member works in, read-only. Like the result rail it is a GET
// bound by the per-request proof over this CONSTANT path
// (IntelRailSigningMessage, empty canonical query), so a captured proof for
// another rail never replays here. A server that predates the rail answers
// 404, which the node renders as "your org server does not offer digests".
const IntelDigestsPath = "/api/agent/intel/digests"

// IntelDigestRow is one finished weekly project digest as a developer sees
// it. Model prose over the org's enrichment results for the project and
// week; it carries no spend, no token counts, no provider and no member
// names (the digest prose is built from evidence that names none).
type IntelDigestRow struct {
	// ProjectID is the org's project key (the first 16 hex of the project
	// root hash), the same value the developer's own sessions carry.
	ProjectID string `json:"project_id"`
	// ProjectLabel is the admin-authored display label, when one is set.
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

// IntelDigestsResponse is the rail's body.
type IntelDigestsResponse struct {
	// Projects is how many projects the member was matched to (their own
	// sessions in the window), so an empty list can say why.
	Projects int              `json:"projects"`
	Digests  []IntelDigestRow `json:"digests"`
	// Withheld names why nothing is shown even though the rail answered:
	// IntelDigestsWithheldPosture when this member's own org posture does not
	// let the org disclose its derived results to them (an individual-tenancy
	// member without a covering consent, the same read scope the result rail
	// applies). Empty when digests were looked up.
	Withheld string `json:"withheld,omitempty"`
}

// IntelDigestsWithheldPosture is the Withheld value for a member outside the
// org's enrichment read scope.
const IntelDigestsWithheldPosture = "posture"
