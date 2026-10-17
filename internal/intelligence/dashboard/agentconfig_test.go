package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/govern"
	"github.com/marmutapp/superbased-observer/internal/orgcontract"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// The AI tool settings routes (agentconfig.go). Every test points HOME at a
// throwaway directory with a fake vendor file, so nothing real is read.

const agentConfigSecret = "plain-secret-value-for-dashboard-test"

func agentConfigHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	dir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{
  "model": "sonnet",
  "env": {"OPENAI_API_KEY": "` + agentConfigSecret + `"}
}
`
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return home
}

func agentConfigDo(t *testing.T, s *Server, method, target, body string, token string, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
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

func agentConfigToken(t *testing.T, s *Server) (string, *http.Cookie) {
	t.Helper()
	rr := agentConfigDo(t, s, http.MethodGet, "/api/agent-config/tools", "", "", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET tools: %d %s", rr.Code, rr.Body.String())
	}
	var got struct {
		Token string `json:"confirm_token"`
		Tools []struct {
			Tool      string `json:"tool"`
			Installed bool   `json:"installed"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	installed := false
	for _, tl := range got.Tools {
		if tl.Tool == "claude-code" && tl.Installed {
			installed = true
		}
	}
	if !installed {
		t.Fatalf("claude-code should read as installed from the fake HOME: %s", rr.Body.String())
	}
	var cookie *http.Cookie
	for _, c := range rr.Result().Cookies() {
		if c.Name == remoteConfirmCookie {
			cookie = c
		}
	}
	if got.Token == "" || cookie == nil {
		t.Fatal("GET tools must mint a confirm token and cookie")
	}
	return got.Token, cookie
}

