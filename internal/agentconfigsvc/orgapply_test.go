package agentconfigsvc

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/agentconfig"
	"github.com/marmutapp/superbased-observer/internal/agentconfigorg"
	"github.com/marmutapp/superbased-observer/internal/orgcontract"
	"github.com/marmutapp/superbased-observer/internal/store"
)

func directive(setting, value, mode string) orgcontract.AgentConfigDirective {
	return orgcontract.AgentConfigDirective{
		Setting: setting, Value: json.RawMessage(value), Mode: mode,
		Layer: orgcontract.AgentConfigLayerOrg, Policy: "Baseline",
	}
}

func stateOf(t *testing.T, rows []store.AgentConfigOrgStateRow, setting, fileID string) store.AgentConfigOrgStateRow {
	t.Helper()
	for _, r := range rows {
		if r.SettingID == setting && r.FileID == fileID {
			return r
		}
	}
	t.Fatalf("no state row for %s in %s: %+v", setting, fileID, rows)
	return store.AgentConfigOrgStateRow{}
}

// TestApplyOrgPasses walks one managed node through successive sweeps, one
// row per pass: the first apply writes through the shared pipeline (backup,
// history row on the org layer), an unchanged file is left alone, a hand edit
// is re-asserted (reassert) or only reported (report), three drifts within
// the hour back off, and the developer's restore clears the drift.
func TestApplyOrgPasses(t *testing.T) {
	h := newHarness(t, func(o *Options) { o.Origin = "org" })
	cfg := h.write(".codex/config.toml", "# my settings\nmodel = \"gpt-5\"\n")
	base := time.Date(2026, 10, 4, 10, 0, 0, 0, time.UTC)
	ctx := context.Background()
	var prior []store.AgentConfigOrgStateRow
	type pass struct {
		name       string
		hand       string // file content written before the pass ("" = leave)
		onDrift    string
		at         time.Duration
		wantState  agentconfigorg.State
		wantWrites int
		wantDrift  int
		wantModel  string // the model line expected in the file after the pass
	}
	passes := []pass{
		{name: "first apply writes the org value", wantState: agentconfigorg.StateApplied, wantWrites: 1, wantModel: `model = "o3"`},
		{name: "an unchanged file is left alone", at: time.Minute, wantState: agentconfigorg.StateApplied, wantModel: `model = "o3"`},
		{name: "a hand edit is re-asserted", hand: "model = \"gpt-4\"\n", at: 2 * time.Minute, wantState: agentconfigorg.StateReasserted, wantWrites: 1, wantDrift: 1, wantModel: `model = "o3"`},
		{name: "report-only leaves the edit and reports it", hand: "model = \"gpt-4\"\n", onDrift: "report", at: 3 * time.Minute, wantState: agentconfigorg.StateDrifted, wantDrift: 1, wantModel: `model = "gpt-4"`},
		{name: "still drifted is not a new event", onDrift: "report", at: 4 * time.Minute, wantState: agentconfigorg.StateDrifted, wantModel: `model = "gpt-4"`},
		{name: "the third drift in the hour backs off", onDrift: "reassert", at: 5 * time.Minute, wantState: agentconfigorg.StateBackedOff, wantDrift: 1, wantModel: `model = "gpt-4"`},
		{name: "backed off stays backed off", at: 6 * time.Minute, wantState: agentconfigorg.StateBackedOff, wantModel: `model = "gpt-4"`},
		{name: "the developer's restore clears the drift", hand: "model = \"o3\"\n", at: 7 * time.Minute, wantState: agentconfigorg.StateApplied, wantModel: `model = "o3"`},
	}
	for _, p := range passes {
		if p.hand != "" {
			h.write(".codex/config.toml", p.hand)
		}
		body := orgcontract.AgentConfigPolicyBody{OnDrift: p.onDrift, Directives: []orgcontract.AgentConfigDirective{directive("codex/model", `"o3"`, "enforced")}}
		res := h.svc.ApplyOrg(ctx, OrgApplyInput{Body: body, Version: 7, Prior: prior, Now: base.Add(p.at)})
		st := stateOf(t, res.Rows, "codex/model", "codex/user-config")
		if agentconfigorg.State(st.State) != p.wantState || res.Writes != p.wantWrites || res.DriftEvents != p.wantDrift {
			t.Fatalf("%s: state=%s writes=%d drift=%d (%+v)", p.name, st.State, res.Writes, res.DriftEvents, st)
		}
		if got := h.read(cfg); !strings.Contains(got, p.wantModel) {
			t.Fatalf("%s: file =\n%s", p.name, got)
		}
		if st.PolicyVersion != 7 || st.Mode != "enforced" || st.Personal {
			t.Fatalf("%s: row %+v", p.name, st)
		}
		prior = res.Rows
	}
	// The first apply kept the developer's comment and went through the
	// history on the org layer.
	if got := h.read(cfg); !strings.Contains(got, "# my settings") && !strings.Contains(got, "o3") {
		t.Fatalf("file lost content:\n%s", got)
	}
	if len(h.st.writes) != 2 || h.st.writes[0].Layer != "o1_org" || h.st.writes[0].Origin != "org" || h.st.writes[0].BackupPath == "" {
		t.Fatalf("history = %+v", h.st.writes)
	}
}

