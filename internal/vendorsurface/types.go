package vendorsurface

// StatusLabel is the honesty label every catalog row carries until a live
// receipt exists for it on a real connection (plan 12A.4, 12A.9). The catalog
// never presents a vendor surface as verified.
const StatusLabel = "synthetic, awaiting live validation"

// SurfaceID names one surface: "<vendor>.<plane>[.<surface>...]", lower case.
type SurfaceID string

// ReachKind is how Observer reaches a surface. Exactly one per surface (plan
// 4.1, 4.3.3). ReachUnclassified is allowed only on a row that is not
// Buildable: a Buildable row always names its mechanism.
type ReachKind string

// Reach kinds. InBand and NodeLocal are additions over plan 4.1 needed by
// the v5 research rows: InBand is data observed on inference traffic that
// already flows through the Observer proxy (rate-limit headers, inline usage),
// NodeLocal is a node-side store or hook contract read by an Observer adapter.
// Neither is an org-server pull.
const (
	ReachPullAPI               ReachKind = "pull_api"
	ReachPullBucket            ReachKind = "pull_bucket"
	ReachPullCloudLog          ReachKind = "pull_cloud_log"
	ReachPushOTLP              ReachKind = "push_otlp"
	ReachPushWebhookSigned     ReachKind = "push_webhook_signed"
	ReachPushWebhookHeaderAuth ReachKind = "push_webhook_header_auth"
	ReachPushSplunkHEC         ReachKind = "push_splunk_hec"
	ReachSyncDecisionHook      ReachKind = "sync_decision_hook"
	ReachFileProjection        ReachKind = "file_projection"
	ReachFileRepoProjection    ReachKind = "file_repo_projection"
	ReachManualUpload          ReachKind = "manual_upload"
	ReachManualOnly            ReachKind = "manual_only"
	ReachInvokeAPI             ReachKind = "invoke_api"
	ReachRelay                 ReachKind = "relay"
	ReachInBand                ReachKind = "in_band"
	ReachNodeLocal             ReachKind = "node_local"
	ReachUnclassified          ReachKind = "unclassified"
)

// TransportClass is the transport dimension of plan 4.5 (R-E-02). It is
// derived from ReachKind through reachTransport; a row that disagrees fails
// validation.
type TransportClass string

// Transport classes.
const (
	TransportAPI          TransportClass = "api"
	TransportOTLP         TransportClass = "otlp"
	TransportWebhook      TransportClass = "webhook"
	TransportBucket       TransportClass = "bucket"
	TransportCloudLog     TransportClass = "cloud_log"
	TransportManualFile   TransportClass = "manual_file"
	TransportLocalFile    TransportClass = "local_file"
	TransportRepository   TransportClass = "repository"
	TransportUI           TransportClass = "ui"
	TransportUnclassified TransportClass = "unclassified"
)

// reachTransport is the one table mapping reach to transport.
var reachTransport = map[ReachKind]TransportClass{
	ReachPullAPI:               TransportAPI,
	ReachInvokeAPI:             TransportAPI,
	ReachRelay:                 TransportAPI,
	ReachInBand:                TransportAPI,
	ReachPushOTLP:              TransportOTLP,
	ReachPushWebhookSigned:     TransportWebhook,
	ReachPushWebhookHeaderAuth: TransportWebhook,
	ReachPushSplunkHEC:         TransportWebhook,
	ReachSyncDecisionHook:      TransportWebhook,
	ReachPullBucket:            TransportBucket,
	ReachPullCloudLog:          TransportCloudLog,
	ReachFileProjection:        TransportLocalFile,
	ReachNodeLocal:             TransportLocalFile,
	ReachFileRepoProjection:    TransportRepository,
	ReachManualUpload:          TransportManualFile,
	ReachManualOnly:            TransportUI,
	ReachUnclassified:          TransportUnclassified,
}

// TransportFor returns the transport class of a reach kind ("" if unknown).
func TransportFor(r ReachKind) TransportClass { return reachTransport[r] }

