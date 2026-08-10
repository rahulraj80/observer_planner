package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/briefing"
	"github.com/marmutapp/superbased-observer/internal/db"
	"github.com/marmutapp/superbased-observer/internal/models"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// briefingTestEnv pins the wall clock + local zone so the "most recent
// completed day" window is deterministic: fixed=2026-08-01T22:00Z, TZ=UTC+2 →
// local 2026-08-02T00:00 → completed day = 2026-08-01 local, i.e. the UTC span
// [2026-07-31T22:00Z, 2026-08-01T22:00Z).
type briefingTestEnv struct {
	fixedUTC time.Time
}

func newBriefingTestEnv(t *testing.T) briefingTestEnv {
	e := briefingTestEnv{fixedUTC: time.Date(2026, 8, 1, 22, 0, 0, 0, time.UTC)}
	time.Local = time.FixedZone("briefing-test", 2*60*60)
	t.Cleanup(func() { time.Local = time.UTC })
	return e
}

// newBriefingServer opens a fresh DB + seeded briefing server with a pinned
// clock. seed sets per-turn token usage inside the window.
func newBriefingServer(t *testing.T, e briefingTestEnv) (*Server, *store.Store) {
	t.Helper()
	path := t.TempDir() + "/d.db"
	database, err := db.Open(context.Background(), db.Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	st := store.New(database)
	s, err := New(Options{DB: database, DBPath: path})
	if err != nil {
		t.Fatal(err)
	}
	s.now = func() time.Time { return e.fixedUTC }
	return s, st
}

func seedBriefingTurn(t *testing.T, st *store.Store, id, tool, model string, ts time.Time, input, output, cacheRead int64, seq int) {
	t.Helper()
	_, err := st.Ingest(context.Background(), []models.ToolEvent{{
		SourceFile: "brief.jsonl", SourceEventID: id + "-ev" + itoa(seq), SessionID: id,
		ProjectRoot: t.TempDir(), Timestamp: ts,
		Tool: tool, ActionType: models.ActionReadFile, Target: "a.go", Success: true,
	}}, []models.TokenEvent{{
		SessionID: id, Timestamp: ts, Tool: tool, Model: model,
		InputTokens: input, OutputTokens: output, CacheReadTokens: cacheRead,
		Source: models.TokenSourceJSONL, Reliability: models.ReliabilityApproximate,
		SourceFile: "brief.jsonl", SourceEventID: id + "-tok" + itoa(seq),
	}}, store.IngestOptions{})
	if err != nil {
		t.Fatal(err)
	}
}

// seedBriefingAPITurn seeds a real proxy api_turn (session row + APITurn with
// latency + http status) so latency percentiles and error rates have a source.
func seedBriefingAPITurn(t *testing.T, st *store.Store, id, tool, model string, ts time.Time, input, output int64, ttftMS, totalMS, httpStatus int, seq int) {
	seedBriefingAPITurnWithError(t, st, id, tool, model, ts, input, output, ttftMS, totalMS, httpStatus, "", "", seq)
}

// seedBriefingAPITurnWithError seeds an api_turn with optional error class/message.
func seedBriefingAPITurnWithError(t *testing.T, st *store.Store, id, tool, model string, ts time.Time, input, output int64, ttftMS, totalMS, httpStatus int, errorClass, errorMessage string, seq int) {
	t.Helper()
	if _, err := st.Ingest(context.Background(), []models.ToolEvent{{
		SourceFile: "brief-proxy.jsonl", SourceEventID: id + "-ev" + itoa(seq), SessionID: id,
		ProjectRoot: t.TempDir(), Timestamp: ts,
		Tool: tool, ActionType: models.ActionReadFile, Target: "a.go", Success: true,
	}}, nil, store.IngestOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.InsertAPITurn(context.Background(), models.APITurn{
		SessionID: id, Provider: models.ProviderAnthropic, Model: model,
		Timestamp: ts, RequestID: "msg_" + id + "_" + itoa(seq),
		InputTokens: input, OutputTokens: output,
		TimeToFirstTokenMS: int64(ttftMS), TotalResponseMS: int64(totalMS),
		HTTPStatus:   httpStatus,
		ErrorClass:   errorClass,
		ErrorMessage: errorMessage,
	}); err != nil {
		t.Fatal(err)
	}
}

func getBriefing(t *testing.T, s *Server, path string) (int, briefing.Briefing) {
	t.Helper()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET %s → %d body=%s", path, rr.Code, rr.Body.String())
	}
	var b briefing.Briefing
	if err := json.Unmarshal(rr.Body.Bytes(), &b); err != nil {
		t.Fatalf("GET %s decode: %v", path, err)
	}
	return rr.Code, b
}

func TestBriefingEndpoint_ComposeAndPersist(t *testing.T) {
	e := newBriefingTestEnv(t)
	s, st := newBriefingServer(t, e)

	// In-window: one trivial opus session (2 turns) → trivial-model card.
	seedBriefingTurn(t, st, "s1", models.ToolClaudeCode, "claude-opus-4",
		time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC), 20_000, 8_000, 0, 0)
	seedBriefingTurn(t, st, "s1", models.ToolClaudeCode, "claude-opus-4",
		time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC), 10_000, 4_000, 0, 1)
	// Out-of-window turn must be excluded.
	seedBriefingTurn(t, st, "s2", models.ToolClaudeCode, "claude-opus-4",
		time.Date(2026, 8, 2, 5, 0, 0, 0, time.UTC), 1_000, 500, 0, 2)

	_, b := getBriefing(t, s, "/api/briefing?date=2026-08-01")

	if b.Date != "2026-08-01" || b.Basis != briefing.BasisList {
		t.Fatalf("briefing header: date=%q basis=%q", b.Date, b.Basis)
	}
	if b.AtGlance.Turns != 2 || b.AtGlance.Sessions != 1 {
		t.Fatalf("glance: %+v", b.AtGlance)
	}
	if b.AtGlance.InputTokens != 30_000 || b.AtGlance.OutputTokens != 12_000 {
		t.Fatalf("token sums: %+v", b.AtGlance)
	}
	if len(b.Activity) != 1 || b.Activity[0].Tool != models.ToolClaudeCode || b.Activity[0].Turns != 2 {
		t.Fatalf("activity: %+v", b.Activity)
	}
	var trivial *briefing.Opportunity
	for i := range b.Opportunities {
		if b.Opportunities[i].Detector == "trivial_model_overprovisioning" {
			trivial = &b.Opportunities[i]
			break
		}
	}
	if trivial == nil {
		t.Fatalf("trivial_model card missing; opportunities=%+v", b.Opportunities)
	}
	if trivial.SavingsUSD <= 0 || len(trivial.Justification.Reprice) != 1 {
		t.Fatalf("trivial card justification: %+v", trivial.Justification)
	}

	// Snapshot must be persisted.
	blob, ok, err := st.LoadBriefingSnapshot(context.Background(), "2026-08-01")
	if err != nil || !ok || blob == "" {
		t.Fatalf("snapshot not persisted: ok=%v err=%v", ok, err)
	}
}

