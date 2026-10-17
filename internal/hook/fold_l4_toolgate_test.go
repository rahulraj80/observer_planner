package hook

import (
	"bytes"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/guard"
	"github.com/marmutapp/superbased-observer/internal/models"
)

// TestToolGateBlockDecidedBeforePersist pins review M2-11: on a block the
// caller's Decided hook runs right after the reply is written and BEFORE
// persist, so a panic or a watchdog fire during persist can never turn the
// block into a second (allow) reply.
func TestToolGateBlockDecidedBeforePersist(t *testing.T) {
	var order []string
	var out bytes.Buffer
	res := HandleToolGate(ToolGateRequest{
		Tool: models.ToolCodex, Dialect: ToolGateDialectClaudeJSON, NativeEvent: "PreToolUse", Label: "codex:PreToolUse",
		Body:    toolPayload("s1", "Bash", map[string]any{"command": "rm -rf ~/x"}),
		Decided: func(code int) { order = append(order, "decided") },
	}, &targetEvaluator{denyOn: "rm -rf"}, func(guard.ActionVerdict) {
		if out.Len() == 0 {
			t.Error("persist ran before the reply was written")
		}
		order = append(order, "persist")
	}, &out, &bytes.Buffer{})
	if !res.Replied {
		t.Fatalf("result = %+v, want a block", res)
	}
	if strings.Join(order, ",") != "decided,persist" {
		t.Fatalf("order = %v, want decided before persist", order)
	}
	// The Claude Code receiver's entry point carries the same hook.
	order = nil
	out.Reset()
	blocked, _ := HandleGuardedDecided("claude-code:pre-tool", preToolBody("Bash", "rm -rf ~/x"),
		&targetEvaluator{denyOn: "rm -rf"}, func(guard.ActionVerdict) { order = append(order, "persist") },
		func(int) { order = append(order, "decided") }, &out, &bytes.Buffer{})
	if !blocked || strings.Join(order, ",") != "decided,persist" {
		t.Fatalf("claude-code: blocked=%v order=%v", blocked, order)
	}
}

// TestToolGateUnclassifiedIsRecorded pins review M2-12: a wired gate call
// the gate cannot classify (an unknown tool name, a judged tool with no
// operand) is handed to the caller's Unclassified hook after its reply,
// never a silent pass; a known tool the guard does not judge is not
// unclassified; and a single-operand field never shadows a batch field.
func TestToolGateUnclassifiedIsRecorded(t *testing.T) {
	run := func(body []byte) (ToolGateResult, *targetEvaluator, []string) {
		var got []string
		ev := &targetEvaluator{}
		res := HandleToolGate(ToolGateRequest{
			Tool: models.ToolCodex, Dialect: ToolGateDialectClaudeJSON, NativeEvent: "PreToolUse", Label: "codex:PreToolUse",
			Body: body, Unclassified: func(native string) { got = append(got, native) },
		}, ev, nil, &bytes.Buffer{}, &bytes.Buffer{})
		if res.RecordAfterReply != nil {
			res.RecordAfterReply()
		}
		return res, ev, got
	}
	cases := []struct {
		name string
		body []byte
		want string // "" = no unclassified record
	}{
		{"unknown tool name", toolPayload("s1", "TotallyUnknownTool9", map[string]any{"x": 1}), "TotallyUnknownTool9"},
		{"judged tool, operand missing", toolPayload("s1", "Bash", map[string]any{}), "Bash"},
		{"judged tool with operand", toolPayload("s1", "Bash", map[string]any{"command": "ls"}), ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, _, got := run(tc.body)
			if res.Replied {
				t.Fatalf("an unclassified call was blocked: %+v", res)
			}
			if strings.Join(got, ",") != tc.want {
				t.Fatalf("unclassified = %v, want %q", got, tc.want)
			}
		})
	}
	// No hook asked for: the old silent pass, byte for byte.
	res := HandleToolGate(ToolGateRequest{
		Tool: models.ToolCodex, Dialect: ToolGateDialectClaudeJSON, NativeEvent: "PreToolUse", Label: "codex:PreToolUse",
		Body: toolPayload("s1", "TotallyUnknownTool9", map[string]any{}),
	}, &targetEvaluator{}, nil, &bytes.Buffer{}, &bytes.Buffer{})
	if res.RecordAfterReply != nil || res.Replied || res.Evaluated {
		t.Fatalf("no Unclassified hook = %+v, want the zero result", res)
	}

	// Operand union: `command` beside `commands` judges every operand.
	_, ev, got := run(toolPayload("s1", "Bash", map[string]any{"command": "ls", "commands": []any{"rm -rf ~/x"}}))
	var targets []string
	for _, e := range ev.seen {
		targets = append(targets, e.Target)
	}
	if strings.Join(targets, "|") != "ls|rm -rf ~/x" || len(got) != 0 {
		t.Fatalf("judged %v (unclassified %v), want both operands", targets, got)
	}
}