// TestApplyOrgTargets pins the per-directive outcomes, one row per case.
func TestApplyOrgTargets(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 4, 10, 0, 0, 0, time.UTC)
	cases := []struct {
		name  string
		setup func(h *harness) map[string][]string
		body  orgcontract.AgentConfigPolicyBody
		check func(t *testing.T, h *harness, res OrgApplyResult)
	}{
		{
			name: "an unknown setting is reported, never written",
			body: orgcontract.AgentConfigPolicyBody{Directives: []orgcontract.AgentConfigDirective{directive("codex/no_such_key", `1`, "enforced")}},
			check: func(t *testing.T, _ *harness, res OrgApplyResult) {
				if st := stateOf(t, res.Rows, "codex/no_such_key", OrgFileNone); st.State != string(agentconfigorg.StateUnknownSetting) || res.Writes != 0 {
					t.Fatalf("%+v", st)
				}
			},
		},
		{
			name: "a tool that is not installed is reported",
			body: orgcontract.AgentConfigPolicyBody{Directives: []orgcontract.AgentConfigDirective{directive("claude-code/model", `"opus"`, "enforced")}},
			check: func(t *testing.T, h *harness, res OrgApplyResult) {
				st := stateOf(t, res.Rows, "claude-code/model", "claude-code/user-settings")
				if st.State != string(agentconfigorg.StateNotInstalled) || res.Writes != 0 {
					t.Fatalf("%+v", st)
				}
				if _, err := os.Stat(filepath.Join(h.home, ".claude")); err == nil {
					t.Fatal("an uninstalled tool's directory was created")
				}
			},
		},
		{
			name: "a granted developer's own value beats an org default",
			setup: func(h *harness) map[string][]string {
				h.write(".codex/config.toml", "model = \"gpt-5\"\n")
				return nil
			},
			body: orgcontract.AgentConfigPolicyBody{
				Grants:     []orgcontract.AgentConfigGrant{{Setting: "codex/model"}},
				Directives: []orgcontract.AgentConfigDirective{directive("codex/model", `"o3"`, "default")},
			},
			check: func(t *testing.T, h *harness, res OrgApplyResult) {
				st := stateOf(t, res.Rows, "codex/model", "codex/user-config")
				if st.State != string(agentconfigorg.StateDeveloperValue) || res.Writes != 0 || st.ObservedValue != `"gpt-5"` {
					t.Fatalf("%+v", st)
				}
			},
		},
		{
			name: "an ungranted default is applied",
			setup: func(h *harness) map[string][]string {
				h.write(".codex/config.toml", "model = \"gpt-5\"\n")
				return nil
			},
			body: orgcontract.AgentConfigPolicyBody{Directives: []orgcontract.AgentConfigDirective{directive("codex/model", `"o3"`, "default")}},
			check: func(t *testing.T, h *harness, res OrgApplyResult) {
				if st := stateOf(t, res.Rows, "codex/model", "codex/user-config"); st.State != string(agentconfigorg.StateApplied) || res.Writes != 1 {
					t.Fatalf("%+v", st)
				}
			},
		},
		{
			name: "a file that does not parse is refused by the sanity check and left alone",
			setup: func(h *harness) map[string][]string {
				h.write(".codex/config.toml", "model = = broken\n")
				return nil
			},
			body: orgcontract.AgentConfigPolicyBody{Directives: []orgcontract.AgentConfigDirective{directive("codex/model", `"o3"`, "enforced")}},
			check: func(t *testing.T, h *harness, res OrgApplyResult) {
				st := stateOf(t, res.Rows, "codex/model", "codex/user-config")
				if st.State != string(agentconfigorg.StateRefused) || st.Reason == "" || res.Writes != 0 {
					t.Fatalf("%+v", st)
				}
				if h.read(filepath.Join(h.home, ".codex", "config.toml")) != "model = = broken\n" {
					t.Fatal("a refused file was written")
				}
			},
		},
		{
			name: "a local-only project directive lands in every clone, personal, git-excluded",
			setup: func(h *harness) map[string][]string {
				h.write(".claude/settings.json", "{}\n")
				if err := os.MkdirAll(filepath.Join(h.project, ".git", "info"), 0o755); err != nil {
					t.Fatal(err)
				}
				return map[string][]string{"abc123": {h.project}, "other": {h.project + "-missing"}}
			},
			body: orgcontract.AgentConfigPolicyBody{Directives: []orgcontract.AgentConfigDirective{
				func() orgcontract.AgentConfigDirective {
					d := directive("claude-code/model", `"opus"`, "enforced")
					d.Project, d.Placement = "abc123", orgcontract.AgentConfigPlacementLocalOnly
					return d
				}(),
				func() orgcontract.AgentConfigDirective {
					d := directive("claude-code/model", `"haiku"`, "enforced")
					d.Project = "not-cloned-here"
					return d
				}(),
			}},
			check: func(t *testing.T, h *harness, res OrgApplyResult) {
				st := stateOf(t, res.Rows, "claude-code/model", "claude-code/project-local-settings")
				if st.State != string(agentconfigorg.StateApplied) || !st.Personal || st.ProjectRoot != h.project || st.ProjectHash != "abc123" {
					t.Fatalf("%+v", st)
				}
				if got := h.read(filepath.Join(h.project, ".claude", "settings.local.json")); !strings.Contains(got, `"opus"`) {
					t.Fatalf("local file:\n%s", got)
				}
				if ex := h.read(filepath.Join(h.project, ".git", "info", "exclude")); !strings.Contains(ex, "settings.local.json") {
					t.Fatalf("exclude:\n%s", ex)
				}
				if len(res.Rows) != 1 {
					t.Fatalf("a project with no clone here produced rows: %+v", res.Rows)
				}
			},
		},
		{
			// Review M3-11 (D-103 / D-124): the tracked shared project file
			// that would shadow the enforced value is never rewritten; the
			// value lands in the personal local-only file, which wins over it.
			name: "an enforced value shadows a tracked project file through the local-only file",
			setup: func(h *harness) map[string][]string {
				h.write(".claude/settings.json", "{\"model\": \"sonnet\"}\n")
				h.write("<project>/.claude/settings.json", "{\"model\": \"haiku\"}\n")
				if err := os.MkdirAll(filepath.Join(h.project, ".git", "info"), 0o755); err != nil {
					t.Fatal(err)
				}
				return map[string][]string{"abc123": {h.project}}
			},
			body: orgcontract.AgentConfigPolicyBody{Directives: []orgcontract.AgentConfigDirective{directive("claude-code/model", `"opus"`, "enforced")}},
			check: func(t *testing.T, h *harness, res OrgApplyResult) {
				stateOf(t, res.Rows, "claude-code/model", "claude-code/user-settings")
				st := stateOf(t, res.Rows, "claude-code/model", "claude-code/project-local-settings")
				if st.State != string(agentconfigorg.StateApplied) || !st.Personal || res.Writes != 2 {
					t.Fatalf("%+v writes=%d", st, res.Writes)
				}
				if got := h.read(filepath.Join(h.project, ".claude", "settings.json")); got != "{\"model\": \"haiku\"}\n" {
					t.Fatalf("the tracked project file was rewritten:\n%s", got)
				}
				if got := h.read(filepath.Join(h.project, ".claude", "settings.local.json")); !strings.Contains(got, `"opus"`) {
					t.Fatalf("local file:\n%s", got)
				}
				if ex := h.read(filepath.Join(h.project, ".git", "info", "exclude")); !strings.Contains(ex, "settings.local.json") {
					t.Fatalf("exclude:\n%s", ex)
				}
				for _, r := range res.Rows {
					if r.FileID == "claude-code/project-settings" {
						t.Fatalf("the tracked file is a target: %+v", r)
					}
				}
			},
		},
		{
			// T-431 (D-103): with no local-only file to shadow it, the tracked
			// project file that sets the key is rewritten, so the enforced
			// value takes effect, and the row says it commits with the repo.
			name: "an enforced value rewrites a tracked project file that has no local-only shadow",
			setup: func(h *harness) map[string][]string {
				h.write(".codex/config.toml", "model = \"gpt-5\"\n")
				h.write("<project>/.codex/config.toml", "# team file\nmodel = \"gpt-4\"\n")
				return map[string][]string{"abc123": {h.project}}
			},
			body: orgcontract.AgentConfigPolicyBody{Directives: []orgcontract.AgentConfigDirective{directive("codex/model", `"o3"`, "enforced")}},
			check: func(t *testing.T, h *harness, res OrgApplyResult) {
				st := stateOf(t, res.Rows, "codex/model", "codex/project-config")
				if st.State != string(agentconfigorg.StateApplied) || st.Reason != "" || st.Personal || st.Scope != "project_shared" || res.Writes != 2 {
					t.Fatalf("%+v writes=%d", st, res.Writes)
				}
				got := h.read(filepath.Join(h.project, ".codex", "config.toml"))
				if !strings.Contains(got, `model = "o3"`) || !strings.Contains(got, "# team file") {
					t.Fatalf("tracked project file:\n%s", got)
				}
			},
		},
		{
			// T-428 (D-112): a personal (local-only) file's value is observed
			// like any other; whether it ships is the push seam's decision.
			name: "a personal file's observed value is kept",
			setup: func(h *harness) map[string][]string {
				h.write(".claude/settings.json", "{}\n")
				h.write("<project>/.claude/settings.local.json", "{\"model\": \"haiku\"}\n")
				return map[string][]string{"abc123": {h.project}}
			},
			body: orgcontract.AgentConfigPolicyBody{
				Grants: []orgcontract.AgentConfigGrant{{Setting: "claude-code/model"}},
				Directives: []orgcontract.AgentConfigDirective{func() orgcontract.AgentConfigDirective {
					d := directive("claude-code/model", `"opus"`, "default")
					d.Project, d.Placement = "abc123", orgcontract.AgentConfigPlacementLocalOnly
					return d
				}()},
			},
			check: func(t *testing.T, _ *harness, res OrgApplyResult) {
				st := stateOf(t, res.Rows, "claude-code/model", "claude-code/project-local-settings")
				if !st.Personal || st.ObservedValue != `"haiku"` || st.ObservedSecret {
					t.Fatalf("%+v", st)
				}
			},
		},
		{
			// T-428 (D-112): scrubbed, not withheld. The org sees that the
			// file holds a credential-shaped value; the raw key never lands
			// in the org state.
			name: "a credential-shaped observed value is kept scrubbed",
			setup: func(h *harness) map[string][]string {
				h.write(".codex/config.toml", "model = \"sk-ant-api03-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA-AAAAAAAA\"\n")
				return nil
			},
			body: orgcontract.AgentConfigPolicyBody{
				OnDrift:    "report",
				Grants:     []orgcontract.AgentConfigGrant{{Setting: "codex/model"}},
				Directives: []orgcontract.AgentConfigDirective{directive("codex/model", `"o3"`, "default")},
			},
			check: func(t *testing.T, _ *harness, res OrgApplyResult) {
				st := stateOf(t, res.Rows, "codex/model", "codex/user-config")
				if !st.ObservedSecret || st.ObservedValue == "" || strings.Contains(st.ObservedValue, "sk-ant-api03") {
					t.Fatalf("%+v", st)
				}
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t, nil)
			var roots map[string][]string
			if c.setup != nil {
				roots = c.setup(h)
			}
			c.check(t, h, h.svc.ApplyOrg(ctx, OrgApplyInput{Body: c.body, Version: 3, ProjectRoots: roots, Now: now}))
		})
	}
}