func TestBriefingEndpoint_NoData(t *testing.T) {
	e := newBriefingTestEnv(t)
	s, _ := newBriefingServer(t, e)
	_, b := getBriefing(t, s, "/api/briefing")
	if b.AtGlance.Turns != 0 || b.AtGlance.Sessions != 0 {
		t.Fatalf("empty briefing: %+v", b.AtGlance)
	}
	if len(b.Opportunities) != 0 {
		t.Fatalf("empty briefing must not invent opportunities: %+v", b.Opportunities)
	}
	if len(b.Activity) != 0 {
		t.Fatalf("empty briefing activity: %+v", b.Activity)
	}
}

func TestBriefingEndpoint_JustifyAndSession(t *testing.T) {
	e := newBriefingTestEnv(t)
	s, st := newBriefingServer(t, e)
	seedBriefingTurn(t, st, "s1", models.ToolClaudeCode, "claude-opus-4",
		time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC), 20_000, 8_000, 0, 0)

	_, b := getBriefing(t, s, "/api/briefing?date=2026-08-01")
	if len(b.TopSessions) != 1 || b.TopSessions[0].ID != "s1" {
		t.Fatalf("top sessions: %+v", b.TopSessions)
	}

	// Justify drill-down served from the snapshot.
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/briefing/justify?id=trivial_model_s1&date=2026-08-01", nil)
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("justify → %d body=%s", rr.Code, rr.Body.String())
	}
	var j briefing.Justification
	if err := json.Unmarshal(rr.Body.Bytes(), &j); err != nil {
		t.Fatal(err)
	}
	if j.Source != "api_turns" || j.EstimateVersion != briefing.EstimateVersion {
		t.Fatalf("justification stamps: %+v", j)
	}

	// Unknown claim id → 404 JSON.
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/briefing/justify?id=nope&date=2026-08-01", nil)
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("unknown claim → %d want 404 (body=%s)", rr.Code, rr.Body.String())
	}

	// Session drill-down delegates to /api/session/<id>.
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/briefing/session/s1", nil)
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("session drill-down → %d body=%s", rr.Code, rr.Body.String())
	}
	var detail map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &detail); err != nil {
		t.Fatalf("session drill-down decode: %v", err)
	}
}