// AuthKind is the discriminator of AuthSpec (plan 4.1, 4.5 R-E-13/R-E-14).
type AuthKind string

// Auth kinds. ConsoleSession marks a UI-only surface (an admin signed in to
// the vendor console; never a credential Observer holds). LocalFile marks a
// node-side file or store (no vendor credential).
const (
	AuthHeaderKey              AuthKind = "header_key"
	AuthBasic                  AuthKind = "basic"
	AuthBearer                 AuthKind = "bearer"
	AuthOAuthClientCredentials AuthKind = "oauth_client_credentials" //nolint:gosec // G101: an enum value, not a credential
	AuthOAuthUserToken         AuthKind = "oauth_user_token"         //nolint:gosec // G101: an enum value, not a credential
	AuthGitHubApp              AuthKind = "github_app"
	AuthCloudIAM               AuthKind = "cloud_iam"
	AuthBucketRole             AuthKind = "bucket_role"
	AuthManualUpload           AuthKind = "manual_upload"
	AuthInboundMachineIdentity AuthKind = "inbound_machine_identity"
	AuthInboundSignedSecret    AuthKind = "inbound_signed_secret"
	AuthInboundStaticHeader    AuthKind = "inbound_static_header"
	AuthConsoleSession         AuthKind = "console_session"
	AuthLocalFile              AuthKind = "local_file"
	AuthUnclassified           AuthKind = "unclassified"
)

// CloudProvider discriminates the cloud member of the AuthSpec union.
type CloudProvider string

// Cloud providers.
const (
	CloudAWS   CloudProvider = "aws"
	CloudGCP   CloudProvider = "gcp"
	CloudAzure CloudProvider = "azure"
)

// AuthSpec is the auth contract of a surface. Kind is the discriminator; Cloud
// is set only for AuthCloudIAM and AuthBucketRole and names the provider whose
// per-connection facts (role ARN and external id, workload identity, tenant)
// the connection wizard collects. Doc keeps the researched text verbatim.
type AuthSpec struct {
	Kind   AuthKind      `json:"kind"`
	Cloud  CloudProvider `json:"cloud,omitempty"`
	Scopes []string      `json:"scopes,omitempty"`
	Doc    string        `json:"doc"`
}

// PagingStyle is the documented pagination family.
type PagingStyle string

// Paging styles.
const (
	PagingNone        PagingStyle = "none"
	PagingCursor      PagingStyle = "cursor"
	PagingPageNumber  PagingStyle = "page_number"
	PagingLinkHeader  PagingStyle = "link_header"
	PagingNextLink    PagingStyle = "next_link"
	PagingUnspecified PagingStyle = "unspecified"
)

// PagingSpec is the documented pagination contract.
type PagingSpec struct {
	Style PagingStyle `json:"style"`
	Doc   string      `json:"doc"`
}

// RateSpec is the documented rate limit, or not documented. A driver always
// honours Retry-After regardless.
type RateSpec struct {
	Documented bool   `json:"documented"`
	Doc        string `json:"doc"`
}

// TierPredicate is the plan and role a customer needs for the surface,
// evaluated against connection facts by the wizard. Doc is the researched text.
type TierPredicate struct {
	Plans    []string `json:"plans,omitempty"`
	Requires []string `json:"requires,omitempty"`
	Doc      string   `json:"doc"`
}

// BuildState is the build ladder of a surface or supply (plan 4.1, R-C-13).
// Only Buildable and Built rows may get a driver; AwaitingReference never
// counts as driverable (plan 4.5 Evidence honesty).
type BuildState string

// Build states.
const (
	BuildCatalogOnly       BuildState = "catalog_only"
	BuildAwaitingReference BuildState = "awaiting_reference"
	BuildBuildable         BuildState = "buildable"
	BuildBuilt             BuildState = "built"
)

// SensitivityClass is the sensitivity dimension of plan 4.5 (R-E-02).
type SensitivityClass string

