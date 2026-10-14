package planlimits

import (
	"testing"
	"time"
)

func passive(prov, src, scope string, at time.Time, frac float64) ScopedSnapshot {
	d, _ := SourceByID(src)
	s := newSnap(prov, src, d.Kind, ConfPercentOnly, at)
	reset := at.Add(3 * time.Hour)
	length := 5 * time.Hour
	s.Windows = []Window{{Key: "5h", Unit: UnitPercent, UsedFrac: F(frac), ResetsAt: &reset, Length: &length}}
	return ScopedSnapshot{Snapshot: s, Scope: scope, Reason: ScopeBound}
}

func cardsByProvider(cards []Card) map[string][]Card {
	m := map[string][]Card{}
	for _, c := range cards {
		m[c.Provider] = append(m[c.Provider], c)
	}
	return m
}

func TestBuildViewAttachRules(t *testing.T) {
	now := t0
	codexAcct := AccountInfo{ID: "a1", Provider: ProviderChatGPT, Source: SourceCodexAppServer, Enabled: true, State: RunState{Status: StatusOK}}
	appSnap := newSnap(ProviderChatGPT, SourceCodexAppServer, KindVendorRPC, ConfPercentOnly, now.Add(-time.Minute))
	appSnap.Windows = []Window{{Key: "weekly", Unit: UnitPercent, UsedFrac: F(0.4)}}
	manualCursor := AccountInfo{
		ID: "m1", Provider: ProviderCursor, Source: SourceManual, Enabled: true,
		Params: map[string]string{"unit": "usd", "allowance": "20"},
	}
	manSnap, _ := ManualSnapshot(ProviderCursor, manualCursor.Params, now)

	cases := []struct {
		name  string
		in    ViewInput
		check func(t *testing.T, cards []Card)
	}{
		{
			"one passive scope joins the one account",
			ViewInput{
				Now:      now,
				Accounts: []AccountInfo{codexAcct}, Latest: map[string]Snapshot{"a1": appSnap},
				Passive: []ScopedSnapshot{passive(ProviderChatGPT, SourceCodexLogs, "acct:k1", now.Add(-2*time.Minute), 0.2)},
			},
			func(t *testing.T, cards []Card) {
				if len(cards) != 1 || len(cards[0].Windows) != 2 || cards[0].AccountID != "a1" {
					t.Fatalf("want one merged card: %+v", cards)
				}
			},
		},
		{
			"two passive scopes stay apart",
			ViewInput{
				Now:      now,
				Accounts: []AccountInfo{codexAcct}, Latest: map[string]Snapshot{"a1": appSnap},
				Passive: []ScopedSnapshot{
					passive(ProviderChatGPT, SourceCodexLogs, "acct:k1", now, 0.2),
					passive(ProviderChatGPT, SourceCodexLogs, "acct:k2", now, 0.3),
				},
			},
			func(t *testing.T, cards []Card) {
				if len(cards) != 3 {
					t.Fatalf("two accounts are never merged by guess: %d cards", len(cards))
				}
			},
		},
		{
			"passive alone is its own card",
			ViewInput{
				Now: now,
				Passive: []ScopedSnapshot{
					passive(ProviderClaude, SourceClaudeProxy, DefaultScope, now, 0.5),
					passive(ProviderClaude, SourceClaudeStatusLine, DefaultScope, now.Add(time.Minute), 0.55),
				},
			},
			func(t *testing.T, cards []Card) {
				if len(cards) != 1 || cards[0].ID != "passive:claude:default" || len(cards[0].Sources) != 2 {
					t.Fatalf("cards %+v", cards)
				}
				if cards[0].Windows[0].Source != SourceClaudeStatusLine {
					t.Errorf("the newer reading wins: %+v", cards[0].Windows[0])
				}
			},
		},
		{
			"manual joins the provider's single card",
			ViewInput{
				Now:      now,
				Accounts: []AccountInfo{manualCursor, {ID: "c1", Provider: ProviderCursor, Source: SourceCursorApp, Enabled: true}},
				Latest:   map[string]Snapshot{"m1": manSnap},
			},
			func(t *testing.T, cards []Card) {
				if len(cards) != 1 || cards[0].AccountID != "c1" || !cards[0].Experimental {
					t.Fatalf("cards %+v", cards)
				}
			},
		},
		{
			"auth failure clears the numbers",
			ViewInput{
				Now:      now,
				Accounts: []AccountInfo{{ID: "a1", Provider: ProviderChatGPT, Source: SourceCodexAppServer, Enabled: true, State: RunState{Status: StatusAuthNeeded, Stopped: true}}},
				Latest:   map[string]Snapshot{"a1": appSnap},
			},
			func(t *testing.T, cards []Card) {
				if len(cards) != 1 || len(cards[0].Windows) != 0 || cards[0].Health != StatusAuthNeeded {
					t.Fatalf("cards %+v", cards)
				}
			},
		},
		{
			"no reading yet is waiting, never zero",
			ViewInput{
				Now:      now,
				Accounts: []AccountInfo{{ID: "o1", Provider: ProviderOpenRouter, Source: SourceOpenRouter, Enabled: true}},
			},
			func(t *testing.T, cards []Card) {
				if len(cards) != 1 || cards[0].Health != HealthWaiting || len(cards[0].Windows) != 0 {
					t.Fatalf("cards %+v", cards)
				}
			},
		},
		{
			"a disabled account is hidden",
			ViewInput{
				Now:      now,
				Accounts: []AccountInfo{{ID: "o1", Provider: ProviderOpenRouter, Source: SourceOpenRouter}},
			},
			func(t *testing.T, cards []Card) {
				if len(cards) != 0 {
					t.Fatalf("cards %+v", cards)
				}
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { c.check(t, BuildView(c.in)) })
	}
}

func TestBuildViewFreshnessAndExpiry(t *testing.T) {
	old := passive(ProviderClaude, SourceClaudeProxy, DefaultScope, t0.Add(-10*time.Hour), 0.9)
	cards := BuildView(ViewInput{Now: t0, Passive: []ScopedSnapshot{old}})
	if cards[0].Health != HealthStale {
		t.Errorf("an old passive reading is stale: %s", cards[0].Health)
	}
	if !cards[0].Windows[0].Expired || cards[0].Windows[0].Pace != nil {
		t.Errorf("a window past its reset is expired, never projected: %+v", cards[0].Windows[0])
	}
	if got := cardsByProvider(cards); len(got[ProviderClaude]) != 1 {
		t.Error("grouping")
	}
}

func TestProvidersAndSourcesAreConsistent(t *testing.T) {
	seen := map[string]bool{}
	for _, p := range Providers {
		if seen[p.ID] {
			t.Errorf("duplicate provider %s", p.ID)
		}
		seen[p.ID] = true
	}
	ids := map[string]bool{}
	for _, s := range Sources {
		if ids[s.ID] {
			t.Errorf("duplicate source %s", s.ID)
		}
		ids[s.ID] = true
		if s.Kind != KindManual && !seen[s.Provider] {
			t.Errorf("source %s names unknown provider %s", s.ID, s.Provider)
		}
		for _, h := range s.Hosts {
			if h == "" || h != hostOnly(h) {
				t.Errorf("source %s host %q must be a bare host", s.ID, h)
			}
		}
	}
	if !AccountSourceAllowed(ProviderCursor, SourceManual) || AccountSourceAllowed(ProviderCursor, SourceOpenRouter) ||
		AccountSourceAllowed(ProviderClaude, SourceClaudeProxy) || AccountSourceAllowed("nope", SourceManual) {
		t.Error("AccountSourceAllowed")
	}
}

func hostOnly(h string) string {
	for i, r := range h {
		if r == '/' || r == ':' || r == '@' || r == '?' || r == '#' {
			return h[:i]
		}
	}
	return h
}