func TestBriefingEndpoint_SnapshotCache(t *testing.T) {
	e := newBriefingTestEnv(t)
	s, st := newBriefingServer(t, e)
	seedBriefingTurn(t, st, "s1", models.ToolClaudeCode, "claude-opus-4",
		time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC), 20_000, 8_000, 0, 0)

	// First GET composes + persists the snapshot.
	_, b1 := getBriefing(t, s, "/api/briefing?date=2026-08-01")
	if len(b1.Opportunities) == 0 {
		t.Fatal("expected an opportunity on first compose")
	}
	blob1, ok, _ := st.LoadBriefingSnapshot(context.Background(), "2026-08-01")
	if !ok {
		t.Fatal("first GET must persist")
	}

	// Data changes afterwards…
	seedBriefingTurn(t, st, "s2", models.ToolClaudeCode, "claude-opus-4",
		time.Date(2026, 8, 1, 11, 0, 0, 0, time.UTC), 1_000, 500, 0, 1)

	// …but /api/briefing/opportunities + /api/briefing/justify serve the
	// persisted snapshot (page ⇄ drill-down never drift). The full /api/briefing
	// recomposes fresh (1 session → 2 sessions).
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/briefing/opportunities?date=2026-08-01", nil)
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("opportunities → %d body=%s", rr.Code, rr.Body.String())
	}
	var opps map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &opps); err != nil {
		t.Fatal(err)
	}
	list, _ := opps["opportunities"].([]any)
	if len(list) != len(b1.Opportunities) {
		t.Fatalf("opportunities served %d cards, want snapshot's %d", len(list), len(b1.Opportunities))
	}

	// Justify against a card that only exists in the snapshot (s1's trivial
	// card) still resolves.
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/briefing/justify?id=trivial_model_s1&date=2026-08-01", nil)
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("justify → %d body=%s", rr.Code, rr.Body.String())
	}

	// Fresh recompose sees the new session.
	_, b2 := getBriefing(t, s, "/api/briefing?date=2026-08-01")
	if b2.AtGlance.Sessions != 2 {
		t.Fatalf("fresh recompose sessions: %d want 2", b2.AtGlance.Sessions)
	}
	blob2, ok, _ := st.LoadBriefingSnapshot(context.Background(), "2026-08-01")
	if !ok || blob1 == blob2 {
		t.Fatal("recompose must refresh the snapshot")
	}
}

