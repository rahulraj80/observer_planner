package dashboard

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/config"
)

// sshProfilesReq performs a confirmed request against the SSH profile editor
// routes, minting the confirm token from the editor's own GET.
func sshProfilesReq(t *testing.T, h http.Handler, method, path, body string, confirm bool) *httptest.ResponseRecorder {
	t.Helper()
	greq := httptest.NewRequest(http.MethodGet, "/api/terminal/ssh/profiles", nil)
	greq.Host = "127.0.0.1:8080"
	grec := httptest.NewRecorder()
	h.ServeHTTP(grec, greq)
	if grec.Code != http.StatusOK {
		t.Fatalf("GET /api/terminal/ssh/profiles = %d: %s", grec.Code, grec.Body)
	}
	var g struct {
		ConfirmToken string `json:"confirm_token"`
	}
	_ = json.Unmarshal(grec.Body.Bytes(), &g)
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Host = "127.0.0.1:8080"
	req.Header.Set("Content-Type", "application/json")
	if confirm {
		req.Header.Set(remoteConfirmHeader, g.ConfirmToken)
		for _, c := range grec.Result().Cookies() {
			if c.Name == remoteConfirmCookie {
				req.AddCookie(c)
			}
		}
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// TestSSHProfileEditorRoundTrip pins T-488's add / edit / remove: one PUT
// replaces the list, the file holds exactly what was saved, and the GET reads
// it back with key_path as a PATH. The key file exists for validation but is
// never read: its bytes never appear in any response.
func TestSSHProfileEditorRoundTrip(t *testing.T) {
	s, h := newManageServer(t)
	keyDir := t.TempDir()
	key := filepath.Join(keyDir, "id_test")
	const secret = "PRIVATE-KEY-BYTES-MUST-NEVER-LEAVE"
	if err := os.WriteFile(key, []byte(secret), 0o600); err != nil {
		t.Fatal(err)
	}

	add := `{"enabled":true,"profiles":[` +
		`{"name":"devbox","label":"Dev box","host":"devbox.example.test","user":"dev","key_path":"` + filepath.ToSlash(key) + `"},` +
		`{"name":"build","host":"10.0.0.7","port":2222}]}`
	rec := sshProfilesReq(t, h, http.MethodPut, "/api/terminal/ssh/profiles", add, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT add = %d: %s", rec.Code, rec.Body)
	}
	if strings.Contains(rec.Body.String(), secret) {
		t.Fatal("the PUT response carries key file contents")
	}
	cfg, err := loadConfigForDashboard(s.opts.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Terminal.SSH.Enabled || len(cfg.Terminal.SSH.Profiles) != 2 || cfg.Terminal.SSH.Profiles[0].KeyPath != filepath.ToSlash(key) {
		t.Fatalf("persisted [terminal.ssh] = %+v", cfg.Terminal.SSH)
	}

	// Edit devbox's host, remove build.
	edit := `{"profiles":[{"name":"devbox","host":"devbox2.example.test","user":"dev"}]}`
	if rec := sshProfilesReq(t, h, http.MethodPut, "/api/terminal/ssh/profiles", edit, true); rec.Code != http.StatusOK {
		t.Fatalf("PUT edit = %d: %s", rec.Code, rec.Body)
	}
	g := httptest.NewRequest(http.MethodGet, "/api/terminal/ssh/profiles", nil)
	g.Host = "127.0.0.1:8080"
	grec := httptest.NewRecorder()
	h.ServeHTTP(grec, g)
	var got struct {
		Enabled  bool                        `json:"enabled"`
		Profiles []terminalSSHProfilePayload `json:"profiles"`
	}
	if err := json.Unmarshal(grec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.Enabled {
		t.Fatal("a PUT that omitted enabled switched the surface off")
	}
	if len(got.Profiles) != 1 || got.Profiles[0].Host != "devbox2.example.test" {
		t.Fatalf("GET after the edit = %+v", got.Profiles)
	}
	if strings.Contains(grec.Body.String(), secret) {
		t.Fatal("the GET carries key file contents")
	}
}

// TestSSHProfileEditorRejectsBadInput pins the save sanity check: every
// profile is validated with the config loader's own rules, the WHOLE write is
// refused on the first bad entry, and nothing reaches the file.
func TestSSHProfileEditorRejectsBadInput(t *testing.T) {
	s, h := newManageServer(t)
	before, err := os.ReadFile(s.opts.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, body, want string }{
		{"bad name", `{"profiles":[{"name":"Has Space","host":"h.example.test"}]}`, "Has Space"},
		{"option-injection host", `{"profiles":[{"name":"x","host":"-oProxyCommand=touch /tmp/p"}]}`, "profile x"},
		{"duplicate names", `{"profiles":[{"name":"a","host":"h1.example.test"},{"name":"a","host":"h2.example.test"}]}`, "duplicate name"},
		{"relative key path", `{"profiles":[{"name":"k","host":"h.example.test","key_path":"id_rsa"}]}`, "profile k"},
		{"missing key file", `{"profiles":[{"name":"k","host":"h.example.test","key_path":"/definitely/not/a/key"}]}`, "profile k"},
		{"multi-hop jump", `{"profiles":[{"name":"j","host":"h.example.test","jump":"a.example.test,b.example.test"}]}`, "profile j"},
		{"bad port", `{"profiles":[{"name":"p","host":"h.example.test","port":70000}]}`, "profile p"},
		{"unknown field (proxy command)", `{"profiles":[{"name":"x","host":"h.example.test","proxy_command":"nc %h %p"}]}`, "decode"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := sshProfilesReq(t, h, http.MethodPut, "/api/terminal/ssh/profiles", tc.body, true)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("PUT = %d, want 400: %s", rec.Code, rec.Body)
			}
			if !strings.Contains(rec.Body.String(), tc.want) {
				t.Fatalf("refusal %q does not name %q", rec.Body.String(), tc.want)
			}
		})
	}
	after, _ := os.ReadFile(s.opts.ConfigPath)
	if string(after) != string(before) {
		t.Fatal("a refused save changed config.toml")
	}
}

// TestSSHProfileEditorRequiresConfirm pins the CSRF hardening on both writes.
func TestSSHProfileEditorRequiresConfirm(t *testing.T) {
	_, h := newManageServer(t)
	for _, tc := range []struct{ method, path, body string }{
		{http.MethodPut, "/api/terminal/ssh/profiles", `{"profiles":[]}`},
		{http.MethodPost, "/api/terminal/ssh/profiles/test", `{"name":"a","host":"h.example.test"}`},
	} {
		if rec := sshProfilesReq(t, h, tc.method, tc.path, tc.body, false); rec.Code != http.StatusForbidden {
			t.Fatalf("%s %s without the confirm token = %d, want 403", tc.method, tc.path, rec.Code)
		}
	}
}

// draftTesterLM is a LaunchManager that ALSO implements sshProfileTester.
type draftTesterLM struct {
	*fakeLaunchManager
	got  config.SSHProfileConfig
	err  error
	hits int
}

func (m *draftTesterLM) TestSSHProfileDraft(p config.SSHProfileConfig) (InstanceTestResult, error) {
	m.hits++
	m.got = p
	if m.err != nil {
		return InstanceTestResult{}, m.err
	}
	return InstanceTestResult{KnownHostsChecked: true, KnownHostsOK: true, AuthOK: true, LatencyMS: 12}, nil
}

// TestSSHProfileDraftTest pins the editor's test-connect: a valid draft
// reaches the seam (trimmed), an invalid draft is refused BEFORE the seam,
// a daemon without the seam answers 501, and seam errors map to statuses.
func TestSSHProfileDraftTest(t *testing.T) {
	lm := &draftTesterLM{fakeLaunchManager: &fakeLaunchManager{}}
	h := newManageServerWithLaunch(t, lm)
	rec := sshProfilesReq(t, h, http.MethodPost, "/api/terminal/ssh/profiles/test", `{"name":"new","host":" new.example.test ","user":"dev"}`, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("draft test = %d: %s", rec.Code, rec.Body)
	}
	var res InstanceTestResult
	_ = json.Unmarshal(rec.Body.Bytes(), &res)
	if !res.AuthOK || lm.got.Host != "new.example.test" {
		t.Fatalf("draft test result %+v, seam got %+v", res, lm.got)
	}
	if rec := sshProfilesReq(t, h, http.MethodPost, "/api/terminal/ssh/profiles/test", `{"name":"bad name","host":"h"}`, true); rec.Code != http.StatusBadRequest || lm.hits != 1 {
		t.Fatalf("invalid draft = %d (seam hits %d), want 400 before the seam", rec.Code, lm.hits)
	}
	lm.err = ErrInstanceHostKeyUnknown
	if rec := sshProfilesReq(t, h, http.MethodPost, "/api/terminal/ssh/profiles/test", `{"name":"new","host":"new.example.test"}`, true); rec.Code != http.StatusConflict {
		t.Fatalf("seam host-key error = %d, want 409", rec.Code)
	}
	lm.err = errors.New("boom")
	if rec := sshProfilesReq(t, h, http.MethodPost, "/api/terminal/ssh/profiles/test", `{"name":"new","host":"new.example.test"}`, true); rec.Code != http.StatusInternalServerError {
		t.Fatalf("seam generic error = %d, want 500", rec.Code)
	}

	bare := newManageServerWithLaunch(t, &fakeLaunchManager{})
	if rec := sshProfilesReq(t, bare, http.MethodPost, "/api/terminal/ssh/profiles/test", `{"name":"new","host":"new.example.test"}`, true); rec.Code != http.StatusNotImplemented {
		t.Fatalf("no seam = %d, want 501", rec.Code)
	}
}
