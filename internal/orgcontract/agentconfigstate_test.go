package orgcontract

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNormalizeAgentConfigReason(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"not_granted", "not_granted"},
		{"observer_span", "observer_span"},
		{"", ""},
		{"Free text with spaces", ""},
		{"UPPER", ""},
		{"x" + strings.Repeat("y", 60), ""},
		{"path/../etc", ""},
	}
	for _, c := range cases {
		if got := NormalizeAgentConfigReason(c.in); got != c.want {
			t.Errorf("NormalizeAgentConfigReason(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestDigestAgentConfigStateIgnoresOrderAndObservedAt(t *testing.T) {
	a := AgentConfigStateRow{Tool: "codex", Setting: "codex/model", FileID: "codex.user", State: "applied", ExpectedHash: "h1", ObservedAt: "2026-10-04T10:00:00Z"}
	b := AgentConfigStateRow{Tool: "claude-code", Setting: "claude-code/model", FileID: "claude-code.user", State: "drifted", ExpectedHash: "h2", ObservedAt: "2026-10-04T10:00:00Z"}
	d1 := DigestAgentConfigState([]AgentConfigStateRow{a, b})
	a.ObservedAt, b.ObservedAt = "2026-10-04T11:00:00Z", "2026-10-04T11:30:00Z"
	d2 := DigestAgentConfigState([]AgentConfigStateRow{b, a})
	if d1 != d2 {
		t.Fatalf("digest moved on order or timestamp only: %s vs %s", d1, d2)
	}
	b.State = "applied"
	if DigestAgentConfigState([]AgentConfigStateRow{a, b}) == d1 {
		t.Fatal("digest did not move when a state changed")
	}
}

// TestPushEnvelopeAgentConfigStateOmitted pins the compat rule: an envelope
// without a report is byte-identical to the pre-feature shape (no key).
func TestPushEnvelopeAgentConfigStateOmitted(t *testing.T) {
	raw, err := json.Marshal(PushEnvelope{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "agent_config_state") {
		t.Fatalf("empty envelope carries agent_config_state: %s", raw)
	}
	raw, _ = json.Marshal(PushEnvelope{AgentConfigState: &AgentConfigStateReport{PolicyVersion: 3, Rows: []AgentConfigStateRow{}}})
	if !strings.Contains(string(raw), `"agent_config_state":{"policy_version":3`) {
		t.Fatalf("report not carried: %s", raw)
	}
}

func TestCaptureRiskAgentConfigDriftIsValid(t *testing.T) {
	if !validCaptureRisk(CaptureRiskAgentConfigDrift) {
		t.Fatal("agent_config_drift is not a valid capture-risk label")
	}
}
