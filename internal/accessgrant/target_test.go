package accessgrant

import (
	"strings"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/orgcontract"
)

// targetReceipt is a fresh, verified receipt at now.
func targetReceipt(now time.Time) Receipt {
	n := now.UnixNano()
	return Receipt{ServerNowNs: n, ReceivedAtNs: n, LastVerifiedNs: n, HighWaterNs: n, MaxOfflineS: 86400}
}

// TestMatchTarget pins the non-guard grant match (request-grants G8): a grant
// of the asked type naming exactly the asked target, in the asked project
// when it pins one, honoured now. Among several, the narrowest, then the
// earliest expiry, then the lowest id.
func TestMatchTarget(t *testing.T) {
	t.Parallel()
	now := time.Now()
	rc := targetReceipt(now)
	exp := now.Add(time.Hour).UnixNano()
	g := func(id int64, typ, target, proj string, expires, maxUses int64) Cached {
		return Cached{Grant: orgcontract.AccessGrantWire{
			ID: id, Type: typ, Target: target, ProjectRootHash: proj, SubjectKind: orgcontract.AccessGrantSubjectMember,
			StartsAtNs: now.Add(-time.Minute).UnixNano(), ExpiresAtNs: expires, MaxUses: maxUses,
		}}
	}
	cases := []struct {
		name   string
		q      TargetQuery
		grants []Cached
		wantID int64
	}{
		{"exact target", TargetQuery{Type: TypeToolAllow, Target: "codex"}, []Cached{g(1, TypeToolAllow, "codex", "", exp, 0)}, 1},
		{"another target", TargetQuery{Type: TypeToolAllow, Target: "crush"}, []Cached{g(1, TypeToolAllow, "codex", "", exp, 0)}, 0},
		{"another type", TargetQuery{Type: TypeModelAccess, Target: "codex"}, []Cached{g(1, TypeToolAllow, "codex", "", exp, 0)}, 0},
		{
			"a guard exemption never matches",
			TargetQuery{Type: TypeToolAllow, Target: "codex"},
			[]Cached{{Grant: orgcontract.AccessGrantWire{ID: 1, Type: TypeGuardExemption, Target: "codex", StartsAtNs: 1, ExpiresAtNs: exp}}},
			0,
		},
		{"targets compare exactly", TargetQuery{Type: TypeModelAccess, Target: "GPT-5"}, []Cached{g(1, TypeModelAccess, "gpt-5", "", exp, 0)}, 0},
		{"an empty target matches nothing", TargetQuery{Type: TypeToolAllow}, []Cached{g(1, TypeToolAllow, "", "", exp, 0)}, 0},
		{
			"a project-pinned grant needs that project",
			TargetQuery{Type: TypeToolAllow, Target: "codex", ProjectRootHash: "p2"},
			[]Cached{g(1, TypeToolAllow, "codex", "p1", exp, 0)},
			0,
		},
		{
			"the project-pinned grant wins over the any-project one",
			TargetQuery{Type: TypeToolAllow, Target: "codex", ProjectRootHash: "p1"},
			[]Cached{g(1, TypeToolAllow, "codex", "", exp, 0), g(2, TypeToolAllow, "codex", "p1", exp+1, 0)},
			2,
		},
		{
			"then the earliest expiry",
			TargetQuery{Type: TypeToolAllow, Target: "codex"},
			[]Cached{g(3, TypeToolAllow, "codex", "", 0, 0), g(4, TypeToolAllow, "codex", "", exp, 0)},
			4,
		},
		{
			"an exhausted grant is skipped",
			TargetQuery{Type: TypeToolAllow, Target: "codex"},
			[]Cached{{Grant: g(5, TypeToolAllow, "codex", "", exp, 1).Grant, UsesUsed: 1}},
			0,
		},
		{
			"an expired grant is skipped",
			TargetQuery{Type: TypeToolAllow, Target: "codex"},
			[]Cached{g(6, TypeToolAllow, "codex", "", now.Add(-time.Second).UnixNano(), 0)},
			0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hit, ok := MatchTarget(tc.q, tc.grants, rc, now)
			if (tc.wantID != 0) != ok || hit.GrantID != tc.wantID {
				t.Fatalf("hit %+v ok=%v, want grant %d", hit, ok, tc.wantID)
			}
		})
	}
}

// TestHonouredUntil pins the instant a grant stops being honoured if nothing
// changes: the earliest of its expiry, the remaining TTL measured at receipt
// and the offline window after the last verified sync. A feature exception
// written for a reader with no grant cache (the governance sidecar) carries
// it, so the reader ends the exception on its own.
func TestHonouredUntil(t *testing.T) {
	t.Parallel()
	now := time.Unix(1_800_000_000, 0)
	rc := targetReceipt(now)
	g := orgcontract.AccessGrantWire{ID: 1, Type: TypeFeatureEnable, Target: "k", StartsAtNs: now.UnixNano(), ExpiresAtNs: now.Add(2 * time.Hour).UnixNano()}
	if got := HonouredUntilNs(g, rc); got != now.Add(2*time.Hour).UnixNano() {
		t.Fatalf("expiry first: %d", got)
	}
	g.ExpiresAtNs = 0
	if got := HonouredUntilNs(g, rc); got != now.Add(24*time.Hour).UnixNano() {
		t.Fatalf("perpetual: the offline window bounds it, got %d", got)
	}
	// A body built by a server whose clock ran an hour ahead: the remaining
	// TTL at receipt ends it an hour before the absolute expiry.
	g.ExpiresAtNs = now.Add(3 * time.Hour).UnixNano()
	rc.ServerNowNs = now.Add(time.Hour).UnixNano()
	if got := HonouredUntilNs(g, rc); got != now.Add(2*time.Hour).UnixNano() {
		t.Fatalf("remaining TTL: %d", got)
	}
}

