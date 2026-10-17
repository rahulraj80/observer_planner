package agentconfigorg

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/agentconfig"
	"github.com/marmutapp/superbased-observer/internal/orgcontract"
)

const projHash = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func fakeDetect(s string) bool { return strings.HasPrefix(s, "sk-") }

func issueCodes(is []Issue) map[string]string {
	out := map[string]string{}
	for _, i := range is {
		out[i.Code] = i.Severity
	}
	return out
}

func TestLint(t *testing.T) {
	cases := []struct {
		name  string
		spec  Spec
		block bool
		want  []string
	}{
		{"claude model enforced", Spec{Settings: []Entry{{Setting: "claude-code/model", Value: "sonnet", Mode: "enforced"}}}, false, nil},
		{"codex model default", Spec{Settings: []Entry{{Setting: "codex/model", Value: "gpt-5-codex", Mode: "default"}}}, false, nil},
		{"unknown setting", Spec{Settings: []Entry{{Setting: "claude-code/zzNope", Value: 1, Mode: "enforced"}}}, true, []string{"unknown_setting"}},
		{"bad enum", Spec{Settings: []Entry{{Setting: "claude-code/permissions.defaultMode", Value: "yolo", Mode: "enforced"}}}, true, []string{"invalid_value"}},
		{"bad mode", Spec{Settings: []Entry{{Setting: "claude-code/model", Value: "sonnet", Mode: "always"}}}, true, []string{"bad_value"}},
		{"secret value", Spec{Settings: []Entry{{Setting: "claude-code/model", Value: "sk-live-123", Mode: "enforced"}}}, true, []string{"secret_value"}},
		{"secret under credential key", Spec{Settings: []Entry{{Setting: "claude-code/env", Value: map[string]any{"OPENAI_API_KEY": "abc123"}, Mode: "enforced"}}}, true, []string{"secret_value"}},
		{"env holds the proxy route span", Spec{Settings: []Entry{{Setting: "claude-code/env", Value: map[string]any{"OPENAI_API_KEY": "${OPENAI_API_KEY}"}, Mode: "enforced"}}}, true, []string{"observer_span"}},
		{"danger warns", Spec{Settings: []Entry{{Setting: "claude-code/permissions.defaultMode", Value: "bypassPermissions", Mode: "enforced"}}}, false, []string{"dangerous_value"}},
		{"both setting and common", Spec{Settings: []Entry{{Setting: "claude-code/model", Common: "model", Value: "x", Mode: "enforced"}}}, true, []string{"bad_entry"}},
		{"common telemetry", Spec{Settings: []Entry{{Common: "telemetry_off", Value: true, Mode: "enforced"}}}, false, nil},
		{"common observer-owned", Spec{Settings: []Entry{{Common: "base_url", Value: "http://x", Mode: "enforced"}}}, true, []string{"unknown_common"}},
		{"common bad value", Spec{Settings: []Entry{{Common: "approval_mode", Value: "sometimes", Mode: "enforced"}}}, true, []string{"invalid_value"}},
		{"project bad hash", Spec{Settings: []Entry{{Setting: "claude-code/model", Value: "sonnet", Mode: "enforced", Project: "my-repo"}}}, true, []string{"bad_project"}},
		{"project local", Spec{Settings: []Entry{{Setting: "claude-code/model", Value: "sonnet", Mode: "enforced", Project: projHash}}}, false, nil},
		{"project shared warns", Spec{Settings: []Entry{{Setting: "claude-code/model", Value: "sonnet", Mode: "enforced", Project: projHash, Placement: "shared"}}}, false, []string{"tracked_file"}},
		{
			"duplicate", Spec{Settings: []Entry{
				{Setting: "claude-code/model", Value: "sonnet", Mode: "enforced"},
				{Setting: "claude-code/model", Value: "opus", Mode: "enforced"},
			}}, true, []string{"duplicate"},
		},
		{"enforced user value that may land in a tracked file warns", Spec{Settings: []Entry{{Setting: "codex/model", Value: "o3", Mode: "enforced"}}}, false, []string{"tracked_shadow"}},
		{"bad knob", Spec{SecretsPosture: "maybe"}, true, []string{"bad_value"}},
		{"bad grant", Spec{Grants: []string{"nope/*"}}, true, []string{"bad_grant"}},
		{"good grants", Spec{Grants: []string{"codex/*", "claude-code/model"}}, false, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			is := Lint(c.spec, fakeDetect)
			if Blocking(is) != c.block {
				t.Fatalf("blocking = %v, want %v: %+v", Blocking(is), c.block, is)
			}
			got := issueCodes(is)
			for _, w := range c.want {
				if _, ok := got[w]; !ok {
					t.Fatalf("missing %q in %+v", w, is)
				}
			}
		})
	}
}