func TestAgentConfigReadsNeverEchoSecrets(t *testing.T) {
	agentConfigHome(t)
	s, _ := newTestServer(t)
	for _, target := range []string{
		"/api/agent-config/tool?tool=claude-code",
		"/api/agent-config/file?tool=claude-code&file_id=claude-code/user-settings",
		"/api/agent-config/common",
	} {
		rr := agentConfigDo(t, s, http.MethodGet, target, "", "", nil)
		if rr.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", target, rr.Code, rr.Body.String())
		}
		if strings.Contains(rr.Body.String(), agentConfigSecret) {
			t.Fatalf("%s leaks the secret value", target)
		}
	}
	rr := agentConfigDo(t, s, http.MethodGet, "/api/agent-config/coverage", "", "", nil)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"common_keys"`) {
		t.Fatalf("coverage: %d %s", rr.Code, rr.Body.String())
	}
}

func TestAgentConfigPreviewThenSave(t *testing.T) {
	home := agentConfigHome(t)
	s, _ := newTestServer(t)
	edit := `{"tool":"claude-code","file_id":"claude-code/user-settings","edits":[{"path":["model"],"op":"set","value":"opus"}]`

	rr := agentConfigDo(t, s, http.MethodPost, "/api/agent-config/preview", edit+`}`, "", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("preview: %d %s", rr.Code, rr.Body.String())
	}
	var pv struct {
		OK          bool   `json:"ok"`
		PreviewEtag string `json:"preview_etag"`
		Diff        string `json:"diff"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &pv); err != nil {
		t.Fatal(err)
	}
	if !pv.OK || pv.PreviewEtag == "" || !strings.Contains(pv.Diff, "opus") || strings.Contains(pv.Diff, agentConfigSecret) {
		t.Fatalf("preview: %+v", pv)
	}

	save := edit + `,"preview_etag":"` + pv.PreviewEtag + `"}`
	if rr := agentConfigDo(t, s, http.MethodPost, "/api/agent-config/save", save, "", nil); rr.Code != http.StatusForbidden {
		t.Fatalf("save without the confirm token must be refused: %d %s", rr.Code, rr.Body.String())
	}
	token, cookie := agentConfigToken(t, s)
	rr = agentConfigDo(t, s, http.MethodPost, "/api/agent-config/save", save, token, cookie)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"saved":true`) {
		t.Fatalf("save: %d %s", rr.Code, rr.Body.String())
	}
	b, err := os.ReadFile(filepath.Join(home, ".claude", "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"opus"`) || !strings.Contains(string(b), agentConfigSecret) {
		t.Fatalf("written file: %s", b)
	}

	// A second save with the now-stale preview etag is refused, with the
	// fresh preview in the body.
	rr = agentConfigDo(t, s, http.MethodPost, "/api/agent-config/save",
		`{"tool":"claude-code","file_id":"claude-code/user-settings","edits":[{"path":["model"],"op":"set","value":"haiku"}],"preview_etag":"`+pv.PreviewEtag+`"}`,
		token, cookie)
	if rr.Code != http.StatusConflict || !strings.Contains(rr.Body.String(), `"stale"`) {
		t.Fatalf("stale save: %d %s", rr.Code, rr.Body.String())
	}

	rr = agentConfigDo(t, s, http.MethodGet, "/api/agent-config/history", "", "", nil)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"model"`) || strings.Contains(rr.Body.String(), "opus") {
		t.Fatalf("history must list the key name and never the value: %d %s", rr.Code, rr.Body.String())
	}
}

func TestAgentConfigUnknownProjectIs404(t *testing.T) {
	agentConfigHome(t)
	s, _ := newTestServer(t)
	rr := agentConfigDo(t, s, http.MethodGet, "/api/agent-config/tool?tool=claude-code&project=/not/a/project", "", "", nil)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("unknown project: %d %s", rr.Code, rr.Body.String())
	}
	rr = agentConfigDo(t, s, http.MethodGet, "/api/agent-config/tool?tool=no-such-tool", "", "", nil)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("unknown tool: %d %s", rr.Code, rr.Body.String())
	}
	// T-429: a project-level common write is aimed only at a project
	// Observer knows (never an arbitrary directory).
	rr = agentConfigDo(t, s, http.MethodPost, "/api/agent-config/common/preview", `{"key":"model","value":"anthropic/claude-opus-4-1","project":"/not/a/project"}`, "", nil)
	if rr.Code != http.StatusNotFound || !strings.Contains(rr.Body.String(), "unknown_project") {
		t.Fatalf("common preview on an unknown project: %d %s", rr.Code, rr.Body.String())
	}
}

func TestAgentConfigReadOnlyPage(t *testing.T) {
	agentConfigHome(t)
	s := newRemoteTestServer(t, Options{Governance: governedProvider(t, nil, []string{"agent-config"}, nil, nil)})
	edit := `{"tool":"claude-code","file_id":"claude-code/user-settings","edits":[{"path":["model"],"op":"set","value":"opus"}]}`
	// The preview is read-shaped: still allowed on a read-only page.
	if rr := agentConfigDo(t, s, http.MethodPost, "/api/agent-config/preview", edit, "", nil); rr.Code != http.StatusOK {
		t.Fatalf("preview on a read-only page: %d %s", rr.Code, rr.Body.String())
	}
	// The tools read reports the posture.
	rr := agentConfigDo(t, s, http.MethodGet, "/api/agent-config/tools", "", "", nil)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"read_only":true`) {
		t.Fatalf("tools on a read-only page: %d %s", rr.Code, rr.Body.String())
	}
	token, cookie := agentConfigToken(t, s)
	if rr := agentConfigDo(t, s, http.MethodPost, "/api/agent-config/save", edit, token, cookie); rr.Code != http.StatusConflict {
		t.Fatalf("save on a read-only page must be refused: %d %s", rr.Code, rr.Body.String())
	}
	if rr := agentConfigDo(t, s, http.MethodPost, "/api/agent-config/common/save", `{"key":"model","value":"opus"}`, token, cookie); rr.Code != http.StatusConflict {
		t.Fatalf("common save on a read-only page must be refused: %d %s", rr.Code, rr.Body.String())
	}
}