// TestShadowTarget is the M3-11 decision table for one project file that
// sets an enforced user value's key.
func TestShadowTarget(t *testing.T) {
	shared := agentconfig.FileRow{ID: "t/project", Scope: agentconfig.ScopeProjectShared, Precedence: 2}
	sharedIgnored := agentconfig.FileRow{ID: "t/project-ignored", Scope: agentconfig.ScopeProjectShared, Gitignored: true, Precedence: 2}
	local := agentconfig.FileRow{ID: "t/project-local", Scope: agentconfig.ScopeProjectLocal, Precedence: 3}
	weakLocal := agentconfig.FileRow{ID: "t/project-local-weak", Scope: agentconfig.ScopeProjectLocal, Precedence: 1}
	cases := []struct {
		name        string
		sets, local agentconfig.FileRow
		hasLocal    bool
		wantID      string
		wantTracked bool
	}{
		{"a personal file is rewritten in place", local, local, true, "t/project-local", false},
		{"a gitignored shared file is rewritten in place", sharedIgnored, local, true, "t/project-ignored", false},
		{"a tracked file is shadowed by the local-only file", shared, local, true, "t/project-local", false},
		{"no local-only file: the tracked file itself takes the value", shared, agentconfig.FileRow{}, false, "t/project", true},
		{"a local file that does not win is no shadow", shared, weakLocal, true, "t/project", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			row, tracked := shadowTarget(c.sets, c.local, c.hasLocal)
			if row.ID != c.wantID || tracked != c.wantTracked {
				t.Fatalf("shadowTarget = %s tracked=%v, want %s tracked=%v", row.ID, tracked, c.wantID, c.wantTracked)
			}
		})
	}
}

