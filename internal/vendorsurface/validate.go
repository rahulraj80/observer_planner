package vendorsurface

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var surfaceIDRE = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*(\.[a-z0-9][a-z0-9_-]*)+$`)

func set[T ~string](vs ...T) map[T]bool {
	m := make(map[T]bool, len(vs))
	for _, v := range vs {
		m[v] = true
	}
	return m
}

var (
	reachKinds = set(ReachPullAPI, ReachPullBucket, ReachPullCloudLog, ReachPushOTLP, ReachPushWebhookSigned,
		ReachPushWebhookHeaderAuth, ReachPushSplunkHEC, ReachSyncDecisionHook, ReachFileProjection,
		ReachFileRepoProjection, ReachManualUpload, ReachManualOnly, ReachInvokeAPI, ReachRelay, ReachInBand,
		ReachNodeLocal, ReachUnclassified)
	authKinds = set(AuthHeaderKey, AuthBasic, AuthBearer, AuthOAuthClientCredentials, AuthOAuthUserToken,
		AuthGitHubApp, AuthCloudIAM, AuthBucketRole, AuthManualUpload, AuthInboundMachineIdentity,
		AuthInboundSignedSecret, AuthInboundStaticHeader, AuthConsoleSession, AuthLocalFile, AuthUnclassified)
	cloudProviders = set(CloudAWS, CloudGCP, CloudAzure)
	pagingStyles   = set(PagingNone, PagingCursor, PagingPageNumber, PagingLinkHeader, PagingNextLink,
		PagingUnspecified)
	buildStates   = set(BuildCatalogOnly, BuildAwaitingReference, BuildBuildable, BuildBuilt)
	sensitivities = set(SensitivityMetadata, SensitivityPII, SensitivityCredentialMetadata, SensitivityContent,
		SensitivitySecretForbidden)
	secretClasses = set(SecretNever, SecretMay, SecretReturns, SecretUnclassified)
	dataClasses   = set(DataClasses()...)
	grains        = set(GrainPerCall, GrainPerSession, GrainUserHour, GrainUserDay, GrainOrgDay, GrainSnapshot,
		GrainEvent, GrainPerCommit, GrainUnspecified)
	units = set(UnitTokens, UnitUSDMicros, UnitCredits, UnitACU, UnitRequests, UnitLines, UnitCount, UnitSeats,
		UnitText, UnitUnspecified)
	accuracies     = set(AccuracyExact, AccuracyEstimated, AccuracyAggregate, AccuracyUnassessed)
	completenesses = set(CompleteForTool, CompletenessSegment, NeedsExclusiveSelector)
	controlClasses = set(ControlClasses()...)
	facets         = set(FacetAPIWrite, FacetFile, FacetAPIAndFile, FacetReadOnly, FacetUIOnly, FacetGateway,
		FacetAwaitingReference)
	identityKinds = set(IdentityEmail, IdentityVendorUserID, IdentityVendorAccountID, IdentityGitHubLogin,
		IdentityGitHubUserID, IdentityEntraOID, IdentityAPIKeyID, IdentityProjectID, IdentityWorkspaceID)
	nodeSideReach = set(ReachNodeLocal, ReachFileProjection)
)

// syntheticOrder ranks the synthetic ladder.
var syntheticOrder = map[SyntheticRung]int{
	SynthDocOnly: 0, SynthContractFixtures: 1, SynthAssumptionTested: 2,
	SynthMockVerified: 3,
}

// liveOrder ranks the live ladder.
var liveOrder = map[LiveRung]int{
	LivePending: 0, LiveConnectionProven: 1, LivePaginationProven: 2,
	LiveLandingProven: 3, LiveRevisionProven: 4, LiveSpendComparisonProven: 5,
}

// SyntheticRank returns the rank of a synthetic rung (-1 if unknown).
func SyntheticRank(r SyntheticRung) int {
	if v, ok := syntheticOrder[r]; ok {
		return v
	}
	return -1
}

// LiveRank returns the rank of a live rung (-1 if unknown).
func LiveRank(r LiveRung) int {
	if v, ok := liveOrder[r]; ok {
		return v
	}
	return -1
}

// SyntheticCeiling is the evidence ceiling of plan 12A.6: a supply whose
// required fields are all VerifiedOfficial or VerifiedSDK may reach
// mock_verified; any ThirdParty or Inferred required field caps it at
// assumption_tested. An empty ledger caps it at doc_only.
func SyntheticCeiling(required []EvidenceGrade) SyntheticRung {
	if len(required) == 0 {
		return SynthDocOnly
	}
	for _, g := range required {
		if g != EvidenceVerifiedOfficial && g != EvidenceVerifiedSDK {
			return SynthAssumptionTested
		}
	}
	return SynthMockVerified
}

// ControlClasses returns every control class in plan order.
func ControlClasses() []ControlClass {
	return []ControlClass{
		CtlSpendLimitUser, CtlSpendLimitGroup, CtlSpendLimitProject, CtlSpendLimitOrg,
		CtlSpendAlert, CtlSeatAssign, CtlMemberProvision, CtlGroupMembership, CtlRoleAssign, CtlModelAccess,
		CtlRateLimitProject, CtlMCPAllowList, CtlToolPolicy, CtlNetworkPolicy, CtlApprovalPolicy, CtlAgentCapability,
		CtlDelegationPolicy, CtlCredentialCreationPolicy, CtlCredentialLifecycle, CtlFeatureToggle,
		CtlManagedSettings, CtlRetentionPolicy, CtlContentExclusion, CtlSpendLimitSession, CtlSpendLimitCredential,
		CtlCapacityReservation,
	}
}

// Driverable reports whether a build state may have a driver (Buildable or
// Built). AwaitingReference and CatalogOnly never do.
func Driverable(b BuildState) bool { return b == BuildBuildable || b == BuildBuilt }

// ValidEvidenceGrade reports whether g is one of the four ledger grades.
func ValidEvidenceGrade(g EvidenceGrade) bool {
	switch g {
	case EvidenceVerifiedOfficial, EvidenceVerifiedSDK, EvidenceThirdParty, EvidenceInferred:
		return true
	}
	return false
}

// ValidDate reports whether s is a YYYY-MM-DD calendar date.
func ValidDate(s string) bool {
	_, err := time.Parse("2006-01-02", s)
	return err == nil
}

// Validate checks one catalog row against the F1 contract and returns every
// violation (nil when the row is valid). The rules are listed in order in the
// operator reference (docs/vendor-connections.md).
func Validate(s Surface) []error {
	errs := validateHeader(s)
	errs = append(errs, validateEnums(s)...)
	errs = append(errs, validateSources(s)...)
	errs = append(errs, validateHosts(s)...)
	return append(errs, validateBody(s)...)
}

func validateHeader(s Surface) []error {
	var errs []error
	bad := func(format string, a ...any) {
		errs = append(errs, fmt.Errorf("%s: "+format, append([]any{s.ID}, a...)...))
	}
	id := string(s.ID)
	if !surfaceIDRE.MatchString(id) {
		bad("id is not a dotted lower-case name")
	} else {
		segs := strings.Split(id, ".")
		if s.Vendor != segs[0] {
			bad("vendor %q is not the first id segment %q", s.Vendor, segs[0])
		}
		if s.Plane != segs[0]+"."+segs[1] {
			bad("plane %q is not the first two id segments", s.Plane)
		}
	}
	if s.VendorGroup == "" {
		bad("vendor_group is empty")
	}
	if s.Status != StatusLabel {
		bad("status must be %q (the catalog never presents a surface as verified)", StatusLabel)
	}
	return errs
}

func validateEnums(s Surface) []error {
	var errs []error
	bad := func(format string, a ...any) {
		errs = append(errs, fmt.Errorf("%s: "+format, append([]any{s.ID}, a...)...))
	}
	if !reachKinds[s.Reach] {
		bad("unknown reach %q", s.Reach)
	} else if s.Transport != reachTransport[s.Reach] {
		bad("transport %q does not match reach %q (want %q)", s.Transport, s.Reach, reachTransport[s.Reach])
	}
	if s.Reach == ReachUnclassified && Driverable(s.Build) {
		bad("a driverable row must name its reach")
	}
	if !authKinds[s.Auth.Kind] {
		bad("unknown auth kind %q", s.Auth.Kind)
	}
	if s.Auth.Cloud != "" {
		if s.Auth.Kind != AuthCloudIAM && s.Auth.Kind != AuthBucketRole {
			bad("auth cloud %q set on a non-cloud auth kind %q", s.Auth.Cloud, s.Auth.Kind)
		} else if !cloudProviders[s.Auth.Cloud] {
			bad("unknown cloud provider %q", s.Auth.Cloud)
		}
	}
	if !pagingStyles[s.Paging.Style] {
		bad("unknown paging style %q", s.Paging.Style)
	}
	if !buildStates[s.Build] {
		bad("unknown build state %q", s.Build)
	}
	if !sensitivities[s.Sensitivity] {
		bad("unknown sensitivity %q", s.Sensitivity)
	}
	if !secretClasses[s.SecretClass] {
		bad("unknown secret class %q", s.SecretClass)
	}
	return errs
}

func validateBody(s Surface) []error {
	var errs []error
	bad := func(format string, a ...any) {
		errs = append(errs, fmt.Errorf("%s: "+format, append([]any{s.ID}, a...)...))
	}
	if s.ReferenceOnly != (len(s.Supplies) == 0 && len(s.Controls) == 0) {
		bad("reference_only must be set exactly when the row has no supply and no control")
	}
	seen := map[DataClass]bool{}
	for _, sp := range s.Supplies {
		errs = append(errs, validateSupply(s, sp)...)
		if seen[sp.Class] {
			bad("supply class %s listed twice", sp.Class)
		}
		seen[sp.Class] = true
	}
	if seen[ClassContent] && s.Sensitivity != SensitivityContent {
		bad("a Content supply needs sensitivity content")
	}
	if seen[ClassCredentials] {
		if s.Sensitivity != SensitivityCredentialMetadata && s.Sensitivity != SensitivityContent {
			bad("a Credentials supply needs sensitivity credential_metadata (or content)")
		}
		if s.SecretClass == SecretNever {
			bad("a Credentials supply cannot be classified never_returns_secret without a closed DTO review")
		}
	}
	cseen := map[ControlClass]bool{}
	for _, c := range s.Controls {
		errs = append(errs, validateControl(s, c)...)
		if cseen[c.Class] {
			bad("control class %s listed twice", c.Class)
		}
		cseen[c.Class] = true
	}
	return errs
}

func validateSources(s Surface) []error {
	var errs []error
	if len(s.Sources) == 0 {
		return []error{fmt.Errorf("%s: no cited source", s.ID)}
	}
	vendorURL := false
	for _, src := range s.Sources {
		if !ValidDate(src.CheckedOn) {
			errs = append(errs, fmt.Errorf("%s: source checked_on %q is not YYYY-MM-DD", s.ID, src.CheckedOn))
		}
		switch {
		case src.URL != "" && src.RepoPath != "":
			errs = append(errs, fmt.Errorf("%s: a source is either a url or a repo path", s.ID))
		case src.URL != "":
			u, err := url.Parse(src.URL)
			if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil {
				errs = append(errs, fmt.Errorf("%s: source %q is not an absolute public URL", s.ID, src.URL))
			} else {
				vendorURL = true
			}
		case src.RepoPath != "":
			if !nodeSideReach[s.Reach] {
				errs = append(errs, fmt.Errorf("%s: a repo-path source is allowed only on a node-side row", s.ID))
			}
		default:
			errs = append(errs, fmt.Errorf("%s: empty source", s.ID))
		}
	}
	if !vendorURL && !nodeSideReach[s.Reach] {
		errs = append(errs, fmt.Errorf("%s: needs at least one vendor URL source", s.ID))
	}
	return errs
}

func validateVerification(s Surface, what string, v Verification, b BuildState) []error {
	var errs []error
	if SyntheticRank(v.Synthetic) < 0 {
		errs = append(errs, fmt.Errorf("%s: %s unknown synthetic rung %q", s.ID, what, v.Synthetic))
	}
	if v.Live != LivePending {
		errs = append(errs, fmt.Errorf("%s: %s live rung %q; the catalog only ever carries live_pending (live rungs are per connection)", s.ID, what, v.Live))
	}
	if SyntheticRank(v.Synthetic) > 0 && !Driverable(b) {
		errs = append(errs, fmt.Errorf("%s: %s is %s but its build state %s is not driverable", s.ID, what, v.Synthetic, b))
	}
	return errs
}

func validateSupply(s Surface, sp Supply) []error {
	var errs []error
	bad := func(format string, a ...any) {
		errs = append(errs, fmt.Errorf("%s: supply %s: "+format, append([]any{s.ID, sp.Class}, a...)...))
	}
	if !dataClasses[sp.Class] {
		bad("unknown data class")
	}
	if !grains[sp.Grain] {
		bad("unknown grain %q", sp.Grain)
	}
	if !units[sp.Unit] {
		bad("unknown unit %q", sp.Unit)
	}
	if !accuracies[sp.Accuracy] {
		bad("unknown accuracy %q", sp.Accuracy)
	}
	if !completenesses[sp.Coverage.Completeness] {
		bad("unknown completeness %q", sp.Coverage.Completeness)
	}
	for _, k := range sp.Identity {
		if !identityKinds[k] {
			bad("unknown identity kind %q", k)
		}
	}
	if !buildStates[sp.Build] {
		bad("unknown build state %q", sp.Build)
	}
	if Driverable(sp.Build) && !Driverable(s.Build) {
		bad("driverable supply on a non-driverable surface")
	}
	errs = append(errs, validateVerification(s, "supply "+string(sp.Class), sp.Verification, sp.Build)...)
	return errs
}

func validateControl(s Surface, c Control) []error {
	var errs []error
	bad := func(format string, a ...any) {
		errs = append(errs, fmt.Errorf("%s: control %s: "+format, append([]any{s.ID, c.Class}, a...)...))
	}
	if !controlClasses[c.Class] {
		bad("unknown control class")
	}
	if !facets[c.Facet] {
		bad("unknown facet %q", c.Facet)
	}
	k := c.Caps
	switch c.Facet {
	case FacetUIOnly, FacetGateway, FacetAwaitingReference:
		if k.Read || k.Write {
			bad("facet %s carries no machine read or write", c.Facet)
		}
	case FacetReadOnly:
		if !k.Read || k.Write {
			bad("facet R is read without write")
		}
	case FacetAPIWrite:
		if !k.Write {
			bad("facet A needs write")
		}
	case FacetFile:
		if !k.Write || !(k.LocalFile || k.Repository) {
			bad("facet F needs write through a local file or repository")
		}
	case FacetAPIAndFile:
		if !k.Write || !(k.LocalFile || k.Repository) {
			bad("facet AF needs write and a file or repository path")
		}
	}
	if k.Readback && !k.Read {
		bad("readback needs read")
	}
	if c.Comparator != "" && !k.Write {
		bad("a comparator is only meaningful for a writable control")
	}
	errs = append(errs, validateVerification(s, "control "+string(c.Class), c.Verification, s.Build)...)
	return errs
}

// ValidateCatalog validates every row and the catalog-level rules (unique ids).
func ValidateCatalog(rows []Surface) []error {
	var errs []error
	ids := map[SurfaceID]bool{}
	for _, s := range rows {
		if ids[s.ID] {
			errs = append(errs, fmt.Errorf("%s: duplicate surface id", s.ID))
		}
		ids[s.ID] = true
		errs = append(errs, Validate(s)...)
	}
	return errs
}