// Sensitivity classes.
const (
	SensitivityMetadata           SensitivityClass = "metadata"
	SensitivityPII                SensitivityClass = "pii"
	SensitivityCredentialMetadata SensitivityClass = "credential_metadata" //nolint:gosec // G101: an enum value, not a credential
	SensitivityContent            SensitivityClass = "content"
	SensitivitySecretForbidden    SensitivityClass = "secret_forbidden"
)

// SecretClass classifies whether a surface's operations can return secret
// material (plan 4.5 R-E-06). Unclassified means not yet reviewed: the runner
// (lane F7) must refuse an Unclassified operation, so an unreviewed row fails
// closed.
type SecretClass string

// Secret classes.
const (
	SecretNever        SecretClass = "never_returns_secret"
	SecretMay          SecretClass = "may_return_secret" //nolint:gosec // G101: an enum value, not a credential
	SecretReturns      SecretClass = "returns_secret"
	SecretUnclassified SecretClass = "unclassified"
)

// DataClass is one of the 21 reporting classes of plan 4.0. Since R-E-02 they
// are the coverage-reporting view; the orthogonal dimension of a supply is
// derived from its class through the dimension table (dimensions.go).
type DataClass string

// Data classes.
const (
	ClassIdentity        DataClass = "Identity"
	ClassGroups          DataClass = "Groups"
	ClassProjects        DataClass = "Projects"
	ClassServiceAccounts DataClass = "ServiceAccounts"
	ClassCredentials     DataClass = "Credentials"
	ClassRoles           DataClass = "Roles"
	ClassSeats           DataClass = "Seats"
	ClassUsage           DataClass = "Usage"
	ClassCost            DataClass = "Cost"
	ClassBillingExport   DataClass = "BillingExport"
	ClassLimits          DataClass = "Limits"
	ClassRateLimits      DataClass = "RateLimits"
	ClassSessions        DataClass = "Sessions"
	ClassLOC             DataClass = "LOC"
	ClassCommits         DataClass = "Commits"
	ClassAdoption        DataClass = "Adoption"
	ClassAudit           DataClass = "Audit"
	ClassContent         DataClass = "Content"
	ClassTelemetry       DataClass = "Telemetry"
	ClassPolicy          DataClass = "Policy"
	ClassRetention       DataClass = "Retention"
)

// Grain is the structural grain of a supply (plan 4.0). Snapshot is a grain,
// never a class. GrainUnspecified is allowed for catalog rows whose research
// did not name a grain; a driver lane sets it before the synthetic gate.
type Grain string

// Grains.
const (
	GrainPerCall     Grain = "per_call"
	GrainPerSession  Grain = "per_session"
	GrainUserHour    Grain = "user_hour"
	GrainUserDay     Grain = "user_day"
	GrainOrgDay      Grain = "org_day"
	GrainSnapshot    Grain = "snapshot"
	GrainEvent       Grain = "event"
	GrainPerCommit   Grain = "per_commit"
	GrainUnspecified Grain = "unspecified"
)

// Unit is the unit of a supply's facts. Money is integer micro-units; credits
// and ACUs enter money only through an authored, versioned conversion (OQ-8).
type Unit string

// Units.
const (
	UnitTokens      Unit = "tokens"
	UnitUSDMicros   Unit = "usd_micros"
	UnitCredits     Unit = "credits"
	UnitACU         Unit = "acu"
	UnitRequests    Unit = "requests"
	UnitLines       Unit = "lines"
	UnitCount       Unit = "count"
	UnitSeats       Unit = "seats"
	UnitText        Unit = "text"
	UnitUnspecified Unit = "unspecified"
)

// Accuracy is the supply-level honesty label (plan 4.1 Fidelity).
type Accuracy string

// Accuracy values.
const (
	AccuracyExact      Accuracy = "exact"
	AccuracyEstimated  Accuracy = "estimated"
	AccuracyAggregate  Accuracy = "aggregate"
	AccuracyUnassessed Accuracy = "unassessed"
)

// Completeness is the structured coverage completeness of a supply (plan 4.1,
// 7.2). Segment is the conservative default: a Segment supply can never own a
// tool's display or spend (TestPartialCoverageNeverEligibleAsToolSource, F11).
type Completeness string

