package vendorsurface

import (
	"strings"
	"testing"
)

// extraForbiddenImports: vendorsurface must never import the integration
// registry (invariant I2, TestVendorsurfaceDoesNotImportIntegration is the
// imports pin with this entry).
var extraForbiddenImports = []string{"github.com/marmutapp/superbased-observer/internal/integration"}

// TestVendorsurfaceDoesNotImportIntegration names the I2 pin of plan section
// 13; the check itself runs in TestNoForbiddenImports through
// extraForbiddenImports.
func TestVendorsurfaceDoesNotImportIntegration(t *testing.T) {
	found := false
	for _, p := range extraForbiddenImports {
		if strings.HasSuffix(p, "/internal/integration") {
			found = true
		}
	}
	if !found {
		t.Fatal("the integration import ban is missing from extraForbiddenImports")
	}
}

func validRow() Surface {
	pending := Verification{Synthetic: SynthDocOnly, Live: LivePending}
	return Surface{
		ID: "acme.admin.usage", VendorGroup: "acme", Vendor: "acme", Plane: "acme.admin",
		Reach: ReachPullAPI, Transport: TransportAPI,
		Auth:   AuthSpec{Kind: AuthBearer, Doc: "admin key"},
		Paging: PagingSpec{Style: PagingCursor}, Build: BuildBuildable,
		Sensitivity: SensitivityPII, SecretClass: SecretUnclassified,
		Supplies: []Supply{{
			Class: ClassUsage, Grain: GrainUserDay, Unit: UnitTokens, Accuracy: AccuracyAggregate,
			Coverage: CoveragePred{Completeness: CompletenessSegment}, Build: BuildBuildable, Verification: pending,
		}},
		Controls: []Control{{
			Class: CtlSpendLimitUser, Facet: FacetAPIWrite, Caps: ControlCaps{Read: true, Write: true},
			Verification: pending,
		}},
		Sources: []SourceRef{{URL: "https://docs.example.com/admin", CheckedOn: "2026-10-07"}},
		Status:  StatusLabel,
	}
}

