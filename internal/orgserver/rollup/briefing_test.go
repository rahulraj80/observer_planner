package rollup

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

// briefingDay is the UTC day the briefing tests cover.
var briefingDay = time.Date(2026, 5, 25, 0, 0, 0, 0, time.UTC)

// seedBriefing inserts one session (alice, claude-code, /repo/x) with three
// turns on 2026-05-25: two proxy api_turns (0.10 + 0.20) and one unique
// token_usage turn (0.05). A fourth token_usage row duplicates req-b2 and must
// be dropped by the proxy-origin dedup. A second session (bob, codex, /repo/y)
// holds one api_turn outside alice's team for the lead-scope assertion.
func seedBriefing(t *testing.T, d *sql.DB) {
	t.Helper()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := d.ExecContext(context.Background(), q, args...); err != nil {
			t.Fatalf("seed exec: %v\n%s", err, q)
		}
	}
	exec(`INSERT INTO org_members (user_id, user_name, email, display_name, active, created_at, updated_at)
	      VALUES ('u-alice', 'alice', 'alice@acme.example', 'Alice', 1, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`)
	exec(`INSERT INTO org_members (user_id, user_name, email, display_name, active, created_at, updated_at)
	      VALUES ('u-bob', 'bob', 'bob@acme.example', 'Bob', 1, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`)
	exec(`INSERT INTO org_teams (team_id, display_name, created_at, updated_at) VALUES ('team-a','Team A','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`)
	exec(`INSERT INTO org_team_members (team_id, user_id, role) VALUES ('team-a','u-alice','lead')`)

	sess := func(id, user, tool, proj, started string) {
		exec(`INSERT INTO sessions (id, user_id, project_root, project_root_hash, tool, started_at, total_actions, pushed_at, pushed_by_user_id)
		      VALUES (?, ?, ?, ?, ?, ?, 1, '2026-05-25T12:00:00Z', ?)`, id, user, proj, rootHash(proj), tool, started, user)
	}
	sess("s-b1", "u-alice", "claude-code", "/repo/x", "2026-05-25T09:00:00Z")
	sess("s-b2", "u-bob", "codex", "/repo/y", "2026-05-25T09:00:00Z")

	apiTurn := func(user, sess, proj, model, reqID string, cost float64, ts string) {
		exec(`INSERT INTO api_turns (user_id, session_id, project_root, project_root_hash, timestamp, provider, model, request_id, input_tokens, output_tokens, cost_usd, http_status, pushed_at, pushed_by_user_id)
		      VALUES (?, ?, ?, ?, ?, 'anthropic', ?, ?, 100, 50, ?, 200, '2026-05-25T12:00:00Z', ?)`, user, sess, proj, rootHash(proj), ts, model, reqID, cost, user)
	}
	apiTurn("u-alice", "s-b1", "/repo/x", "claude", "req-b1", 0.10, "2026-05-25T10:00:00Z")
	apiTurn("u-alice", "s-b1", "/repo/x", "claude", "req-b2", 0.20, "2026-05-25T10:05:00Z")
	apiTurn("u-bob", "s-b2", "/repo/y", "gpt", "req-b3", 0.40, "2026-05-25T10:10:00Z")

	tu := func(sf, ev, user, sess, proj, model string, cost float64, ts string) {
		exec(`INSERT INTO token_usage (source_file, source_event_id, user_id, session_id, project_root, project_root_hash, timestamp, tool, model, input_tokens, output_tokens, estimated_cost_usd, source, reliability, pushed_at, pushed_by_user_id)
		      VALUES (?, ?, ?, ?, ?, ?, ?, 'claude-code', ?, 100, 50, ?, 'jsonl', 'reliable', '2026-05-25T12:00:00Z', ?)`, sf, ev, user, sess, proj, rootHash(proj), ts, model, cost, user)
	}
	tu("f-x", "req-b2", "u-alice", "s-b1", "/repo/x", "claude", 0.99, "2026-05-25T10:05:01Z") // DUP of proxy req-b2 → dropped
	tu("f-x", "t-b1", "u-alice", "s-b1", "/repo/x", "claude", 0.05, "2026-05-25T11:00:00Z")
}

