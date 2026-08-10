package llmrun

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/briefing"
	"github.com/marmutapp/superbased-observer/internal/db"
	"github.com/marmutapp/superbased-observer/internal/intelligence/learn"
	"github.com/marmutapp/superbased-observer/internal/llmconn"
	"github.com/marmutapp/superbased-observer/internal/store"
)

func sampleBriefing() briefing.Briefing {
	return briefing.Briefing{
		Date: "2026-08-01",
		AtGlance: briefing.AtGlance{
			Sessions: 5, Turns: 120, SpendUSD: 12.5, InputTokens: 1_000_000,
			OutputTokens: 200_000, CacheHitRate: 0.62, ErrorTurns: 3, ActiveHours: 6.5, FastShare: 0.4,
		},
		Activity: []briefing.Activity{
			{Tool: "claude", Sessions: 3, Turns: 90, SpendUSD: 10.0},
			{Tool: "codex", Sessions: 2, Turns: 30, SpendUSD: 2.5},
		},
		TopSessions: []briefing.SessionRef{
			{ID: "deadbeefcafebabec001deadbeefcafe", Tool: "claude", Turns: 40, CostUSD: 6.0},
		},
		Opportunities: []briefing.Opportunity{
			{Detector: "routing_shadow", Category: "routing", Scope: "all-sessions", Title: "Reroute would have saved ~$2.30", SavingsUSD: 2.3, Confidence: 0.9},
		},
		Narrative: "template narrative",
	}
}

// fakeLLM returns a loopback chat/completions server echoing a fixed reply and
// recording the last request body.
func fakeLLM(t *testing.T, reply string, lastBody *[]byte) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if lastBody != nil {
			*lastBody = b
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "chatcmpl_1",
			"choices": []map[string]any{
				{"index": 0, "message": map[string]string{"role": "assistant", "content": reply}},
			},
			"usage": map[string]int{"prompt_tokens": 100, "completion_tokens": 50},
		})
	}))
	t.Cleanup(s.Close)
	return s
}

func localConn(id, url string) llmconn.Connection {
	return llmconn.Connection{ID: id, Name: id, Kind: llmconn.KindLocalOpenAI, BaseURL: url, DefaultModel: "m1", Enabled: true}
}

func TestNarrativeNoConnectionKeepsTemplate(t *testing.T) {
	r := &Runner{}
	b, connID, err := r.Narrative(context.Background(), nil, sampleBriefing())
	if !errors.Is(err, ErrNoConnection) {
		t.Fatalf("err = %v, want ErrNoConnection", err)
	}
	if connID != "" || b.Narrative != "template narrative" {
		t.Fatalf("briefing must be unchanged: conn=%q narrative=%q", connID, b.Narrative)
	}
}

func TestNarrativeReplacesParagraph(t *testing.T) {
	srv := fakeLLM(t, "A focused 120-turn day across claude and codex, dominated by one 40-turn session; rerouting is the top opportunity.", nil)
	r := &Runner{}
	b, connID, err := r.Narrative(context.Background(), []llmconn.Connection{localConn("local", srv.URL)}, sampleBriefing())
	if err != nil {
		t.Fatalf("Narrative: %v", err)
	}
	if connID != "local" {
		t.Fatalf("connID = %q", connID)
	}
	if !strings.Contains(b.Narrative, "120-turn") {
		t.Fatalf("narrative not replaced: %q", b.Narrative)
	}
}

func TestNarrativeFailureFallsBack(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)
	r := &Runner{}
	b, _, err := r.Narrative(context.Background(), []llmconn.Connection{localConn("down", srv.URL)}, sampleBriefing())
	if err == nil {
		t.Fatal("expected error from a down connection")
	}
	if b.Narrative != "template narrative" {
		t.Fatalf("narrative must fall back to template, got %q", b.Narrative)
	}
}

func TestAskNoConnection(t *testing.T) {
	r := &Runner{}
	if _, err := r.Ask(context.Background(), nil, "what cost the most?", sampleBriefing()); !errors.Is(err, ErrNoConnection) {
		t.Fatalf("err = %v, want ErrNoConnection", err)
	}
}

