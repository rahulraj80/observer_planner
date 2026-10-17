package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/models"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// TestModelParam pins how the global Model filter's query param is parsed:
// trimmed, "all"/empty/over-long mean no filter, anything else is the value.
func TestModelParam(t *testing.T) {
	cases := []struct {
		name, raw, want string
	}{
		{"absent", "", ""},
		{"all sentinel", "all", ""},
		{"all any case", "ALL", ""},
		{"plain id", "claude-opus-4-7", "claude-opus-4-7"},
		{"trimmed", "  gpt-5.1 ", "gpt-5.1"},
		{"provider-prefixed", "openrouter/qwen/qwen3-coder:free", "openrouter/qwen/qwen3-coder:free"},
		// Kept, never dropped (Sol F5): an exact match that narrows to nothing.
		{"over-long narrows, never widens", strings.Repeat("m", 201), strings.Repeat("m", 201)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			u := "/api/sessions"
			if tc.raw != "" {
				u += "?model=" + url.QueryEscape(tc.raw)
			}
			if got := modelParam(httptest.NewRequest(http.MethodGet, u, nil)); got != tc.want {
				t.Fatalf("modelParam(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

// seedModelFilterFixture adds two sessions beside newTestServer's sA:
//
//	sA  claude-code  one action  one proxy turn  claude-sonnet-4-6
//	sB  codex        one action  one proxy turn  gpt-5.1
//	sC  claude-code  one action  two proxy turns claude-opus-4-7 + claude-haiku-4-5
func seedModelFilterFixture(t *testing.T) *Server {
	t.Helper()
	s, root := newTestServer(t)
	st := store.New(s.opts.DB)
	ctx := context.Background()
	now := time.Now().UTC().Add(-30 * time.Minute)
	for _, ev := range []models.ToolEvent{
		{
			SourceFile: "f", SourceEventID: "b1", SessionID: "sB", ProjectRoot: root, Timestamp: now,
			Tool: models.ToolCodex, ActionType: models.ActionReadFile, Target: "b.go", Success: true,
		},
		{
			SourceFile: "f", SourceEventID: "c1", SessionID: "sC", ProjectRoot: root, Timestamp: now,
			Tool: models.ToolClaudeCode, ActionType: models.ActionReadFile, Target: "c.go", Success: true,
		},
	} {
		if _, err := st.Ingest(ctx, []models.ToolEvent{ev}, nil, store.IngestOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	for _, turn := range []models.APITurn{
		{SessionID: "sA", Provider: models.ProviderAnthropic, Model: "claude-sonnet-4-6", InputTokens: 1000, OutputTokens: 100, Timestamp: now},
		{SessionID: "sB", Provider: models.ProviderOpenAI, Model: "gpt-5.1", InputTokens: 2000, OutputTokens: 200, Timestamp: now},
		{SessionID: "sC", Provider: models.ProviderAnthropic, Model: "claude-opus-4-7", InputTokens: 3000, OutputTokens: 300, Timestamp: now},
		{SessionID: "sC", Provider: models.ProviderAnthropic, Model: "claude-haiku-4-5", InputTokens: 400, OutputTokens: 40, Timestamp: now.Add(time.Minute)},
	} {
		if _, err := st.InsertAPITurn(ctx, turn); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

func getModelJSON(t *testing.T, s *Server, u string, out any) {
	t.Helper()
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, u, nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET %s: %d body=%s", u, rr.Code, rr.Body.String())
	}
	if err := json.NewDecoder(rr.Body).Decode(out); err != nil {
		t.Fatalf("GET %s: decode: %v", u, err)
	}
}

func withModel(u, model string) string {
	if model == "" {
		return u
	}
	return u + "&model=" + url.QueryEscape(model)
}

// TestGlobalModelFilterHonouredAcrossEndpoints is the table-driven pin of the
// Model filter's two grains over the shared fixture: session-grain endpoints
// (sessions / actions / tools) keep whole sessions that USED the model;
// turn-grain endpoints (cost, status api_turns) keep only that model's turns.
func TestGlobalModelFilterHonouredAcrossEndpoints(t *testing.T) {
	s := seedModelFilterFixture(t)
	cases := []struct {
		model string
		// session grain
		sessions []string
		actions  int
		tools    []string
		// turn grain
		costKeys []string
		apiTurns int64
	}{
		{
			model: "", sessions: []string{"sA", "sB", "sC"}, actions: 3, tools: []string{"claude-code", "codex"},
			costKeys: []string{"claude-haiku-4-5", "claude-opus-4-7", "claude-sonnet-4-6", "gpt-5.1"}, apiTurns: 4,
		},
		{
			model: "all", sessions: []string{"sA", "sB", "sC"}, actions: 3, tools: []string{"claude-code", "codex"},
			costKeys: []string{"claude-haiku-4-5", "claude-opus-4-7", "claude-sonnet-4-6", "gpt-5.1"}, apiTurns: 4,
		},
		{
			model: "gpt-5.1", sessions: []string{"sB"}, actions: 1, tools: []string{"codex"},
			costKeys: []string{"gpt-5.1"}, apiTurns: 1,
		},
		{
			model: "claude-haiku-4-5", sessions: []string{"sC"}, actions: 1, tools: []string{"claude-code"},
			costKeys: []string{"claude-haiku-4-5"}, apiTurns: 1,
		},
		{
			model: "claude-sonnet-4-6", sessions: []string{"sA"}, actions: 1, tools: []string{"claude-code"},
			costKeys: []string{"claude-sonnet-4-6"}, apiTurns: 1,
		},
		{model: "no-such-model", sessions: nil, actions: 0, tools: nil, costKeys: nil, apiTurns: 0},
	}
	for _, tc := range cases {
		t.Run("model="+tc.model, func(t *testing.T) {
			var sess struct {
				Rows []struct {
					ID string `json:"id"`
				} `json:"rows"`
				Total int `json:"total"`
			}
			getModelJSON(t, s, withModel("/api/sessions?days=2&limit=50", tc.model), &sess)
			var ids []string
			for _, r := range sess.Rows {
				ids = append(ids, r.ID)
			}
			sort.Strings(ids)
			if !equalModelStrings(ids, tc.sessions) || sess.Total != len(tc.sessions) {
				t.Errorf("/api/sessions ids=%v total=%d, want %v", ids, sess.Total, tc.sessions)
			}

			var acts struct {
				Total int `json:"total"`
			}
			getModelJSON(t, s, withModel("/api/actions?days=2", tc.model), &acts)
			if acts.Total != tc.actions {
				t.Errorf("/api/actions total=%d, want %d", acts.Total, tc.actions)
			}

			var tools struct {
				Tools []struct {
					Tool string `json:"tool"`
				} `json:"tools"`
			}
			getModelJSON(t, s, withModel("/api/tools?days=2", tc.model), &tools)
			var gotTools []string
			for _, r := range tools.Tools {
				gotTools = append(gotTools, r.Tool)
			}
			sort.Strings(gotTools)
			if !equalModelStrings(gotTools, tc.tools) {
				t.Errorf("/api/tools = %v, want %v", gotTools, tc.tools)
			}

			var cost struct {
				Rows []struct {
					Key string `json:"key"`
				} `json:"rows"`
			}
			getModelJSON(t, s, withModel("/api/cost?days=2&group_by=model", tc.model), &cost)
			var keys []string
			for _, r := range cost.Rows {
				keys = append(keys, r.Key)
			}
			sort.Strings(keys)
			if !equalModelStrings(keys, tc.costKeys) {
				t.Errorf("/api/cost keys = %v, want %v", keys, tc.costKeys)
			}

			var status struct {
				Sessions int64 `json:"sessions"`
				APITurns int64 `json:"api_turns"`
				Actions  int64 `json:"actions"`
			}
			getModelJSON(t, s, withModel("/api/status/scoped?days=2", tc.model), &status)
			if status.Sessions != int64(len(tc.sessions)) || status.APITurns != tc.apiTurns || status.Actions != int64(tc.actions) {
				t.Errorf("/api/status/scoped = %+v, want sessions=%d api_turns=%d actions=%d",
					status, len(tc.sessions), tc.apiTurns, tc.actions)
			}
		})
	}
}

// TestGlobalModelFilterComposesWithTool: model AND tool, never OR.
func TestGlobalModelFilterComposesWithTool(t *testing.T) {
	s := seedModelFilterFixture(t)
	var sess struct {
		Total int `json:"total"`
	}
	getModelJSON(t, s, "/api/sessions?days=2&tool=codex&model=claude-opus-4-7", &sess)
	if sess.Total != 0 {
		t.Fatalf("codex AND claude-opus-4-7 = %d sessions, want 0", sess.Total)
	}
	getModelJSON(t, s, "/api/sessions?days=2&tool=claude-code&model=claude-opus-4-7", &sess)
	if sess.Total != 1 {
		t.Fatalf("claude-code AND claude-opus-4-7 = %d sessions, want 1", sess.Total)
	}
}

func equalModelStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