func TestBriefingWindow(t *testing.T) {
	time.Local = time.FixedZone("briefing-test", 2*60*60)
	t.Cleanup(func() { time.Local = time.UTC })
	nowUTC := time.Date(2026, 8, 1, 22, 30, 0, 0, time.UTC) // local 2026-08-02T00:30

	t.Run("default completed day", func(t *testing.T) {
		date, since, until, err := briefingWindow(nowUTC, "", 1)
		if err != nil {
			t.Fatal(err)
		}
		if date != "2026-08-01" {
			t.Fatalf("date: %q", date)
		}
		if want := time.Date(2026, 7, 31, 22, 0, 0, 0, time.UTC); !since.Equal(want) {
			t.Fatalf("since: %v want %v", since, want)
		}
		if want := time.Date(2026, 8, 1, 22, 0, 0, 0, time.UTC); !until.Equal(want) {
			t.Fatalf("until: %v want %v", until, want)
		}
	})

	t.Run("override", func(t *testing.T) {
		date, since, until, err := briefingWindow(nowUTC, "2026-07-30", 1)
		if err != nil {
			t.Fatal(err)
		}
		if date != "2026-07-30" {
			t.Fatalf("date: %q", date)
		}
		if want := time.Date(2026, 7, 29, 22, 0, 0, 0, time.UTC); !since.Equal(want) {
			t.Fatalf("since: %v want %v", since, want)
		}
		if want := time.Date(2026, 7, 30, 22, 0, 0, 0, time.UTC); !until.Equal(want) {
			t.Fatalf("until: %v want %v", until, want)
		}
	})

	t.Run("weekly default", func(t *testing.T) {
		// Last 7 completed local days: [2026-07-25T22:00Z, 2026-08-01T22:00Z).
		date, since, until, err := briefingWindow(nowUTC, "", 7)
		if err != nil {
			t.Fatal(err)
		}
		if date != "2026-08-01" {
			t.Fatalf("date: %q", date)
		}
		if want := time.Date(2026, 7, 25, 22, 0, 0, 0, time.UTC); !since.Equal(want) {
			t.Fatalf("since: %v want %v", since, want)
		}
		if want := time.Date(2026, 8, 1, 22, 0, 0, 0, time.UTC); !until.Equal(want) {
			t.Fatalf("until: %v want %v", until, want)
		}
	})

	t.Run("weekly override anchors end day", func(t *testing.T) {
		// date=2026-07-30 is the END day: window = [2026-07-24, 2026-07-31) local.
		date, since, until, err := briefingWindow(nowUTC, "2026-07-30", 7)
		if err != nil {
			t.Fatal(err)
		}
		if date != "2026-07-30" {
			t.Fatalf("date: %q", date)
		}
		if want := time.Date(2026, 7, 23, 22, 0, 0, 0, time.UTC); !since.Equal(want) {
			t.Fatalf("since: %v want %v", since, want)
		}
		if want := time.Date(2026, 7, 30, 22, 0, 0, 0, time.UTC); !until.Equal(want) {
			t.Fatalf("until: %v want %v", until, want)
		}
	})

	t.Run("bad override", func(t *testing.T) {
		if _, _, _, err := briefingWindow(nowUTC, "not-a-date", 1); err == nil {
			t.Fatal("bad date must error")
		}
	})
}

func TestParseBriefingWindowDays(t *testing.T) {
	for raw, want := range map[string]int{
		"":        1,
		"1d":      1,
		"1":       1,
		"day":     1,
		"daily":   1,
		"7d":      7,
		"7":       7,
		"week":    7,
		"weekly":  7,
		"28d":     28,
		"4w":      28,
		"month":   28,
		"monthly": 28,
		"14d":     14,
		"3w":      21,
		"30":      30,
		"90":      90,
	} {
		got, err := parseBriefingWindowDays(raw)
		if err != nil {
			t.Errorf("parseBriefingWindowDays(%q): %v", raw, err)
			continue
		}
		if got != want {
			t.Errorf("parseBriefingWindowDays(%q) = %d want %d", raw, got, want)
		}
	}
	for _, bad := range []string{"0", "-3", "91", "1m", "foo", "7x", "w", "d"} {
		if _, err := parseBriefingWindowDays(bad); err == nil {
			t.Errorf("parseBriefingWindowDays(%q) must error", bad)
		}
	}
}

