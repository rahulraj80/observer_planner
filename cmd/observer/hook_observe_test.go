package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/hook"
	"github.com/marmutapp/superbased-observer/internal/integration"
	"github.com/marmutapp/superbased-observer/internal/models"
)

// Hook-parity Lane E (2026-10-03): the observe-family recorders
// (compaction, task).

func TestDecodeObservePayload(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, body, sid, cwd, trigger string
		ok                            bool
	}{
		{"claude-shaped", `{"session_id":"s","cwd":"/r","trigger":"auto"}`, "s", "/r", "auto", true},
		{"camelCase", `{"sessionId":"s2","cwd":"/r2","trigger":"manual"}`, "s2", "/r2", "manual", true},
		{"cursor", `{"conversation_id":"c","workspace_roots":["/w","/x"],"trigger":"auto"}`, "c", "/w", "auto", true},
		{"empty session string falls through", `{"session_id":"","sessionId":"s3"}`, "s3", "", "", true},
		{"garbage", `nope`, "", "", "", false},
		{"array", `[]`, "", "", "", false},
	}
	for _, tc := range cases {
		p, ok := decodeObservePayload([]byte(tc.body))
		if ok != tc.ok || p.SessionID != tc.sid || p.Cwd != tc.cwd || p.Trigger != tc.trigger {
			t.Errorf("%s: got %+v ok=%v", tc.name, p, ok)
		}
	}
}

func TestBuildHookTaskEvent(t *testing.T) {
	t.Parallel()
	start := integration.HookBinding{Cap: integration.HookCapTaskObserve, Phase: integration.HookPhaseStart, NativeEvent: "TaskCreated"}
	end := integration.HookBinding{Cap: integration.HookCapTaskObserve, Phase: integration.HookPhaseEnd, NativeEvent: "TodoCompleted"}
	cases := []struct {
		name   string
		b      integration.HookBinding
		tool   string
		body   string
		ok     bool
		target string
		id     string
	}{
		{
			"claude created", start, models.ToolClaudeCode,
			`{"session_id":"s","cwd":"/r","task_id":"task_123","task_title":"Fix auth","task_description":"x"}`,
			true, "created: Fix auth", "s:task_event:created:task_123",
		},
		{
			"qwen validation pass skipped", end, models.ToolQwenCode,
			`{"session_id":"q","todo_id":"1","todo_content":"Write tests","phase":"validation"}`, false, "", "",
		},
		{
			"qwen postWrite recorded", end, models.ToolQwenCode,
			`{"session_id":"q","todo_id":"1","todo_content":"Write tests","phase":"postWrite"}`,
			true, "completed: Write tests", "q:task_event:completed:1",
		},
		{"no session", start, models.ToolClaudeCode, `{"task_id":"t"}`, false, "", ""},
	}
	for _, tc := range cases {
		ev, ok := buildHookTaskEvent(observeRecord{Tool: tc.tool, Binding: tc.b, Body: []byte(tc.body), Now: time.Now()})
		if ok != tc.ok {
			t.Fatalf("%s: ok=%v", tc.name, ok)
		}
		if !ok {
			continue
		}
		if ev.ActionType != models.ActionTaskEvent || ev.Target != tc.target || ev.SourceEventID != tc.id || ev.Tool != tc.tool || ev.RawToolName != tc.b.NativeEvent {
			t.Errorf("%s: %+v", tc.name, ev)
		}
	}
}