func TestValidateTable(t *testing.T) {
	if errs := Validate(validRow()); len(errs) != 0 {
		t.Fatalf("base row invalid: %v", errs)
	}
	cases := []struct {
		name string
		mut  func(*Surface)
		want string
	}{
		{"bad id", func(s *Surface) { s.ID = "Acme" }, "dotted lower-case"},
		{"vendor mismatch", func(s *Surface) { s.Vendor = "other" }, "first id segment"},
		{"plane mismatch", func(s *Surface) { s.Plane = "acme" }, "first two id segments"},
		{"no group", func(s *Surface) { s.VendorGroup = "" }, "vendor_group is empty"},
		{"verified claim", func(s *Surface) { s.Status = "verified" }, "never presents"},
		{"unknown reach", func(s *Surface) { s.Reach = "ftp" }, "unknown reach"},
		{"transport mismatch", func(s *Surface) { s.Transport = TransportOTLP }, "does not match reach"},
		{"driverable unclassified", func(s *Surface) { s.Reach, s.Transport = ReachUnclassified, TransportUnclassified }, "must name its reach"},
		{"unknown auth", func(s *Surface) { s.Auth.Kind = "magic" }, "unknown auth kind"},
		{"cloud on bearer", func(s *Surface) { s.Auth.Cloud = CloudAWS }, "non-cloud auth kind"},
		{"unknown cloud", func(s *Surface) { s.Auth.Kind, s.Auth.Cloud = AuthCloudIAM, "ibm" }, "unknown cloud provider"},
		{"unknown paging", func(s *Surface) { s.Paging.Style = "scroll" }, "unknown paging"},
		{"unknown build", func(s *Surface) { s.Build = "done" }, "unknown build state"},
		{"unknown sensitivity", func(s *Surface) { s.Sensitivity = "secret" }, "unknown sensitivity"},
		{"unknown secret class", func(s *Surface) { s.SecretClass = "maybe" }, "unknown secret class"},
		{"no source", func(s *Surface) { s.Sources = nil }, "no cited source"},
		{"bad date", func(s *Surface) { s.Sources[0].CheckedOn = "07/10/2026" }, "not YYYY-MM-DD"},
		{"relative url", func(s *Surface) { s.Sources[0].URL = "docs/admin" }, "absolute public URL"},
		{"userinfo url", func(s *Surface) { s.Sources[0].URL = "https://u:p@docs.example.com/" }, "absolute public URL"},
		{"repo path on api row", func(s *Surface) { s.Sources[0] = SourceRef{RepoPath: "docs/x.md", CheckedOn: "2026-10-07"} }, "only on a node-side row"},
		{"url and repo", func(s *Surface) { s.Sources[0].RepoPath = "docs/x.md" }, "either a url or a repo path"},
		{"reference only with supplies", func(s *Surface) { s.ReferenceOnly = true }, "reference_only"},
		{"empty row not reference", func(s *Surface) { s.Supplies, s.Controls = nil, nil }, "reference_only"},
		{"unknown class", func(s *Surface) { s.Supplies[0].Class = "Snapshot" }, "unknown data class"},
		{"duplicate class", func(s *Surface) { s.Supplies = append(s.Supplies, s.Supplies[0]) }, "listed twice"},
		{"unknown grain", func(s *Surface) { s.Supplies[0].Grain = "weekly" }, "unknown grain"},
		{"unknown unit", func(s *Surface) { s.Supplies[0].Unit = "euros" }, "unknown unit"},
		{"unknown accuracy", func(s *Surface) { s.Supplies[0].Accuracy = "good" }, "unknown accuracy"},
		{"unknown completeness", func(s *Surface) { s.Supplies[0].Coverage.Completeness = "all" }, "unknown completeness"},
		{"unknown identity", func(s *Surface) { s.Supplies[0].Identity = []IdentityKind{"ssn"} }, "unknown identity kind"},
		{"live claim on supply", func(s *Surface) { s.Supplies[0].Verification.Live = LiveLandingProven }, "only ever carries live_pending"},
		{"live claim on control", func(s *Surface) { s.Controls[0].Verification.Live = LiveConnectionProven }, "only ever carries live_pending"},
		{"synthetic on AR", func(s *Surface) {
			s.Build = BuildAwaitingReference
			s.Supplies[0].Build = BuildAwaitingReference
			s.Supplies[0].Verification.Synthetic = SynthContractFixtures
		}, "not driverable"},
		{"driverable supply on catalog row", func(s *Surface) { s.Build = BuildCatalogOnly }, "non-driverable surface"},
		{"content needs sensitivity", func(s *Surface) {
			s.Supplies = append(s.Supplies, Supply{
				Class: ClassContent, Grain: GrainEvent, Unit: UnitText, Accuracy: AccuracyExact,
				Coverage: CoveragePred{Completeness: CompletenessSegment}, Build: BuildBuildable,
				Verification: Verification{Synthetic: SynthDocOnly, Live: LivePending},
			})
		}, "sensitivity content"},
		{"credentials never secret", func(s *Surface) {
			s.Sensitivity, s.SecretClass = SensitivityCredentialMetadata, SecretNever
			s.Supplies[0].Class = ClassCredentials
		}, "never_returns_secret"},
		{"unknown control", func(s *Surface) { s.Controls[0].Class = "Everything" }, "unknown control class"},
		{"unknown facet", func(s *Surface) { s.Controls[0].Facet = "Z" }, "unknown facet"},
		{"ui facet with write", func(s *Surface) { s.Controls[0].Facet = FacetUIOnly }, "no machine read or write"},
		{"read facet with write", func(s *Surface) { s.Controls[0].Facet = FacetReadOnly }, "read without write"},
		{"api facet without write", func(s *Surface) { s.Controls[0].Caps.Write = false }, "needs write"},
		{"file facet without file", func(s *Surface) { s.Controls[0].Facet = FacetFile }, "local file or repository"},
		{"readback without read", func(s *Surface) { s.Controls[0].Caps = ControlCaps{Write: true, Readback: true} }, "readback needs read"},
		{"comparator on mirror", func(s *Surface) {
			s.Controls[0].Facet = FacetReadOnly
			s.Controls[0].Caps = ControlCaps{Read: true}
			s.Controls[0].Comparator = "usd_per_period"
		}, "comparator"},
		{"duplicate control", func(s *Surface) { s.Controls = append(s.Controls, s.Controls[0]) }, "listed twice"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := validRow()
			s.Supplies = append([]Supply(nil), s.Supplies...)
			s.Controls = append([]Control(nil), s.Controls...)
			s.Sources = append([]SourceRef(nil), s.Sources...)
			tc.mut(&s)
			errs := Validate(s)
			for _, e := range errs {
				if strings.Contains(e.Error(), tc.want) {
					return
				}
			}
			t.Fatalf("want an error containing %q, got %v", tc.want, errs)
		})
	}
}

func TestValidateAllowsNodeSideRepoSource(t *testing.T) {
	s := validRow()
	s.Reach, s.Transport, s.Auth.Kind = ReachNodeLocal, TransportLocalFile, AuthLocalFile
	s.Controls = nil
	s.Sources = []SourceRef{{RepoPath: "docs/hermes-adapter.md", CheckedOn: "2026-10-07"}}
	if errs := Validate(s); len(errs) != 0 {
		t.Fatalf("node-side row with a repo source should validate: %v", errs)
	}
}

func TestValidateCatalogDuplicateID(t *testing.T) {
	errs := ValidateCatalog([]Surface{validRow(), validRow()})
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "duplicate surface id") {
		t.Fatalf("want one duplicate error, got %v", errs)
	}
}

