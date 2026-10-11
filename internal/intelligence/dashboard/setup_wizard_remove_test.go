package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// wizardRemovePost sends a removal step POST the way the SPA does: JSON plus
// the owner-local confirm token.
func wizardRemovePost(s *Server, path, body string) *httptest.ResponseRecorder {
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, armLocalWrite(httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))))
	return rr
}

type wizardRemoveReply struct {
	Undo       bool     `json:"undo"`
	Part       string   `json:"part"`
	ConfigPath string   `json:"config_path"`
	Removed    []string `json:"removed"`
	Kept       []string `json:"kept"`
	DryRun     bool     `json:"dry_run"`
	Error      string   `json:"error"`
}

func decodeRemove(t *testing.T, rr *httptest.ResponseRecorder) wizardRemoveReply {
	t.Helper()
	var out wizardRemoveReply
	if err := json.NewDecoder(rr.Body).Decode(&out); err != nil {
		t.Fatalf("decode %d: %v", rr.Code, err)
	}
	return out
}

// TestSetupWizardRemovesHooks pins T-421 item 1: the wizard's hooks step can
// be undone from the dashboard. The dry run names every event that would go
// and writes nothing; the apply removes Observer's entries only (the user's
// own settings stay) and the step is then a no-op.
func TestSetupWizardRemovesHooks(t *testing.T) {
	server, home := wizardTestServer(t)
	settings := filepath.Join(home, ".claude", "settings.json")
	if err := os.WriteFile(settings, []byte(`{"permissions":{"allow":["Bash(ls)"]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if rr := wizardPost(t, server, "/api/setup/hooks", `{"tool":"claude-code"}`); rr.Code != 200 {
		t.Fatalf("register: %d %s", rr.Code, rr.Body.String())
	}
	registered, _ := os.ReadFile(settings)

	rr := wizardRemovePost(server, "/api/setup/hooks", `{"tool":"claude-code","undo":true,"dry_run":true}`)
	if rr.Code != 200 {
		t.Fatalf("dry run: %d %s", rr.Code, rr.Body.String())
	}
	dry := decodeRemove(t, rr)
	if !dry.Undo || !dry.DryRun || dry.Part != "hooks" || len(dry.Removed) == 0 || dry.ConfigPath != settings {
		t.Fatalf("dry run preview = %+v", dry)
	}
	if b, _ := os.ReadFile(settings); string(b) != string(registered) {
		t.Fatal("the removal preview wrote settings.json")
	}

	rr = wizardRemovePost(server, "/api/setup/hooks", `{"tool":"claude-code","undo":true}`)
	if rr.Code != 200 {
		t.Fatalf("apply: %d %s", rr.Code, rr.Body.String())
	}
	if got := decodeRemove(t, rr); len(got.Removed) != len(dry.Removed) {
		t.Errorf("apply removed %v, preview promised %v", got.Removed, dry.Removed)
	}
	after, _ := os.ReadFile(settings)
	if strings.Contains(string(after), "hook claude-code") || !strings.Contains(string(after), "Bash(ls)") {
		t.Errorf("settings.json after removal:\n%s", after)
	}
	if got := decodeRemove(t, wizardRemovePost(server, "/api/setup/hooks", `{"tool":"claude-code","undo":true,"dry_run":true}`)); len(got.Removed) != 0 {
		t.Errorf("a second removal still finds %v", got.Removed)
	}
}

// TestSetupWizardRemovesMCP: the MCP step's undo removes only the observer
// server; another server in the same (credential-bearing) file stays.
func TestSetupWizardRemovesMCP(t *testing.T) {
	server, home := wizardTestServer(t)
	cfg := filepath.Join(home, ".claude.json")
	if err := os.WriteFile(cfg, []byte(`{"oauthAccount":{"emailAddress":"dev@example.test"},"mcpServers":{"mine":{"command":"/usr/bin/mine","args":[]}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if rr := wizardPost(t, server, "/api/setup/mcp", `{"tool":"claude-code","force":true}`); rr.Code != 200 {
		t.Fatalf("register: %d %s", rr.Code, rr.Body.String())
	}
	rr := wizardRemovePost(server, "/api/setup/mcp", `{"tool":"claude-code","undo":true}`)
	if rr.Code != 200 {
		t.Fatalf("remove: %d %s", rr.Code, rr.Body.String())
	}
	if got := decodeRemove(t, rr); len(got.Removed) != 1 || got.Removed[0] != "observer" {
		t.Fatalf("removed = %v", got.Removed)
	}
	body, _ := os.ReadFile(cfg)
	if strings.Contains(string(body), `"observer"`) || !strings.Contains(string(body), `"mine"`) || !strings.Contains(string(body), "dev@example.test") {
		t.Errorf(".claude.json after removal:\n%s", body)
	}
}

// TestSetupWizardRemovalRefusesDriftWithoutForce: a file modified since
// Observer wrote it is refused with 409 and the reason (the `observer
// uninstall` checksum rule), and force removes Observer's entries anyway.
func TestSetupWizardRemovalRefusesDriftWithoutForce(t *testing.T) {
	server, home := wizardTestServer(t)
	if rr := wizardPost(t, server, "/api/setup/hooks", `{"tool":"cursor"}`); rr.Code != 200 {
		t.Fatalf("register: %d %s", rr.Code, rr.Body.String())
	}
	hooks := filepath.Join(home, ".cursor", "hooks.json")
	body, err := os.ReadFile(hooks)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hooks, append(body, '\n', ' '), 0o600); err != nil {
		t.Fatal(err)
	}
	rr := wizardRemovePost(server, "/api/setup/hooks", `{"tool":"cursor","undo":true}`)
	if rr.Code != http.StatusConflict || decodeRemove(t, rr).Error == "" {
		t.Fatalf("drifted file: %d %s, want 409 with a reason", rr.Code, rr.Body.String())
	}
	rr = wizardRemovePost(server, "/api/setup/hooks", `{"tool":"cursor","undo":true,"force":true}`)
	if rr.Code != 200 || len(decodeRemove(t, rr).Removed) == 0 {
		t.Fatalf("forced removal: %d %s", rr.Code, rr.Body.String())
	}
}

// TestSetupWizardRemovalGuards: a removal without the confirm token is
// refused before it acts, and a tool with no removable surface is a 400.
func TestSetupWizardRemovalGuards(t *testing.T) {
	server, home := wizardTestServer(t)
	if rr := wizardPost(t, server, "/api/setup/hooks", `{"tool":"claude-code"}`); rr.Code != 200 {
		t.Fatalf("register: %d", rr.Code)
	}
	settings := filepath.Join(home, ".claude", "settings.json")
	before, _ := os.ReadFile(settings)
	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/setup/hooks", strings.NewReader(`{"tool":"claude-code","undo":true}`))
	r.Header.Set("Content-Type", "application/json")
	server.Handler().ServeHTTP(rr, r)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("no confirm token: %d, want 403", rr.Code)
	}
	if after, _ := os.ReadFile(settings); string(after) != string(before) {
		t.Fatal("a refused removal still wrote")
	}
	if rr := wizardRemovePost(server, "/api/setup/mcp", `{"tool":"no-such-tool","undo":true}`); rr.Code != http.StatusBadRequest {
		t.Errorf("unknown tool: %d, want 400", rr.Code)
	}
}

// TestSetupWizardRemovalFollowsManagedPosture: the removal rides the hooks /
// MCP routes, so an organization's read-only pin on those Settings
// subsections refuses it exactly as it refuses the registration (T-401's one
// guard), and a hidden pin refuses it as hidden.
func TestSetupWizardRemovalFollowsManagedPosture(t *testing.T) {
	s := newRemoteTestServer(t, Options{
		Governance: governedProvider(t, nil, nil, []string{"mcp"}, []string{"hooks"}),
	})
	for _, tc := range []struct{ path, want string }{
		{"/api/setup/hooks", "governance_read_only"},
		{"/api/setup/mcp", "governance_hidden"},
	} {
		r := loopbackRequest(http.MethodPost, tc.path)
		r.Body = http.NoBody
		rec := httptest.NewRecorder()
		s.guardedHandler("127.0.0.1:8081").ServeHTTP(rec, armLocalWrite(r))
		if got := governanceRefusalCode(t, rec); got != tc.want {
			t.Errorf("POST %s (removal) refusal = %q, want %q (%d)", tc.path, got, tc.want, rec.Code)
		}
	}
}
