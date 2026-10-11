package main

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/guardposture"
	"github.com/marmutapp/superbased-observer/internal/hook"
	"github.com/marmutapp/superbased-observer/internal/integration"
)

// servedToolGates lists every (tool, binding) the tool-gate path serves,
// read from the registry (data), so a new vendor row is covered here with no
// edit to this test.
func servedToolGates(t *testing.T) []struct {
	tool string
	b    integration.HookBinding
} {
	t.Helper()
	var out []struct {
		tool string
		b    integration.HookBinding
	}
	for _, c := range integration.Capabilities() {
		for _, b := range c.Hook.Bindings {
			if toolGateServed(b) && b.Dialect != hook.ToolGateDialectClaudeCode {
				out = append(out, struct {
					tool string
					b    integration.HookBinding
				}{c.Tool, b})
			}
		}
	}
	if len(out) < 6 {
		t.Fatalf("served tool gates = %d, want at least the six hook-parity Lane A bindings", len(out))
	}
	return out
}

// driveToolGateReceiver runs the real receiver an `observer hook <tool>
// <event>` invocation reaches for a tool gate, with body on stdin.
func driveToolGateReceiver(t *testing.T, tool, event, body, configPath string) {
	t.Helper()
	if b, ok := standaloneToolGate(tool, event); ok {
		handleToolGateHook(tool, tool+":"+event, b, strings.NewReader(body), configPath)
		return
	}
	fn, ok := hookReceivers[tool]
	if !ok {
		t.Fatalf("%s %s: neither the binding route nor a receiver serves this tool gate", tool, event)
	}
	withStdinT(t, body, func() { fn(context.Background(), event, configPath) })
}

// TestToolGateFailureSemantics is the shared tool-gate failure table (hook-
// parity Lane A): every served tool gate x {garbage, truncated, timeout,
// panic} x {enforced, not enforced}. Under enforcement each failure BLOCKS
// in the dialect's own shape; otherwise it answers with the dialect's own
// allow (never the legacy decision:"approve") or, on a timeout, exits 0.
func TestToolGateFailureSemantics(t *testing.T) {
	huge := `{"session_id":"s1","tool_name":"Bash","tool_input":{"command":"` + strings.Repeat("a", promptSubmitBodyLimit+16) + `"}}`
	kinds := []struct {
		name string
		run  func(t *testing.T, h *failureHarness, tool, event, cp string)
	}{
		{"garbage", func(t *testing.T, _ *failureHarness, tool, event, cp string) {
			driveToolGateReceiver(t, tool, event, "this is not json", cp)
		}},
		{"truncated", func(t *testing.T, _ *failureHarness, tool, event, cp string) {
			driveToolGateReceiver(t, tool, event, huge, cp)
		}},
		{"timeout", func(_ *testing.T, h *failureHarness, _, _, _ string) {
			hookWatchdogFire(time.Millisecond, h.exit, &h.stderr)
		}},
		{"panic", func(_ *testing.T, h *failureHarness, tool, event, _ string) {
			func() {
				defer recoverHookPanic(tool+":"+event, hookStdout, &h.stderr, h.exit)
				panic("simulated tool-gate receiver panic")
			}()
		}},
	}
	for _, g := range servedToolGates(t) {
		for _, enforced := range []bool{true, false} {
			for _, k := range kinds {
				name := fmt.Sprintf("%s %s/enforced=%v/%s", g.tool, g.b.NativeEvent, enforced, k.name)
				t.Run(name, func(t *testing.T) {
					dir := t.TempDir()
					cp := filepath.Join(dir, "config.toml")
					mode := "observe"
					if enforced {
						mode = "enforce"
					}
					writeFileT(t, cp, fmt.Sprintf("[observer]\ndb_path = %q\n[guard]\nenabled = true\nmode = %q\n", filepath.Join(dir, "o.db"), mode))
					if enforced {
						writePosture(t, cp, enforcedPosture)
					} else {
						writePosture(t, cp, guardposture.Posture{Enforced: false, Source: guardposture.SourceLocal})
					}
					plan := resolveHookFailurePlan(g.tool, g.b.NativeEvent, cp)
					if plan.Cap != integration.HookCapToolGate || plan.FailClosed != enforced {
						t.Fatalf("plan = %+v, want a tool_gate plan failClosed=%v", plan, enforced)
					}
					h := newFailureHarness(t, plan)
					k.run(t, h, g.tool, g.b.NativeEvent, cp)
					out := h.stdout.String()
					allow := string(hook.ToolGateAllowReply(g.b.Dialect))

					if !enforced {
						switch k.name {
						case "timeout":
							if out != "" || fmt.Sprint(h.exitCodes()) != "[0]" {
								t.Errorf("fail-open timeout: stdout %q exits %v", out, h.exitCodes())
							}
						default:
							if out != allow {
								t.Errorf("stdout = %q, want the dialect's allow %q", out, allow)
							}
						}
						if strings.Contains(out, "approve") {
							t.Errorf("a tool-gate host got the legacy approve: %q", out)
						}
						return
					}
					// The dialect's own deny: its stdout reply, or (an exit-code
					// host: Cascade, Kiro CLI) nothing on stdout and exit 2.
					block, _ := hook.BuildFailClosedReply(g.b.Dialect, hook.FailClosedErrorReason)
					switch k.name {
					case "timeout", "panic", "garbage":
						reason := hook.FailClosedErrorReason
						if k.name == "timeout" {
							reason = hook.FailClosedTimeoutReason
						}
						fc, _ := hook.BuildFailClosedReply(g.b.Dialect, reason)
						if out != string(fc.Stdout) {
							t.Errorf("stdout = %q, want the dialect's fail-closed deny %q", out, fc.Stdout)
						}
					case "truncated":
						if block.ExitCode == 0 && (out == allow || out == "" || (!strings.Contains(out, "exceeded the size") && !strings.Contains(out, hook.FailClosedErrorReason))) {
							t.Errorf("truncated under enforcement: stdout %q, want a hold carrying a reason", out)
						}
						if block.ExitCode != 0 && out != "" {
							t.Errorf("truncated under enforcement on an exit-code host: stdout %q, want none", out)
						}
					}
					codes := h.exitCodes()
					if block.ExitCode != 0 && (len(codes) == 0 || codes[0] != block.ExitCode) {
						t.Errorf("an exit-code tool-gate dialect exited %v, want %d", codes, block.ExitCode)
					}
					for _, c := range codes {
						if block.ExitCode == 0 && c != 0 {
							t.Errorf("a JSON tool-gate dialect exited %d", c)
						}
					}
				})
			}
		}
	}
}