func TestSyntheticCeiling(t *testing.T) {
	cases := []struct {
		in   []EvidenceGrade
		want SyntheticRung
	}{
		{nil, SynthDocOnly},
		{[]EvidenceGrade{EvidenceVerifiedOfficial}, SynthMockVerified},
		{[]EvidenceGrade{EvidenceVerifiedOfficial, EvidenceVerifiedSDK}, SynthMockVerified},
		{[]EvidenceGrade{EvidenceVerifiedOfficial, EvidenceThirdParty}, SynthAssumptionTested},
		{[]EvidenceGrade{EvidenceInferred}, SynthAssumptionTested},
	}
	for _, tc := range cases {
		if got := SyntheticCeiling(tc.in); got != tc.want {
			t.Errorf("SyntheticCeiling(%v) = %s, want %s", tc.in, got, tc.want)
		}
	}
}

// TestThirdPartyCeilingIsAssumptionTested is the plan section 13 pin.
func TestThirdPartyCeilingIsAssumptionTested(t *testing.T) {
	if SyntheticCeiling([]EvidenceGrade{EvidenceThirdParty}) != SynthAssumptionTested {
		t.Fatal("a third-party contract must cap at assumption_tested")
	}
}

func TestRungOrder(t *testing.T) {
	syn := []SyntheticRung{SynthDocOnly, SynthContractFixtures, SynthAssumptionTested, SynthMockVerified}
	for i, r := range syn {
		if SyntheticRank(r) != i {
			t.Errorf("SyntheticRank(%s) = %d, want %d", r, SyntheticRank(r), i)
		}
	}
	live := []LiveRung{
		LivePending, LiveConnectionProven, LivePaginationProven, LiveLandingProven, LiveRevisionProven,
		LiveSpendComparisonProven,
	}
	for i, r := range live {
		if LiveRank(r) != i {
			t.Errorf("LiveRank(%s) = %d, want %d", r, LiveRank(r), i)
		}
	}
	if SyntheticRank("x") != -1 || LiveRank("x") != -1 {
		t.Error("unknown rungs must rank -1")
	}
}

func TestEveryReachHasATransport(t *testing.T) {
	for r := range reachKinds {
		if TransportFor(r) == "" {
			t.Errorf("reach %s has no transport", r)
		}
	}
	if len(reachTransport) != len(reachKinds) {
		t.Errorf("reachTransport has %d rows, reach kinds %d", len(reachTransport), len(reachKinds))
	}
}

func TestDimensionTableIsBijective(t *testing.T) {
	if len(DataClasses()) != 21 {
		t.Fatalf("plan 4.0 has 21 reporting classes, got %d", len(DataClasses()))
	}
	dims := map[Dimension]bool{}
	for _, c := range DataClasses() {
		d, ok := DimensionOf(c)
		if !ok {
			t.Fatalf("class %s has no dimension", c)
		}
		if dims[d] {
			t.Fatalf("dimension %+v used twice", d)
		}
		dims[d] = true
		back, ok := ReportingClass(d)
		if !ok || back != c {
			t.Fatalf("round trip of %s gave %s", c, back)
		}
	}
	if len(dimensionTable) != 21 {
		t.Fatalf("dimension table has %d rows", len(dimensionTable))
	}
}

func TestControlClassCount(t *testing.T) {
	// 23 classes of plan 4.0 plus SpendLimitSession, SpendLimitCredential and
	// CapacityReservation from the 4.3.3 review fold.
	if n := len(ControlClasses()); n != 26 {
		t.Fatalf("got %d control classes, want 26", n)
	}
}

func TestParseResourceType(t *testing.T) {
	cases := []struct {
		in   ResourceType
		kind ResourceKind
		ok   bool
	}{
		{"Container/github.repository/v1", KindContainer, true},
		{"Credential/anthropic.wif_rule/v2", KindCredential, true},
		{"Container/repository/v1", "", false},
		{"Widget/github.repository/v1", "", false},
		{"Container/github.repository/v0", "", false},
		{"container/github.repository/v1", "", false},
	}
	for _, tc := range cases {
		k, _, err := ParseResourceType(tc.in)
		if (err == nil) != tc.ok || k != tc.kind {
			t.Errorf("ParseResourceType(%s) = %s, %v", tc.in, k, err)
		}
	}
}

func TestExternalIDNamespace(t *testing.T) {
	if !(ExternalID{Namespace: "github.user", Value: "123"}).Valid() {
		t.Error("namespaced id should be valid")
	}
	for _, e := range []ExternalID{
		{Namespace: "", Value: "1"},
		{Namespace: "github", Value: "1"},
		{Namespace: "GitHub.user", Value: "1"},
		{Namespace: "github.user"},
	} {
		if e.Valid() {
			t.Errorf("%+v should be invalid", e)
		}
	}
}