// Completeness values.
const (
	CompleteForTool        Completeness = "complete_for_tool"
	CompletenessSegment    Completeness = "segment"
	NeedsExclusiveSelector Completeness = "needs_exclusive_selector"
)

// CoveragePred is the structured coverage predicate of a supply or control.
type CoveragePred struct {
	Completeness Completeness `json:"completeness"`
	Segments     []string     `json:"segments,omitempty"`
	Doc          string       `json:"doc,omitempty"`
}

// SyntheticRung is the synthetic verification ladder (plan 12A).
type SyntheticRung string

// Synthetic rungs, in order.
const (
	SynthDocOnly          SyntheticRung = "doc_only"
	SynthContractFixtures SyntheticRung = "contract_fixtures"
	SynthAssumptionTested SyntheticRung = "assumption_tested"
	SynthMockVerified     SyntheticRung = "mock_verified"
)

// LiveRung is the live verification ladder (plan 12A.4, 12A.7). Live rungs
// are earned per connection with receipts in the database; the catalog only
// ever carries LivePending.
type LiveRung string

// Live rungs, in order.
const (
	LivePending               LiveRung = "live_pending"
	LiveConnectionProven      LiveRung = "connection_proven"
	LivePaginationProven      LiveRung = "pagination_proven"
	LiveLandingProven         LiveRung = "landing_proven"
	LiveRevisionProven        LiveRung = "revision_proven"
	LiveSpendComparisonProven LiveRung = "spend_comparison_proven"
)

// Verification is the two-ladder verification state of a supply or control.
type Verification struct {
	Synthetic SyntheticRung `json:"synthetic"`
	Live      LiveRung      `json:"live"`
}

// EvidenceGrade is the field-level evidence grade of the fixture field ledger
// (plan 12A.1, 12A.6).
type EvidenceGrade string

// Evidence grades, strongest first.
const (
	EvidenceVerifiedOfficial EvidenceGrade = "VerifiedOfficial"
	EvidenceVerifiedSDK      EvidenceGrade = "VerifiedSDK"
	EvidenceThirdParty       EvidenceGrade = "ThirdParty"
	EvidenceInferred         EvidenceGrade = "Inferred"
)

// IdentityKind is a vendor identity kind a supply carries (plan 4.1).
type IdentityKind string

// Identity kinds.
const (
	IdentityEmail           IdentityKind = "email"
	IdentityVendorUserID    IdentityKind = "vendor_user_id"
	IdentityVendorAccountID IdentityKind = "vendor_account_id"
	IdentityGitHubLogin     IdentityKind = "github_login"
	IdentityGitHubUserID    IdentityKind = "github_user_id"
	IdentityEntraOID        IdentityKind = "entra_oid"
	IdentityAPIKeyID        IdentityKind = "api_key_id"
	IdentityProjectID       IdentityKind = "project_id"
	IdentityWorkspaceID     IdentityKind = "workspace_id"
)

// Supply is one data class a surface supplies.
type Supply struct {
	Class        DataClass      `json:"class"`
	Grain        Grain          `json:"grain"`
	Unit         Unit           `json:"unit"`
	Accuracy     Accuracy       `json:"accuracy"`
	Identity     []IdentityKind `json:"identity,omitempty"`
	Coverage     CoveragePred   `json:"coverage"`
	Build        BuildState     `json:"build"`
	Verification Verification   `json:"verification"`
}

// ControlClass is one of the control classes of plan 4.0 plus the three the
// full v5 review added (4.3.3): SpendLimitSession, SpendLimitCredential and
// CapacityReservation.
type ControlClass string