func TestDecodeSpecStrict(t *testing.T) {
	if _, err := DecodeSpec([]byte(`{"settings":[],"surprise":1}`)); err == nil {
		t.Fatal("unknown field accepted")
	}
	s, err := DecodeSpec([]byte(`{"settings":[{"setting":"claude-code/cleanupPeriodDays","value":30,"mode":"enforced"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Settings[0].Value.(int64); !ok {
		t.Fatalf("number not native: %T", s.Settings[0].Value)
	}
}

func directive(b orgcontract.AgentConfigPolicyBody, setting, project, mode string) (orgcontract.AgentConfigDirective, bool) {
	for _, d := range b.Directives {
		if d.Setting == setting && d.Project == project && d.Mode == mode {
			return d, true
		}
	}
	return orgcontract.AgentConfigDirective{}, false
}

func TestComposeLayers(t *testing.T) {
	org := Source{Name: "Org baseline", Layer: agentconfig.LayerO1, Spec: Spec{
		SecretsPosture: "warn", OnDrift: "reassert",
		Settings: []Entry{
			{Setting: "claude-code/model", Value: "sonnet", Mode: "enforced"},
			{Setting: "codex/model", Value: "gpt-5", Mode: "default"},
			{Common: "approval_mode", Value: "ask_edits", Mode: "enforced"},
		},
	}}
	team := Source{Name: "Apps", Layer: agentconfig.LayerO2, Team: "t-apps", Spec: Spec{
		SecretsPosture: "refuse", OnDrift: "report", Grants: []string{"codex/model"},
		Settings: []Entry{
			{Setting: "claude-code/model", Value: "opus", Mode: "enforced"},
			{Setting: "codex/model", Value: "gpt-5-codex", Mode: "enforced"},
			{Common: "approval_mode", Value: "full_auto", Mode: "enforced"},
		},
	}}
	member := Source{Name: "Dana", Layer: agentconfig.LayerO3, Spec: Spec{SecretsPosture: "allow", DeveloperAccess: "editable"}}
	got, err := Compose(ComposeInput{Sources: []Source{member, team, org}})
	if err != nil {
		t.Fatal(err)
	}
	b := got.Body
	// Most specific enforced value wins.
	if d, ok := directive(b, "claude-code/model", "", "enforced"); !ok || string(d.Value) != `"opus"` || d.Layer != "o2_team" || d.Policy != "Apps" {
		t.Fatalf("claude model = %+v", d)
	}
	// An org default is dropped where a team enforces the same setting.
	if _, ok := directive(b, "codex/model", "", "default"); ok {
		t.Fatal("default kept beside an enforced value")
	}
	// Ordered-danger common key: strictest wins across layers (ask_edits
	// beats full_auto), expanded to Claude Code's native key.
	d, ok := directive(b, "claude-code/permissions.defaultMode", "", "enforced")
	if !ok || d.Common != "approval_mode" || string(d.Value) != `"default"` {
		t.Fatalf("approval_mode expansion = %+v (ok %v)", d, ok)
	}
	if !strings.Contains(d.Policy, "Apps") || !strings.Contains(d.Policy, "Org baseline") {
		t.Fatalf("merged policy names = %q", d.Policy)
	}
	// Knobs: secrets strictest, drift and access most specific, grants union.
	if b.SecretsPosture != "refuse" || b.OnDrift != "report" || b.DeveloperAccess != "editable" {
		t.Fatalf("knobs = %s %s %s", b.SecretsPosture, b.OnDrift, b.DeveloperAccess)
	}
	if len(b.Grants) != 1 || b.Grants[0].Setting != "codex/model" {
		t.Fatalf("grants = %+v", b.Grants)
	}
	// Deterministic: the same inputs give byte-identical bodies.
	again, _ := Compose(ComposeInput{Sources: []Source{org, member, team}})
	e1, _ := orgcontract.EncodeAgentConfigPolicyBody(b)
	e2, _ := orgcontract.EncodeAgentConfigPolicyBody(again.Body)
	if e1 != e2 {
		t.Fatalf("composition is not deterministic:\n%s\n%s", e1, e2)
	}
}

func TestComposeTeamConflict(t *testing.T) {
	a := Source{Name: "A", Layer: agentconfig.LayerO2, Team: "ta", Spec: Spec{Settings: []Entry{{Setting: "codex/model", Value: "gpt-5", Mode: "enforced"}}}}
	b := Source{Name: "B", Layer: agentconfig.LayerO2, Team: "tb", Spec: Spec{Settings: []Entry{{Setting: "codex/model", Value: "o3", Mode: "enforced"}}}}
	got, err := Compose(ComposeInput{Sources: []Source{a, b}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Body.Directives) != 0 || len(got.Skipped) != 1 || !strings.HasPrefix(got.Skipped[0].Reason, "team_conflict") {
		t.Fatalf("conflict not skipped: %+v", got)
	}
	got, _ = Compose(ComposeInput{Sources: []Source{a, b}, TeamPriority: []string{"tb", "ta"}})
	if d, ok := directive(got.Body, "codex/model", "", "enforced"); !ok || string(d.Value) != `"o3"` {
		t.Fatalf("priority did not resolve: %+v", got)
	}
	conf := LintTeamConflicts([]TeamDoc{{Name: "A", Teams: []string{"ta"}, Spec: a.Spec}, {Name: "B", Teams: []string{"tb"}, Spec: b.Spec}},
		nil, map[string][]string{"u1": {"ta", "tb"}, "u2": {"ta"}})
	if len(conf) != 1 || conf[0].Member != "u1" {
		t.Fatalf("conflicts = %+v", conf)
	}
	if c := LintTeamConflicts([]TeamDoc{{Name: "A", Teams: []string{"ta"}, Spec: a.Spec}, {Name: "B", Teams: []string{"tb"}, Spec: b.Spec}},
		[]string{"ta", "tb"}, map[string][]string{"u1": {"ta", "tb"}}); len(c) != 0 {
		t.Fatalf("priority should clear the conflict: %+v", c)
	}
}

func TestComposeEmpty(t *testing.T) {
	got, err := Compose(ComposeInput{})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Body.Empty() || got.Body.DeveloperAccess != "read_only" || got.Body.SecretsPosture != "warn" {
		t.Fatalf("empty = %+v", got.Body)
	}
	raw, _ := json.Marshal(got.Body)
	if strings.Contains(string(raw), "null") {
		t.Fatalf("empty body encodes nulls: %s", raw)
	}
}

func TestDecide(t *testing.T) {
	now := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	exp := ValueHash("sonnet")
	base := DecideInput{KnownSetting: true, Installed: true, HasFile: true, Mode: "enforced", Expected: "sonnet", OnDrift: "reassert", Now: now}
	held := Prior{Known: true, ExpectedHash: exp, State: StateApplied}
	mins := func(m ...int) []time.Time {
		var out []time.Time
		for _, x := range m {
			out = append(out, now.Add(-time.Duration(x)*time.Minute))
		}
		return out
	}
	cases := []struct {
		name  string
		mod   func(*DecideInput)
		write bool
		state State
		event bool
	}{
		{"unknown setting", func(in *DecideInput) { in.KnownSetting = false }, false, StateUnknownSetting, false},
		{"not installed", func(in *DecideInput) { in.Installed = false }, false, StateNotInstalled, false},
		{"no file", func(in *DecideInput) { in.HasFile = false }, false, StateNoFile, false},
		{"first apply", func(in *DecideInput) { in.Current, in.CurrentFound = "opus", true }, true, StateApplied, false},
		{"first apply, file empty", nil, true, StateApplied, false},
		{"in place", func(in *DecideInput) { in.Current, in.CurrentFound = "sonnet", true }, false, StateApplied, false},
		{"drift reasserted", func(in *DecideInput) { in.Current, in.CurrentFound, in.Prior = "opus", true, held }, true, StateReasserted, true},
		{"drift deleted reasserted", func(in *DecideInput) { in.Prior = held }, true, StateReasserted, true},
		{"drift report only", func(in *DecideInput) {
			in.Current, in.CurrentFound, in.Prior, in.OnDrift = "opus", true, held, "report"
		}, false, StateDrifted, true},
		{"still drifted is not a new event", func(in *DecideInput) {
			in.Current, in.CurrentFound, in.OnDrift = "opus", true, "report"
			in.Prior = Prior{Known: true, ExpectedHash: exp, State: StateDrifted, DriftTimes: mins(5)}
		}, false, StateDrifted, false},
		{"third drift in an hour backs off", func(in *DecideInput) {
			in.Current, in.CurrentFound = "opus", true
			in.Prior = Prior{Known: true, ExpectedHash: exp, State: StateApplied, DriftTimes: mins(50, 20)}
		}, false, StateBackedOff, true},
		{"old drifts age out", func(in *DecideInput) {
			in.Current, in.CurrentFound = "opus", true
			in.Prior = Prior{Known: true, ExpectedHash: exp, State: StateApplied, DriftTimes: mins(90, 70)}
		}, true, StateReasserted, true},
		{"backed off stays", func(in *DecideInput) {
			in.Current, in.CurrentFound = "opus", true
			in.Prior = Prior{Known: true, ExpectedHash: exp, State: StateBackedOff, DriftTimes: mins(50, 20, 10)}
		}, false, StateBackedOff, false},
		{"backed off recovers when the value returns", func(in *DecideInput) {
			in.Current, in.CurrentFound = "sonnet", true
			in.Prior = Prior{Known: true, ExpectedHash: exp, State: StateBackedOff, DriftTimes: mins(50)}
		}, false, StateApplied, false},
		{"policy change is a fresh apply", func(in *DecideInput) {
			in.Current, in.CurrentFound = "opus", true
			in.Prior = Prior{Known: true, ExpectedHash: ValueHash("haiku"), State: StateBackedOff, DriftTimes: mins(5, 4, 3)}
		}, true, StateApplied, false},
		{"granted developer overrides a default", func(in *DecideInput) {
			in.Mode, in.Granted, in.Current, in.CurrentFound, in.Prior = "default", true, "opus", true, held
		}, false, StateDeveloperValue, false},
		{"ungranted default is reasserted", func(in *DecideInput) {
			in.Mode, in.Current, in.CurrentFound, in.Prior = "default", "opus", true, held
		}, true, StateReasserted, true},
		{"granted, no own value: default written", func(in *DecideInput) { in.Mode, in.Granted = "default", true }, true, StateApplied, false},
		// Review M3-17 / D-122: a granted developer who DELETES an applied
		// org default chose the tool's own default; that is their value,
		// not drift, and it is never written back.
		{"granted developer deletes an applied default", func(in *DecideInput) {
			in.Mode, in.Granted, in.Prior = "default", true, held
		}, false, StateDeveloperValue, false},
		{"ungranted deletion of a default is reasserted", func(in *DecideInput) {
			in.Mode, in.Prior = "default", held
		}, true, StateReasserted, true},
		{"granted deletion of an ENFORCED value is reasserted", func(in *DecideInput) {
			in.Granted, in.Prior = true, held
		}, true, StateReasserted, true},
		// T-436: an AI tool setting edit grant lifts an enforced lock.
		{"an edit grant keeps the developer's value", func(in *DecideInput) {
			in.LockLifted, in.Current, in.CurrentFound, in.Prior = true, "opus", true, held
		}, false, StateDeveloperValue, false},
		{"an edit grant keeps the developer's deletion", func(in *DecideInput) {
			in.LockLifted, in.Prior = true, held
		}, false, StateDeveloperValue, false},
		{"an edit grant still writes a value never applied", func(in *DecideInput) { in.LockLifted = true }, true, StateApplied, false},
		{"an edit grant never covers a default (D-122 decides it)", func(in *DecideInput) {
			in.Mode, in.LockLifted, in.Current, in.CurrentFound, in.Prior = "default", true, "opus", true, held
		}, true, StateReasserted, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := base
			if c.mod != nil {
				c.mod(&in)
			}
			d := Decide(in)
			if d.Write != c.write || d.State != c.state || d.DriftEvent != c.event {
				t.Fatalf("Decide = %+v, want write=%v state=%s event=%v", d, c.write, c.state, c.event)
			}
		})
	}
}

func TestStatesClosed(t *testing.T) {
	for _, s := range States() {
		if !KnownState(string(s)) {
			t.Fatalf("%s not known", s)
		}
	}
	if KnownState("hacked") {
		t.Fatal("unknown state accepted")
	}
}

func TestSecretIn(t *testing.T) {
	cases := []struct {
		v    any
		want bool
	}{
		{map[string]any{"OPENAI_API_KEY": "${OPENAI_API_KEY}"}, false},
		{map[string]any{"OPENAI_API_KEY": "$OPENAI_API_KEY"}, false},
		{map[string]any{"OPENAI_API_KEY": "abc123"}, true},
		{map[string]any{"PATH_EXTRA": "/opt/bin"}, false},
		{[]any{"ok", "sk-xyz"}, true},
		{"plain", false},
	}
	for _, c := range cases {
		if got := secretIn(c.v, fakeDetect); got != c.want {
			t.Errorf("secretIn(%v) = %v, want %v", c.v, got, c.want)
		}
	}
}
