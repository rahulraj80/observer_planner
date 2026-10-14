//go:build !sbo_omit_benchmark

package dashboard

import (
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/benchmark"
	"github.com/marmutapp/superbased-observer/internal/models"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// modelfilter_parity_test.go pins the global Model filter on the node
// endpoints that did not read it before the node-parity lane (D-323, T-424):
// Projects, Routing (decisions / savings / health), Live, the Cost statement
// (/api/report/monthly), Benchmarks and the Cache health / entry-state cards.
// Each case runs over seedModelFilterFixture (sA sonnet, sB gpt-5.1, sC opus +
// haiku) plus the rows its endpoint reads; "" is the unfiltered baseline, so a
// handler that ignored the param fails the narrowed rows.

// seedParityExtras adds the rows the newly-filtered endpoints read: two router
// decisions, two benchmark runs, two cache entries and two cache events.
func seedParityExtras(t *testing.T, s *Server) {
	t.Helper()
	ctx := context.Background()
	st := store.New(s.opts.DB)
	now := time.Now().UTC().Add(-20 * time.Minute)
	if err := st.InsertRouterDecisions(ctx, []store.RouterDecisionRow{
		{SessionID: "sB", Timestamp: now, Mode: "advise", Channel: "proxy", OriginalModel: "gpt-5.1", SelectedModel: "gpt-5.1-mini", TurnKind: "edit", PolicyName: "p", PolicyHash: "h", ReasonCodes: []string{"cheaper_tier"}},
		{SessionID: "sC", Timestamp: now, Mode: "advise", Channel: "proxy", OriginalModel: "claude-opus-4-7", SelectedModel: "claude-haiku-4-5", TurnKind: "edit", PolicyName: "p", PolicyHash: "h", ReasonCodes: []string{"cheaper_tier"}},
	}); err != nil {
		t.Fatal(err)
	}
	for i, models := range [][]string{{"gpt-5.1", "claude-opus-4-7"}, {"claude-haiku-4-5"}} {
		spec := benchmark.Spec{Name: "bench"}
		for j, m := range models {
			spec.Configs = append(spec.Configs, benchmark.Config{ID: string(rune('a' + j)), Harness: "claude-code", Model: m})
		}
		raw, _ := json.Marshal(spec)
		if err := st.InsertBenchmarkRun(ctx, benchmark.RunRecord{
			RunID: "run" + string(rune('1'+i)), SpecName: "bench", SpecHash: "h", SpecJSON: string(raw),
			StartedAt: now.Add(time.Duration(i) * time.Minute), Status: "completed",
		}); err != nil {
			t.Fatal(err)
		}
	}
	ts := now.Format(time.RFC3339Nano)
	exp := now.Add(time.Hour).Format(time.RFC3339Nano)
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO cache_entries (model, prefix_hash, token_count, tier, created_at, last_refresh_at, expires_at, state) VALUES (?, ?, 100, 'proxy', ?, ?, ?, 'live')`, []any{"claude-opus-4-7", "p1", ts, ts, exp}},
		{`INSERT INTO cache_entries (model, prefix_hash, token_count, tier, created_at, last_refresh_at, expires_at, state) VALUES (?, ?, 100, 'proxy', ?, ?, ?, 'expired')`, []any{"claude-haiku-4-5", "p2", ts, ts, exp}},
		{`INSERT INTO cache_events (session_id, tier, timestamp, model, kind, tokens_read, tokens_written) VALUES ('sC', 'proxy', ?, 'claude-opus-4-7', 'hit', 10, 0)`, []any{ts}},
		{`INSERT INTO cache_events (session_id, tier, timestamp, model, kind, tokens_read, tokens_written) VALUES ('sC', 'proxy', ?, 'claude-haiku-4-5', 'write', 0, 10)`, []any{ts}},
	} {
		if _, err := s.opts.DB.ExecContext(ctx, q.sql, q.args...); err != nil {
			t.Fatalf("%s: %v", q.sql, err)
		}
	}
}

func sortedJoin(v []string) string {
	sort.Strings(v)
	return strings.Join(v, ",")
}

func TestModelFilterParityEndpoints(t *testing.T) {
	s := seedModelFilterFixture(t)
	seedParityExtras(t, s)
	month := time.Now().UTC().Add(-30 * time.Minute).Format("2006-01")

	type projectsResp struct {
		Rows []struct {
			SessionCount int `json:"session_count"`
			ActionCount  int `json:"action_count"`
		} `json:"rows"`
	}
	type decisionsResp struct {
		Decisions []struct {
			SessionID string `json:"SessionID"`
		} `json:"decisions"`
		Total int `json:"total"`
	}
	type liveResp struct {
		Active []struct {
			SessionID string `json:"session_id"`
		} `json:"active"`
	}
	type healthResp struct {
		Models []struct {
			Model string `json:"model"`
		} `json:"models"`
	}
	type reportResp struct {
		ByModel []struct {
			Key string `json:"key"`
		} `json:"by_model"`
		Model string `json:"model"`
	}
	type benchResp struct {
		Runs []struct {
			RunID string `json:"run_id"`
		} `json:"runs"`
	}
	type entryStatesResp struct {
		Rows []struct {
			State string `json:"state"`
		} `json:"rows"`
		Total int64 `json:"total"`
	}

	cases := []struct {
		model       string
		projSess    int // the one project's session_count (-1 = no row)
		decisions   string
		savings     int
		health      string
		live        string
		report      string
		bench       string
		entryStates string
	}{
		{"", 3, "sB,sC", 2, "claude-haiku-4-5,claude-opus-4-7,claude-sonnet-4-6,gpt-5.1", "sA,sB,sC", "claude-haiku-4-5,claude-opus-4-7,claude-sonnet-4-6,gpt-5.1", "run1,run2", "expired,live"},
		{"gpt-5.1", 1, "sB", 1, "gpt-5.1", "sB", "gpt-5.1", "run1", ""},
		// A router decision is "in" both the model asked for and the one it
		// ran on: haiku is sC's selected model.
		{"claude-haiku-4-5", 1, "sC", 1, "claude-haiku-4-5", "sC", "claude-haiku-4-5", "run2", "expired"},
		{"claude-opus-4-7", 1, "sC", 1, "claude-opus-4-7", "sC", "claude-opus-4-7", "run1", "live"},
		{"no-such-model", -1, "", 0, "", "", "", "", ""},
	}
	for _, tc := range cases {
		t.Run("model="+tc.model, func(t *testing.T) {
			var pr projectsResp
			getModelJSON(t, s, withModel("/api/projects?x=1", tc.model), &pr)
			switch {
			case tc.projSess < 0 && len(pr.Rows) != 0:
				t.Errorf("projects: %d rows, want none", len(pr.Rows))
			case tc.projSess >= 0 && (len(pr.Rows) != 1 || pr.Rows[0].SessionCount != tc.projSess):
				t.Errorf("projects: %+v, want one row with %d sessions", pr.Rows, tc.projSess)
			}

			// The routing endpoints answer 404 not_compiled when proxy.routing
			// is omitted (feature_omit_routes_test.go pins that answer).
			var got []string
			if !compiledOutInTest("proxy.routing") {
				var dr decisionsResp
				getModelJSON(t, s, withModel("/api/routing/decisions?days=2", tc.model), &dr)
				for _, d := range dr.Decisions {
					got = append(got, d.SessionID)
				}
				if sortedJoin(got) != tc.decisions || dr.Total != len(got) {
					t.Errorf("routing decisions = %v (total %d), want %q", got, dr.Total, tc.decisions)
				}

				var sv store.RouterSavingsReport
				getModelJSON(t, s, withModel("/api/routing/savings?days=2", tc.model), &sv)
				if sv.Decisions != tc.savings {
					t.Errorf("routing savings decisions = %d, want %d", sv.Decisions, tc.savings)
				}

				var hr healthResp
				getModelJSON(t, s, withModel("/api/routing/health?x=1", tc.model), &hr)
				got = nil
				for _, m := range hr.Models {
					got = append(got, m.Model)
				}
				if sortedJoin(got) != tc.health {
					t.Errorf("routing health models = %v, want %q", got, tc.health)
				}
			}

			var lr liveResp
			getModelJSON(t, s, withModel("/api/live?window_minutes=240", tc.model), &lr)
			got = nil
			for _, a := range lr.Active {
				got = append(got, a.SessionID)
			}
			if sortedJoin(got) != tc.live {
				t.Errorf("live sessions = %v, want %q", got, tc.live)
			}

			var rr reportResp
			getModelJSON(t, s, withModel("/api/report/monthly?month="+month, tc.model), &rr)
			got = nil
			for _, m := range rr.ByModel {
				got = append(got, m.Key)
			}
			if sortedJoin(got) != tc.report {
				t.Errorf("report by_model = %v, want %q", got, tc.report)
			}
			if tc.model != "" && rr.Model != tc.model {
				t.Errorf("report echoes model %q, want %q", rr.Model, tc.model)
			}

			var br benchResp
			getModelJSON(t, s, withModel("/api/benchmarks?limit=10", tc.model), &br)
			got = nil
			for _, run := range br.Runs {
				got = append(got, run.RunID)
			}
			if sortedJoin(got) != tc.bench {
				t.Errorf("benchmark runs = %v, want %q", got, tc.bench)
			}

			var er entryStatesResp
			getModelJSON(t, s, withModel("/api/cache/entry-states?x=1", tc.model), &er)
			got = nil
			for _, row := range er.Rows {
				got = append(got, row.State)
			}
			if sortedJoin(got) != tc.entryStates {
				t.Errorf("cache entry states = %v, want %q", got, tc.entryStates)
			}
		})
	}
}

// TestCacheHealthHonoursModelFilter: the §10 gate's event counts narrow to
// the chosen model's cache events (turn grain).
func TestCacheHealthHonoursModelFilter(t *testing.T) {
	s := seedModelFilterFixture(t)
	seedParityExtras(t, s)
	for _, tc := range []struct {
		model string
		want  int
	}{{"", 2}, {"claude-opus-4-7", 1}, {"no-such-model", 0}} {
		summary, err := loadCacheHealthSummary(context.Background(), s.opts.DB, tc.model)
		if err != nil {
			t.Fatal(err)
		}
		// GradedEvents excludes zero-usage events; both seeded events carry
		// usage, so it counts exactly the events the filter kept.
		if summary.GradedEvents != tc.want {
			t.Errorf("model=%q: graded events = %d, want %d", tc.model, summary.GradedEvents, tc.want)
		}
	}
}

// TestProjectsListHonoursGlobalFilters pins the Projects list's reading of
// the whole filter bar (window / tool / model / project), the sibling of the
// org Projects list: with no filter it is the old all-time list; a window
// bounds the counted actions and drops a project idle in it; tool and model
// narrow at session grain; the project filter keeps its one row.
func TestProjectsListHonoursGlobalFilters(t *testing.T) {
	s := seedModelFilterFixture(t)
	ctx := context.Background()
	old := t.TempDir()
	if _, err := store.New(s.opts.DB).Ingest(ctx, []models.ToolEvent{{
		SourceFile: "f", SourceEventID: "o1", SessionID: "sOld", ProjectRoot: old,
		Timestamp: time.Now().UTC().Add(-10 * 24 * time.Hour), Tool: models.ToolCodex,
		ActionType: models.ActionReadFile, Target: "o.go", Success: true,
	}}, nil, store.IngestOptions{}); err != nil {
		t.Fatal(err)
	}
	type resp struct {
		Rows []struct {
			RootPath     string `json:"root_path"`
			SessionCount int    `json:"session_count"`
			ActionCount  int    `json:"action_count"`
		} `json:"rows"`
		Window map[string]string `json:"window"`
	}
	for _, tc := range []struct {
		name, query string
		// want: "<root marker>=<sessions>/<actions>" joined, root marker
		// "new" for the fixture root, "old" for the idle project.
		want     string
		windowed bool
	}{
		{"no filter: all-time", "", "new=3/3,old=1/1", false},
		{"one-day window drops the idle project", "days=1", "new=3/3", true},
		{"thirty-day window keeps both", "days=30", "new=3/3,old=1/1", true},
		{"tool narrows sessions", "tool=codex", "new=1/1,old=1/1", false},
		{"tool and window", "tool=codex&days=1", "new=1/1", true},
		{"model narrows", "model=gpt-5.1", "new=1/1", false},
		{"project keeps its row", "project=" + old, "old=1/1", false},
		{"nothing in scope", "tool=no-such-tool&days=1", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var r resp
			getModelJSON(t, s, "/api/projects?"+tc.query, &r)
			var got []string
			for _, row := range r.Rows {
				mark := "new"
				if row.RootPath == old {
					mark = "old"
				}
				got = append(got, mark+"="+strconv.Itoa(row.SessionCount)+"/"+strconv.Itoa(row.ActionCount))
			}
			if sortedJoin(got) != tc.want {
				t.Fatalf("rows = %v, want %q", got, tc.want)
			}
			if (r.Window != nil) != tc.windowed {
				t.Fatalf("window block = %v, want present=%v", r.Window, tc.windowed)
			}
		})
	}
}
