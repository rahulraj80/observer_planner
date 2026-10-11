package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// livePolicyLM is a LaunchManager (via the embedded fake) that ALSO implements
// the optional terminalPolicyLive seam, as the daemon's adapter does once its
// terminal service reads the launch policy through the live config holder.
type livePolicyLM struct {
	*fakeLaunchManager
	live bool
}

func (m *livePolicyLM) TerminalPolicyLive() bool { return m.live }

// TestTerminalPolicyRestartFlagFollowsLiveSeam pins T-489: the launch policy
// GET and PUT say "restart required" only when the daemon cannot bind the
// save live. Before T-489 both always said true, so the Terminals page and the
// New Terminal dialog told the operator to restart after every save.
func TestTerminalPolicyRestartFlagFollowsLiveSeam(t *testing.T) {
	for _, tc := range []struct {
		name string
		lm   LaunchManager
		want bool
	}{
		{"no launch manager: restart", nil, true},
		{"launch manager without the seam: restart", &fakeLaunchManager{}, true},
		{"live seam reporting false: restart", &livePolicyLM{fakeLaunchManager: &fakeLaunchManager{}, live: false}, true},
		{"live seam: no restart", &livePolicyLM{fakeLaunchManager: &fakeLaunchManager{}, live: true}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newManageServerWithLaunch(t, tc.lm)
			req := httptest.NewRequest(http.MethodGet, "/api/terminal/policy", nil)
			req.Host = "127.0.0.1:8080"
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("GET = %d: %s", rec.Code, rec.Body)
			}
			var got struct {
				RestartRequiredOnSave bool `json:"restart_required_on_save"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if got.RestartRequiredOnSave != tc.want {
				t.Fatalf("GET restart_required_on_save = %v, want %v", got.RestartRequiredOnSave, tc.want)
			}
			code, out := putPolicy(t, h, `{"allow_fresh_agent":false,"allowed_tools":[],"allowed_project_roots":[]}`)
			if code != http.StatusOK {
				t.Fatalf("PUT = %d: %v", code, out)
			}
			if out["restart_required"] != tc.want {
				t.Fatalf("PUT restart_required = %v, want %v", out["restart_required"], tc.want)
			}
		})
	}
}

// TestTerminalPolicyAllowInstall pins the guided-install switch on the launch
// policy route (T-489: the dashboard control that replaces "set
// [terminal.launch].allow_install in config.toml"). A PUT that carries the
// field writes it; a PUT that omits it (an older client) keeps the saved value.
func TestTerminalPolicyAllowInstall(t *testing.T) {
	s, h := newManageServer(t)
	base := `"allow_fresh_agent":false,"allowed_tools":[],"allowed_project_roots":[]`
	if code, out := putPolicy(t, h, `{`+base+`,"allow_install":false}`); code != http.StatusOK || out["allow_install"] != false {
		t.Fatalf("PUT allow_install=false = %d %v", code, out)
	}
	cfg, err := loadConfigForDashboard(s.opts.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Terminal.Launch.AllowInstall {
		t.Fatal("allow_install=false was not persisted")
	}
	if code, out := putPolicy(t, h, `{`+base+`}`); code != http.StatusOK || out["allow_install"] != false {
		t.Fatalf("PUT without allow_install = %d %v, want the saved false kept", code, out)
	}
	if code, out := putPolicy(t, h, `{`+base+`,"allow_install":true}`); code != http.StatusOK || out["allow_install"] != true {
		t.Fatalf("PUT allow_install=true = %d %v", code, out)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/terminal/policy", nil)
	req.Host = "127.0.0.1:8080"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var got struct {
		AllowInstall bool `json:"allow_install"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if !got.AllowInstall {
		t.Fatal("GET does not report the saved allow_install=true")
	}
}