// TestToolGateRouting pins which receiver serves each tool gate: the
// binding-driven route for a mechanism without its own receiver, the
// mechanism's receiver otherwise (it rides other work on the event).
func TestToolGateRouting(t *testing.T) {
	for _, g := range servedToolGates(t) {
		c, _ := integration.For(g.tool)
		_, own := toolGateOwnReceiver[c.Hook.Mechanism]
		_, standalone := standaloneToolGate(g.tool, g.b.NativeEvent)
		if own == standalone {
			t.Errorf("%s %s: own receiver=%v, binding route=%v (exactly one must serve it)", g.tool, g.b.NativeEvent, own, standalone)
		}
		if own {
			if _, ok := hookReceivers[g.tool]; !ok {
				t.Errorf("%s: mechanism %s claims its own receiver but hookReceivers has none", g.tool, c.Hook.Mechanism)
			}
		}
	}
	// Non-gate events of the same tools keep their existing receiver.
	if _, ok := standaloneToolGate("qwen-code", "UserPromptSubmit"); ok {
		t.Error("qwen-code UserPromptSubmit was routed to the tool gate")
	}
	if _, ok := standaloneToolGate("claude-code", "pre-tool"); ok {
		t.Error("claude-code PreToolUse must stay on its own receiver (rewrite + effort)")
	}
}

// toolGateBlockWords matches a block in any stdout tool-gate dialect: the
// deny / ask words (Antigravity's force_ask, Goose's block) or Cline's
// cancel.
var toolGateBlockWords = regexp.MustCompile(`"(deny|ask|force_ask|block)"|"cancel":true`)

// toolGateE2EFixture is a destructive call ("rm -rf ~/", or a write to
// ~/.ssh where the event carries no command) in the payload shape of the
// binding's dialect, with the vendor's native tool name.
func toolGateE2EFixture(tool, event, dialect, dir string) string {
	switch dialect {
	case hook.ToolGateDialectAntigravity:
		return fmt.Sprintf(`{"conversationId":"s1","workspacePaths":[%q],"hookEventName":%q,"toolCall":{"name":"run_command","args":{"CommandLine":"rm -rf ~/"}}}`, dir, event)
	case hook.ToolGateDialectCopilotCLI:
		return fmt.Sprintf(`{"sessionId":"s1","timestamp":1,"cwd":%q,"toolName":"bash","toolArgs":"{\"command\":\"rm -rf ~/\"}"}`, dir)
	case hook.ToolGateDialectGoose:
		return fmt.Sprintf(`{"session_id":"s1","working_dir":%q,"hook_event_name":%q,"tool_name":"shell","tool_input":{"command":"rm -rf ~/"}}`, dir, event)
	case hook.ToolGateDialectCline:
		// T-342: served under cline-cli, so the CLI's tool vocabulary.
		return fmt.Sprintf(`{"hookName":"tool_call","sessionContext":{"sessionId":"s1"},"workspaceRoots":[%q],"tool_call":{"id":"t1","name":"run_commands","input":{"command":"rm -rf ~/"}}}`, dir)
	case hook.ToolGateDialectCascade:
		info := map[string]string{
			"pre_run_command":  fmt.Sprintf(`{"command_line":"rm -rf ~/","cwd":%q}`, dir),
			"pre_write_code":   `{"file_path":"~/.ssh/authorized_keys","edits":[{"old_string":"","new_string":"k"}]}`,
			"pre_read_code":    `{"file_path":"~/.ssh/id_rsa"}`,
			"pre_mcp_tool_use": `{"mcp_server_name":"shell","mcp_tool_name":"exec","mcp_tool_arguments":{}}`,
		}[event]
		return fmt.Sprintf(`{"agent_action_name":%q,"trajectory_id":"s1","tool_info":%s}`, event, info)
	}
	native := map[string]string{
		"codex": "Bash", "qwen-code": "run_shell_command", "droid": "Execute",
		"qoder": "Bash", "gemini-cli": "run_shell_command", "cursor": "Write",
		"grok": "run_terminal_command", "mistral-code": "bash", "poolside": "shell",
		"junie": "bash", "crush": "bash", "zcode": "Bash", "command-code": "Bash",
		"open-interpreter": "Bash", "muse": "bash", "devin": "exec", "kimi-code": "Bash",
	}[tool]
	input := `{"command":"rm -rf ~/"}`
	if tool == "cursor" {
		input = `{"file_path":"~/.ssh/authorized_keys","contents":"k"}`
	}
	return fmt.Sprintf(`{"session_id":"s1","sessionId":"s1","conversation_id":"s1","cwd":%q,"hook_event_name":%q,"tool_name":%q,"tool_input":%s}`, dir, event, native, input)
}