// TestCompactionReachesTheOneRecorder drives every receiver that now routes a
// compact_lifecycle binding to the shared recorder: the prompt-only receiver
// (qwen-code), the binding fallback (kimi-code), the cursor receiver and the
// claude-code dispatcher. Each answers its dialect's allow first.
func TestCompactionReachesTheOneRecorder(t *testing.T) {
	ctx := context.Background()
	env := newObserveTestEnv(t, map[string]string{
		"q1": models.ToolQwenCode, "k1": models.ToolKimiCode, "c1": models.ToolCursor, "cc1": models.ToolClaudeCode,
	})
	q := func(sid string) int {
		return env.count(t, `SELECT COUNT(*) FROM compaction_events WHERE session_id = ?`, sid)
	}

	out := captureStdio(t, `{"session_id":"q1","cwd":`+strconvQuote(env.root)+`,"trigger":"auto"}`, func() {
		handlePromptSubmitOnlyHook(models.ToolQwenCode, "qwen-code", hook.PromptDialectTopLevelBlock, "UserPromptSubmit", "PreCompact", env.cfgPath)
	})
	if strings.TrimSpace(out) != `{"decision":"approve"}` || q("q1") != 1 {
		t.Fatalf("qwen PreCompact: stdout %q rows %d", out, q("q1"))
	}

	b, ok := resolveHookBinding(models.ToolKimiCode, "PreCompact")
	if !ok {
		t.Fatal("kimi PreCompact binding missing")
	}
	out = captureStdio(t, `{"hook_event_name":"PreCompact","session_id":"k1","cwd":`+strconvQuote(env.root)+`,"trigger":"auto","token_count":9}`, func() {
		if !dispatchBindingReceiver(ctx, models.ToolKimiCode, "PreCompact", env.cfgPath, b) {
			t.Error("kimi PreCompact must have a binding receiver")
		}
	})
	if out != "" || q("k1") != 1 {
		t.Fatalf("kimi PreCompact: stdout %q (want empty) rows %d", out, q("k1"))
	}

	captureStdio(t, `{"conversation_id":"c1","hook_event_name":"preCompact","workspace_roots":[`+strconvQuote(env.root)+`],"trigger":"auto"}`, func() {
		cursorReceiverForTest(t)(ctx, "preCompact", env.cfgPath)
	})
	if q("c1") != 1 {
		t.Fatalf("cursor preCompact rows %d", q("c1"))
	}

	out = captureStdio(t, `{"session_id":"cc1","cwd":`+strconvQuote(env.root)+`,"trigger":"manual"}`, func() {
		handleClaudeCodeHook(ctx, "pre-compact", env.cfgPath)
	})
	if strings.TrimSpace(out) != `{"decision":"approve"}` || q("cc1") != 1 {
		t.Fatalf("claude pre-compact: stdout %q rows %d", out, q("cc1"))
	}
	captureStdio(t, `{"session_id":"cc1"}`, func() {
		handleClaudeCodeHook(ctx, "post-compact", env.cfgPath)
	})
	if q("cc1") != 1 {
		t.Fatalf("claude post-compact must reconcile, not add: rows %d", q("cc1"))
	}

	// A payload with no session id is answered and records nothing.
	out = captureStdio(t, `{"trigger":"auto"}`, func() {
		handlePromptSubmitOnlyHook(models.ToolQwenCode, "qwen-code", hook.PromptDialectTopLevelBlock, "UserPromptSubmit", "PreCompact", env.cfgPath)
	})
	if strings.TrimSpace(out) != `{"decision":"approve"}` {
		t.Fatalf("no-session PreCompact reply %q", out)
	}
}

// TestClaudeTaskEventsAreCaptureOnly: TaskCreated / TaskCompleted reply the
// plain approve (never a block: exit 2 would roll the task back) and land
// one task_event row each.
func TestClaudeTaskEventsAreCaptureOnly(t *testing.T) {
	ctx := context.Background()
	env := newObserveTestEnv(t, map[string]string{"t1": models.ToolClaudeCode})
	// hook_event_name is Claude Code's own PascalCase spelling: a
	// lower-case name is a foreign host's payload (hook-parity Lane C
	// foreign-replay classification) and is answered without recording.
	for ev, name := range map[string]string{"task-created": "TaskCreated", "task-completed": "TaskCompleted"} {
		out := captureStdio(t, `{"session_id":"t1","cwd":`+strconvQuote(env.root)+`,"hook_event_name":"`+name+`","task_id":"7","task_title":"Ship it"}`, func() {
			handleClaudeCodeHook(ctx, ev, env.cfgPath)
		})
		if strings.TrimSpace(out) != `{"decision":"approve"}` {
			t.Fatalf("%s reply %q", ev, out)
		}
	}
	if n := env.count(t, `SELECT COUNT(*) FROM actions WHERE action_type = ? AND session_id = 't1'`, models.ActionTaskEvent); n != 2 {
		t.Fatalf("task_event rows %d, want 2", n)
	}
	// A double fire of the same transition collapses.
	captureStdio(t, `{"session_id":"t1","task_id":"7","task_title":"Ship it"}`, func() {
		handleClaudeCodeHook(ctx, "task-created", env.cfgPath)
	})
	if n := env.count(t, `SELECT COUNT(*) FROM actions WHERE action_type = ? AND session_id = 't1'`, models.ActionTaskEvent); n != 2 {
		t.Fatalf("double fire added a row: %d", n)
	}
}