func TestBriefing_AdminScope(t *testing.T) {
	d := newDB(t)
	seedBriefing(t, d)
	b, err := Briefing(context.Background(), d, briefingDay, adminScope, fixedNow)
	if err != nil {
		t.Fatalf("Briefing: %v", err)
	}
	g := b.AtGlance
	// Turns: req-b1 + req-b2 + t-b1 (the req-b2 dup is excluded) + req-b3.
	if g.Sessions != 2 || g.Turns != 4 {
		t.Errorf("at_glance = sessions %d turns %d, want 2/4", g.Sessions, g.Turns)
	}
	if !near(g.SpendUSD, 0.75) {
		t.Errorf("spend = %v, want 0.75 (proxy dup 0.99 excluded)", g.SpendUSD)
	}
	if g.InputTokens != 400 || g.OutputTokens != 200 {
		t.Errorf("tokens = %d/%d, want 400/200", g.InputTokens, g.OutputTokens)
	}
	if b.Date != "2026-05-25" {
		t.Errorf("date = %q, want 2026-05-25", b.Date)
	}
	if len(b.Activity) != 2 {
		t.Errorf("activity = %v, want 2 tools", b.Activity)
	}
}

func TestBriefing_LeadScopedToOwnTeam(t *testing.T) {
	d := newDB(t)
	seedBriefing(t, d)
	b, err := Briefing(context.Background(), d, briefingDay, aliceScope, fixedNow)
	if err != nil {
		t.Fatalf("Briefing: %v", err)
	}
	// Team A = alice: 3 turns (req-b1, req-b2, t-b1) = 0.35; bob's codex turn
	// must be excluded by the user_id scope.
	if b.AtGlance.Sessions != 1 || b.AtGlance.Turns != 3 {
		t.Errorf("alice-scoped at_glance = sessions %d turns %d, want 1/3", b.AtGlance.Sessions, b.AtGlance.Turns)
	}
	if !near(b.AtGlance.SpendUSD, 0.35) {
		t.Errorf("alice-scoped spend = %v, want 0.35", b.AtGlance.SpendUSD)
	}
}

func TestBriefing_EmptyDayComposesZero(t *testing.T) {
	d := newDB(t)
	b, err := Briefing(context.Background(), d, briefingDay.AddDate(0, 0, 1), adminScope, fixedNow)
	if err != nil {
		t.Fatalf("Briefing: %v", err)
	}
	if b.AtGlance.Sessions != 0 || b.AtGlance.Turns != 0 || !near(b.AtGlance.SpendUSD, 0) {
		t.Errorf("empty-day at_glance = %+v, want all zero", b.AtGlance)
	}
}

func TestBriefingAggregates(t *testing.T) {
	d := newDB(t)
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := d.ExecContext(context.Background(), q, args...); err != nil {
			t.Fatalf("exec: %v\n%s", err, q)
		}
	}
	exec(`INSERT INTO briefing_aggregates (org_id, user_email, day, tool, project_hash, sessions, turns, input_tokens, output_tokens, cost_usd, updated_at)
	      VALUES ('org-acme','alice@acme.example','2026-05-25','claude-code','h1',5,120,60000,12000,1.20,'2026-05-26T07:00:00Z')`)
	exec(`INSERT INTO briefing_aggregates (org_id, user_email, day, tool, project_hash, sessions, turns, input_tokens, output_tokens, cost_usd, updated_at)
	      VALUES ('org-acme','bob@acme.example','2026-05-25','codex','h2',2,40,20000,4000,0.80,'2026-05-26T07:00:00Z')`)
	// A neighbouring day must not leak into the window.
	exec(`INSERT INTO briefing_aggregates (org_id, user_email, day, tool, project_hash, sessions, turns, input_tokens, output_tokens, cost_usd, updated_at)
	      VALUES ('org-acme','alice@acme.example','2026-05-24','claude-code','h1',9,999,99999,9999,9.99,'2026-05-25T07:00:00Z')`)

	got, err := BriefingAggregates(context.Background(), d, briefingDay)
	if err != nil {
		t.Fatalf("BriefingAggregates: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("rows = %d, want 2 (neighbouring day excluded)", len(got))
	}
	// Ordered cost desc → bob (0.80) then alice (1.20)? No — 1.20 > 0.80,
	// so alice first.
	if got[0].Tool != "claude-code" || got[0].CostUSD != 1.20 || got[0].Sessions != 5 || got[0].Turns != 120 {
		t.Errorf("first row = %+v, want claude-code 1.20 / 5 sessions / 120 turns", got[0])
	}
	if got[1].UserEmail != "bob@acme.example" || got[1].Tool != "codex" {
		t.Errorf("second row = %+v, want codex/bob", got[1])
	}
}