// TestToolGateReceiversAnswerEndToEnd drives each served tool gate's real
// receiver with a guard-enforced config and a destructive command fixture:
// the reply is the dialect's deny (or Qwen/Droid/Qoder's native ask for an
// ask rule) and the allow fixture writes the dialect's allow.
func TestToolGateReceiversAnswerEndToEnd(t *testing.T) {
	for _, g := range servedToolGates(t) {
		t.Run(g.tool+" "+g.b.NativeEvent, func(t *testing.T) {
			if g.b.NativeEvent == "pre_mcp_tool_use" {
				// No builtin rule judges an arbitrary MCP call; the MCP
				// path of this dialect is pinned with a rule evaluator in
				// internal/hook TestToolGateDialects.
				t.Skip("no builtin rule blocks an MCP call")
			}
			dir := t.TempDir()
			cp := filepath.Join(dir, "config.toml")
			writeFileT(t, cp, fmt.Sprintf("[observer]\ndb_path = %q\n[guard]\nenabled = true\nmode = \"enforce\"\n", filepath.Join(dir, "o.db")))
			writePosture(t, cp, enforcedPosture)
			body := toolGateE2EFixture(g.tool, g.b.NativeEvent, g.b.Dialect, dir)
			h := newFailureHarness(t, resolveHookFailurePlan(g.tool, g.b.NativeEvent, cp))
			driveToolGateReceiver(t, g.tool, g.b.NativeEvent, body, cp)
			out := h.stdout.String()
			if fc, _ := hook.BuildFailClosedReply(g.b.Dialect, "x"); fc.ExitCode != 0 {
				// An exit-code host (Cascade): the block is the exit code
				// alone, nothing on stdout.
				if codes := h.exitCodes(); out != "" || len(codes) == 0 || codes[0] != fc.ExitCode {
					t.Fatalf("%s %s: stdout %q exits %v, want exit %d and no stdout", g.tool, g.b.NativeEvent, out, codes, fc.ExitCode)
				}
				return
			}
			if !toolGateBlockWords.MatchString(out) {
				t.Fatalf("%s %s: stdout %q, want the dialect's block", g.tool, g.b.NativeEvent, out)
			}
			if bytes.Count([]byte(out), []byte("\n")) != 1 {
				t.Errorf("want exactly one reply line, got %q", out)
			}
		})
	}
}

// TestAutoRegisterOwesLiveIsCurrent: every autoRegisterOwesLive entry is a
// Registered binding that is still not live-grounded (once it goes live,
// remove the entry so `observer start` writes it), and the skip applies to
// the native and the -windows target alike.
func TestAutoRegisterOwesLiveIsCurrent(t *testing.T) {
	for key := range autoRegisterOwesLive {
		tool, event, ok := strings.Cut(key, " ")
		if !ok {
			t.Fatalf("malformed key %q", key)
		}
		c, ok := integration.For(tool)
		if !ok {
			t.Fatalf("%q: no registry row", key)
		}
		b, ok := c.Hook.Binding(event)
		if !ok || !b.Registered {
			t.Errorf("%q: not a Registered binding; remove the entry", key)
		}
		if b.Grounding.Kind == integration.GroundingLive {
			t.Errorf("%q is live-grounded now: remove it from autoRegisterOwesLive", key)
		}
		if !autoRegisterSkipEvent(tool, event) || !autoRegisterSkipEvent(tool+"-windows", event) {
			t.Errorf("%q: the auto-register filter does not skip it", key)
		}
	}
	if autoRegisterSkipEvent("codex", "PreToolUse") {
		t.Error("a live-grounded binding must not be skipped")
	}
}