// TestDeveloperCannotEditALockedSetting pins lever A: under the org's Access
// an enforced setting refuses the developer's own save (form AND raw).
func TestDeveloperCannotEditALockedSetting(t *testing.T) {
	body := orgcontract.AgentConfigPolicyBody{
		DeveloperAccess: "editable",
		Directives:      []orgcontract.AgentConfigDirective{directive("claude-code/model", `"opus"`, "enforced")},
	}
	h := newHarness(t, func(o *Options) {
		o.Access = func(context.Context) agentconfig.Access { return agentconfigorg.AccessFor(body) }
	})
	p := h.write(".claude/settings.json", claudeSettings)
	_, err := h.saveForm([]agentconfig.Edit{{Path: agentconfig.KeyPath{"model"}, Op: agentconfig.OpSet, Value: "haiku"}}, true)
	if err == nil {
		t.Fatal("a locked setting saved")
	}
	if h.read(p) != claudeSettings {
		t.Fatal("a locked setting was written")
	}
}

// TestScrubOrgValue is the T-428 value rule, one row per shape: a plain value
// is unchanged, a credential anywhere in it is scrubbed in place (never the
// whole value dropped), and the structure survives.
func TestScrubOrgValue(t *testing.T) {
	h := newHarness(t, nil)
	key := "sk-ant-api03-" + strings.Repeat("A", 93) + "-AAAAAAAA"
	cases := []struct {
		name      string
		in        any
		changed   bool
		mustHave  string
		mustLacks string
	}{
		{"plain string", "opus", false, `"opus"`, ""},
		{"plain list", []any{"a", "b"}, false, `["a","b"]`, ""},
		{"credential-named key", map[string]any{"OPENAI_API_KEY": "abc123", "LOG": "debug"}, true, `"LOG":"debug"`, "abc123"},
		{"a key inside a string", "token " + key, true, "token", key},
		{"a key inside a nested list", map[string]any{"args": []any{"--key", key}}, true, `"--key"`, key},
		{"numbers and bools pass", map[string]any{"n": float64(3), "b": true}, false, `"n":3`, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, changed := h.svc.ScrubOrgValue(c.in)
			b, err := json.Marshal(out)
			if err != nil {
				t.Fatal(err)
			}
			if changed != c.changed || !strings.Contains(string(b), c.mustHave) || (c.mustLacks != "" && strings.Contains(string(b), c.mustLacks)) {
				t.Fatalf("ScrubOrgValue(%v) = %s changed=%v", c.in, b, changed)
			}
		})
	}
}

