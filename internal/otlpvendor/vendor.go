package otlpvendor

// Vendor is one row of the mapping table: one AI tool's OpenTelemetry export.
type Vendor struct {
	// Tool is the registry tool id the export's rows land under
	// (internal/integration, models.Tool*).
	Tool string
	// ServiceNames claim a resource whose service.name equals one of them.
	// A claimed resource is read with THIS vendor's rules only, so another
	// vendor's (or the GenAI-shaped) events inside it are never
	// double-counted.
	ServiceNames []string
	// ClaimByEventName lets the vendor's rules also read a resource that no
	// vendor claimed by service.name (an exporter that sets no service.name).
	// Only the Claude Code row sets it: its original parser read every
	// resource, and a configured OTEL_SERVICE_NAME must not stop it working.
	ClaimByEventName bool
	// Provider is the upstream provider stamped on a turn when the export
	// does not name one.
	Provider string
	// SourceTag is the provenance tag a turn persists with
	// (api_turns.source); the store maps it to a merge fidelity.
	SourceTag string
	// Grain is whose data one export carries (see Grain).
	Grain Grain
	// NoSessionRows keeps the export from creating session rows: its
	// sessions always come from the tool's hooks and transcript (Claude
	// Code, whose export has only ever landed on api_turns).
	NoSessionRows bool
	// Norm says how this vendor's token counts relate to the proxy-row
	// convention (see Normalize).
	Norm Normalize
	// Owner says which observation kinds this export owns while the tool's
	// own transcript adapter is ALSO capturing (see Ownership).
	Owner Ownership
	// ActionTypes maps a raw tool name to an Observer action type; a name
	// not listed falls back to models.ActionUnknown at the store boundary.
	ActionTypes map[string]string
	// Rules are walked top-down for every record of a claimed resource; a
	// record may match several rules of different kinds (a tool result is
	// both a tool call and tool-output content).
	Rules []Rule
	// Evidence records how the rule set was grounded (a live capture with
	// the version, or the vendor document) so a reader can tell a captured
	// shape from a documented one.
	Evidence string
}

// Normalize converts a vendor's token counts to the proxy-row convention.
type Normalize struct {
	// InputIncludesCacheRead: the vendor's input count already contains
	// the cache-read tokens (OpenAI, Gemini, GenAI semconv), so they are
	// subtracted to get the NET input.
	InputIncludesCacheRead bool
	// InputIncludesCacheWrite: the vendor's input count also contains the
	// cache-write tokens (GenAI semconv), so they are subtracted too.
	InputIncludesCacheWrite bool
	// OutputExcludesReasoning: the vendor reports reasoning separately from
	// output (Gemini candidates vs thoughts), so reasoning is added to get
	// the GROSS output a proxy row carries.
	OutputExcludesReasoning bool
	// ReasoningByTotal lets the record's own total decide whether output
	// contains reasoning, per record, before OutputExcludesReasoning is
	// consulted: a total equal to input + output + reasoning proves the
	// buckets disjoint (reasoning is added back), a total equal to input +
	// output proves reasoning already sits inside output (nothing added).
	// Qwen Code needs it: its OpenAI-compatible generator reports
	// completion_tokens (reasoning included) as output, its Gemini
	// generator reports candidates (reasoning excluded), both with the
	// thoughts count beside (T-542). OutputExcludesReasoning is the
	// fallback when the record has no usable total.
	ReasoningByTotal bool
}

