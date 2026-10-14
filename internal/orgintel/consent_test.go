package orgintel

import (
	"strings"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/orgcontract"
	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

func req(required bool, version int64) *orgcontract.IntelRequirementBody {
	return &orgcontract.IntelRequirementBody{
		OrgName: "Acme", Enabled: true, EvidenceLevel: "titles",
		Purposes:                 []string{"structural_activity_insights"},
		RequireIndividualConsent: required, RequirementVersion: version,
		RequiredSince: "2026-10-01T00:00:00Z", DeadlineDays: 5,
	}
}

// TestPendingTable pins one case per row of the pending table.
func TestPendingTable(t *testing.T) {
	now := time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)
	active := &Consent{RequirementVersion: 2, EvidenceLevel: "titles", Purposes: []string{"structural_activity_insights"}}
	cases := []struct {
		name    string
		in      Inputs
		pending bool
		reason  string
	}{
		{"no_requirement_document", Inputs{Enrolled: true, ConfigOptIn: true, Active: active}, false, ""},
		{"not_required", Inputs{Enrolled: true, Requirement: req(false, 0)}, false, ""},
		{"never_accepted", Inputs{Enrolled: true, Requirement: req(true, 2)}, true, PendingNotAccepted},
		{"opt_in_switched_off", Inputs{Enrolled: true, Requirement: req(true, 2), Active: active}, true, PendingNotAccepted},
		{"older_version_accepted", Inputs{Enrolled: true, Requirement: req(true, 3), ConfigOptIn: true, Active: active}, true, PendingTermsChanged},
		{"current_version_accepted", Inputs{Enrolled: true, Requirement: req(true, 2), ConfigOptIn: true, Active: active}, false, ""},
	}
	if len(PendingRuleNames()) != len(cases) {
		t.Fatalf("pending table rows = %v, cases = %d", PendingRuleNames(), len(cases))
	}
	for _, c := range cases {
		c.in.Now = now
		st := Derive(c.in)
		if st.Class != ClassIndividual || st.Pending != c.pending || st.PendingReason != c.reason {
			t.Errorf("%s: %+v, want pending=%v reason=%q", c.name, st, c.pending, c.reason)
		}
	}
	// The deadline is measured from RequiredSince.
	st := Derive(Inputs{Enrolled: true, Requirement: req(true, 2), Now: now})
	if st.DeadlineAt.Format(time.RFC3339) != "2026-10-06T00:00:00Z" || st.DeadlinePassed {
		t.Fatalf("deadline = %v passed=%v", st.DeadlineAt, st.DeadlinePassed)
	}
}

// TestINV13NoOrgDocumentCreatesAnOptIn: whatever the org's requirement says,
// an individual node is effectively opted in only with BOTH its own config key
// and its own consent record.
func TestINV13NoOrgDocumentCreatesAnOptIn(t *testing.T) {
	active := &Consent{RequirementVersion: 1, EvidenceLevel: "titles", Purposes: []string{"structural_activity_insights"}}
	for _, r := range []*orgcontract.IntelRequirementBody{nil, req(false, 0), req(true, 1), req(true, 9)} {
		for _, cfg := range []bool{false, true} {
			for _, a := range []*Consent{nil, active} {
				st := Derive(Inputs{Enrolled: true, Requirement: r, ConfigOptIn: cfg, Active: a, Now: time.Now()})
				want := cfg && a != nil
				if st.EffectiveOptIn != want {
					t.Fatalf("req=%+v cfg=%v active=%v: effective=%v, want %v", r, cfg, a != nil, st.EffectiveOptIn, want)
				}
				// A node that accepted before reports its posture either way.
				rep, ok := Report(Inputs{Enrolled: true, Requirement: r, ConfigOptIn: cfg, Active: a, EverAccepted: true}, st)
				if !ok || rep.OptIn != want {
					t.Fatalf("report opt_in = %v (ok=%v), want %v", rep.OptIn, ok, want)
				}
				// One that never accepted has nothing to report (outstanding,
				// not a withdrawal).
				if a == nil {
					if _, ok := Report(Inputs{Enrolled: true, Requirement: r, ConfigOptIn: cfg}, st); ok {
						t.Fatalf("a never-accepted node reported a consent posture (req=%+v cfg=%v)", r, cfg)
					}
				}
			}
		}
	}
	// Managed and unenrolled nodes never report and are never "opted in" here.
	for _, in := range []Inputs{{Enrolled: true, Managed: true, ConfigOptIn: true, Active: active}, {ConfigOptIn: true, Active: active}} {
		st := Derive(in)
		if st.EffectiveOptIn {
			t.Fatalf("class %s effective opt-in", st.Class)
		}
		if _, ok := Report(in, st); ok {
			t.Fatalf("class %s reports a consent", st.Class)
		}
	}
	if st := Derive(Inputs{Enrolled: true, Managed: true, Requirement: req(false, 0)}); !st.NoticeOnly || !st.OrgEnabled {
		t.Fatalf("managed status = %+v", st)
	}
}