// TestEditGrantLiftsAnEnforcedLock (T-436): with an honoured AI tool
// setting edit grant the developer may save the locked setting, the apply
// pass leaves their value alone (developer_value, no write, no drift), and
// once the grant ends the next pass re-asserts the org value.
func TestEditGrantLiftsAnEnforcedLock(t *testing.T) {
	body := orgcontract.AgentConfigPolicyBody{
		DeveloperAccess: "editable",
		Directives:      []orgcontract.AgentConfigDirective{directive("codex/model", `"o3"`, "enforced")},
	}
	grants := []string{"codex/model"}
	h := newHarness(t, func(o *Options) {
		o.Origin = "org"
		o.Access = func(context.Context) agentconfig.Access {
			return agentconfigorg.WithEditGrants(agentconfigorg.AccessFor(body), grants)
		}
	})
	cfg := h.write(".codex/config.toml", "model = \"gpt-5\"\n")
	ctx := context.Background()
	base := time.Date(2026, 10, 4, 10, 0, 0, 0, time.UTC)
	res := h.svc.ApplyOrg(ctx, OrgApplyInput{Body: body, Version: 1, Now: base})
	if st := stateOf(t, res.Rows, "codex/model", "codex/user-config"); agentconfigorg.State(st.State) != agentconfigorg.StateApplied || res.Writes != 1 {
		t.Fatalf("first apply: %+v writes=%d", st, res.Writes)
	}
	h.write(".codex/config.toml", "model = \"gpt-4\"\n")
	res = h.svc.ApplyOrg(ctx, OrgApplyInput{Body: body, Version: 1, Prior: res.Rows, EditGrants: grants, Now: base.Add(time.Minute)})
	st := stateOf(t, res.Rows, "codex/model", "codex/user-config")
	if agentconfigorg.State(st.State) != agentconfigorg.StateDeveloperValue || res.Writes != 0 || res.DriftEvents != 0 {
		t.Fatalf("under the grant: %+v writes=%d drift=%d", st, res.Writes, res.DriftEvents)
	}
	if got := h.read(cfg); !strings.Contains(got, `model = "gpt-4"`) {
		t.Fatalf("the developer's value was overwritten under the grant:\n%s", got)
	}
	res = h.svc.ApplyOrg(ctx, OrgApplyInput{Body: body, Version: 1, Prior: res.Rows, Now: base.Add(2 * time.Minute)})
	st = stateOf(t, res.Rows, "codex/model", "codex/user-config")
	if agentconfigorg.State(st.State) != agentconfigorg.StateReasserted || res.Writes != 1 {
		t.Fatalf("after the grant ended: %+v writes=%d", st, res.Writes)
	}
	if got := h.read(cfg); !strings.Contains(got, `model = "o3"`) {
		t.Fatalf("the org value was not re-asserted after the grant:\n%s", got)
	}
}

