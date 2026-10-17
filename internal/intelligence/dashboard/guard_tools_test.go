package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// Guard tools (T-154): the dashboard twins of `observer guard test`,
// `guard rescan`, `guard compile [--diff]` and `guard mcp scan|report`.

func guardToolPost(s *Server, path, body, token string, cookie *http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set(remoteConfirmHeader, token)
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)
	return rr
}

// guardJobsInfo GETs a guard job route: its facts plus the confirm token.
func guardJobsInfo(t *testing.T, s *Server, path string) (guardJobsView, *http.Cookie) {
	t.Helper()
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, path, nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET %s = %d %s", path, rr.Code, rr.Body.String())
	}
	var v guardJobsView
	if err := json.Unmarshal(rr.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	for _, c := range rr.Result().Cookies() {
		if c.Name == remoteConfirmCookie {
			return v, c
		}
	}
	t.Fatal("no confirm cookie")
	return v, nil
}

func TestGuardTestDryRun(t *testing.T) {
	s, _ := newEvidenceServer(t, nil)
	rr := guardToolPost(s, "/api/guard/test", `{"kind":"command","target":"git push --force origin main"}`, "", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("dry run = %d %s", rr.Code, rr.Body.String())
	}
	var out guardTestResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Report.RuleID != "R-110" || out.Report.EnforceDecision != "deny" || out.Mode == "" {
		t.Fatalf("dry run = %+v", out)
	}
	if out.Report.Kind != "shell_exec" || out.Report.Target != "git push --force origin main" {
		t.Fatalf("event = %+v", out.Report)
	}

	quiet := guardToolPost(s, "/api/guard/test", `{"kind":"file","target":"./notes.txt"}`, "", nil)
	if quiet.Code != http.StatusOK || !strings.Contains(quiet.Body.String(), `"kind":"file_access"`) {
		t.Fatalf("file write dry run = %d %s", quiet.Code, quiet.Body.String())
	}

	bad := []string{
		`{"kind":"event","target":"x"}`,
		`{"kind":"command","target":"   "}`,
		`{"kind":"command","target":"ls","project_root":"/not/a/known/project"}`,
		`{"kind":"command","target":"` + strings.Repeat("a", 5000) + `"}`,
		`not json`,
	}
	for _, body := range bad {
		if rr := guardToolPost(s, "/api/guard/test", body, "", nil); rr.Code != http.StatusBadRequest {
			t.Errorf("body %.40s: status = %d, want 400", body, rr.Code)
		}
	}
	get := httptest.NewRecorder()
	s.Handler().ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/api/guard/test", nil))
	if get.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET = %d, want 405", get.Code)
	}
}

// TestGuardTestUsesAKnownProject: a picked project root must be one this
// observer has seen; it then becomes the event's project root.
func TestGuardTestUsesAKnownProject(t *testing.T) {
	s, _ := newEvidenceServer(t, nil)
	if _, err := s.opts.DB.ExecContext(context.Background(),
		`INSERT INTO projects (id, root_path, created_at) VALUES (1, '/work/app', '2026-09-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	rr := guardToolPost(s, "/api/guard/test", `{"kind":"command","target":"ls","project_root":"/work/app"}`, "", nil)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"project_root":"/work/app"`) {
		t.Fatalf("known project = %d %s", rr.Code, rr.Body.String())
	}
}

func TestGuardJobRoutes(t *testing.T) {
	var calls atomic.Int32
	var lastArgs atomic.Value
	fake := func(_ context.Context, args []string, _ func([]byte)) (int, error) {
		calls.Add(1)
		lastArgs.Store(strings.Join(args, " "))
		return 0, nil
	}
	s, _ := newEvidenceServer(t, fake)

	policyView, ck := guardJobsInfo(t, s, "/api/guard/jobs/policy")
	if policyView.ConfirmToken == "" || len(policyView.Kinds) != 3 {
		t.Fatalf("policy jobs view = %+v", policyView)
	}
	mcpView, _ := guardJobsInfo(t, s, "/api/guard/jobs/mcp")
	if len(mcpView.Kinds) != 2 {
		t.Fatalf("mcp jobs view = %+v", mcpView)
	}
	tok := policyView.ConfirmToken

	cases := []struct {
		name     string
		path     string
		body     string
		token    bool
		want     int
		wantArgs string
	}{
		{"a read kind needs no token", "/api/guard/jobs/policy", `{"kind":"compile-diff"}`, false, 200, "guard compile --diff"},
		{"a write kind without the token", "/api/guard/jobs/policy", `{"kind":"rescan","period":"168h"}`, false, 403, ""},
		{"rescan with the token", "/api/guard/jobs/policy", `{"kind":"rescan","period":"168h"}`, true, 200, "guard rescan --since 168h"},
		{"compile with the token", "/api/guard/jobs/policy", `{"kind":"compile"}`, true, 200, "guard compile"},
		{"a period outside the vocabulary", "/api/guard/jobs/policy", `{"kind":"rescan","period":"; rm -rf /"}`, true, 400, ""},
		{"an unknown kind", "/api/guard/jobs/policy", `{"kind":"bogus"}`, true, 400, ""},
		{"an mcp kind on the policy route", "/api/guard/jobs/policy", `{"kind":"mcp-scan"}`, true, 400, ""},
		{"mcp report needs no token", "/api/guard/jobs/mcp", `{"kind":"mcp-report"}`, false, 200, "guard mcp report"},
		{"mcp scan without the token", "/api/guard/jobs/mcp", `{"kind":"mcp-scan"}`, false, 403, ""},
		{"mcp scan with the token", "/api/guard/jobs/mcp", `{"kind":"mcp-scan"}`, true, 200, "guard mcp scan"},
	}
	for _, tc := range cases {
		before := calls.Load()
		token, cookie := "", (*http.Cookie)(nil)
		if tc.token {
			token, cookie = tok, ck
		}
		rr := guardToolPost(s, tc.path, tc.body, token, cookie)
		if rr.Code != tc.want {
			t.Errorf("%s: status = %d %s, want %d", tc.name, rr.Code, rr.Body.String(), tc.want)
			continue
		}
		if tc.want != 200 {
			continue
		}
		var resp struct {
			JobID string `json:"job_id"`
		}
		_ = json.Unmarshal(rr.Body.Bytes(), &resp)
		job := waitEvidenceJob(t, s, resp.JobID)
		if job.Status != "done" || calls.Load() != before+1 {
			t.Errorf("%s: job = %+v", tc.name, job)
		}
		if got, _ := lastArgs.Load().(string); !strings.HasPrefix(got, tc.wantArgs+" --config ") {
			t.Errorf("%s: argv = %q, want %q --config ...", tc.name, got, tc.wantArgs)
		}
	}
}

// TestGuardToolRoutesClassified: the dry run is a View route and read-shaped
// (a read-only Security page can still test a command); the job routes are
// owner-local.
func TestGuardToolRoutesClassified(t *testing.T) {
	s, _ := newEvidenceServer(t, nil)
	_, capMap, _ := s.registerRoutes(nil)
	want := map[string]Capability{
		"/api/guard/test":        CapabilityView,
		"/api/guard/jobs/policy": CapabilityLocal,
		"/api/guard/jobs/mcp":    CapabilityLocal,
	}
	for p, c := range want {
		if got, ok := capMap[p]; !ok || got != c {
			t.Errorf("%s capability = %v (registered %v), want %v", p, got, ok, c)
		}
	}
	if _, ok := readShapedPosts["/api/guard/test"]; !ok {
		t.Error("/api/guard/test is not listed as a read-shaped POST")
	}
}
