package main

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// writeT519Admission writes a raw budget-admission.json beside dbPath, without
// the package's writer, so the end-to-end test below compiles and runs on the
// pre-fix tree too (where the file is simply ignored: the red half).
func writeT519Admission(t *testing.T, dbPath, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(filepath.Dir(dbPath), "budget-admission.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestT519HooksStopOnThePublishedBudget is the end-to-end red/green for the
// node 1 incident: an enforce-mode managed budget the daemon has published as
// exhausted must stop the NEXT prompt and the NEXT tool call through the real
// hook receivers, in each seam's own wire shape (D-030), and must leave a
// post-hoc event alone (D-031).
//
// Pre-fix every row below answered allow: the hook path never consulted a
// budget.
func TestT519HooksStopOnThePublishedBudget(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "observer.db")
	configPath := filepath.Join(dir, "config.toml")
	// The hook reads the admission only on a node with an org client (D-359,
	// P3-05): the publisher runs nowhere else. No server URL is needed; the
	// hook never contacts the organization.
	const orgOn = "[org_client]\nenabled = true\n"
	cfgBody := "[observer]\ndb_path = " + strconv.Quote(filepath.ToSlash(dbPath)) + "\n\n" +
		"[guard]\nenabled = true\nmode = \"enforce\"\n\n" + orgOn
	if err := os.WriteFile(configPath, []byte(cfgBody), 0o600); err != nil {
		t.Fatal(err)
	}
	var exitCodes []int
	prevExit := hookOSExit
	hookOSExit = func(code int) { exitCodes = append(exitCodes, code) }
	t.Cleanup(func() { hookOSExit = prevExit })

	run := func(handler func(context.Context, string, string), event, payload string) (stdout, stderr string) {
		t.Helper()
		hookReplied, hookPendingExitCode, hookExpectsStdoutReply = false, 0, true
		stdinR, stdinW, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		go func() {
			_, _ = stdinW.Write([]byte(payload))
			_ = stdinW.Close()
		}()
		stdoutR, stdoutW, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		stderrR, stderrW, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		oldIn, oldOut, oldErr := os.Stdin, os.Stdout, os.Stderr
		os.Stdin, os.Stdout, os.Stderr = stdinR, stdoutW, stderrW
		handler(context.Background(), event, configPath)
		os.Stdin, os.Stdout, os.Stderr = oldIn, oldOut, oldErr
		_ = stdoutW.Close()
		_ = stderrW.Close()
		o, _ := io.ReadAll(stdoutR)
		e, _ := io.ReadAll(stderrR)
		return string(o), string(e)
	}

	const conv = "c0c0c001-0000-4000-8000-000000051900"
	cursorPayload := func(step, extra string) string {
		return `{"conversation_id":"` + conv + `","generation_id":"g-1","model":"cursor-grok-4.5-high-fast",` + extra +
			`"hook_event_name":"` + step + `","cursor_version":"2026.10.01-14929f9",` +
			`"workspace_roots":["` + filepath.ToSlash(dir) + `"],"user_email":"dev@example.test","transcript_path":null}`
	}
	tomorrow := time.Now().UTC().Add(24 * time.Hour).Format(time.RFC3339)
	// The daemon keeps a file with a stop fresh (D-359, P3-05), so the fixture
	// is written "now"; the stale and org-off cases below age or unplug it.
	writtenNow := time.Now().UTC().Format(time.RFC3339)
	stopAll := `{"schema":1,"written_at":"` + writtenNow + `","stops":[{"rule_id":"B-602",` +
		`"reason":"organization daily budget (you, blocking) reached: daily spend $0.42 exceeds the $0.30 budget",` +
		`"valid_until":"` + tomorrow + `"}]}`
	stopCodex := `{"schema":1,"written_at":"` + writtenNow + `","stops":[{"tool":"codex","rule_id":"B-626","reason":"codex spent"}]}`

	type cursorPromptReply struct {
		Continue    bool   `json:"continue"`
		UserMessage string `json:"user_message"`
	}
	type cursorToolReply struct {
		Permission   string `json:"permission"`
		UserMessage  string `json:"user_message"`
		AgentMessage string `json:"agent_message"`
	}

	t.Run("cursor beforeSubmitPrompt is blocked with the budget and the request flow", func(t *testing.T) {
		writeT519Admission(t, dbPath, stopAll)
		out, _ := run(cursorReceiverForTest(t), "beforeSubmitPrompt", cursorPayload("beforeSubmitPrompt", `"prompt":"what is the logical next step?","attachments":[],`))
		var r cursorPromptReply
		if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &r); err != nil {
			t.Fatalf("reply %q is not Cursor's JSON prompt reply: %v", out, err)
		}
		if r.Continue || !strings.Contains(r.UserMessage, "B-602") || !strings.Contains(r.UserMessage, "observer request budget --usd") ||
			!strings.Contains(r.UserMessage, "Requests > Ask for a budget increase") {
			t.Fatalf("reply = %+v, want continue:false naming B-602 and the budget request", r)
		}
	})
	t.Run("cursor preToolUse is denied", func(t *testing.T) {
		writeT519Admission(t, dbPath, stopAll)
		out, _ := run(cursorReceiverForTest(t), "preToolUse", cursorPayload("preToolUse",
			`"tool_name":"Shell","tool_input":{"command":"git status"},"tool_use_id":"tu-1","cwd":"`+filepath.ToSlash(dir)+`",`))
		var r cursorToolReply
		if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &r); err != nil {
			t.Fatalf("reply %q is not Cursor's JSON tool reply: %v", out, err)
		}
		if r.Permission != "deny" || !strings.Contains(r.UserMessage+r.AgentMessage, "B-602") {
			t.Fatalf("reply = %+v, want deny naming B-602", r)
		}
	})
	t.Run("claude code UserPromptSubmit blocks with exit 2 and the reason on stderr", func(t *testing.T) {
		writeT519Admission(t, dbPath, stopAll)
		before := len(exitCodes)
		_, stderr := run(handleClaudeCodeHook, "user-prompt-submit",
			`{"session_id":"s-t519","cwd":"`+filepath.ToSlash(dir)+`","hook_event_name":"UserPromptSubmit","prompt":"hello"}`)
		if len(exitCodes) != before+1 || exitCodes[before] != 2 {
			t.Fatalf("exit codes = %v, want a 2 for the blocked prompt", exitCodes[before:])
		}
		if !strings.Contains(stderr, "B-602") {
			t.Fatalf("stderr %q does not carry the budget reason", stderr)
		}
	})
	t.Run("a tool-scoped stop for another tool lets cursor through", func(t *testing.T) {
		writeT519Admission(t, dbPath, stopCodex)
		out, _ := run(cursorReceiverForTest(t), "beforeSubmitPrompt", cursorPayload("beforeSubmitPrompt", `"prompt":"hi","attachments":[],`))
		var r cursorPromptReply
		if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &r); err != nil || !r.Continue {
			t.Fatalf("reply %q, want continue:true", out)
		}
	})
	t.Run("an expired stop lets the prompt through", func(t *testing.T) {
		writeT519Admission(t, dbPath, strings.Replace(stopAll, tomorrow, "2026-10-05T00:00:00Z", 1))
		out, _ := run(cursorReceiverForTest(t), "beforeSubmitPrompt", cursorPayload("beforeSubmitPrompt", `"prompt":"hi","attachments":[],`))
		var r cursorPromptReply
		if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &r); err != nil || !r.Continue {
			t.Fatalf("reply %q, want continue:true", out)
		}
	})
	t.Run("a malformed admission file stops nothing (transport fails open)", func(t *testing.T) {
		writeT519Admission(t, dbPath, "{not json")
		out, _ := run(cursorReceiverForTest(t), "beforeSubmitPrompt", cursorPayload("beforeSubmitPrompt", `"prompt":"hi","attachments":[],`))
		var r cursorPromptReply
		if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &r); err != nil || !r.Continue {
			t.Fatalf("reply %q, want continue:true", out)
		}
	})
	t.Run("a stale stop with no live publisher lets the prompt through (P3-05)", func(t *testing.T) {
		stale := time.Now().UTC().Add(-24 * time.Hour).Format(time.RFC3339)
		writeT519Admission(t, dbPath, `{"schema":1,"written_at":"`+stale+`","stops":[{"rule_id":"B-625","reason":"unavailable"}]}`)
		out, stderr := run(cursorReceiverForTest(t), "beforeSubmitPrompt", cursorPayload("beforeSubmitPrompt", `"prompt":"hi","attachments":[],`))
		var r cursorPromptReply
		if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &r); err != nil || !r.Continue {
			t.Fatalf("reply %q, want continue:true for a day-old B-625", out)
		}
		if !strings.Contains(stderr, "stale") {
			t.Fatalf("stderr %q does not say the admission is stale", stderr)
		}
	})
	t.Run("a node with [org_client] off ignores a leftover stop (P3-05)", func(t *testing.T) {
		writeT519Admission(t, dbPath, stopAll)
		offPath := filepath.Join(dir, "config-org-off.toml")
		if err := os.WriteFile(offPath, []byte(strings.Replace(cfgBody, orgOn, "[org_client]\nenabled = false\n", 1)), 0o600); err != nil {
			t.Fatal(err)
		}
		saved := configPath
		configPath = offPath
		defer func() { configPath = saved }()
		out, _ := run(cursorReceiverForTest(t), "beforeSubmitPrompt", cursorPayload("beforeSubmitPrompt", `"prompt":"hi","attachments":[],`))
		var r cursorPromptReply
		if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &r); err != nil || !r.Continue {
			t.Fatalf("reply %q, want continue:true on a node with no org client", out)
		}
	})
	t.Run("a post-hoc event is not a pre-send seam", func(t *testing.T) {
		writeT519Admission(t, dbPath, stopAll)
		out, _ := run(cursorReceiverForTest(t), "postToolUse", cursorPayload("postToolUse",
			`"tool_name":"Shell","tool_input":{"command":"ls"},"tool_use_id":"tu-2","tool_output":"ok",`))
		if strings.Contains(out, "deny") || strings.Contains(out, `"continue":false`) {
			t.Fatalf("post-hoc reply %q must not block", out)
		}
	})
}
