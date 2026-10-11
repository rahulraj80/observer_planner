package proxyroute

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/routeprov"
)

const provPort = 18644

func provReg(t *testing.T, home string, w routeprov.Writer) *Registrar {
	t.Helper()
	r, err := NewRegistrar(RegisterOptions{ProxyPort: provPort, HomeDir: home, Writer: w})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func provWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func provClaudeEnv(t *testing.T, home string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(home, ".claude", "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var s struct {
		Env map[string]any `json:"env"`
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatal(err)
	}
	return s.Env
}

func onlyRoute(t *testing.T, routes []LeaveRoute, tool string) LeaveRoute {
	t.Helper()
	for _, r := range routes {
		if r.Tool == tool {
			return r
		}
	}
	t.Fatalf("no %s route in %+v", tool, routes)
	return LeaveRoute{}
}

// TestLeaveRoutesClaudeProvenance: one case per provenance situation for
// Claude Code's ANTHROPIC_BASE_URL.
func TestLeaveRoutesClaudeProvenance(t *testing.T) {
	own := claudeBaseURL(provPort)
	cases := []struct {
		name string
		// setup prepares the home and returns nothing; the route ends set.
		setup      func(t *testing.T, home string)
		wantAction routeprov.Action
		wantReason routeprov.Reason
		wantKept   bool
	}{
		{
			name: "enrolment wrote it: removed",
			setup: func(t *testing.T, home string) {
				provWrite(t, filepath.Join(home, ".claude", "settings.json"), `{"env":{"KEEP_ME":"1"},"hooks":{}}`)
				if res := provReg(t, home, routeprov.WriterEnrolment).RegisterClaudeCode(); !res.Added || res.ProvenanceError != nil {
					t.Fatalf("register = %+v", res)
				}
			},
			wantAction: routeprov.ActionRemove, wantReason: routeprov.ReasonWrittenByEnrolment,
		},
		{
			name: "you set it before enrolling: kept",
			setup: func(t *testing.T, home string) {
				provWrite(t, filepath.Join(home, ".claude", "settings.json"), `{"env":{"ANTHROPIC_BASE_URL":"`+own+`"}}`)
				if res := provReg(t, home, routeprov.WriterEnrolment).RegisterClaudeCode(); !res.AlreadySet {
					t.Fatalf("register = %+v", res)
				}
			},
			wantAction: routeprov.ActionKeep, wantReason: routeprov.ReasonNoRecord, wantKept: true,
		},
		{
			name: "observer init wrote it, enrolment found it: kept",
			setup: func(t *testing.T, home string) {
				provReg(t, home, routeprov.WriterInit).RegisterClaudeCode()
				provReg(t, home, routeprov.WriterEnrolment).RegisterClaudeCode()
			},
			wantAction: routeprov.ActionKeep, wantReason: routeprov.ReasonOtherWriter, wantKept: true,
		},
		{
			name: "enrolment wrote it, then Setup confirmed it: kept",
			setup: func(t *testing.T, home string) {
				provReg(t, home, routeprov.WriterEnrolment).RegisterClaudeCode()
				provReg(t, home, routeprov.WriterSetup).RegisterClaudeCode()
			},
			wantAction: routeprov.ActionKeep, wantReason: routeprov.ReasonOtherWriter, wantKept: true,
		},
		{
			name: "enrolment wrote it, then you changed it: kept",
			setup: func(t *testing.T, home string) {
				provReg(t, home, routeprov.WriterEnrolment).RegisterClaudeCode()
				provWrite(t, filepath.Join(home, ".claude", "settings.json"), `{"env":{"ANTHROPIC_BASE_URL":"http://127.0.0.1:9999"}}`)
			},
			wantAction: routeprov.ActionKeep, wantReason: routeprov.ReasonChangedAfterEnrol, wantKept: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			home := t.TempDir()
			c.setup(t, home)
			reg := provReg(t, home, "")
			before := provClaudeEnv(t, home)["ANTHROPIC_BASE_URL"]

			plan, err := reg.LeaveRoutes(false)
			if err != nil {
				t.Fatal(err)
			}
			p := onlyRoute(t, plan, "claude-code")
			if p.Action != c.wantAction || p.Reason != c.wantReason || p.Message == "" || p.Removed {
				t.Fatalf("plan = %+v", p)
			}
			if got := provClaudeEnv(t, home)["ANTHROPIC_BASE_URL"]; got != before {
				t.Fatalf("planning changed the route: %v -> %v", before, got)
			}

			done, err := reg.LeaveRoutes(true)
			if err != nil {
				t.Fatal(err)
			}
			d := onlyRoute(t, done, "claude-code")
			env := provClaudeEnv(t, home)
			if c.wantKept {
				if d.Removed || env["ANTHROPIC_BASE_URL"] != before {
					t.Fatalf("route must be kept: result %+v, env %v", d, env)
				}
				return
			}
			if !d.Removed || d.Error != "" {
				t.Fatalf("route must be removed: %+v", d)
			}
			if _, ok := env["ANTHROPIC_BASE_URL"]; ok {
				t.Fatalf("ANTHROPIC_BASE_URL still set: %v", env)
			}
			if _, ok := env[ClaudeGatewayHintEnv]; ok {
				t.Fatalf("the writer's hint switch must go with the route: %v", env)
			}
			if env["KEEP_ME"] != "1" {
				t.Fatalf("other env keys must survive: %v", env)
			}
			// The claim is gone, so a second Leave has nothing to do.
			again, _ := reg.LeaveRoutes(true)
			for _, r := range again {
				if r.Tool == "claude-code" {
					t.Fatalf("second Leave still lists claude-code: %+v", r)
				}
			}
		})
	}
}