// TestEditGrantOpensTheEditor (T-436): the editor saves a locked setting
// the developer holds an edit grant for.
func TestEditGrantOpensTheEditor(t *testing.T) {
	body := orgcontract.AgentConfigPolicyBody{
		DeveloperAccess: "editable",
		Directives:      []orgcontract.AgentConfigDirective{directive("claude-code/model", `"opus"`, "enforced")},
	}
	h := newHarness(t, func(o *Options) {
		o.Access = func(context.Context) agentconfig.Access {
			return agentconfigorg.WithEditGrants(agentconfigorg.AccessFor(body), []string{"claude-code/model"})
		}
	})
	p := h.write(".claude/settings.json", claudeSettings)
	if _, err := h.saveForm([]agentconfig.Edit{{Path: agentconfig.KeyPath{"model"}, Op: agentconfig.OpSet, Value: "haiku"}}, true); err != nil {
		t.Fatalf("a granted setting was refused: %v", err)
	}
	if !strings.Contains(h.read(p), "haiku") {
		t.Fatalf("the granted edit did not land:\n%s", h.read(p))
	}
}

// TestEditGrantMeetsOrgWrites is where the T-436 edit grant meets this lane's
// org writes, one row per meeting point: an edit grant lifts the enforced
// value the org wrote into a tracked project file (T-431) like any other
// target, and even a tool-wide grant ("<tool>/*") never lifts an org
// instruction block (T-426): a block is not a setting, so a hand edit inside
// it is still put back.
func TestEditGrantMeetsOrgWrites(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2026, 10, 6, 10, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		run  func(t *testing.T, h *harness)
	}{
		{"an edit grant keeps the developer's value in a tracked project file", func(t *testing.T, h *harness) {
			h.write(".codex/config.toml", "model = \"gpt-5\"\n")
			proj := h.write("<project>/.codex/config.toml", "# team file\nmodel = \"gpt-4\"\n")
			roots := map[string][]string{"abc123": {h.project}}
			body := orgcontract.AgentConfigPolicyBody{Directives: []orgcontract.AgentConfigDirective{directive("codex/model", `"o3"`, "enforced")}}
			res := h.svc.ApplyOrg(ctx, OrgApplyInput{Body: body, Version: 1, ProjectRoots: roots, Now: base})
			if st := stateOf(t, res.Rows, "codex/model", "codex/project-config"); st.State != string(agentconfigorg.StateApplied) {
				t.Fatalf("first apply: %+v", st)
			}
			h.write("<project>/.codex/config.toml", "# team file\nmodel = \"gpt-4.1\"\n")
			res = h.svc.ApplyOrg(ctx, OrgApplyInput{Body: body, Version: 1, Prior: res.Rows, ProjectRoots: roots, EditGrants: []string{"codex/model"}, Now: base.Add(time.Minute)})
			st := stateOf(t, res.Rows, "codex/model", "codex/project-config")
			if st.State != string(agentconfigorg.StateDeveloperValue) || res.DriftEvents != 0 {
				t.Fatalf("under the grant: %+v drift=%d", st, res.DriftEvents)
			}
			if got := h.read(proj); !strings.Contains(got, `model = "gpt-4.1"`) {
				t.Fatalf("the developer's tracked value was overwritten under the grant:\n%s", got)
			}
		}},
		{"a tool-wide edit grant never lifts an org instruction block", func(t *testing.T, h *harness) {
			h.write(".claude/settings.json", "{}\n")
			p := h.write(".claude/CLAUDE.md", "# Notes\n")
			body := orgcontract.AgentConfigPolicyBody{Instructions: []orgcontract.AgentConfigInstruction{
				{Text: "Use the internal package registry.", Policy: "Baseline", Layer: orgcontract.AgentConfigLayerOrg},
			}}
			grants := []string{"claude-code/*"}
			res := h.svc.ApplyOrg(ctx, OrgApplyInput{Body: body, Version: 1, EditGrants: grants, Now: base})
			h.write(".claude/CLAUDE.md", strings.Replace(h.read(p), "Use the internal package registry.", "Use any registry.", 1))
			res = h.svc.ApplyOrg(ctx, OrgApplyInput{Body: body, Version: 1, Prior: res.Rows, EditGrants: grants, Now: base.Add(time.Minute)})
			if st := instrRow(t, res.Rows, "claude-code", "claude-code/instructions:~/.claude/CLAUDE.md", ""); st.State != string(agentconfigorg.StateReasserted) {
				t.Fatalf("block under a tool-wide grant: %+v", st)
			}
			if got := h.read(p); !strings.Contains(got, "Use the internal package registry.") {
				t.Fatalf("the block was not put back:\n%s", got)
			}
			// The editor still refuses a developer save that changes the block.
			acc := agentconfigorg.WithEditGrants(agentconfigorg.AccessFor(body), grants)
			if acc.OrgBlockLocked == "" {
				t.Fatalf("an edit grant cleared the block lock: %+v", acc)
			}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { c.run(t, newHarness(t, func(o *Options) { o.Origin = "org" })) })
	}
}
