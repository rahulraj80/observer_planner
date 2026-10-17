package accessgrant

import (
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/orgcontract"
)

// TestResolveRuleFacts pins the C156-05 resolution table, one case per row:
// severity and lock come from the server's own sources, an unreadable bundle
// or an unknown rule is unknown (the strict reading), no org bundle locks
// nothing, and under a bundle a rule is locked unless marked overridable.
func TestResolveRuleFacts(t *testing.T) {
	sev := map[string]string{"R-171": "high", "R-101": "critical", "ORG-1": "warn"}
	cases := []struct {
		name string
		rule string
		src  RuleSources
		want RuleFacts
	}{
		{"bundle unreadable", "R-171", RuleSources{Severity: sev}, RuleFacts{}},
		{"unknown rule", "R-999", RuleSources{Read: true, Bundle: true, Severity: sev}, RuleFacts{}},
		{"empty rule id", "", RuleSources{Read: true, Severity: map[string]string{"": "high"}}, RuleFacts{}},
		{"no org bundle", "R-101", RuleSources{Read: true, Severity: sev}, RuleFacts{Known: true, Severity: "critical"}},
		{"bundle, rule not overridable", "R-171", RuleSources{Read: true, Bundle: true, Severity: sev}, RuleFacts{Known: true, Locked: true, Severity: "high"}},
		{"bundle, rule overridable", "R-171", RuleSources{Read: true, Bundle: true, Severity: sev, Overridable: map[string]bool{"R-171": true}}, RuleFacts{Known: true, Severity: "high"}},
		{"org-defined rule", "ORG-1", RuleSources{Read: true, Bundle: true, Severity: sev}, RuleFacts{Known: true, Locked: true, Severity: "warn"}},
	}
	for _, c := range cases {
		if got := ResolveRuleFacts(c.rule, c.src); got != c.want {
			t.Errorf("%s: got %+v, want %+v", c.name, got, c.want)
		}
	}
}

// TestDecideRuleSwapIsWidening: an approver who names a different rule than
// the request asked about is widening it (C156-05), never a silent swap.
func TestDecideRuleSwapIsWidening(t *testing.T) {
	req := Normalized{
		Duration: 8 * time.Hour,
		Request: Request{
			SubjectKind: orgcontract.AccessGrantSubjectMachine,
			Payload:     orgcontract.AccessRequestPayload{RuleID: "R-171", Effect: orgcontract.AccessGrantEffectSoften},
		},
	}
	p := Proposal{
		Type: TypeGuardExemption, Effect: orgcontract.AccessGrantEffectSoften,
		SubjectKind: orgcontract.AccessGrantSubjectMachine, UserID: "alice", MachineIdentity: "laptop-7",
		RuleID: "R-101", ExpiresAt: t0.Add(8 * time.Hour),
	}
	facts := Facts{
		Now: t0, Requested: &req, RequesterID: "alice",
		Rule:     RuleFacts{Known: true, Severity: "high"},
		Approver: Approver{UserID: "bob", HasKey: true},
	}
	d := Decide(p, facts, DefaultSettings())
	if !d.Widened {
		t.Fatalf("a swapped rule was not read as widening: %+v", d)
	}
	p.RuleID = "R-171"
	d = Decide(p, facts, DefaultSettings())
	if d.Widened {
		t.Fatalf("the asked rule was read as widening: %+v", d.WidenedBy)
	}
}

// TestReviewDue pins D-080's 90-day re-review (C156-30): only a grant with no
// expiry is ever due, at its issue time plus 90 days.
func TestReviewDue(t *testing.T) {
	issued := t0.UnixNano()
	if at, due := ReviewDue(issued, 0, t0.Add(89*24*time.Hour)); due || at != t0.Add(PerpetualReviewEvery).UnixNano() {
		t.Fatalf("89 days: at=%d due=%v", at, due)
	}
	if _, due := ReviewDue(issued, 0, t0.Add(90*24*time.Hour)); !due {
		t.Fatal("90 days: not due")
	}
	if at, due := ReviewDue(issued, t0.Add(time.Hour).UnixNano(), t0.Add(365*24*time.Hour)); due || at != 0 {
		t.Fatal("a grant with an expiry is never due")
	}
}

// TestNormalizeFixesDistinctApprover: D-086 is not an org choice. A stored
// or sent false is read as true, and the rest of the document is kept.
func TestNormalizeFixesDistinctApprover(t *testing.T) {
	s := DefaultSettings()
	s.RequireDistinctApprover = false
	s.MaxOfflineS = 3600
	out, probs := s.Normalize()
	if len(probs) > 0 || !out.RequireDistinctApprover || out.MaxOfflineS != 3600 {
		t.Fatalf("got %+v %v", out, probs)
	}
}