// TestLeaveRoutesCodexRestoresPriorProvider: removing enrolment's Codex
// route drops the provider block and puts model_provider back.
func TestLeaveRoutesCodexRestoresPriorProvider(t *testing.T) {
	cases := []struct {
		name     string
		seed     string
		force    bool
		wantMP   any // nil = key absent
		keepPeer bool
	}{
		{"no provider before", "", false, nil, false},
		{"another provider before (forced switch)", "model_provider = \"azure\"\n[model_providers.azure]\nbase_url = \"https://azure.example\"\n", true, "azure", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			home := t.TempDir()
			cfg := filepath.Join(home, ".codex", "config.toml")
			provWrite(t, cfg, c.seed)
			r, err := NewRegistrar(RegisterOptions{ProxyPort: provPort, HomeDir: home, Writer: routeprov.WriterEnrolment, Force: c.force})
			if err != nil {
				t.Fatal(err)
			}
			if res := r.RegisterCodex(); !res.Added {
				t.Fatalf("register = %+v", res)
			}
			done, err := provReg(t, home, "").LeaveRoutes(true)
			if err != nil {
				t.Fatal(err)
			}
			if d := onlyRoute(t, done, "codex"); !d.Removed {
				t.Fatalf("codex route not removed: %+v", d)
			}
			root, err := readCodexRoot(cfg)
			if err != nil {
				t.Fatal(err)
			}
			if got := root["model_provider"]; got != c.wantMP {
				t.Fatalf("model_provider = %v, want %v", got, c.wantMP)
			}
			providers, _ := root["model_providers"].(map[string]any)
			if _, ok := providers[ProviderName]; ok {
				t.Fatal("the Observer provider block survived")
			}
			if _, ok := providers["azure"]; ok != c.keepPeer {
				t.Fatalf("other providers must be untouched: %v", providers)
			}
		})
	}
}

// TestRouteProvenanceRecordsOnlyRealWrites: a dry run or a refused write
// records nothing, and an unrelated (non-observer) route is not listed.
func TestRouteProvenanceRecordsOnlyRealWrites(t *testing.T) {
	home := t.TempDir()
	dry, err := NewRegistrar(RegisterOptions{ProxyPort: provPort, HomeDir: home, Writer: routeprov.WriterEnrolment, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	dry.RegisterClaudeCode()
	if _, err := os.Stat(dry.ProvenancePath()); !os.IsNotExist(err) {
		t.Fatalf("a dry run wrote provenance (%v)", err)
	}

	provWrite(t, filepath.Join(home, ".claude", "settings.json"), `{"env":{"ANTHROPIC_BASE_URL":"https://gateway.corp.example"}}`)
	if res := provReg(t, home, routeprov.WriterEnrolment).RegisterClaudeCode(); res.Error == nil {
		t.Fatalf("a foreign route must be refused without force: %+v", res)
	}
	plan, err := provReg(t, home, "").LeaveRoutes(false)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range plan {
		if r.Tool == "claude-code" {
			t.Fatalf("a non-observer route someone else set must not be listed: %+v", r)
		}
	}
	if raw, _ := os.ReadFile(provReg(t, home, "").ProvenancePath()); strings.Contains(string(raw), "claude-code") {
		t.Fatalf("a refused write was recorded:\n%s", raw)
	}
}