// TestBriefingEndpoint_WeeklyTrend pins the multi-day mode: a 7-day rollup
// over the last 7 completed local days with a trend block vs the prior 7 days
// (spend, turns, error rate, TTFT percentiles from real proxy captures) and
// the same grounded narrative. The per-day snapshot must NOT be written for
// multi-day windows (the table is keyed by date UNIQUE).
func TestBriefingEndpoint_WeeklyTrend(t *testing.T) {
	e := newBriefingTestEnv(t)
	s, st := newBriefingServer(t, e)

	// In-window (current 7 days): 2 healthy opus turns + 1 erroring sonnet.
	seedBriefingAPITurn(t, st, "w1", models.ToolClaudeCode, "claude-opus-4",
		time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC), 20_000, 8_000, 400, 2000, 200, 0)
	seedBriefingAPITurn(t, st, "w1", models.ToolClaudeCode, "claude-opus-4",
		time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC), 10_000, 4_000, 600, 3000, 200, 1)
	seedBriefingAPITurn(t, st, "w2", models.ToolOpenCode, "claude-sonnet-4",
		time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC), 5_000, 2_000, 200, 1500, 500, 2)

	// Prior window: 3 turns with higher latency and (mostly opus) higher spend.
	seedBriefingAPITurn(t, st, "p1", models.ToolClaudeCode, "claude-opus-4",
		time.Date(2026, 7, 20, 10, 0, 0, 0, time.UTC), 20_000, 8_000, 900, 5000, 200, 3)
	seedBriefingAPITurn(t, st, "p1", models.ToolClaudeCode, "claude-opus-4",
		time.Date(2026, 7, 21, 11, 0, 0, 0, time.UTC), 20_000, 8_000, 1100, 6000, 200, 4)
	seedBriefingAPITurn(t, st, "p2", models.ToolClaudeCode, "claude-opus-4",
		time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC), 5_000, 2_000, 800, 4000, 200, 5)

	// Out of any window: older than the prior window — must be excluded.
	seedBriefingAPITurn(t, st, "old", models.ToolClaudeCode, "claude-opus-4",
		time.Date(2026, 7, 10, 10, 0, 0, 0, time.UTC), 1_000, 500, 100, 500, 200, 6)

	_, b := getBriefing(t, s, "/api/briefing?window=7d")

	if b.Period != "week" {
		t.Fatalf("period: %q", b.Period)
	}
	if b.Date != "2026-08-01" {
		t.Fatalf("date (end day): %q", b.Date)
	}
	if b.AtGlance.Turns != 3 || b.AtGlance.Sessions != 2 {
		t.Fatalf("glance: %+v", b.AtGlance)
	}
	if b.Trend == nil {
		t.Fatal("weekly briefing must carry a trend block")
	}
	if b.Trend.WindowDays != 7 {
		t.Fatalf("trend window_days: %d", b.Trend.WindowDays)
	}

	// Nearest-rank TTFT percentiles over [400,600,200]: p50=400, p95=600.
	if b.AtGlance.TTFTP50MS != 400 || b.AtGlance.TTFTP95MS != 600 {
		t.Fatalf("ttft pct: p50=%d p95=%d", b.AtGlance.TTFTP50MS, b.AtGlance.TTFTP95MS)
	}
	// Total p50 over [2000,3000,1500] sorted [1500,2000,3000] → idx 1 = 2000.
	if b.AtGlance.TotalP50MS != 2000 {
		t.Fatalf("total p50: %d", b.AtGlance.TotalP50MS)
	}

	// Prior-window percentiles over [900,1100,800]: p50=900, p95=1100 → both
	// deltas are -500ms (latency got better).
	if b.Trend.TTFTP50DeltaMS != -500 || b.Trend.TTFTP95DeltaMS != -500 {
		t.Fatalf("ttft deltas: p50=%d p95=%d", b.Trend.TTFTP50DeltaMS, b.Trend.TTFTP95DeltaMS)
	}
	if b.Trend.Turns != 3 || b.Trend.TurnsPrior != 3 {
		t.Fatalf("trend turns: cur=%d prior=%d", b.Trend.Turns, b.Trend.TurnsPrior)
	}
	// Error rate: 1/3 current (the sonnet 500), 0 prior.
	if b.Trend.ErrorRate != 1.0/3.0 || b.Trend.ErrorRatePrior != 0 {
		t.Fatalf("trend error rate: cur=%v prior=%v", b.Trend.ErrorRate, b.Trend.ErrorRatePrior)
	}
	// Prior window was mostly-opus 45K/18K vs current 30K/12K opus + cheap
	// sonnet — spend must be DOWN vs prior (the whole point of the trend).
	if b.Trend.SpendDeltaUSD >= 0 {
		t.Fatalf("spend must be down vs prior window: %+v", b.Trend)
	}

	if !strings.Contains(b.Narrative, "prior 7 days") {
		t.Fatalf("narrative must compare vs the prior window: %q", b.Narrative)
	}

	// Multi-day must not clobber the per-day snapshot table.
	if _, ok, _ := st.LoadBriefingSnapshot(context.Background(), "2026-08-01"); ok {
		t.Fatal("multi-day briefing must not write the per-day snapshot")
	}
}

