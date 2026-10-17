package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/agentconfig"
	"github.com/marmutapp/superbased-observer/internal/orgcontract"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// seedAgentConfigPolicy enrols the store as o/u (managed) and caches a policy
// signed for that enrolment: the readers are enrolment-bound (review M3-01).
func seedAgentConfigPolicy(t *testing.T, st *store.Store, version int64, body string) {
	t.Helper()
	ctx := context.Background()
	if err := st.WriteEnrolment(ctx, store.Enrolment{OrgID: "o", OrgServerURL: "https://o.example", UserID: "u", Tenancy: "managed"}); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertOrgAgentConfigPolicy(ctx, store.OrgAgentConfigPolicyRow{OrgID: "o", Subject: "u", Version: version, Body: body, ReceivedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
}

// TestAgentConfigDriftRiskIgnoresAnotherEnrolmentsState pins review M3-01 on
// the capture-risk path: state left by an earlier enrolment's policy never
// raises the current org's drift label.
func TestAgentConfigDriftRiskIgnoresAnotherEnrolmentsState(t *testing.T) {
	ctx := context.Background()
	st, _ := openTestStore(t)
	seedAgentConfigPolicy(t, st, 1, "{}")
	if err := st.ReplaceAgentConfigOrgState(ctx, []store.AgentConfigOrgStateRow{{SettingID: "codex/model", FileID: "codex/user-config", Mode: "enforced", State: "drifted"}}); err != nil {
		t.Fatal(err)
	}
	if err := st.WriteEnrolment(ctx, store.Enrolment{OrgID: "other", OrgServerURL: "https://other.example", UserID: "u", Tenancy: "managed"}); err != nil {
		t.Fatal(err)
	}
	rep := orgcontract.ManagedIntegrityReport{}
	agentConfigDriftRisk(ctx, st, &rep)
	if len(rep.CaptureRisks) != 0 {
		t.Fatalf("labels = %v, want none for another enrolment's state", rep.CaptureRisks)
	}
}

// TestAgentConfigDriftRisk pins the closed capture-risk label: only an
// ENFORCED setting that drifted (or backed off) raises it, once.
func TestAgentConfigDriftRisk(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name string
		rows []store.AgentConfigOrgStateRow
		want bool
	}{
		{"no state", nil, false},
		{"applied", []store.AgentConfigOrgStateRow{{SettingID: "codex/model", FileID: "codex/user-config", Mode: "enforced", State: "applied"}}, false},
		{"a default drifted", []store.AgentConfigOrgStateRow{{SettingID: "codex/model", FileID: "codex/user-config", Mode: "default", State: "drifted"}}, false},
		{"an enforced setting drifted", []store.AgentConfigOrgStateRow{{SettingID: "codex/model", FileID: "codex/user-config", Mode: "enforced", State: "drifted"}}, true},
		{"an enforced setting backed off", []store.AgentConfigOrgStateRow{{SettingID: "codex/model", FileID: "codex/user-config", Mode: "enforced", State: "backed_off"}}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			st, _ := openTestStore(t)
			seedAgentConfigPolicy(t, st, 1, "{}")
			if err := st.ReplaceAgentConfigOrgState(ctx, c.rows); err != nil {
				t.Fatal(err)
			}
			rep := orgcontract.ManagedIntegrityReport{CaptureRisks: []string{orgcontract.CaptureRiskHookConfigChanged}}
			agentConfigDriftRisk(ctx, st, &rep)
			agentConfigDriftRisk(ctx, st, &rep)
			n := 0
			for _, l := range rep.CaptureRisks {
				if l == orgcontract.CaptureRiskAgentConfigDrift {
					n++
				}
			}
			if (n == 1) != c.want || n > 1 {
				t.Fatalf("labels = %v", rep.CaptureRisks)
			}
		})
	}
}

// TestAgentConfigOrgStatusCLI pins the CLI status: an individual node says
// so; a managed node lists the policy, the grants and each target's state.
func TestAgentConfigOrgStatusCLI(t *testing.T) {
	ctx := context.Background()
	st, _ := openTestStore(t)
	var out bytes.Buffer
	if err := printAgentConfigOrgStatus(ctx, &out, agentConfigEnv{st: st}, false); err != nil || !strings.Contains(out.String(), "not managed") {
		t.Fatalf("individual: %v %q", err, out.String())
	}
	body, err := orgcontract.EncodeAgentConfigPolicyBody(orgcontract.AgentConfigPolicyBody{
		Schema: 1, OnDrift: "report", Grants: []orgcontract.AgentConfigGrant{{Setting: "codex/model"}},
		Directives: []orgcontract.AgentConfigDirective{{Setting: "claude-code/model", Value: json.RawMessage(`"opus"`), Mode: "enforced", Layer: "o1_org", Policy: "Baseline"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	seedAgentConfigPolicy(t, st, 9, body)
	if err := st.ReplaceAgentConfigOrgState(ctx, []store.AgentConfigOrgStateRow{{
		SettingID: "claude-code/model", FileID: "claude-code/user-settings", Tool: "claude-code", Scope: "user", Mode: "enforced",
		Layer: "o1_org", PolicyName: "Baseline", State: "drifted", DriftTimes: []time.Time{time.Now()}, PolicyVersion: 9, ObservedAt: time.Now(),
	}}); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	env := agentConfigEnv{st: st, managed: true, access: agentconfig.Access{Locked: map[string]string{"claude-code/model": "Baseline"}}}
	if err := printAgentConfigOrgStatus(ctx, &out, env, false); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"version 9", "On drift: report", "codex/model", "claude-code/model", "drifted", "Baseline"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("status lacks %q:\n%s", want, out.String())
		}
	}
	out.Reset()
	if err := printAgentConfigOrgStatus(ctx, &out, env, true); err != nil || !strings.Contains(out.String(), `"locked": {`) {
		t.Fatalf("json: %v %s", err, out.String())
	}
}

// TestPickSettingFileNamesTheOrgLock: on a managed machine whose file is not
// editable, the CLI says why (the org lock, else the read-only reason)
// before the generic "no editable file" note.
func TestPickSettingFileNamesTheOrgLock(t *testing.T) {
	v := agentconfig.ToolView{Tool: "codex", Files: []agentconfig.FileView{{ID: "codex/user-config", Shape: "codex/config", Editable: false}}}
	cases := []struct {
		name string
		s    agentconfig.SettingView
		want string
	}{
		{"locked", agentconfig.SettingView{ID: "codex/model", Shape: "codex/config", Locked: "Apps baseline"}, "set by your organization's policy Apps baseline"},
		{"not granted", agentconfig.SettingView{ID: "codex/model", Shape: "codex/config", ReadOnlyReason: "Not granted."}, "codex/model: Not granted."},
		{"plain", agentconfig.SettingView{ID: "codex/model", Shape: "codex/config"}, "no editable file"},
	}
	for _, c := range cases {
		if _, err := pickSettingFile(v, c.s, "", ""); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v", c.name, err)
		}
	}
}