// Ownership names which observation kinds this export may write while the
// tool's own transcript adapter is ALSO enabled on the node. When the
// adapter is disabled (or the tool has none) the export owns every kind.
// The rule exists because only tokens have a read-time dedup that works
// across capture paths (internal/sessionmsg folds an api_turns row against
// its transcript twin); actions and content have none, so writing them from
// two paths would double them.
type Ownership struct {
	// TokensWithTranscript: the export's turns fold against the
	// transcript's rows (request-id merge, or a same-shape twin in the same
	// session), proven for this vendor by a fixture test, so they may land
	// alongside the transcript.
	TokensWithTranscript bool
	// ContentWithTranscript: the export's content lands alongside the
	// transcript (the Claude Code path has always done this).
	ContentWithTranscript bool
	// TokensMergeWithProxy: the export's per-call id IS the request id the
	// proxy stores, so an export turn merges into the proxy's row for the
	// same call (store.UpsertTurnByRequestID) instead of adding a second
	// one. Only then may export tokens land while the tool's proxy route
	// is active; otherwise the proxy owns the tokens (T-536, D-343).
	TokensMergeWithProxy bool
}

// RouteResourceAttr / RouteProxy are the resource attribute an Observer
// launcher (`observer <tool>`) adds to the tool's own OpenTelemetry resource
// (OTEL_RESOURCE_ATTRIBUTES) when it routes the run through this node's
// proxy, so the export of THAT run is known to be proxied even when the
// tool's persisted config is not routed (T-536).
//
// RouteToolResourceAttr scopes the marker to the tool the launcher started
// (P3-36): OTEL_RESOURCE_ATTRIBUTES is inherited by every process the run
// spawns, so a nested OTel-exporting tool would otherwise carry the marker
// too and have its token rows dropped as proxy-owned although its own calls
// never went through the proxy. A turn counts as proxied only when both
// attributes are present and the tool attribute names the turn's own tool.
const (
	RouteResourceAttr     = "sbo.route"
	RouteToolResourceAttr = "sbo.route.tool"
	RouteProxy            = "proxy"
)

// Rule maps one kind of record.
type Rule struct {
	Signal Signal
	// Names match the event name (logs) or the span name (spans).
	Names []string
	// NamePrefix matches span names by prefix ("chat " matches
	// "chat gpt-5.5").
	NamePrefix bool
	// Where are extra conditions, all of which must hold.
	Where []Cond
	Kind  Kind
	// RequireRequest drops (and counts in Result.Skipped) a turn without a
	// vendor request id instead of synthesizing one.
	RequireRequest bool
	F              Fields
}

// Cond is one attribute condition on a record.
type Cond struct {
	Key string
	// Equals, when set, requires the string value to equal it.
	Equals string
	// Absent inverts the condition: the key must NOT be present.
	Absent bool
}

// Fields are the candidate attribute keys per field, tried in order, record
// attributes before resource attributes for each key.
type Fields struct {
	Session    []string
	Request    []string
	Model      []string
	Provider   []string
	Input      []string
	Output     []string
	CacheRead  []string
	CacheWrite []string
	Reasoning  []string
	// Total is the vendor's total token count, read only by
	// Normalize.ReasoningByTotal.
	Total []string
	// MemberEmail / MemberUserID / MemberAccountID / MemberTeamID identify
	// a team-grain call's owner (Turn.Member); BillingID is the vendor's
	// request-grain billing key (Turn.BillingID).
	MemberEmail     []string
	MemberUserID    []string
	MemberAccountID []string
	MemberTeamID    []string
	BillingID       []string
	Cost            []string
	Duration        []string

	ToolName []string
	CallID   []string
	// Success is a boolean attribute; Failure is an attribute whose mere
	// presence marks the call failed (GenAI error.type).
	Success []string
	Failure []string

	ContentKind string
	Content     []string
	// ContentFromBody falls back to the log record body when no content
	// attribute is set.
	ContentFromBody bool
	// ContentPlaceholders are values an exporter sends in place of withheld
	// content ("[REDACTED]"); they are never stored as content.
	ContentPlaceholders []string
	ToolUseID           []string
}

// PlaceholderProjectRoot is the project root a session or action from a
// native export lands under when the export carries no working directory:
// the bracketed per-tool placeholder the other root-less capture paths use
// ("[cursor]", "[grokbot]"). The tool's transcript, when it later upserts
// the same session, replaces it with the real project.
func PlaceholderProjectRoot(tool string) string { return "[" + tool + "]" }