// TestDecideTarget pins the target checks a non-guard grant needs (G8): a
// tool grant names a tool Observer launches (an MCP server is approved in
// Agent Access, never through a second path), a feature grant names a
// setting the org can pin and is time-bound only.
func TestDecideTarget(t *testing.T) {
	t.Parallel()
	now := time.Now()
	base := func(typ, target string) Proposal {
		return Proposal{
			Type: typ, SubjectKind: orgcontract.AccessGrantSubjectMember, UserID: "u1", Target: target,
			ExpiresAt: now.Add(24 * time.Hour),
		}
	}
	facts := func(tf TargetFacts) Facts {
		return Facts{Now: now, Approver: Approver{UserID: "a1", HasKey: true}, Target: tf}
	}
	cases := []struct {
		name     string
		p        Proposal
		tf       TargetFacts
		wantCode string
		wantText string
	}{
		{name: "a registry tool", p: base(TypeToolAllow, "codex"), tf: TargetFacts{KnownTool: true}},
		{
			name: "an MCP server routes to Agent Access", p: base(TypeToolAllow, "mcp__github"), tf: TargetFacts{},
			wantCode: CodeBadValue, wantText: "Agent Access",
		},
		{name: "a pinnable setting", p: base(TypeFeatureEnable, "terminal.enabled"), tf: TargetFacts{PinnableSetting: true}},
		{name: "an unknown setting", p: base(TypeFeatureEnable, "nope.key"), tf: TargetFacts{}, wantCode: CodeBadValue, wantText: "pin"},
		{name: "a node feature", p: base(TypeFeatureEnable, "terminals"), tf: TargetFacts{NodeFeature: true}},
		{name: "a use-limited feature grant", p: func() Proposal {
			p := base(TypeFeatureEnable, "terminal.enabled")
			p.MaxUses = 3
			return p
		}(), tf: TargetFacts{PinnableSetting: true}, wantCode: CodeBadValue, wantText: "time"},
		{name: "a model needs no registry", p: base(TypeModelAccess, "gpt-5"), tf: TargetFacts{}},
		{name: "an AI tool setting", p: base(TypeConfigEdit, "codex/model"), tf: TargetFacts{ConfigSetting: true}},
		{name: "not an AI tool setting", p: base(TypeConfigEdit, "codex/nope"), tf: TargetFacts{}, wantCode: CodeBadValue, wantText: "catalogue"},
		{name: "a use-limited edit grant", p: func() Proposal {
			p := base(TypeConfigEdit, "codex/*")
			p.MaxUses = 2
			return p
		}(), tf: TargetFacts{ConfigSetting: true}, wantCode: CodeBadValue, wantText: "time"},
		{name: "a hideable page", p: base(TypePageAccess, "security/rules"), tf: TargetFacts{NodePage: true}},
		{name: "not a page", p: base(TypePageAccess, "nope"), tf: TargetFacts{}, wantCode: CodeBadValue, wantText: "page"},
		{name: "a use-limited page grant", p: func() Proposal {
			p := base(TypePageAccess, "cost")
			p.MaxUses = 1
			return p
		}(), tf: TargetFacts{NodePage: true}, wantCode: CodeBadValue, wantText: "time"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := Decide(tc.p, facts(tc.tf), DefaultSettings())
			var hit *Problem
			for i := range d.Problems {
				if d.Problems[i].Field == "target" || d.Problems[i].Field == "max_uses" {
					hit = &d.Problems[i]
				}
			}
			if tc.wantCode == "" {
				if hit != nil {
					t.Fatalf("unexpected problem %+v (all: %+v)", *hit, d.Problems)
				}
				return
			}
			if hit == nil || hit.Code != tc.wantCode || !strings.Contains(hit.Message, tc.wantText) {
				t.Fatalf("problems %+v, want %s mentioning %q", d.Problems, tc.wantCode, tc.wantText)
			}
		})
	}
}

// TestHonouredTargets (T-435): the targets of the honoured grants of one
// type, sorted, once each; another type, an expired or a not-yet-started
// grant contributes nothing.
func TestHonouredTargets(t *testing.T) {
	t.Parallel()
	now := time.Now()
	rc := targetReceipt(now)
	g := func(id int64, typ, target string, starts, expires time.Time) Cached {
		return Cached{Grant: orgcontract.AccessGrantWire{
			ID: id, Type: typ, Target: target, SubjectKind: orgcontract.AccessGrantSubjectMember,
			StartsAtNs: starts.UnixNano(), ExpiresAtNs: expires.UnixNano(),
		}}
	}
	past, soon := now.Add(-time.Minute), now.Add(time.Hour)
	got := HonouredTargets(TypePageAccess, []Cached{
		g(1, TypePageAccess, "security/rules", past, soon),
		g(2, TypePageAccess, "cost", past, soon),
		g(3, TypePageAccess, "cost", past, soon),
		g(4, TypePageAccess, "cache", past, now.Add(-time.Second)),
		g(5, TypePageAccess, "routing", now.Add(time.Minute), soon),
		g(6, TypeFeatureEnable, "terminals", past, soon),
	}, rc, now)
	if strings.Join(got, ",") != "cost,security/rules" {
		t.Fatalf("honoured targets = %v", got)
	}
	if HonouredTargets(TypeGuardExemption, nil, rc, now) != nil {
		t.Fatal("a non-target type lists targets")
	}
}
