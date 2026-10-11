package hook

import "testing"

// One case per signature row, plus genuine Claude Code payloads and
// undecodable input. Payload shapes are anonymized from cursor-agent's
// hook request (executeHookForStep: the step payload plus session_id,
// hook_event_name, cursor_version, workspace_roots, user_email,
// transcript_path) and from Claude Code's documented hook envelope.
func TestForeignClaudeHookHost(t *testing.T) {
	for _, tc := range []struct {
		name     string
		body     string
		wantHost string
		wantOK   bool
	}{
		{
			"cursor_prompt_with_version",
			`{"conversation_id":"c0c0c001-0000-4000-8000-000000000001","generation_id":"g1","prompt":"hi","session_id":"c0c0c001-0000-4000-8000-000000000001","hook_event_name":"beforeSubmitPrompt","cursor_version":"2026.09.18-9a7762b","workspace_roots":["/home/dev/repo"],"user_email":null,"transcript_path":null}`,
			"cursor", true,
		},
		{
			"cursor_step_name_without_version",
			`{"conversation_id":"c1","session_id":"c1","hook_event_name":"preToolUse","tool_name":"Shell","tool_input":{"command":"ls"}}`,
			"cursor", true,
		},
		{
			"cursor_version_empty_string_still_cursor",
			`{"session_id":"c2","hook_event_name":"sessionEnd","cursor_version":""}`,
			"cursor", true,
		},
		{
			"claude_code_pre_tool",
			`{"session_id":"s1","transcript_path":"/home/dev/.claude/projects/x/s1.jsonl","cwd":"/home/dev/repo","permission_mode":"default","hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"ls"}}`,
			"", false,
		},
		{
			"claude_code_prompt",
			`{"session_id":"s1","cwd":"/r","hook_event_name":"UserPromptSubmit","prompt":"hi"}`,
			"", false,
		},
		{"claude_code_stop_pascal_case", `{"session_id":"s1","cwd":"/r","hook_event_name":"Stop"}`, "", false},
		{
			"grok_build_replay",
			`{"session_id":"g1","sessionId":"g1","hook_event_name":"SessionEnd","hookEventName":"session_end","cwd":"/r","workspaceRoot":"/r"}`,
			"grok", true,
		},
		{"unidentified_lower_case_event", `{"session_id":"x1","hook_event_name":"afterSomething"}`, "unidentified", true},
		{"unidentified_camel_session_only", `{"sessionId":"x2","hook_event_name":"Stop"}`, "unidentified", true},
		{"unidentified_conversation_only", `{"conversation_id":"x3","hook_event_name":"Stop"}`, "unidentified", true},
		{"undecodable", `{"session_id":`, "", false},
		{"empty", ``, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			host, ok := ForeignClaudeHookHost([]byte(tc.body))
			if host != tc.wantHost || ok != tc.wantOK {
				t.Fatalf("ForeignClaudeHookHost = %q,%v want %q,%v", host, ok, tc.wantHost, tc.wantOK)
			}
		})
	}
}

// TestClassifyClaudeHookPayload pins the verdict per signature row: the
// action, the dispatch tool and the exact reply bytes.
func TestClassifyClaudeHookPayload(t *testing.T) {
	for _, tc := range []struct {
		name       string
		body       string
		wantAction ForeignAction
		wantTool   string
		wantReply  string
	}{
		{"cursor_prompt", `{"session_id":"c","hook_event_name":"beforeSubmitPrompt","cursor_version":"2026.10.01"}`, ForeignNeutral, "", "{\"continue\":true}\n"},
		{"cursor_pre_tool", `{"session_id":"c","hook_event_name":"preToolUse","cursor_version":"2026.10.01"}`, ForeignNeutral, "", "{}\n"},
		{"cursor_session_end_no_version", `{"session_id":"c","hook_event_name":"sessionEnd"}`, ForeignNeutral, "", "{}\n"},
		{"grok_dispatch", `{"session_id":"g","sessionId":"g","hook_event_name":"Stop","hookEventName":"stop"}`, ForeignDispatch, "grok", "{}\n"},
		{"unidentified_noop", `{"sessionId":"x","hook_event_name":"Stop"}`, ForeignNoop, "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := ClassifyClaudeHookPayload([]byte(tc.body))
			if !v.Foreign || v.Action != tc.wantAction || v.Tool != tc.wantTool || string(v.Reply) != tc.wantReply {
				t.Fatalf("verdict = %+v (reply %q), want action %s tool %q reply %q", v, v.Reply, tc.wantAction, tc.wantTool, tc.wantReply)
			}
		})
	}
	if v := ClassifyClaudeHookPayload([]byte(`{"session_id":"s","hook_event_name":"SessionEnd","cwd":"/r"}`)); v.Foreign {
		t.Fatalf("a genuine Claude Code payload was classified foreign: %+v", v)
	}
}