func TestAskAnswersAndRecordsAccounting(t *testing.T) {
	srv := fakeLLM(t, "claude was the biggest cost driver at $10.00.", nil)

	database, err := db.Open(context.Background(), db.Options{Path: filepath.Join(t.TempDir(), "llmrun.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })

	r := &Runner{
		Store: store.New(database),
		Env:   func(string) string { return "" },
		Now:   func() time.Time { return time.Date(2026, 8, 2, 9, 0, 0, 0, time.UTC) },
	}
	res, err := r.Ask(context.Background(), []llmconn.Connection{localConn("local", srv.URL)}, "what cost the most?", sampleBriefing())
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if !strings.Contains(res.Text, "claude") || res.Model != "m1" || res.ConnectionID != "local" || res.ConnectionName != "local" {
		t.Fatalf("AskResult = %+v", res)
	}

	calls, err := r.Store.ListAnalysisCalls(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 {
		t.Fatalf("got %d analysis calls, want 1", len(calls))
	}
	c := calls[0]
	if c.Task != "ask" || c.ConnectionID != "local" || c.Model != "m1" || c.InputTokens != 100 || c.OutputTokens != 50 || c.Status != "ok" {
		t.Fatalf("analysis row wrong: %+v", c)
	}
}

func TestAskRemoteRequiresCredentialRecorded(t *testing.T) {
	conn := llmconn.Connection{ID: "remote", Kind: llmconn.KindOpenAICompat, BaseURL: "https://api.example.com", DefaultModel: "m1", Enabled: true, AllowRemote: true, APIKeyEnv: "NOPE_KEY"}
	database, err := db.Open(context.Background(), db.Options{Path: filepath.Join(t.TempDir(), "llmrun.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	r := &Runner{Store: store.New(database), Env: func(string) string { return "" }}
	if _, err := r.Ask(context.Background(), []llmconn.Connection{conn}, "q", sampleBriefing()); err == nil {
		t.Fatal("expected credential error")
	}
	calls, err := r.Store.ListAnalysisCalls(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0].Status != "error" {
		t.Fatalf("error row not recorded: %+v", calls)
	}
}

// TestNarrativePromptContentFree pins that the prompt projection never embeds
// the template narrative or any free-text — only numbers, tool names, and
// truncated opaque ids.
func TestNarrativePromptContentFree(t *testing.T) {
	p := narrativePrompt(sampleBriefing())
	if strings.Contains(p, "template narrative") {
		t.Error("prompt must not embed the template narrative")
	}
	if strings.Contains(p, "deadbeefcafebabec001deadbeefcafe") {
		t.Error("prompt must not embed full session ids")
	}
	for _, want := range []string{"sessions: 5", "claude", "$2.30", "turns: 120"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
}

func TestEvidenceProjectionDeterministic(t *testing.T) {
	a := evidenceProjection(sampleBriefing())
	for i := 0; i < 5; i++ {
		if b := evidenceProjection(sampleBriefing()); b != a {
			t.Fatal("projection not deterministic")
		}
	}
}

// TestRunnerZeroValueSafe pins that a zero Runner never panics and never
// egresses (no connections → ErrNoConnection for both tasks).
func TestRunnerZeroValueSafe(t *testing.T) {
	var r Runner
	b, _, err := r.Narrative(context.Background(), nil, sampleBriefing())
	if !errors.Is(err, ErrNoConnection) || b.Narrative != "template narrative" {
		t.Fatalf("zero-value narrative: err=%v", err)
	}
	if _, err := r.Ask(context.Background(), nil, "q", sampleBriefing()); !errors.Is(err, ErrNoConnection) {
		t.Fatalf("zero-value ask: err=%v", err)
	}
	if _, err := r.AskPast(context.Background(), nil, PastContext{Question: "q"}); !errors.Is(err, ErrNoConnection) {
		t.Fatalf("zero-value ask-past: err=%v", err)
	}
	if _, err := r.Coach(context.Background(), nil, CoachingContext{}); !errors.Is(err, ErrNoConnection) {
		t.Fatalf("zero-value coach: err=%v", err)
	}
}

func pastSampleContext() PastContext {
	return PastContext{
		Question:   "What was I fighting with on the build last week?",
		WindowDays: 7,
		Sessions: []store.PastSession{
			{
				SessionID: "deadbeefcafebabec001deadbeefcafe", Tool: "claude", Model: "opus",
				Project: "/home/alice/secret/repo-x", StartedAt: time.Date(2026, 7, 25, 9, 0, 0, 0, time.UTC),
				SummaryMD:   "Fixed the build that failed; the import was the culprit.",
				ActionCount: 40, FailureCount: 3,
				ErrorMessages: []string{"permission denied on /etc/hosts"},
			},
			{
				SessionID: "b", Tool: "codex", Model: "sonnet",
				Project: "C:\\Users\\bob\\Work\\proj\\app", StartedAt: time.Date(2026, 7, 24, 9, 0, 0, 0, time.UTC),
				SummaryMD: "Refactored the dashboard; no build involved.",
			},
		},
	}
}

func TestAskPastNoConnection(t *testing.T) {
	r := &Runner{}
	if _, err := r.AskPast(context.Background(), nil, pastSampleContext()); !errors.Is(err, ErrNoConnection) {
		t.Fatalf("err = %v, want ErrNoConnection", err)
	}
}

func TestAskPastAnswersAndRecordsAccounting(t *testing.T) {
	srv := fakeLLM(t, "The build kept failing on a bad import in the repo-x session.", nil)

	database, err := db.Open(context.Background(), db.Options{Path: filepath.Join(t.TempDir(), "llmrun.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })

	r := &Runner{
		Store: store.New(database),
		Env:   func(string) string { return "" },
		Now:   func() time.Time { return time.Date(2026, 8, 2, 9, 0, 0, 0, time.UTC) },
	}
	res, err := r.AskPast(context.Background(), []llmconn.Connection{localConn("local", srv.URL)}, pastSampleContext())
	if err != nil {
		t.Fatalf("AskPast: %v", err)
	}
	if !strings.Contains(res.Text, "build") || res.Model != "m1" || res.ConnectionID != "local" || res.ConnectionName != "local" {
		t.Fatalf("AskPast result = %+v", res)
	}

	calls, err := r.Store.ListAnalysisCalls(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 {
		t.Fatalf("got %d analysis calls, want 1", len(calls))
	}
	c := calls[0]
	if c.Task != "ask_past" || c.ConnectionID != "local" || c.Model != "m1" || c.Status != "ok" {
		t.Fatalf("analysis row wrong: %+v", c)
	}
}

// TestAskPastPromptNoRawPaths pins that the prompt carries only bare repo
// markers — never the full project roots (POSIX or Windows).
func TestAskPastPromptNoRawPaths(t *testing.T) {
	now := time.Date(2026, 8, 2, 9, 0, 0, 0, time.UTC)
	p := askPastPrompt(pastSampleContext(), now)
	for _, forbidden := range []string{"/home/alice/secret", "C:\\Users\\bob"} {
		if strings.Contains(p, forbidden) {
			t.Errorf("prompt leaks raw path %q:\n%s", forbidden, p)
		}
	}
	for _, want := range []string{"repo=repo-x", "repo=app", "model=opus", "8 days ago", "9 days ago", "permission denied", "40 actions, 3 failed"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt missing %q:\n%s", want, p)
		}
	}
}

func TestAskPastPromptDeterministic(t *testing.T) {
	now := time.Date(2026, 8, 2, 9, 0, 0, 0, time.UTC)
	a := askPastPrompt(pastSampleContext(), now)
	for i := 0; i < 5; i++ {
		if b := askPastPrompt(pastSampleContext(), now); b != a {
			t.Fatal("ask-past prompt not deterministic")
		}
	}
}

func TestAskPastPromptEmptySessions(t *testing.T) {
	p := askPastPrompt(PastContext{Question: "q", WindowDays: 30}, time.Now())
	if !strings.Contains(p, "(none matched)") {
		t.Fatalf("empty context must say so:\n%s", p)
	}
}

func TestPastRepoMarker(t *testing.T) {
	cases := map[string]string{
		"/home/alice/secret/repo-x": "repo-x",
		"C:\\Users\\bob\\proj\\app": "app",
		"/repo":                     "repo",
		"":                          "",
	}
	for in, want := range cases {
		if got := pastRepoMarker(in); got != want {
			t.Errorf("pastRepoMarker(%q) = %q, want %q", in, got, want)
		}
	}
}

// coachSampleContext is the evidence bundle the coaching prompt renders:
// one recurring failure pattern + one failure→recovery rule, with paths that
// must never reach the model raw.
func coachSampleContext() CoachingContext {
	return CoachingContext{
		WindowDays: 30,
		Project:    "/home/alice/secret/repo-x",
		Patterns: []briefing.Pattern{
			{
				Category: "permission", NormMsg: "permission denied on /etc/hosts",
				Tool: "claude", Model: "opus",
				Count:     8,
				Sessions:  []string{"s1", "s2", "s3", "s4"},
				Days:      []string{"2026-07-01", "2026-07-02", "2026-07-03"},
				FirstSeen: time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC),
				LastSeen:  time.Date(2026, 7, 3, 9, 0, 0, 0, time.UTC),
			},
		},
		Rules: []learn.Rule{
			{
				CommandHash:    "abc",
				CommandSummary: "go test ./...",
				ErrorCategory:  "test_failure",
				ErrorMessage:   "FAIL TestFoo want 1 got 0",
				FailureCount:   8,
				RecoveryCount:  3,
				EditedFiles: []string{
					"/home/alice/secret/repo-x/foo_test.go",
					"C:\\Users\\bob\\Work\\proj\\app\\main.go",
				},
				FailureToolMix: []string{"claude", "codex"},
				LastObservedAt: time.Date(2026, 7, 2, 9, 0, 0, 0, time.UTC).Format(time.RFC3339),
			},
		},
	}
}

func TestCoachNoConnection(t *testing.T) {
	r := &Runner{}
	if _, err := r.Coach(context.Background(), nil, coachSampleContext()); !errors.Is(err, ErrNoConnection) {
		t.Fatalf("err = %v, want ErrNoConnection", err)
	}
}

func TestCoachAnswersAndRecordsAccounting(t *testing.T) {
	srv := fakeLLM(t, "You kept hitting permission errors on /etc/hosts — verify your credential before retrying.", nil)

	database, err := db.Open(context.Background(), db.Options{Path: filepath.Join(t.TempDir(), "llmrun.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })

	r := &Runner{
		Store: store.New(database),
		Env:   func(string) string { return "" },
		Now:   func() time.Time { return time.Date(2026, 8, 2, 9, 0, 0, 0, time.UTC) },
	}
	res, err := r.Coach(context.Background(), []llmconn.Connection{localConn("local", srv.URL)}, coachSampleContext())
	if err != nil {
		t.Fatalf("Coach: %v", err)
	}
	if !strings.Contains(res.Text, "permission") || res.Model != "m1" || res.ConnectionID != "local" || res.ConnectionName != "local" {
		t.Fatalf("Coach result = %+v", res)
	}

	calls, err := r.Store.ListAnalysisCalls(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 {
		t.Fatalf("got %d analysis calls, want 1", len(calls))
	}
	c := calls[0]
	if c.Task != "coach" || c.ConnectionID != "local" || c.Model != "m1" || c.Status != "ok" {
		t.Fatalf("analysis row wrong: %+v", c)
	}
}

// TestCoachPromptNoRawPaths pins that the prompt carries only the bare repo
// marker and edited-file basenames — never the full project roots or edited
// file paths (POSIX or Windows).
func TestCoachPromptNoRawPaths(t *testing.T) {
	now := time.Date(2026, 8, 2, 9, 0, 0, 0, time.UTC)
	p := coachPrompt(coachSampleContext(), now)
	for _, forbidden := range []string{"/home/alice/secret", "C:\\Users\\bob"} {
		if strings.Contains(p, forbidden) {
			t.Errorf("prompt leaks raw path %q:\n%s", forbidden, p)
		}
	}
	for _, want := range []string{
		"project: repo-x", "Evidence window: last 30 days",
		"permission: 8 occurrences across 4 sessions", "permission denied on /etc/hosts",
		"failed 8 times, recovered 3 times", "category=test_failure", "tools=claude,codex",
		"foo_test.go", "main.go", "fix touched",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt missing %q:\n%s", want, p)
		}
	}
}

func TestCoachPromptDeterministic(t *testing.T) {
	now := time.Date(2026, 8, 2, 9, 0, 0, 0, time.UTC)
	a := coachPrompt(coachSampleContext(), now)
	for i := 0; i < 5; i++ {
		if b := coachPrompt(coachSampleContext(), now); b != a {
			t.Fatal("coach prompt not deterministic")
		}
	}
}

func TestCoachPromptEmptyEvidence(t *testing.T) {
	p := coachPrompt(CoachingContext{WindowDays: 30}, time.Now())
	if !strings.Contains(p, "(none)") {
		t.Fatalf("empty evidence must say so:\n%s", p)
	}
}

func TestCoachBasenames(t *testing.T) {
	cases := []struct {
		in   []string
		want []string
	}{
		{[]string{"/home/alice/secret/repo-x/foo.go"}, []string{"foo.go"}},
		{[]string{"C:\\Users\\bob\\proj\\app\\main.go"}, []string{"main.go"}},
		{[]string{"/repo/."}, nil},
		{nil, nil},
	}
	for _, c := range cases {
		got := coachBasenames(c.in)
		if len(got) != len(c.want) {
			t.Errorf("coachBasenames(%v) = %v, want %v", c.in, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("coachBasenames(%v) = %v, want %v", c.in, got, c.want)
			}
		}
	}
}

// triageSampleContext is a three-card opportunity set for the triage prompt.
func triageSampleContext() TriageContext {
	return TriageContext{
		WindowDays: 7,
		Date:       "2026-08-01",
		Opportunities: []briefing.Opportunity{
			{DedupKey: "routing_shadow_1", Detector: "routing_shadow", Category: "routing", Scope: "all-sessions", Title: "Rerouting would save money", SavingsUSD: 3.1, Confidence: 0.8},
			{DedupKey: "cache_hit_rate_1", Detector: "cache_hit_rate", Category: "cache", Scope: "all-sessions", Title: "Cache hit rate low", SavingsUSD: 1.2, Confidence: 0.6},
			{DedupKey: "trivial_model_s1", Detector: "trivial_model_overprovisioning", Category: "model-selection", Scope: "session", Title: "Trivial premium session", SavingsUSD: 0.4, Confidence: 0.7},
		},
	}
}

func TestTriageNoConnection(t *testing.T) {
	r := &Runner{}
	if _, err := r.Triage(context.Background(), nil, triageSampleContext()); !errors.Is(err, ErrNoConnection) {
		t.Fatalf("err = %v, want ErrNoConnection", err)
	}
}

func TestTriageAnswersAndAppendsOmittedCards(t *testing.T) {
	srv := fakeLLM(t, `{"order":["cache_hit_rate_1","routing_shadow_1"],"groups":[{"label":"top savings","keys":["routing_shadow_1","cache_hit_rate_1"]}],"summary":"cache first because it compounds daily"}`, nil)
	r := &Runner{}
	res, err := r.Triage(context.Background(), []llmconn.Connection{localConn("local", srv.URL)}, triageSampleContext())
	if err != nil {
		t.Fatalf("Triage: %v", err)
	}
	want := []string{"cache_hit_rate_1", "routing_shadow_1", "trivial_model_s1"}
	if len(res.Order) != len(want) {
		t.Fatalf("order = %v, want %v", res.Order, want)
	}
	for i := range want {
		if res.Order[i] != want[i] {
			t.Fatalf("order = %v, want %v", res.Order, want)
		}
	}
	if len(res.Groups) != 1 || res.Groups[0].Label != "top savings" {
		t.Fatalf("groups = %+v", res.Groups)
	}
	if res.Summary == "" || res.Model != "m1" || res.ConnectionID != "local" || res.ConnectionName != "local" {
		t.Fatalf("TriageResult = %+v", res)
	}
}

func TestTriageRejectsUnknownKeysAndDuplicates(t *testing.T) {
	srv := fakeLLM(t, `{"order":["not_a_card","cache_hit_rate_1","cache_hit_rate_1","routing_shadow_1","trivial_model_s1"],"groups":[{"label":"g","keys":["bogus","routing_shadow_1","routing_shadow_1"]}]}`, nil)
	r := &Runner{}
	res, err := r.Triage(context.Background(), []llmconn.Connection{localConn("local", srv.URL)}, triageSampleContext())
	if err != nil {
		t.Fatalf("Triage: %v", err)
	}
	want := []string{"cache_hit_rate_1", "routing_shadow_1", "trivial_model_s1"}
	if len(res.Order) != len(want) {
		t.Fatalf("order = %v, want %v", res.Order, want)
	}
	for i := range want {
		if res.Order[i] != want[i] {
			t.Fatalf("order = %v, want %v", res.Order, want)
		}
	}
	if len(res.Groups) != 1 || len(res.Groups[0].Keys) != 1 || res.Groups[0].Keys[0] != "routing_shadow_1" {
		t.Fatalf("groups = %+v", res.Groups)
	}
}

func TestTriageUnparseableIsError(t *testing.T) {
	srv := fakeLLM(t, "I don't think reordering is useful.", nil)
	r := &Runner{}
	if _, err := r.Triage(context.Background(), []llmconn.Connection{localConn("local", srv.URL)}, triageSampleContext()); err == nil {
		t.Fatal("expected an error from a non-JSON reply")
	}
}

func TestTriagePromptDeterministic(t *testing.T) {
	a := triagePrompt(triageSampleContext())
	for i := 0; i < 5; i++ {
		if b := triagePrompt(triageSampleContext()); b != a {
			t.Fatal("triage prompt not deterministic")
		}
	}
	for _, want := range []string{
		"Window: 2026-08-01 (7 days)",
		"routing_shadow_1 | routing_shadow | Rerouting would save money | savings_usd=3.10 | confidence=80%",
		"cache_hit_rate_1", "trivial_model_s1",
	} {
		if !strings.Contains(a, want) {
			t.Errorf("prompt missing %q:\n%s", want, a)
		}
	}
}

// diagnoseSampleContext is two recurring failure patterns for the diagnose
// prompt, with a project root that must never reach the model raw.
func diagnoseSampleContext() DiagnoseContext {
	return DiagnoseContext{
		WindowDays: 30,
		Project:    "/home/alice/secret/repo-x",
		Patterns: []briefing.Pattern{
			{
				Category: "permission", NormMsg: "permission denied on /etc/hosts",
				Tool: "claude", Model: "opus",
				Count:     8,
				Sessions:  []string{"s1", "s2", "s3", "s4"},
				Days:      []string{"2026-07-01", "2026-07-02", "2026-07-03"},
				FirstSeen: time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC),
				LastSeen:  time.Date(2026, 7, 3, 9, 0, 0, 0, time.UTC),
			},
			{
				Category: "test_failure", NormMsg: "FAIL TestFoo want 1 got 0",
				Tool: "codex", Model: "gpt-5",
				Count:     5,
				Sessions:  []string{"s5", "s6"},
				Days:      []string{"2026-07-02"},
				FirstSeen: time.Date(2026, 7, 2, 9, 0, 0, 0, time.UTC),
				LastSeen:  time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC),
			},
		},
	}
}

func TestDiagnoseNoConnection(t *testing.T) {
	r := &Runner{}
	if _, err := r.Diagnose(context.Background(), nil, diagnoseSampleContext()); !errors.Is(err, ErrNoConnection) {
		t.Fatalf("err = %v, want ErrNoConnection", err)
	}
}

func TestDiagnoseAnswersAndValidatesIndexes(t *testing.T) {
	// index 0 valid, index 5 out of range, duplicate index 1, index 3 out of
	// range (sample context has 2 patterns) → [0, 1] sorted; out-of-range and
	// duplicate entries are dropped.
	srv := fakeLLM(t, `{"diagnoses":[
		{"index":0,"diagnosis":"credential scope too narrow","next_step":"verify access"},
		{"index":5,"diagnosis":"junk","next_step":"junk"},
		{"index":1,"diagnosis":"flaky test env","next_step":"run in isolation"},
		{"index":1,"diagnosis":"dup","next_step":"dup"},
		{"index":3,"diagnosis":"runtime nil guard","next_step":"add nil checks"}
	]}`, nil)
	r := &Runner{}
	res, err := r.Diagnose(context.Background(), []llmconn.Connection{localConn("local", srv.URL)}, diagnoseSampleContext())
	if err != nil {
		t.Fatalf("Diagnose: %v", err)
	}
	if len(res.Diagnoses) != 2 {
		t.Fatalf("diagnoses = %+v", res.Diagnoses)
	}
	for i, want := range []int{0, 1} {
		if res.Diagnoses[i].Index != want {
			t.Fatalf("diagnoses = %+v", res.Diagnoses)
		}
	}
	if res.Diagnoses[0].Diagnosis != "credential scope too narrow" || res.Model != "m1" || res.ConnectionID != "local" {
		t.Fatalf("DiagnoseResult = %+v", res)
	}
}

func TestDiagnoseNoValidDiagnosesIsError(t *testing.T) {
	for _, reply := range []string{`{"diagnoses":[]}`, `{"diagnoses":[{"index":9,"diagnosis":"x","next_step":"y"}]}`, "no json here"} {
		srv := fakeLLM(t, reply, nil)
		r := &Runner{}
		if _, err := r.Diagnose(context.Background(), []llmconn.Connection{localConn("local", srv.URL)}, diagnoseSampleContext()); err == nil {
			t.Fatalf("reply %q must be an error", reply)
		}
	}
}

func TestDiagnosePromptNoRawPaths(t *testing.T) {
	p := diagnosePrompt(diagnoseSampleContext())
	for _, forbidden := range []string{"/home/alice/secret"} {
		if strings.Contains(p, forbidden) {
			t.Errorf("prompt leaks raw path %q:\n%s", forbidden, p)
		}
	}
	for _, want := range []string{
		"project: repo-x", "Evidence window: last 30 days",
		"k0 | category=permission (Permission) | tool=claude | model=opus | 8 occurrences across 4 sessions / 3 days",
		"permission denied on /etc/hosts",
		"k1 | category=test_failure (Test failure) | tool=codex",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt missing %q:\n%s", want, p)
		}
	}
}

func TestDiagnosePromptDeterministic(t *testing.T) {
	a := diagnosePrompt(diagnoseSampleContext())
	for i := 0; i < 5; i++ {
		if b := diagnosePrompt(diagnoseSampleContext()); b != a {
			t.Fatal("diagnose prompt not deterministic")
		}
	}
}

func TestExtractJSONObject(t *testing.T) {
	cases := []struct{ in, want string }{
		{`{"a":1}`, `{"a":1}`},
		{"```json\n{\"a\":1}\n```", `{"a":1}`},
		{`prefix {"a":1} suffix`, `{"a":1}`},
		{"no object", "no object"},
		{``, ``},
	}
	for _, c := range cases {
		if got := extractJSONObject(c.in); got != c.want {
			t.Errorf("extractJSONObject(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestQuestionsNoConnectionIsErrNoConnection(t *testing.T) {
	r := &Runner{}
	if _, err := r.Questions(context.Background(), nil, sampleBriefing()); !errors.Is(err, ErrNoConnection) {
		t.Fatalf("err = %v, want ErrNoConnection", err)
	}
}

func TestQuestionsReturnsList(t *testing.T) {
	srv := fakeLLM(t, `["Why is the cache hit rate only 62%?", "Is the routing opportunity worth enabling?"]`, nil)
	r := &Runner{}
	qs, err := r.Questions(context.Background(), []llmconn.Connection{localConn("local", srv.URL)}, sampleBriefing())
	if err != nil {
		t.Fatalf("Questions: %v", err)
	}
	if len(qs) != 2 || qs[0] != "Why is the cache hit rate only 62%?" {
		t.Fatalf("qs = %q", qs)
	}
}

func TestQuestionsParseFailureIsError(t *testing.T) {
	srv := fakeLLM(t, "sorry, no questions", nil)
	r := &Runner{}
	if _, err := r.Questions(context.Background(), []llmconn.Connection{localConn("local", srv.URL)}, sampleBriefing()); err == nil {
		t.Fatal("unparseable reply must be an error (caller keeps template)")
	}
}

func TestParseQuestions(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    []string
		wantErr bool
	}{
		{name: "plain array", in: `["a","b"]`, want: []string{"a", "b"}},
		{name: "fenced", in: "```json\n[\"a\"]\n```", want: []string{"a"}},
		{name: "prose around array", in: `Here: ["a", "b", "c"] thanks`, want: []string{"a", "b", "c"}},
		{name: "blanks dropped", in: `["a","", "  "]`, want: []string{"a"}},
		{name: "capped at five", in: `["1","2","3","4","5","6"]`, want: []string{"1", "2", "3", "4", "5"}},
		{name: "empty array", in: `[]`, wantErr: true},
		{name: "no array", in: "no questions today", wantErr: true},
		{name: "empty", in: "", wantErr: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := parseQuestions(c.in)
			if c.wantErr {
				if err == nil {
					t.Fatalf("parseQuestions(%q) = %v, want error", c.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseQuestions(%q): %v", c.in, err)
			}
			if len(got) != len(c.want) {
				t.Fatalf("parseQuestions(%q) = %v, want %v", c.in, got, c.want)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Fatalf("parseQuestions(%q) = %v, want %v", c.in, got, c.want)
				}
			}
		})
	}
}