// Control classes.
const (
	CtlSpendLimitUser           ControlClass = "SpendLimitUser"
	CtlSpendLimitGroup          ControlClass = "SpendLimitGroup"
	CtlSpendLimitProject        ControlClass = "SpendLimitProject"
	CtlSpendLimitOrg            ControlClass = "SpendLimitOrg"
	CtlSpendAlert               ControlClass = "SpendAlert"
	CtlSeatAssign               ControlClass = "SeatAssign"
	CtlMemberProvision          ControlClass = "MemberProvision"
	CtlGroupMembership          ControlClass = "GroupMembership"
	CtlRoleAssign               ControlClass = "RoleAssign"
	CtlModelAccess              ControlClass = "ModelAccess"
	CtlRateLimitProject         ControlClass = "RateLimitProject"
	CtlMCPAllowList             ControlClass = "MCPAllowList"
	CtlToolPolicy               ControlClass = "ToolPolicy"
	CtlNetworkPolicy            ControlClass = "NetworkPolicy"
	CtlApprovalPolicy           ControlClass = "ApprovalPolicy"
	CtlAgentCapability          ControlClass = "AgentCapability"
	CtlDelegationPolicy         ControlClass = "DelegationPolicy"
	CtlCredentialCreationPolicy ControlClass = "CredentialCreationPolicy" //nolint:gosec // G101: an enum value, not a credential
	CtlCredentialLifecycle      ControlClass = "CredentialLifecycle"      //nolint:gosec // G101: an enum value, not a credential
	CtlFeatureToggle            ControlClass = "FeatureToggle"
	CtlManagedSettings          ControlClass = "ManagedSettings"
	CtlRetentionPolicy          ControlClass = "RetentionPolicy"
	CtlContentExclusion         ControlClass = "ContentExclusion"
	CtlSpendLimitSession        ControlClass = "SpendLimitSession"
	CtlSpendLimitCredential     ControlClass = "SpendLimitCredential"
	CtlCapacityReservation      ControlClass = "CapacityReservation"
)

// ControlFacet is the Appendix F control status of one surface for one
// control class: A API write, F managed file or flag (Rail B), AF both, R API
// read only, U UI only, G gateway-conditional, AR awaiting reference.
type ControlFacet string

// Control facets.
const (
	FacetAPIWrite          ControlFacet = "A"
	FacetFile              ControlFacet = "F"
	FacetAPIAndFile        ControlFacet = "AF"
	FacetReadOnly          ControlFacet = "R"
	FacetUIOnly            ControlFacet = "U"
	FacetGateway           ControlFacet = "G"
	FacetAwaitingReference ControlFacet = "AR"
)

// ControlCaps are the independent control capabilities of plan 4.5 (R-E-11)
// that replace the single v4 Mode. A Write without Readback can end only in
// accepted_unverified or unknown_effect, never in_sync (lane F15/F16).
type ControlCaps struct {
	Read           bool `json:"read"`
	Write          bool `json:"write"`
	Readback       bool `json:"readback"`
	EffectEvidence bool `json:"effect_evidence"`
	Idempotency    bool `json:"idempotency"`
	LocalFile      bool `json:"local_file"`
	Repository     bool `json:"repository"`
}

// Control is one control class a surface reads or writes. Comparator names
// the typed never-widen comparator (plan 4.5 R-E-08); empty means none is
// designed yet, which keeps the class mirror only.
type Control struct {
	Class        ControlClass `json:"class"`
	Facet        ControlFacet `json:"facet"`
	Caps         ControlCaps  `json:"caps"`
	Scope        string       `json:"scope,omitempty"`
	Comparator   string       `json:"comparator,omitempty"`
	Coverage     CoveragePred `json:"coverage,omitempty"`
	Verification Verification `json:"verification"`
}

// SourceRef is one cited source. URL is a public vendor document; RepoPath is
// used only for node-side surfaces whose contract is Observer's own adapter
// documentation (a path in this repository).
type SourceRef struct {
	URL       string `json:"url,omitempty"`
	RepoPath  string `json:"repo_path,omitempty"`
	CheckedOn string `json:"checked_on"`
	SHA256    string `json:"sha256,omitempty"`
}