// TestCoveredAndPlanAccept: an accept records exactly the disclosed coverage,
// and a later widening is no longer covered.
func TestCoveredAndPlanAccept(t *testing.T) {
	r := req(true, 1)
	plan := PlanAccept(*r)
	if plan.EvidenceLevel != "titles" || len(plan.Purposes) != 1 || plan.RequirementVersion != 1 || len(plan.DisclosureDigest) != 64 {
		t.Fatalf("plan = %+v", plan)
	}
	active := &Consent{RequirementVersion: plan.RequirementVersion, EvidenceLevel: plan.EvidenceLevel, Purposes: plan.Purposes}
	if !Derive(Inputs{Enrolled: true, Requirement: r, ConfigOptIn: true, Active: active}).Covered {
		t.Fatal("the accepted coverage is not covered")
	}
	wider := req(true, 2)
	wider.EvidenceLevel = "excerpts"
	if Derive(Inputs{Enrolled: true, Requirement: wider, ConfigOptIn: true, Active: active}).Covered {
		t.Fatal("a widened level is still covered")
	}
	unknown := *r
	unknown.EvidenceLevel = "everything"
	if PlanAccept(unknown).EvidenceLevel != "structural" {
		t.Fatal("an unknown level must be recorded as the narrowest")
	}
}

// TestPureImports pins the package free of SQL, HTTP and fsnotify.
func TestPureImports(t *testing.T) {
	importpin.Check(t, importpin.Spec{
		Custom: func(_, p string) string {
			for _, bad := range []string{"database/sql", "net/http", "fsnotify"} {
				if strings.Contains(p, bad) {
					return "contains " + bad
				}
			}
			return ""
		},
		Reason: "internal/orgintel must stay pure",
	})
}

// TestPlanSubmit pins org-plane submission: one case per refusal row in table
// order, then the level rules (managed = the org's level; individual = the
// lower of the org's and the accepted level, within the accepted purposes; a
// level whose purpose is not allowed falls back to structural).
func TestPlanSubmit(t *testing.T) {
	now := time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)
	both := []string{"structural_activity_insights", "bounded_context_enrichment"}
	r := func(level string, purposes []string) *orgcontract.IntelRequirementBody {
		b := req(false, 1)
		b.EvidenceLevel, b.Purposes = level, purposes
		return b
	}
	off := r("titles", both)
	off.Enabled = false
	accepted := func(level string, purposes ...string) *Consent {
		return &Consent{RequirementVersion: 1, EvidenceLevel: level, Purposes: purposes}
	}
	refusals := []struct {
		in     Inputs
		railOn bool
		reason string
	}{
		{Inputs{Now: now}, true, "not_enrolled"},
		{Inputs{Enrolled: true, Now: now}, true, "no_terms"},
		{Inputs{Enrolled: true, Requirement: off, ConfigOptIn: true, Active: accepted("titles", both...), Now: now}, true, "org_off"},
		{Inputs{Enrolled: true, Managed: true, Requirement: r("titles", both), Now: now}, false, "managed_rail_off"},
		{Inputs{Enrolled: true, Requirement: r("titles", both), Active: accepted("titles", both...), Now: now}, true, "individual_not_opted_in"},
	}
	if len(refusals) != len(SubmitRuleNames()) {
		t.Fatalf("rows %v, cases %d", SubmitRuleNames(), len(refusals))
	}
	for i, c := range refusals {
		if p := PlanSubmit(c.in, c.railOn); p.OK || p.Reason != c.reason || p.Reason != SubmitRuleNames()[i] {
			t.Errorf("case %d: %+v, want %s", i, p, c.reason)
		}
	}
	levels := []struct {
		name   string
		in     Inputs
		level  string
		reason string
	}{
		{"managed takes the org level", Inputs{Enrolled: true, Managed: true, Requirement: r("excerpts", both), Now: now}, "excerpts", ""},
		{"individual capped at accepted", Inputs{Enrolled: true, Requirement: r("excerpts", both), ConfigOptIn: true, Active: accepted("titles", both...), Now: now}, "titles", ""},
		{"individual capped at org", Inputs{Enrolled: true, Requirement: r("structural", both), ConfigOptIn: true, Active: accepted("excerpts", both...), Now: now}, "structural", ""},
		{"purpose not accepted falls back", Inputs{Enrolled: true, Requirement: r("excerpts", both), ConfigOptIn: true, Active: accepted("excerpts", "structural_activity_insights"), Now: now}, "structural", ""},
		{"no allowed purpose", Inputs{Enrolled: true, Requirement: r("titles", []string{"bounded_context_enrichment"}), ConfigOptIn: true, Active: accepted("titles", "structural_activity_insights"), Now: now}, "", "purpose_not_allowed"},
	}
	for _, c := range levels {
		p := PlanSubmit(c.in, true)
		if string(p.Level) != c.level || p.Reason != c.reason || p.OK != (c.reason == "") {
			t.Errorf("%s: %+v", c.name, p)
		}
		if p.OK && p.Purpose != p.Level.Purpose() {
			t.Errorf("%s: purpose %q for level %q", c.name, p.Purpose, p.Level)
		}
	}
}