// TestBriefingEndpoint_WeeklyOpportunities verifies the flat cards + justify
// drill-down work in multi-day mode (recomposed on demand, not snapshotted).
func TestBriefingEndpoint_WeeklyOpportunities(t *testing.T) {
	e := newBriefingTestEnv(t)
	s, st := newBriefingServer(t, e)
	// A premium-model trivial session inside the week fires the trivial card.
	seedBriefingTurn(t, st, "s1", models.ToolClaudeCode, "claude-opus-4",
		time.Date(2026, 7, 28, 10, 0, 0, 0, time.UTC), 20_000, 8_000, 0, 0)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/briefing/opportunities?window=7d", nil)
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("opportunities → %d body=%s", rr.Code, rr.Body.String())
	}
	var opps map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &opps); err != nil {
		t.Fatal(err)
	}
	if date, _ := opps["date"].(string); date != "2026-08-01" {
		t.Fatalf("opportunities date: %q", date)
	}
	list, _ := opps["opportunities"].([]any)
	if len(list) == 0 {
		t.Fatal("weekly opportunities must include the trivial-model card")
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/briefing/justify?id=trivial_model_s1&window=7d", nil)
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("weekly justify → %d body=%s", rr.Code, rr.Body.String())
	}
}

// TestBriefingEndpoint_WeeklyRecurrence pins Stage 1 item 2 end-to-end: a
// 28-day rollup where cache is cold on every Wednesday surfaces the
// recurring_cache_miss card (Recurrence=true, named weekday, affected days
// listed, reproducible savings) while the aggregate-rate single-day card stays
// suppressed because the healthy days dominate the window total.
func TestBriefingEndpoint_WeeklyRecurrence(t *testing.T) {
	time.Local = time.FixedZone("briefing-test", 2*60*60)
	t.Cleanup(func() { time.Local = time.UTC })
	e := newBriefingTestEnv(t)
	s, st := newBriefingServer(t, e)

	// Low-cache Wednesdays inside the 28-day window [07-05, 08-01] local:
	// 07-08, 15, 22, 29 (all Wednesdays).
	for i, day := range []int{8, 15, 22, 29} {
		seedBriefingTurn(t, st, fmt.Sprintf("wc%d", i), models.ToolClaudeCode, "claude-opus-4",
			time.Date(2026, 7, day, 10, 0, 0, 0, time.UTC), 60_000, 5_000, 0, i)
	}
	// Healthy (90% cached) Fridays: 07-10, 17, 24.
	for i, day := range []int{10, 17, 24} {
		seedBriefingTurn(t, st, fmt.Sprintf("hc%d", i), models.ToolClaudeCode, "claude-opus-4",
			time.Date(2026, 7, day, 10, 0, 0, 0, time.UTC), 60_000, 5_000, 540_000, 10+i)
	}

	_, b := getBriefing(t, s, "/api/briefing?window=28d")
	if b.Period != "month" {
		t.Fatalf("period: %q", b.Period)
	}
	// Aggregate rate 1,620,000 / 2,040,000 = 0.79 → single-day card suppressed.
	if b.AtGlance.CacheHitRate != 0.79 {
		t.Fatalf("glance cache_hit_rate: %v, want 0.79", b.AtGlance.CacheHitRate)
	}
	if len(b.Opportunities) != 1 {
		t.Fatalf("want exactly the recurrence card, got %+v", b.Opportunities)
	}
	o := b.Opportunities[0]
	if o.Detector != "recurring_cache_miss" {
		t.Fatalf("detector: %q", o.Detector)
	}
	if !o.Recurrence {
		t.Fatal("card must be flagged Recurrence")
	}
	if !strings.Contains(o.Title, "Wednesday") {
		t.Fatalf("title must name the weekday: %q", o.Title)
	}
	if o.SavingsUSD != 0.36 {
		t.Fatalf("savings: %.2f, want 0.36 (4 low days × 0.6 gap × 60k tokens × $5/1M × 0.5)", o.SavingsUSD)
	}
	if len(o.Justification.Days) != 4 {
		t.Fatalf("justification days: %v, want the 4 Wednesdays", o.Justification.Days)
	}

	// The /justify drill-down resolves the recurrence card in multi-day mode.
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/briefing/justify?id=recurring_cache_28d&window=28d", nil)
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("recurrence justify → %d body=%s", rr.Code, rr.Body.String())
	}
	var j struct {
		Days []string `json:"days"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &j); err != nil {
		t.Fatal(err)
	}
	if len(j.Days) != 4 {
		t.Fatalf("justify days: %v", j.Days)
	}
}

// TestBriefingEndpoint_FailurePatterns pins Stage 1 item 3 end-to-end: a
// 7-day rollup where the same permission-denied error occurs across two
// sessions surfaces the failure_pattern card (Recurrence=true, categorized
// nudge, affected days listed, drill-down works).
func TestBriefingEndpoint_FailurePatterns(t *testing.T) {
	time.Local = time.FixedZone("briefing-test", 2*60*60)
	t.Cleanup(func() { time.Local = time.UTC })
	e := newBriefingTestEnv(t)
	s, st := newBriefingServer(t, e)

	// Two sessions on different days with the same permission-denied error
	seedBriefingAPITurnWithError(t, st, "f1", models.ToolClaudeCode, "claude-opus-4",
		time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC), 10_000, 4_000, 400, 2000, 403,
		"permission_denied", "permission denied on /etc/hosts", 0)
	seedBriefingAPITurnWithError(t, st, "f2", models.ToolClaudeCode, "claude-opus-4",
		time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC), 5_000, 2_000, 600, 3000, 403,
		"permission_denied", "permission denied on /etc/hosts", 1)

	// Healthy turns on other days (to suppress recurrence cache/error cards)
	seedBriefingAPITurn(t, st, "h1", models.ToolClaudeCode, "claude-opus-4",
		time.Date(2026, 7, 27, 10, 0, 0, 0, time.UTC), 20_000, 8_000, 400, 2000, 200, 2)
	seedBriefingAPITurn(t, st, "h2", models.ToolClaudeCode, "claude-opus-4",
		time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC), 10_000, 4_000, 600, 3000, 200, 3)
	seedBriefingAPITurn(t, st, "h3", models.ToolClaudeCode, "claude-opus-4",
		time.Date(2026, 7, 30, 10, 0, 0, 0, time.UTC), 20_000, 8_000, 400, 2000, 200, 4)

	_, b := getBriefing(t, s, "/api/briefing?window=7d")
	if b.Period != "week" {
		t.Fatalf("period: %q", b.Period)
	}
	// Find the failure_pattern card
	var found *briefing.Opportunity
	for i := range b.Opportunities {
		if b.Opportunities[i].Detector == "failure_pattern" {
			found = &b.Opportunities[i]
			break
		}
	}
	if found == nil {
		// Debug: print all dedup keys
		for _, o := range b.Opportunities {
			t.Logf("opp: detector=%s dedup=%s title=%s", o.Detector, o.DedupKey, o.Title)
		}
		t.Fatalf("expected failure_pattern card, got %+v", b.Opportunities)
	}
	if !found.Recurrence {
		t.Fatal("failure pattern must be flagged Recurrence")
	}
	if !strings.Contains(found.Title, "Permission errors recur") {
		t.Fatalf("title should mention permission: %q", found.Title)
	}
	if !strings.Contains(found.Nudge, "permissions and API credentials") {
		t.Fatalf("nudge should be categorized: %q", found.Nudge)
	}
	if len(found.Justification.Days) != 2 {
		t.Fatalf("justification days = %v, want 2 affected days", found.Justification.Days)
	}

	// The /justify drill-down resolves the failure card in multi-day mode.
	id := "failure_pattern_permission|permission denied on /etc/hosts|claude-code|claude-opus-4"
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/briefing/justify?id="+url.QueryEscape(id)+"&window=7d", nil)
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("failure justify → %d body=%s", rr.Code, rr.Body.String())
	}
	var j2 struct {
		Days []string `json:"days"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &j2); err != nil {
		t.Fatal(err)
	}
	if len(j2.Days) != 2 {
		t.Fatalf("justify days: %v", j2.Days)
	}
}
