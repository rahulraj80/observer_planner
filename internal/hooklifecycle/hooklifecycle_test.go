package hooklifecycle

import (
	"strings"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/integration"
	"github.com/marmutapp/superbased-observer/internal/models"
)

var at = time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC)

func bind(c integration.HookCap, phase string, rel integration.ProcRelation) integration.HookBinding {
	return integration.HookBinding{Cap: c, Phase: phase, ProcRel: rel, Wired: true, Dialect: "lifecycle"}
}

func TestNormalize(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		want       Payload
	}{
		{
			"claude shape", `{"session_id":"s","cwd":"/r","permission_mode":"plan","effort":{"level":"high"},"last_assistant_message":"done"}`,
			Payload{SessionID: "s", Cwd: "/r", PermissionMode: "plan", EffortLevel: "high", LastAssistantMessage: "done"},
		},
		{"camel session id", `{"sessionId":"s2","workspace_roots":["/w"]}`, Payload{SessionID: "s2", Cwd: "/w"}},
		{"conversation id", `{"conversation_id":"c","workspace_roots":["/a","/b"]}`, Payload{SessionID: "c", Cwd: "/a"}},
		{
			"grok camel fields", `{"sessionId":"g","workspaceRoot":"/g","lastAssistantMessage":"hi","transcriptPath":"/t"}`,
			Payload{SessionID: "g", Cwd: "/g", LastAssistantMessage: "hi", TranscriptPath: "/t"},
		},
		{
			"snake wins over camel", `{"session_id":"a","sessionId":"b","last_assistant_message":"x","lastAssistantMessage":"y"}`,
			Payload{SessionID: "a", LastAssistantMessage: "x"},
		},
		{"subagent id", `{"session_id":"p","subagent_id":"k","agent_type":"general"}`, Payload{SessionID: "p", AgentID: "k", AgentType: "general"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := Normalize([]byte(tc.body))
			if !ok || got != tc.want {
				t.Fatalf("Normalize = %+v (%v), want %+v", got, ok, tc.want)
			}
		})
	}
	if _, ok := Normalize([]byte(`[1]`)); ok {
		t.Fatal("a non-object payload must not normalize")
	}
}

// TestDecide is one case per rules row plus the no-session and unknown-cap
// cases.
func TestDecide(t *testing.T) {
	for _, tc := range []struct {
		name       string
		b          integration.HookBinding
		body       string
		wantAction string
		wantSEID   string
		wantSeed   bool
	}{
		{
			"session start descendant seeds", bind(integration.HookCapSessionLifecycle, integration.HookPhaseStart, integration.ProcDescendant),
			`{"session_id":"s","cwd":"/r"}`, "", "", true,
		},
		{
			"session start non-descendant", bind(integration.HookCapSessionLifecycle, integration.HookPhaseStart, integration.ProcNone),
			`{"session_id":"s"}`, "", "", false,
		},
		{
			"session end", bind(integration.HookCapSessionLifecycle, integration.HookPhaseEnd, integration.ProcDescendant),
			`{"session_id":"s","reason":"other"}`, models.ActionSessionEnd, "s:session_end", false,
		},
		{
			"turn end with text", bind(integration.HookCapTurnLifecycle, integration.HookPhaseEnd, integration.ProcDescendant),
			`{"session_id":"s","last_assistant_message":"all done"}`, models.ActionTaskComplete, "s:stop:", false,
		},
		{
			"turn end without text", bind(integration.HookCapTurnLifecycle, integration.HookPhaseEnd, integration.ProcDescendant),
			`{"session_id":"s"}`, "", "", false,
		},
		{
			"subagent start", bind(integration.HookCapSubagentLifecycle, integration.HookPhaseStart, integration.ProcDescendant),
			`{"session_id":"p","agent_id":"k1","agent_type":"general-purpose"}`, models.ActionSubagentStart, "k1:subagent_start", false,
		},
		{
			"subagent stop", bind(integration.HookCapSubagentLifecycle, integration.HookPhaseEnd, integration.ProcDescendant),
			`{"session_id":"p","agent_id":"k1"}`, models.ActionSubagentStop, "k1:subagent_stop", false,
		},
		{
			"subagent without id", bind(integration.HookCapSubagentLifecycle, integration.HookPhaseEnd, integration.ProcDescendant),
			`{"session_id":"p"}`, models.ActionSubagentStop, "p:subagent_stop:", false,
		},
		{
			"no session id", bind(integration.HookCapSessionLifecycle, integration.HookPhaseEnd, integration.ProcDescendant),
			`{"cwd":"/r"}`, "", "", false,
		},
		{
			"unhandled cap", bind(integration.HookCapToolGate, integration.HookPhasePre, integration.ProcDescendant),
			`{"session_id":"s"}`, "", "", false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan := Decide("qoder", tc.b, []byte(tc.body), at)
			if plan.SeedPidbridge != tc.wantSeed {
				t.Errorf("seed = %v, want %v", plan.SeedPidbridge, tc.wantSeed)
			}
			if tc.wantAction == "" {
				if plan.Row != nil {
					t.Fatalf("unexpected row %+v", *plan.Row)
				}
				return
			}
			if plan.Row == nil {
				t.Fatal("no row")
			}
			r := plan.Row
			if r.ActionType != tc.wantAction || !strings.HasPrefix(r.SourceEventID, tc.wantSEID) || r.Tool != "qoder" || r.SourceFile != "qoder:hook" || !r.Timestamp.Equal(at) {
				t.Fatalf("row = %+v", *r)
			}
		})
	}
}

