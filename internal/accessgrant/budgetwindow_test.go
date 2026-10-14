package accessgrant

import (
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/orgcontract"
)

// TestBudgetIncreaseWindow pins T-785: a budget increase is bounded by the
// END of the budget's current period, computed in the budget's own timezone,
// and "until the period ends" is resolved by the server at decide time, so a
// request asked minutes earlier never goes stale. The grid is every window
// shape (a short fixed window, exactly until the period end, the stale
// seconds a node measured when the developer asked, and longer than the
// period) across every period type, in timezones that are not UTC.
func TestBudgetIncreaseWindow(t *testing.T) {
	// 07:45 UTC is 13:15 in Kolkata and 00:45 in Los Angeles: the operator's
	// own repro time (T-785), well inside every period.
	now := time.Date(2026, 10, 13, 7, 45, 0, 0, time.UTC)
	approver := Approver{UserID: "fin", HasKey: true}
	type want struct {
		ok      bool
		expires func(end time.Time) time.Time
	}
	windows := []struct {
		name string
		set  func(p *Proposal, end time.Time)
		want want
	}{
		{
			name: "1h",
			set:  func(p *Proposal, _ time.Time) { p.ExpiresAt = now.Add(time.Hour) },
			want: want{ok: true, expires: func(time.Time) time.Time { return now.Add(time.Hour) }},
		},
		{
			name: "until end of period",
			set:  func(p *Proposal, _ time.Time) { p.UntilPeriodEnd = true },
			want: want{ok: true, expires: func(end time.Time) time.Time { return end }},
		},
		{
			name: "exactly the period end as a time",
			set:  func(p *Proposal, end time.Time) { p.ExpiresAt = end },
			want: want{ok: true, expires: func(end time.Time) time.Time { return end }},
		},
		{
			// The seconds a node measured when the developer asked "until
			// the period ends" two minutes ago, replayed from now: this is
			// what the grant dialog used to send, and it IS past the end.
			name: "stale until-end seconds",
			set: func(p *Proposal, end time.Time) {
				asked := now.Add(-2 * time.Minute)
				p.ExpiresAt = now.Add(end.Sub(asked))
			},
			want: want{ok: false},
		},
		{
			name: "longer than the period",
			set:  func(p *Proposal, _ time.Time) { p.ExpiresAt = now.Add(31 * 24 * time.Hour) },
			want: want{ok: false},
		},
	}
	periods := []struct {
		period, tz string
		end        time.Time
	}{
		{PeriodRolling30d, "Asia/Kolkata", now.Add(30 * 24 * time.Hour)},
		{PeriodCalendarMonth, "Asia/Kolkata", time.Date(2026, 10, 31, 18, 30, 0, 0, time.UTC)},
		{PeriodCalendarDay, "Asia/Kolkata", time.Date(2026, 10, 13, 18, 30, 0, 0, time.UTC)},
		{PeriodCalendarMonth, "America/Los_Angeles", time.Date(2026, 11, 1, 7, 0, 0, 0, time.UTC)},
		{PeriodCalendarDay, "America/Los_Angeles", time.Date(2026, 10, 14, 7, 0, 0, 0, time.UTC)},
	}
	for _, pr := range periods {
		end := PeriodEnd(pr.period, pr.tz, now)
		if !end.Equal(pr.end) {
			t.Fatalf("PeriodEnd(%s, %s) = %s, want %s", pr.period, pr.tz, end, pr.end)
		}
		for _, w := range windows {
			t.Run(pr.period+"/"+pr.tz+"/"+w.name, func(t *testing.T) {
				p := Proposal{
					Type: TypeBudgetIncrease, SubjectKind: orgcontract.AccessGrantSubjectMachine, MachineIdentity: "laptop-7",
					UserID: "alice", BudgetID: "b-alice", DeltaUSD: 1,
				}
				w.set(&p, end)
				d := Decide(p, Facts{Now: now, Approver: approver, BaseCapUSD: 5, BudgetPeriodEnd: end}, Settings{})
				if !w.want.ok {
					if !hasFieldCode(d, "expires_at", CodeOutOfRange) {
						t.Fatalf("want an expires_at out_of_range refusal, got %+v", d.Problems)
					}
					return
				}
				if len(d.Problems) != 0 {
					t.Fatalf("refused: %+v", d.Problems)
				}
				if got := w.want.expires(end); !d.ExpiresAt.Equal(got) {
					t.Fatalf("expires %s, want %s", d.ExpiresAt, got)
				}
				if d.Tier != TierKey {
					t.Fatalf("a bounded budget window needs the super-admin: %+v", d.TierReasons)
				}
			})
		}
	}
}

// TestUntilPeriodEndShapes pins the edges of the until-period-end ask: it is
// never perpetual, it is a budget increase's alone, and an unknown period
// falls back to MaxBudgetFallback like any other budget window.
func TestUntilPeriodEndShapes(t *testing.T) {
	now := t0
	approver := Approver{UserID: "fin", HasKey: true}
	if (Proposal{UntilPeriodEnd: true}).Perpetual() {
		t.Fatal("until_period_end read as perpetual")
	}
	guard := Proposal{
		Type: TypeGuardExemption, SubjectKind: orgcontract.AccessGrantSubjectMember, UserID: "alice",
		RuleID: "R-171", Effect: orgcontract.AccessGrantEffectSoften, UntilPeriodEnd: true,
	}
	if d := Decide(guard, Facts{Now: now, Approver: approver, Rule: RuleFacts{Known: true}}, Settings{}); !hasFieldCode(d, "expires_at", CodeBadValue) {
		t.Fatalf("a guard exemption until the period end: %+v", d.Problems)
	}
	budget := Proposal{
		Type: TypeBudgetIncrease, SubjectKind: orgcontract.AccessGrantSubjectMember, UserID: "alice",
		BudgetID: "b-alice", DeltaTokens: 1, UntilPeriodEnd: true,
	}
	d := Decide(budget, Facts{Now: now, Approver: approver, BaseCapTokens: 10}, Settings{})
	if len(d.Problems) != 0 || !d.ExpiresAt.Equal(now.Add(MaxBudgetFallback)) {
		t.Fatalf("unknown period: expires %s problems %+v", d.ExpiresAt, d.Problems)
	}
	if PeriodEnd("calendar_day", "", now).IsZero() || !PeriodEnd("fortnight", "UTC", now).IsZero() {
		t.Fatal("PeriodEnd: a known period needs an end and an unknown one none")
	}
}

func hasFieldCode(d Decision, field, code string) bool {
	for _, p := range d.Problems {
		if p.Field == field && p.Code == code {
			return true
		}
	}
	return false
}