// TestAgentConfigOrgPolicy walks the Organization tab on a managed node: no
// policy yet is read-only; a cached policy locks its enforced setting (the
// form and the raw editor both refuse it) and opens the rest per its access;
// apply-now writes the org value and the state lists it. An individual node
// has nothing to apply.
func TestAgentConfigOrgPolicy(t *testing.T) {
	home := agentConfigHome(t)
	managed := func(context.Context) govern.Effective { return govern.Effective{Managed: true} }
	s := newRemoteTestServer(t, Options{Governance: managed})

	rr := agentConfigDo(t, s, http.MethodGet, "/api/agent-config/tools", "", "", nil)
	if !strings.Contains(rr.Body.String(), `"read_only":true`) {
		t.Fatalf("managed with no policy must be read-only: %s", rr.Body.String())
	}
	readOnlyPage := func() bool {
		var g struct {
			Visibility struct {
				ReadOnly []string `json:"read_only_sections"`
			} `json:"visibility"`
		}
		rr := agentConfigDo(t, s, http.MethodGet, "/api/governance", "", "", nil)
		if err := json.Unmarshal(rr.Body.Bytes(), &g); err != nil {
			t.Fatalf("governance: %v %s", err, rr.Body.String())
		}
		for _, id := range g.Visibility.ReadOnly {
			if id == "agent-config" {
				return true
			}
		}
		return false
	}
	if !readOnlyPage() {
		t.Fatal("the managed default must pin the page read-only until a policy arrives")
	}
	body, err := orgcontract.EncodeAgentConfigPolicyBody(orgcontract.AgentConfigPolicyBody{
		Schema: 1, DeveloperAccess: "editable", OnDrift: "reassert", Policies: []string{"Baseline"},
		Directives: []orgcontract.AgentConfigDirective{{Setting: "claude-code/model", Value: json.RawMessage(`"opus"`), Mode: "enforced", Layer: "o1_org", Policy: "Baseline"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.New(s.opts.DB).UpsertOrgAgentConfigPolicy(context.Background(), store.OrgAgentConfigPolicyRow{
		OrgID: "org-1", Subject: "u-1", Version: 4, Body: body, ReceivedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	// The cache is enrolment-bound (review M3-01): a policy signed for
	// another org or member is no policy at all.
	if err := store.New(s.opts.DB).WriteEnrolment(context.Background(), store.Enrolment{
		OrgID: "org-2", OrgServerURL: "https://org.example", UserID: "u-1", Tenancy: "managed",
	}); err != nil {
		t.Fatal(err)
	}
	rr = agentConfigDo(t, s, http.MethodGet, "/api/agent-config/tools", "", "", nil)
	if !strings.Contains(rr.Body.String(), `"read_only":true`) || strings.Contains(rr.Body.String(), `"claude-code/model":"Baseline"`) {
		t.Fatalf("another enrolment's policy must neither lock nor open anything: %s", rr.Body.String())
	}
	if err := store.New(s.opts.DB).WriteEnrolment(context.Background(), store.Enrolment{
		OrgID: "org-1", OrgServerURL: "https://org.example", UserID: "u-1", Tenancy: "managed",
	}); err != nil {
		t.Fatal(err)
	}
	rr = agentConfigDo(t, s, http.MethodGet, "/api/agent-config/tools", "", "", nil)
	if strings.Contains(rr.Body.String(), `"read_only":true`) || !strings.Contains(rr.Body.String(), `"claude-code/model":"Baseline"`) {
		t.Fatalf("a policy must lock its setting and open the rest: %s", rr.Body.String())
	}
	if readOnlyPage() {
		t.Fatal("a held policy must lift the managed default's read-only bar")
	}
	token, cookie := agentConfigToken(t, s)
	form := `{"tool":"claude-code","file_id":"claude-code/user-settings","edits":[{"path":["model"],"op":"set","value":"haiku"}]}`
	if rr := agentConfigDo(t, s, http.MethodPost, "/api/agent-config/preview", form, "", nil); !strings.Contains(rr.Body.String(), "org_locked") {
		t.Fatalf("the form must refuse a locked setting: %d %s", rr.Code, rr.Body.String())
	}
	raw := `{"tool":"claude-code","file_id":"claude-code/user-settings","raw":"{\"model\": \"haiku\"}\n"}`
	if rr := agentConfigDo(t, s, http.MethodPost, "/api/agent-config/preview", raw, "", nil); !strings.Contains(rr.Body.String(), "org_locked") {
		t.Fatalf("the raw editor must refuse a locked setting: %d %s", rr.Code, rr.Body.String())
	}

	rr = agentConfigDo(t, s, http.MethodPost, "/api/agent-config/org/apply", "{}", token, cookie)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"writes":1`) {
		t.Fatalf("apply: %d %s", rr.Code, rr.Body.String())
	}
	b, err := os.ReadFile(filepath.Join(home, ".claude", "settings.json"))
	if err != nil || !strings.Contains(string(b), `"opus"`) || !strings.Contains(string(b), agentConfigSecret) {
		t.Fatalf("written file: %s %v", b, err)
	}
	rr = agentConfigDo(t, s, http.MethodGet, "/api/agent-config/org", "", "", nil)
	var view struct {
		Managed    bool `json:"managed"`
		HasPolicy  bool `json:"has_policy"`
		Version    int64
		Directives []struct{ Setting string } `json:"directives"`
		Rows       []struct {
			Setting string `json:"setting"`
			State   string `json:"state"`
		} `json:"rows"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if !view.Managed || !view.HasPolicy || len(view.Directives) != 1 || len(view.Rows) != 1 || view.Rows[0].State != "applied" {
		t.Fatalf("org view: %s", rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), agentConfigSecret) {
		t.Fatal("the org view echoed a secret")
	}

	// An individual node: nothing to apply, and a stray cached body locks nothing.
	ind := newRemoteTestServer(t, Options{})
	itoken, icookie := agentConfigToken(t, ind)
	if rr := agentConfigDo(t, ind, http.MethodPost, "/api/agent-config/org/apply", "{}", itoken, icookie); rr.Code != http.StatusConflict {
		t.Fatalf("individual apply: %d %s", rr.Code, rr.Body.String())
	}
}
