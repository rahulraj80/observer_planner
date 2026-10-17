package dashapi

// orgIntelResultWire is one cached enrichment result on the wire. The node
// cache carries no provider / model / tokens / cost - only the content the org
// derived - so those fields are absent here by construction (the shared
// IntelResultCard omits what is absent rather than zeroing it).
type OrgIntelResultWire struct {
	SessionID     string   `json:"session_id"`
	JobID         string   `json:"job_id"`
	Title         string   `json:"title"`
	Description   string   `json:"description"`
	TaxonomyTags  []string `json:"taxonomy_tags"`
	SuggestedTags []string `json:"suggested_tags"`
	Limitations   []string `json:"limitations"`
	// The five NARRATIVE lists the org derived (agent migration 124): what the
	// session did, whether the stated plans landed, what issues were found,
	// what failed, what to do next. omitempty, so a row cached before 124 - or
	// pulled from a server predating server migration 156 - renders as absence
	// in the shared IntelResultCard rather than as five empty headings.
	WorkDone         []string `json:"work_done,omitempty"`
	PlansImplemented []string `json:"plans_implemented,omitempty"`
	IssuesFound      []string `json:"issues_found,omitempty"`
	Failures         []string `json:"failures,omitempty"`
	NextSteps        []string `json:"next_steps,omitempty"`
	Confidence       string   `json:"confidence"`
	SchemaVersion    string   `json:"schema_version"`
	FetchedAt        string   `json:"fetched_at"`
}