// TestSessionEndParityWithClaude pins the session_end row to the shape the
// Claude Code receiver has always written (buildClaudeSessionEndEvent:
// source event id <sid>:session_end, target session_ended, project root =
// cwd, permission mode carried), so moving Claude Code's SessionEnd onto the
// lifecycle handler changes no row.
func TestSessionEndParityWithClaude(t *testing.T) {
	b := bind(integration.HookCapSessionLifecycle, integration.HookPhaseEnd, integration.ProcDescendant)
	plan := Decide(models.ToolClaudeCode, b, []byte(`{"session_id":"abc","cwd":"/repo","permission_mode":"default","reason":"exit"}`), at)
	r := plan.Row
	if r == nil || r.SourceEventID != "abc:session_end" || r.Target != "session_ended" || r.ProjectRoot != "/repo" ||
		r.SourceFile != "claude-code:hook" || r.Metadata == nil || r.Metadata.PermissionMode != "default" || !r.Success {
		t.Fatalf("session_end row = %+v", r)
	}
}

func TestTurnEndScrubsAndCaps(t *testing.T) {
	b := bind(integration.HookCapTurnLifecycle, integration.HookPhaseEnd, integration.ProcDescendant)
	secret := "sk-ant-api03-1234567890ABCDEFGhijKLmnopqRSTuvwxyz"
	long := strings.Repeat("y", maxAssistantBytes+10)
	plan := Decide("qwen-code", b, []byte(`{"session_id":"s","last_assistant_message":"key `+secret+` `+long+`"}`), at)
	r := plan.Row
	if r == nil {
		t.Fatal("no row")
	}
	if strings.Contains(r.ToolOutput, secret) || strings.Contains(r.Target, secret) {
		t.Fatal("the final assistant text was stored unscrubbed")
	}
	if len(r.ToolOutput) > maxAssistantBytes || len(r.Target) > previewMax {
		t.Fatalf("caps not applied: output %d target %d", len(r.ToolOutput), len(r.Target))
	}
	if r.RawToolName != "qwen-code.assistant_text" {
		t.Fatalf("raw tool name %q", r.RawToolName)
	}
	// Deterministic: the same body is the same row id.
	again := Decide("qwen-code", b, []byte(`{"session_id":"s","last_assistant_message":"key `+secret+` `+long+`"}`), at)
	if again.Row.SourceEventID != r.SourceEventID {
		t.Fatal("turn-end source event id is not deterministic")
	}
}

func TestHandled(t *testing.T) {
	for _, c := range []integration.HookCap{integration.HookCapSessionLifecycle, integration.HookCapTurnLifecycle, integration.HookCapSubagentLifecycle} {
		if !Handled(c) {
			t.Errorf("%s not handled", c)
		}
	}
	if Handled(integration.HookCapToolGate) || Handled(integration.HookCapPromptGate) {
		t.Error("a gate capability is not a lifecycle capability")
	}
}