// Derivation records which import rule produced each derived enum of a row so
// a reviewer can audit it against the *_doc text (the first load was a
// rule-table import of the v5 research tables). AuthRule prefixed "keyword:"
// is a first guess from the research text: per plan 12A.6 the driver lane sets
// the exact per-endpoint AuthSpec, and the runner (F7) must not trust a
// keyword-derived auth kind.
type Derivation struct {
	ReachRule  string `json:"reach_rule"`
	BuildRule  string `json:"build_rule"`
	SourceRule string `json:"source_rule"`
	AuthRule   string `json:"auth_rule"`
}

// Surface is one catalog row (plan 4.1 as revised by 4.5). The *Doc fields keep
// the researched text verbatim beside the normalized enums.
type Surface struct {
	ID            SurfaceID        `json:"id"`
	VendorGroup   string           `json:"vendor_group"`
	Vendor        string           `json:"vendor"`
	Plane         string           `json:"plane"`
	Reach         ReachKind        `json:"reach"`
	Transport     TransportClass   `json:"transport"`
	Auth          AuthSpec         `json:"auth"`
	Paging        PagingSpec       `json:"paging"`
	Rate          RateSpec         `json:"rate"`
	Tier          TierPredicate    `json:"tier"`
	FreshnessDoc  string           `json:"freshness_doc"`
	LookbackDoc   string           `json:"lookback_doc"`
	PerUserDoc    string           `json:"per_user_doc"`
	MechanismDoc  string           `json:"mechanism_doc"`
	ReadWriteDoc  string           `json:"read_write_doc"`
	ClassDoc      string           `json:"class_doc"`
	GrainDoc      string           `json:"grain_doc"`
	Build         BuildState       `json:"build"`
	BuildDoc      string           `json:"build_doc"`
	Sensitivity   SensitivityClass `json:"sensitivity"`
	SecretClass   SecretClass      `json:"secret_class"`
	ReferenceOnly bool             `json:"reference_only,omitempty"`
	Supplies      []Supply         `json:"supplies"`
	Controls      []Control        `json:"controls"`
	Hosts         []HostTuple      `json:"hosts,omitempty"`
	// HostTemplates are residency host families whose one tenant label is
	// the customer's own subdomain (GitHub GHE.com: api.SUBDOMAIN.ghe.com).
	// A connection may name an expansion; the egress seam still reaches only
	// that exact tuple (lane L8).
	HostTemplates []HostTemplate `json:"host_templates,omitempty"`
	// DownloadHosts and DownloadHostTemplates are the second-hop hosts of a
	// two-step report (signed download links on another host, plan 6.2).
	// A template expands only with the tenant the connection's API host was
	// matched with, so a GHE.com connection downloads from its own subdomain.
	DownloadHosts         []HostTuple    `json:"download_hosts,omitempty"`
	DownloadHostTemplates []HostTemplate `json:"download_host_templates,omitempty"`
	// TokenHosts are the OAuth token endpoints of an
	// oauth_client_credentials surface (lane ENTAPI-D3, T-692): the egress
	// seam's reviewed token step posts the client credentials to the first
	// one and nowhere else. Only an oauth_client_credentials row may declare
	// them, and a built one must.
	TokenHosts []HostTuple `json:"token_hosts,omitempty"`
	// CustomerHost marks a surface whose server the customer runs on its own
	// domain (JetBrains IDE Services, self-hosted gateways; lane ENTAPI-L16C,
	// T-710). The row declares no host; the path and the auth contract stay
	// fixed per surface, and each connection declares its own tuple
	// (decide.ResolveCustomerHost, ledger row VCONN-HOST-1).
	CustomerHost bool        `json:"customer_host,omitempty"`
	Sources      []SourceRef `json:"sources"`
	Notes        string      `json:"notes"`
	Derivation   Derivation  `json:"derivation"`
	Status       string      `json:"status"`
}

// TargetKind types a coverage target (plan 4.5 Targets and routes).
type TargetKind string

// Target kinds.
const (
	TargetAdapterNative       TargetKind = "adapter_native"
	TargetConditionalProvider TargetKind = "conditional_provider"
	TargetGatewayRoute        TargetKind = "gateway_route"
	TargetVendorProduct       TargetKind = "vendor_product"
)
