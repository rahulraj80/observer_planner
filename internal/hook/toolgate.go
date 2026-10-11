package hook

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/marmutapp/superbased-observer/internal/guard"
	"github.com/marmutapp/superbased-observer/internal/models"
	"github.com/marmutapp/superbased-observer/internal/policy"
	"github.com/marmutapp/superbased-observer/internal/tooltax"
)

// Tool gate (hook-parity audit 2026-10-02, Lane A): ONE handler for every
// pre-tool hook channel, table-driven on the reply DIALECT (never on the
// tool name). A dialect row says how to read a payload into policy events
// (extract) and how to write the vendor's documented block and allow
// replies. A new vendor whose pre-tool event speaks an existing wire shape
// is one registry binding (internal/integration) naming the dialect; a new
// wire shape is one row below. The channel's capabilities (can it block,
// can it ask) come from the guard conformance matrix, which is generated
// from the same bindings, so ask degrades to deny on a deny-only host
// through guard.ResolveEmission without any branch here.

// Tool-gate dialect ids: the internal/integration HookBinding.Dialect
// values of the tool_gate bindings this table serves.
const (
	// ToolGateDialectClaudeCode is Claude Code's PreToolUse: the dual-shape
	// (permissionDecision plus legacy decision/reason) block; the receiver
	// owns the allow reply because it may rewrite the input.
	ToolGateDialectClaudeCode = "claude-code-pretool"
	// ToolGateDialectClaudeJSON is the Claude-shaped JSON family on
	// hosts other than Claude Code (Codex, Qwen Code, Factory Droid,
	// Qoder): the modern hookSpecificOutput.permissionDecision block, and
	// an empty object as the allow. The empty object matters: these hosts
	// read a legacy decision:"approve" as an EXPLICIT allow that skips the
	// host's own permission prompt (Codex rejects it outright), so the tool
	// gate never sends it.
	ToolGateDialectClaudeJSON = "claude-json-pretool"
	// ToolGateDialectGemini is Gemini CLI's BeforeTool: a top-level
	// decision:"deny" with the reason (sent to the agent as the tool
	// error) and a systemMessage (shown to the developer). Deny only.
	ToolGateDialectGemini = "gemini-beforetool"
	// ToolGateDialectCursor is Cursor's universal preToolUse: permission
	// allow|deny plus user_message / agent_message. Cursor's per-tool
	// before* events (shell, MCP, read) gate those action classes
	// already, so this dialect skips them (no double verdict).
	ToolGateDialectCursor = "cursor-pretool"
	// ToolGateDialectAntigravity is Antigravity's PreToolUse (agy CLI and
	// the desktop app share ~/.gemini/config/hooks.json): camelCase
	// toolCall.{name,args}, a REQUIRED top-level decision. deny blocks;
	// force_ask prompts even past a cached "always allow".
	ToolGateDialectAntigravity = "antigravity-pretool"
	// ToolGateDialectGrok is Grok Build's PreToolUse: camelCase payload,
	// top-level decision deny|ask, and an allow that only means "not
	// blocked" (it never auto-approves).
	ToolGateDialectGrok = "grok-pretool"
	// ToolGateDialectCopilotCLI is Copilot CLI's preToolUse: camelCase
	// payload whose toolArgs may be a JSON-encoded string, top-level
	// permissionDecision deny|ask.
	ToolGateDialectCopilotCLI = "copilot-cli-pretool"
	// ToolGateDialectVibe is Mistral vibe's pre_tool: JSON decision only
	// (exit 2 is a hook failure, not a block).
	ToolGateDialectVibe = "vibe-pretool"
	// ToolGateDialectGoose is Goose's Open Plugins PreToolUse: the cwd is
	// working_dir, a block is decision:"block"; the allow is silence.
	ToolGateDialectGoose = "goose-pretool"
	// ToolGateDialectCline is the Cline SDK hook runtime's file-per-event
	// PreToolUse: cancel stops the run (no per-call deny exists); exit codes
	// are ignored, so the allow is silence.
	ToolGateDialectCline = "cline-pretool"
	// ToolGateDialectPoolside is Poolside's snake_case PreToolUse block.
	ToolGateDialectPoolside = "poolside-pretool"
	// ToolGateDialectJunie is Junie CLI's PreToolUse: top-level decision
	// deny|ask.
	ToolGateDialectJunie = "junie-pretool"
	// ToolGateDialectCrush is Crush's PreToolUse: top-level decision deny;
	// an explicit allow would skip the permission prompt, so the allow is an
	// empty object (decision null falls through).
	ToolGateDialectCrush = "crush-pretool"
	// ToolGateDialectCascade is Windsurf / Devin Desktop Cascade's per-action
	// pre hooks: no tool name (the event names the action), the input under
	// tool_info, and a block that is exit code 2 with the reason on stderr
	// (shown to the developer).
	ToolGateDialectCascade = "cascade-pretool"
	// ToolGateDialectKiro is Kiro CLI's preToolUse: exit code 2 blocks and
	// stderr goes back to the agent; stdout on exit 0 joins the agent's
	// context, so the allow is silence.
	ToolGateDialectKiro = "kiro-pretool"
	// ToolGateDialectKimi is Kimi Code's PreToolUse (T-378): snake_case
	// Claude-shaped payload (tool_name, tool_input); exit code 2 blocks and
	// stderr is the reason written back to the model; stdout on exit 0 is
	// added to the model context, so the allow is silence.
	ToolGateDialectKimi = "kimi-pretool"
)

// ToolGateRequest is one tool-gate invocation.
type ToolGateRequest struct {
	// Tool is the registry tool id (models.Tool*): the policy event's tool
	// and the conformance-matrix row the channel capabilities come from.
	Tool string
	// Dialect is the binding's tool-gate dialect id.
	Dialect string
	// NativeEvent is the vendor's event name (the conformance channel is
	// "hook:"+NativeEvent, and the reply's hookEventName).
	NativeEvent string
	// Label names the invocation in forensics and stderr ("codex:PreToolUse").
	Label string
	// Body is the raw payload (UTF-8 BOM already stripped or not: the
	// extractor strips it).
	Body []byte
	// Truncated is true when the caller's read hit its bound: the body is
	// not the whole payload and cannot be evaluated.
	Truncated bool
	// Enforcing is true when guard enforcement is in force; together with
	// Truncated it holds the call (ask where the host can ask, else deny)
	// instead of approving it unexamined (SR27-D2, generalized).
	Enforcing bool
	// Decided, when non-nil, is called with the block's exit code right
	// after a block (deny / ask) reply is written and BEFORE anything else
	// runs on that path (forensics, persist). The cmd layer marks the
	// verdict decided there (markHookDecided), so a panic or a watchdog
	// fire during persist delivers the block and never a second (allow)
	// reply or an exit 0 on an exit-code dialect (review M2-11).
	Decided func(exitCode int)
	// Unclassified, when non-nil, is called AFTER the caller's reply
	// (through ToolGateResult.RecordAfterReply) for a wired gate call the
	// gate could not classify: a tool name the shared taxonomy does not
	// know, or a known judged tool whose operand is missing. Such a call
	// passes unjudged; the callback records that (rate-limited by the cmd
	// layer) instead of letting it pass silently (review M2-12). nativeTool
	// is "" when the payload names no tool at all.
	Unclassified func(nativeTool string)
}

// ToolGateResult is what HandleToolGate decided.
type ToolGateResult struct {
	// Replied is true when HandleToolGate wrote the block (deny / ask)
	// reply; the caller must not write another. On the allow path nothing
	// is written and the caller owns the reply (WriteToolGateAllow, or its
	// own rewrite / approve).
	Replied bool
	// ExitCode is the process exit code that carries the reply (0 for
	// every JSON dialect in the table today).
	ExitCode int
	// Evaluated is true when the payload yielded at least one evaluable
	// tool event (false: unknown tool or no operand, an allow).
	Evaluated bool
	// RecordAfterReply, when non-nil, persists a record-worthy verdict on
	// the allow path; the caller runs it AFTER writing its own reply.
	RecordAfterReply func()
}

// toolGateDialect is one row of the dialect table.
type toolGateDialect struct {
	// extract reads the payload into the policy events to judge (one per
	// target: an apply_patch touching three files is three events) and
	// the native tool name for forensics. ok=false means "not an evaluable
	// tool shape" (allow; unknown is never a violation).
	extract func(req ToolGateRequest) (events []policy.Event, nativeTool string, ok bool)
	// block renders a blocking emission (Permission "deny" or "ask") as the
	// stdout reply; nil (or a nil result) writes nothing on stdout (an
	// exit-code-only dialect).
	block func(em guard.Emission, nativeEvent string) any
	// allow is the dialect's neutral allow reply; nil means the receiver
	// owns the allow (Claude Code's approve / rewrite), or, with
	// silentAllow, that an allow writes nothing at all.
	allow any
	// blockExitCode is the process exit code that carries a block (0: the
	// stdout reply carries it).
	blockExitCode int
	// stderrReason is true when the host shows the hook's stderr as the
	// block reason: on a block the reason, and nothing else, goes to
	// stderr (the T-048 rule the prompt dialects follow).
	stderrReason bool
	// silentAllow is true when the host's allow is an empty stdout (an
	// exit-code-only host, or one whose only documented allow is silence:
	// an explicit allow value there would skip the host's own permission
	// prompt or is undocumented).
	silentAllow bool
}

// topDecisionReply is the top-level {decision, reason} block several hosts
// read on their pre-tool event (Antigravity, Grok Build, Junie CLI, Crush,
// Mistral vibe, Goose; the decision word is the dialect's).
type topDecisionReply struct {
	Decision string `json:"decision"`
	Reason   string `json:"reason,omitempty"`
}

// topDecision returns a block builder speaking a top-level decision: the
// emission's permission mapped through words ("deny" / "ask" to the
// host's spelling).
func topDecision(words map[string]string) func(guard.Emission, string) any {
	return func(em guard.Emission, _ string) any {
		w, ok := words[em.Permission]
		if !ok {
			w = words["deny"]
		}
		return topDecisionReply{Decision: w, Reason: em.Reason}
	}
}

// poolsideToolReply is Poolside's snake_case PreToolUse block.
type poolsideToolReply struct {
	HookSpecificOutput struct {
		PermissionDecision       string `json:"permission_decision"`
		PermissionDecisionReason string `json:"permission_decision_reason"`
	} `json:"hook_specific_output"`
}

// copilotToolReply is Copilot CLI's preToolUse block (top-level, camelCase).
type copilotToolReply struct {
	PermissionDecision       string `json:"permissionDecision"`
	PermissionDecisionReason string `json:"permissionDecisionReason"`
}

// clineToolReply is the Cline SDK hook runtime's PreToolUse cancel.
type clineToolReply struct {
	Cancel       bool   `json:"cancel"`
	ErrorMessage string `json:"errorMessage"`
}

// claudeJSONPermissionOut is the modern Claude-shaped PreToolUse envelope
// (no legacy decision field).
type claudeJSONPermissionReply struct {
	HookSpecificOutput claudeCodePermissionOut `json:"hookSpecificOutput"`
}

// geminiToolReply is Gemini CLI's BeforeTool block.
type geminiToolReply struct {
	Decision      string `json:"decision"`
	Reason        string `json:"reason"`
	SystemMessage string `json:"systemMessage"`
}

// cursorToolReply is Cursor's preToolUse reply.
type cursorToolReply struct {
	Permission   string `json:"permission"`
	Continue     bool   `json:"continue"`
	UserMessage  string `json:"user_message,omitempty"`
	AgentMessage string `json:"agent_message,omitempty"`
}

// toolGateDialects is THE tool-gate dialect table.
var toolGateDialects = map[string]toolGateDialect{
	ToolGateDialectClaudeCode: {
		extract: func(req ToolGateRequest) ([]policy.Event, string, bool) {
			ev, ok := BuildClaudeCodeEvent(req.Body)
			if !ok {
				return nil, "", false
			}
			return []policy.Event{ev}, payloadFieldString(req.Body, "tool_name"), true
		},
		block: func(em guard.Emission, _ string) any { return buildClaudeCodeBlockReply(em) },
	},
	ToolGateDialectClaudeJSON: {
		extract: genericToolExtractor(nil),
		block: func(em guard.Emission, nativeEvent string) any {
			return claudeJSONPermissionReply{HookSpecificOutput: claudeCodePermissionOut{
				HookEventName: nativeEvent, PermissionDecision: em.Permission, PermissionDecisionReason: em.Reason,
			}}
		},
		allow: struct{}{},
	},
	ToolGateDialectGemini: {
		extract: genericToolExtractor(nil),
		block: func(em guard.Emission, _ string) any {
			// Deny only: the binding declares no ask, so ResolveEmission
			// has already turned an ask into a deny.
			return geminiToolReply{Decision: "deny", Reason: em.Reason, SystemMessage: em.Reason}
		},
		allow: struct{}{},
	},
	ToolGateDialectCursor: {
		// beforeShellExecution / beforeMCPExecution / beforeReadFile gate
		// these canonical classes with the native ask; judging them again
		// here would double every verdict row. A content search (Grep) is
		// NOT among them, although the gate judges it as a read.
		extract: genericToolExtractor(map[string]bool{
			models.ActionRunCommand: true, models.ActionMCPCall: true, models.ActionReadFile: true,
		}),
		block: func(em guard.Emission, _ string) any {
			return cursorToolReply{Permission: "deny", Continue: true, UserMessage: em.Reason, AgentMessage: em.Reason}
		},
		allow: cursorReply{Permission: "allow", Continue: true},
	},
	ToolGateDialectAntigravity: {
		extract: shapedToolExtractor(toolPayloadShape{
			session: []string{"conversationId"}, root: []string{"workspacePaths"},
			tool: []string{"toolCall.name"}, input: []string{"toolCall.args"},
		}, nil),
		// Deny only: live (agy 1.2.16) force_ask under
		// --dangerously-skip-permissions ran the command, so the binding
		// lists no ask and ResolveEmission has turned an ask into a deny.
		block: topDecision(map[string]string{"deny": "deny"}),
		// Live: an empty object {} DENIES the call (decision is required)
		// and decision:allow is documented to auto-approve past the
		// developer's own prompt; an empty stdout leaves the call to agy's
		// own permission flow.
		silentAllow: true,
	},
	ToolGateDialectGrok: {
		extract: genericToolExtractor(nil),
		// Deny only: live (grok 1.0.46) an ask under --always-approve ran
		// the command, so the binding lists no ask.
		block: topDecision(map[string]string{"deny": "deny"}),
		// allow "means only not blocked" (vendor doc; live, ls ran): it
		// never skips the permission prompt.
		allow: topDecisionReply{Decision: "allow"},
	},
	ToolGateDialectCopilotCLI: {
		extract: shapedToolExtractor(toolPayloadShape{
			session: []string{"sessionId", "session_id"}, cwd: []string{"cwd"},
			tool: []string{"toolName", "tool_name"}, input: []string{"toolArgs", "tool_input"},
		}, nil),
		block: func(em guard.Emission, _ string) any {
			return copilotToolReply{PermissionDecision: em.Permission, PermissionDecisionReason: em.Reason}
		},
		// "allow" skips the normal prompting (vendor doc); an empty object
		// is no decision.
		allow: struct{}{},
	},
	ToolGateDialectVibe: {
		extract: genericToolExtractor(nil),
		block:   topDecision(map[string]string{"deny": "deny"}),
		allow:   struct{}{},
	},
	ToolGateDialectGoose: {
		extract: shapedToolExtractor(toolPayloadShape{
			session: []string{"session_id"}, cwd: []string{"working_dir", "cwd"},
			tool: []string{"tool_name"}, input: []string{"tool_input"},
		}, nil),
		block:       topDecision(map[string]string{"deny": "block"}),
		silentAllow: true,
	},
	ToolGateDialectCline: {
		extract: shapedToolExtractor(toolPayloadShape{
			session: []string{"taskId", "sessionContext.sessionId", "sessionContext.rootSessionId"}, root: []string{"workspaceRoots"},
			tool: []string{"tool_call.name", "preToolUse.toolName"}, input: []string{"tool_call.input"},
			// The SDK hook runtime the extension and the CLI share names
			// tools in the CLI vocabulary (run_commands, read_files,
			// editor, apply_patch): live, cline 3.0.68 on demo node 1,
			// 2026-10-04. The gate is registered for cline-cli (T-342), so
			// that is the host's own vocabulary; the extension's
			// (execute_command, read_file) is the fallback, for a payload
			// the shared runtime names in the extension's words.
			vocab: []string{models.ToolCline},
		}, nil),
		block: func(em guard.Emission, _ string) any {
			return clineToolReply{Cancel: true, ErrorMessage: em.Reason}
		},
		silentAllow: true,
	},
	ToolGateDialectPoolside: {
		extract: genericToolExtractor(nil),
		block: func(em guard.Emission, _ string) any {
			var r poolsideToolReply
			r.HookSpecificOutput.PermissionDecision = "deny"
			r.HookSpecificOutput.PermissionDecisionReason = em.Reason
			return r
		},
		// The documented allow is an empty stdout ("observe only").
		silentAllow: true,
	},
	ToolGateDialectJunie: {
		extract: genericToolExtractor(nil),
		block:   topDecision(map[string]string{"deny": "deny", "ask": "ask"}),
		allow:   struct{}{},
	},
	ToolGateDialectCrush: {
		extract: genericToolExtractor(nil),
		block:   topDecision(map[string]string{"deny": "deny"}),
		allow:   struct{}{},
	},
	ToolGateDialectCascade: {
		extract: shapedToolExtractor(toolPayloadShape{
			session: []string{"trajectory_id"}, cwd: []string{"tool_info.cwd"},
			input: []string{"tool_info"},
			eventTools: map[string]string{
				"pre_run_command": "run_command", "pre_write_code": "edit_file", "pre_read_code": "read_file",
			},
			mcpServer: []string{"tool_info.mcp_server_name"}, mcpTool: []string{"tool_info.mcp_tool_name"},
		}, nil),
		blockExitCode: 2, stderrReason: true, silentAllow: true,
	},
	ToolGateDialectKiro: {
		extract:       genericToolExtractor(nil),
		blockExitCode: 2, stderrReason: true, silentAllow: true,
	},
	ToolGateDialectKimi: {
		extract:       genericToolExtractor(nil),
		blockExitCode: 2, stderrReason: true, silentAllow: true,
	},
}

// IsToolGateDialect reports whether d is a row of the tool-gate table.
func IsToolGateDialect(d string) bool {
	_, ok := toolGateDialects[d]
	return ok
}

// ToolGateDialects returns the tool-gate dialect ids, sorted.
func ToolGateDialects() []string {
	out := make([]string, 0, len(toolGateDialects))
	for d := range toolGateDialects {
		out = append(out, d)
	}
	sort.Strings(out)
	return out
}

// ToolGateAllowReply returns the dialect's neutral allow reply as bytes
// (nil when the dialect has none: the receiver owns its allow).
func ToolGateAllowReply(dialect string) []byte {
	d, ok := toolGateDialects[dialect]
	if !ok || d.allow == nil {
		return nil
	}
	return encodeReply(d.allow)
}

// WriteToolGateAllow writes the dialect's neutral allow reply (nothing for
// a dialect whose receiver owns the allow).
func WriteToolGateAllow(dialect string, stdout io.Writer) {
	if b := ToolGateAllowReply(dialect); b != nil {
		_, _ = stdout.Write(b)
	}
}

// HandleDialectAllow writes the "nothing to say" reply of ANY reply
// dialect (with HandleApprove's forensics): a tool-gate dialect's neutral
// allow, else the prompt dialect's allow (HandlePromptAllow, which falls
// back to the generic approve). The recovered-panic path uses it so a
// fail-open crash on a tool-gate host answers with that host's own allow,
// never the legacy decision:"approve" (which some Claude-shaped hosts read
// as an explicit allow that skips their own permission prompt).
func HandleDialectAllow(dialect, event string, stdin io.Reader, stdout, stderr io.Writer) {
	if d, ok := toolGateDialects[dialect]; ok {
		switch {
		case d.silentAllow:
			// An exit-code-only host parses no stdout: an allow is silence.
			_, _ = io.Copy(io.Discard, io.LimitReader(stdin, 2*1024*1024))
			return
		case d.allow != nil:
			handleAllow(event, stdin, stdout, stderr, d.allow)
			return
		}
	}
	HandlePromptAllow(dialect, event, stdin, stdout, stderr)
}

// ToolGateAllowWritesStdout reports whether dialect answers on stdout at all
// (false for an exit-code-only tool-gate dialect; true for every other
// dialect, tool gate or not).
func ToolGateAllowWritesStdout(dialect string) bool {
	d, ok := toolGateDialects[dialect]
	return !ok || !d.silentAllow
}

// ToolGateDialectStderrReason reports whether dialect's host shows the
// hook's stderr as the block reason (so a receiver writes no diagnostics
// there).
func ToolGateDialectStderrReason(dialect string) bool {
	d, ok := toolGateDialects[dialect]
	return ok && d.stderrReason
}

// toolGateBlock is a dialect's rendered block: the stdout bytes (nil =
// nothing on stdout), the exit code that carries it, and whether stderr is
// the host's reason channel.
type toolGateBlock struct {
	stdout       []byte
	exitCode     int
	stderrReason bool
}

// toolGateBlockFor renders the dialect's block for em ("deny" or "ask").
func toolGateBlockFor(dialect, nativeEvent string, em guard.Emission) (toolGateBlock, bool) {
	d, ok := toolGateDialects[dialect]
	if !ok || (d.block == nil && d.blockExitCode == 0) {
		return toolGateBlock{}, false
	}
	out := toolGateBlock{exitCode: d.blockExitCode, stderrReason: d.stderrReason}
	if d.block != nil {
		if r := d.block(em, nativeEvent); r != nil {
			out.stdout = encodeReply(r)
		}
	}
	return out, true
}

// writeToolGateBlock writes a rendered block: the stdout reply, then
// either the bare reason on stderr (a host that shows stderr as the
// reason) or Observer's diagnostic line. It returns the exit code that
// carries the block.
func writeToolGateBlock(b toolGateBlock, reason, diagnostic string, stdout, stderr io.Writer) int {
	if len(b.stdout) > 0 {
		_, _ = stdout.Write(b.stdout)
	}
	if b.stderrReason {
		fmt.Fprintln(stderr, reason)
	} else {
		fmt.Fprintln(stderr, diagnostic)
	}
	return b.exitCode
}

// truncatedPreToolReason is the agent/human-facing reason for the
// fail-closed hold HandleToolGate emits for a truncated payload.
const truncatedPreToolReason = "Observer guard could not evaluate this tool call: its hook payload exceeded the size the guard reads, so the call is held for your approval instead of being approved unexamined."

// HandleToolGate is the ONE tool-gate handler (generalized from
// HandleGuarded): read the payload through the dialect's extractor,
// evaluate each event, resolve the emission against the channel's
// capabilities, and, when the verdict blocks or asks, write the dialect's
// reply FIRST and persist after it (spec §6.4).
//
// Reply-ownership contract (unchanged from HandleGuarded): it writes to
// stdout ONLY when it blocks or asks (Replied=true, persist already ran).
// On the allow path it writes nothing and returns RecordAfterReply for a
// record-worthy verdict, which the caller runs after its own reply.
//
// A payload naming several targets (a patch over several files) yields one
// event per target; the most severe emission wins and is the one verdict
// persisted (persist runs at most once).
func HandleToolGate(req ToolGateRequest, ev guard.Evaluator, persist func(guard.ActionVerdict), stdout, stderr io.Writer) ToolGateResult {
	d, ok := toolGateDialects[req.Dialect]
	if !ok || ev == nil {
		return ToolGateResult{}
	}
	if req.Truncated {
		return handleTruncatedToolGate(req, stdout, stderr)
	}
	events, nativeTool, ok := d.extract(req)

	var (
		best      guard.ActionVerdict
		bestEm    guard.Emission
		bestEv    policy.Event
		bestWorth bool
		found     bool
	)
	if !ok || len(events) == 0 {
		// An exhausted managed budget stops the call whatever it is (T-519):
		// a call the policy extractor cannot classify, or one this event
		// leaves to a sibling, still spends against the cap. The budget is
		// the only verdict an unclassified call can get.
		if pe, av, stopped := toolGateBudgetStop(req, ev); stopped {
			best, bestEm, bestEv, bestWorth, found = av, guard.ResolveEmission(av.Verdict, pe.Caps), pe, true, true
			events = nil
		} else if !ok {
			// Not classifiable (an extractor's ok=false): the call passes, and a
			// caller that asked is told after its own reply (review M2-12).
			if req.Unclassified != nil {
				native := nativeTool
				return ToolGateResult{RecordAfterReply: func() { req.Unclassified(native) }}
			}
			return ToolGateResult{}
		}
	}
	for _, pe := range events {
		verdict, recordWorthy := ev.EvaluateHook(pe)
		em := guard.ResolveEmission(verdict.Verdict, pe.Caps)
		if !found || emissionRank(em, recordWorthy) > emissionRank(bestEm, bestWorth) {
			best, bestEm, bestEv, bestWorth, found = verdict, em, pe, recordWorthy, true
		}
	}
	if !found {
		return ToolGateResult{}
	}
	verdict, em := best, bestEm
	// Human first, agent second (org guardrail control wave, Track B), then
	// the deny-only ask explanation (audit section 5.7 decision table): both
	// are no-ops on an individual node's ask-capable channel, so the wire
	// reply stays byte-identical there.
	em = guard.HumanizeEmission(em, verdict, bestEv.SessionID)
	em = guard.ExplainDegradedAsk(em, verdict, bestEv.SessionID)
	verdict.Enforced = em.Enforced
	if em.DegradedFrom != "" {
		// Don't clobber a marker EvaluateHook already set (the §6.3
		// "approved" exception marker) with the empty no-degradation case.
		verdict.DegradedFrom = em.DegradedFrom
	}

	doPersist := func() {
		if persist != nil {
			persist(verdict)
		}
	}
	logForensics := func(action string) {
		appendHookEventLog(hookEvent{
			Event:        req.Label,
			Bytes:        len(req.Body),
			SessionID:    bestEv.SessionID,
			Tool:         nativeTool,
			Action:       action,
			RuleID:       verdict.Verdict.RuleID,
			Decision:     verdict.Verdict.Decision.String(),
			Severity:     verdict.Verdict.Severity.String(),
			DegradedFrom: em.DegradedFrom,
		})
	}

	if em.Permission == "ask" || em.Permission == "deny" {
		b, _ := toolGateBlockFor(req.Dialect, req.NativeEvent, em)
		code := writeToolGateBlock(b, em.Reason, fmt.Sprintf("observer-hook: %s guard %s (%s: %s)",
			req.Label, em.Permission, verdict.Verdict.RuleID, verdict.Verdict.Reason), stdout, stderr)
		if req.Decided != nil {
			req.Decided(code)
		}
		logForensics("guard:" + em.Permission)
		doPersist()
		return ToolGateResult{Replied: true, Evaluated: true, ExitCode: code}
	}
	if !bestWorth {
		return ToolGateResult{Evaluated: true}
	}
	// Flag-class (or degraded) verdict on the allow path: record it, but
	// only after the caller's reply.
	logForensics("approve+flag")
	return ToolGateResult{Evaluated: true, RecordAfterReply: doPersist}
}

// handleTruncatedToolGate holds a call whose payload the caller could not
// read in full (SR27-D2): a truncated body cannot be parsed, so without
// this an enforce-mode deny rule over its target would be approved
// unexamined. Under enforcement it asks where the channel can ask and
// denies where it cannot; otherwise nothing is written (the caller keeps
// its normal allow path).
func handleTruncatedToolGate(req ToolGateRequest, stdout, stderr io.Writer) ToolGateResult {
	if !req.Enforcing {
		return ToolGateResult{}
	}
	caps := toolGateChannelCaps(req)
	em := guard.Emission{Permission: "deny", Enforced: true, Reason: truncatedPreToolReason}
	if caps.CanAsk {
		em.Permission = "ask"
	}
	b, ok := toolGateBlockFor(req.Dialect, req.NativeEvent, em)
	if !ok {
		return ToolGateResult{}
	}
	code := writeToolGateBlock(b, em.Reason,
		fmt.Sprintf("observer-hook: %s guard %s (payload truncated, not evaluable)", req.Label, em.Permission), stdout, stderr)
	if req.Decided != nil {
		req.Decided(code)
	}
	appendHookEventLog(hookEvent{Event: req.Label, Action: "guard:" + em.Permission + ":truncated"})
	return ToolGateResult{Replied: true, ExitCode: code}
}

// emissionRank orders emissions for the multi-target pick: deny over ask
// over allow; on a tie a record-worthy verdict wins.
func emissionRank(em guard.Emission, recordWorthy bool) int {
	r := 0
	switch em.Permission {
	case "deny":
		r = 4
	case "ask":
		r = 2
	}
	if recordWorthy {
		r++
	}
	return r
}

// toolGateChannelCaps is the channel's capability row from the conformance
// matrix (generated from the registry bindings). Claude Code's dialect
// pins its own constants (claudeCodePreToolCaps, conformance-tested).
func toolGateChannelCaps(req ToolGateRequest) policy.Capabilities {
	if req.Dialect == ToolGateDialectClaudeCode {
		return claudeCodePreToolCaps
	}
	caps, _ := guard.CapabilitiesFor(req.Tool, "hook:"+req.NativeEvent)
	return caps
}

// genericToolPayload is the Claude-shaped pre-tool payload the generic
// extractor reads; every spelling a dialect of the family uses is listed
// (snake_case and camelCase; Cursor's conversation_id).
type genericToolPayload struct {
	SessionID      string          `json:"session_id"`
	SessionIDCamel string          `json:"sessionId"`
	ConversationID string          `json:"conversation_id"`
	Cwd            string          `json:"cwd"`
	ToolName       string          `json:"tool_name"`
	ToolNameCamel  string          `json:"toolName"`
	ToolInput      json.RawMessage `json:"tool_input"`
	ToolInputCamel json.RawMessage `json:"toolInput"`
	WorkspaceRoots json.RawMessage `json:"workspace_roots"`
	// WorkspaceRoot is Grok Build's camelCase single root.
	WorkspaceRoot string `json:"workspaceRoot"`
}

// genericToolExtractor returns the Claude-shaped extractor; skip names the
// CANONICAL (tooltax) action types the channel leaves to a sibling event of
// the same host.
func genericToolExtractor(skip map[string]bool) func(ToolGateRequest) ([]policy.Event, string, bool) {
	return func(req ToolGateRequest) ([]policy.Event, string, bool) {
		return extractGenericToolEvents(req, skip)
	}
}

// extractGenericToolEvents reads a Claude-shaped pre-tool payload into
// policy events: the native tool name resolves through internal/tooltax
// (action type + operand), the operand's value comes from the tool input
// under any of its field spellings, and an edit whose input is a patch
// envelope fans out into one event per file the patch touches.
func extractGenericToolEvents(req ToolGateRequest, skip map[string]bool) ([]policy.Event, string, bool) {
	var p genericToolPayload
	if err := json.Unmarshal(bytes.TrimPrefix(req.Body, []byte{0xEF, 0xBB, 0xBF}), &p); err != nil {
		return nil, "", false
	}
	native := firstNonEmpty(p.ToolName, p.ToolNameCamel)
	if native == "" {
		return nil, "", false
	}
	input := map[string]any{}
	raw := p.ToolInput
	if len(raw) == 0 {
		raw = p.ToolInputCamel
	}
	_ = json.Unmarshal(raw, &input)
	return buildToolGateEvents(req, skip, native, input, toolGateEventContext{
		session: firstNonEmpty(p.SessionID, p.SessionIDCamel, p.ConversationID),
		cwd:     p.Cwd,
		root:    firstNonEmpty(firstWorkspaceRoot(p.WorkspaceRoots), p.WorkspaceRoot),
	})
}

// toolGateEventContext is the per-payload context every judged event
// carries.
type toolGateEventContext struct {
	session, cwd, root string
	// vocab is toolPayloadShape.vocab.
	vocab []string
}

// buildToolGateEvents judges one native tool call: it resolves the native
// name through internal/tooltax, reads the operand from the tool input, and
// returns one event per target (a patch envelope fans out per file).
//
// ok is the classification answer (review M2-12): false means the call
// could not be classified (a tool name no vocabulary knows, or a judged
// tool whose operand is missing), true with no events means there is
// legitimately nothing to judge here (a known tool the guard does not
// evaluate, an action a sibling event of the host judges, a channel that is
// not a grounded pre-execution gate).
func buildToolGateEvents(req ToolGateRequest, skip map[string]bool, native string, input map[string]any, ctx toolGateEventContext) ([]policy.Event, string, bool) {
	shape, ok := resolveGateShapeIn(req.Tool, ctx.vocab, native)
	if !ok {
		return nil, native, nativeKnownIn(req.Tool, ctx.vocab, native)
	}
	if skip[shape.canonical] {
		return nil, native, true
	}
	caps := toolGateChannelCaps(req)
	if !caps.PreExecution {
		// Not a wired pre-execution channel (no conformance row): judge
		// nothing; the binding has not been grounded as a gate.
		return nil, native, true
	}
	var targets []string
	switch shape.operand {
	case tooltax.OperandMCPName:
		targets = []string{native}
	default:
		// Every operand the input carries is judged: a single-operand field
		// never shadows a batch field beside it (`command` next to
		// `commands`, review M2-12).
		targets = unionOperands(operandValue(shape.operand, input), operandList(shape.operand, input))
	}
	if len(targets) == 0 && (shape.actionType == models.ActionEditFile || shape.actionType == models.ActionWriteFile) {
		targets = patchTargetsIn(input)
	}
	if len(targets) == 0 {
		return nil, native, false
	}
	caps.Sandboxed = sandboxedChild()
	base := policy.Event{
		Kind:        shape.kind,
		ActionType:  shape.actionType,
		Tool:        req.Tool,
		SessionID:   ctx.session,
		Cwd:         ctx.cwd,
		ProjectRoot: ctx.root,
		Caps:        caps,
		Dialect:     shape.dialect,
		Now:         time.Now().UTC(),
	}
	events := make([]policy.Event, 0, len(targets))
	for _, t := range targets {
		e := base
		e.Target = t
		events = append(events, e)
	}
	return events, native, true
}

// toolPayloadShape names where a vendor's pre-tool payload carries each
// field the gate reads, as dotted JSON paths tried in order (data: one value
// per wire shape; the Claude-shaped family reads genericToolPayload
// instead). A tool input found as a JSON-encoded STRING is decoded.
type toolPayloadShape struct {
	session, cwd, root, tool, input []string
	// eventTools maps a native EVENT to the native tool name it implies, for
	// a host whose per-action pre-tool events carry no tool name (Cascade's
	// pre_run_command / pre_write_code / pre_read_code).
	eventTools map[string]string
	// mcpServer / mcpTool are the paths of a payload that names an MCP call
	// in two fields; together they compose the canonical
	// mcp__<server>__<tool> name.
	mcpServer, mcpTool []string
	// vocab lists further tooltax vocabularies (tool ids) the payload's tool
	// names may come from, tried in order after the host's own: a hook
	// runtime shared by several products names tools in one product's
	// vocabulary whichever host runs it.
	vocab []string
}

// shapedToolExtractor returns the extractor for a toolPayloadShape; skip is
// as for genericToolExtractor.
func shapedToolExtractor(shape toolPayloadShape, skip map[string]bool) func(ToolGateRequest) ([]policy.Event, string, bool) {
	return func(req ToolGateRequest) ([]policy.Event, string, bool) {
		var m map[string]any
		if err := json.Unmarshal(bytes.TrimPrefix(req.Body, []byte{0xEF, 0xBB, 0xBF}), &m); err != nil {
			return nil, "", false
		}
		native := firstPathString(m, shape.tool)
		if server, tool := firstPathString(m, shape.mcpServer), firstPathString(m, shape.mcpTool); server != "" && tool != "" {
			native = "mcp__" + server + "__" + tool
		}
		if native == "" {
			native = shape.eventTools[req.NativeEvent]
		}
		if native == "" {
			return nil, "", false
		}
		return buildToolGateEvents(req, skip, native, firstPathObject(m, shape.input), toolGateEventContext{
			session: firstPathString(m, shape.session),
			cwd:     firstPathString(m, shape.cwd),
			root:    firstPathString(m, shape.root),
			vocab:   shape.vocab,
		})
	}
}

// pathValue walks a dotted path through decoded JSON objects.
func pathValue(m map[string]any, path string) (any, bool) {
	var cur any = m
	for _, k := range strings.Split(path, ".") {
		obj, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		if cur, ok = obj[k]; !ok {
			return nil, false
		}
	}
	return cur, true
}

// firstPathString returns the first non-empty string value among paths; a
// list of strings (Antigravity's workspacePaths) yields its first entry.
func firstPathString(m map[string]any, paths []string) string {
	for _, p := range paths {
		v, ok := pathValue(m, p)
		if !ok {
			continue
		}
		if list, ok := v.([]any); ok && len(list) > 0 {
			v = list[0]
		}
		if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

// firstPathObject returns the first object value among paths; a string
// value that decodes to a JSON object counts (Copilot CLI's toolArgs). An
// empty map when none does.
func firstPathObject(m map[string]any, paths []string) map[string]any {
	for _, p := range paths {
		v, ok := pathValue(m, p)
		if !ok {
			continue
		}
		switch t := v.(type) {
		case map[string]any:
			return t
		case string:
			var obj map[string]any
			if json.Unmarshal([]byte(t), &obj) == nil && obj != nil {
				return obj
			}
		}
	}
	return map[string]any{}
}

// gateShape is what the gate judges for one native tool.
type gateShape struct {
	// canonical is the tooltax action type before any gate override.
	canonical  string
	kind       policy.EventKind
	actionType string
	operand    string
	dialect    policy.Dialect
}

// gateActionOverrides re-reads a canonical action the guard does not
// classify as a gateable one where the call still exposes what a rule
// judges: a content search returns file bytes, so it is judged as a read
// of its search root (the claude-code Grep precedent, claudeToolShape).
var gateActionOverrides = map[string]struct{ actionType, operand string }{
	models.ActionSearchText: {models.ActionReadFile, tooltax.OperandPath},
}

// gateUnclassifiedNatives covers native tools internal/tooltax leaves
// unclassified (no delete action type exists): deleting a file is a write
// to it for the sensitive-path and persistence rules. Keys are
// normalized (lower case, no separators).
var gateUnclassifiedNatives = map[string]struct{ actionType, operand string }{
	"delete":     {models.ActionWriteFile, tooltax.OperandFilePath},
	"deletefile": {models.ActionWriteFile, tooltax.OperandFilePath},
	"removefile": {models.ActionWriteFile, tooltax.OperandFilePath},
}

// powerShellNatives are native shell tools whose command is PowerShell.
var powerShellNatives = map[string]bool{"powershell": true, "pwsh": true}

// unionOperands is single followed by every list operand not equal to an
// earlier one, in order ("" contributes nothing).
func unionOperands(single string, list []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, v := range append([]string{single}, list...) {
		if strings.TrimSpace(v) == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

// nativeKnownIn reports whether native is a tool name the shared taxonomy
// knows under the host's vocabulary or an extra one (or an MCP tool name):
// a known tool the guard does not judge is not an unclassified call.
func nativeKnownIn(tool string, vocab []string, native string) bool {
	if native == "" {
		return false
	}
	if tooltax.IsMCPToolName(native) {
		return true
	}
	for _, v := range append([]string{tool}, vocab...) {
		if _, ok := tooltax.Resolve(v, native); ok {
			return true
		}
	}
	return false
}

// resolveGateShapeIn is resolveGateShape over the host's own vocabulary
// first, then each extra vocabulary in order (toolPayloadShape.vocab).
func resolveGateShapeIn(tool string, vocab []string, native string) (gateShape, bool) {
	if g, ok := resolveGateShape(tool, native); ok {
		return g, true
	}
	for _, v := range vocab {
		if g, ok := resolveGateShape(v, native); ok {
			return g, true
		}
	}
	return gateShape{}, false
}

// resolveGateShape maps (tool, native) onto the gate's event vocabulary.
func resolveGateShape(tool, native string) (gateShape, bool) {
	actionType, operand := "", ""
	if e, ok := tooltax.Resolve(tool, native); ok {
		actionType, operand = e.ActionType, e.Operand
	} else if tooltax.IsMCPToolName(native) {
		actionType, operand = models.ActionMCPCall, tooltax.OperandMCPName
	}
	canonical := actionType
	if o, ok := gateActionOverrides[actionType]; ok {
		actionType, operand = o.actionType, o.operand
	}
	key := normalizeNative(native)
	if operand == tooltax.OperandNone {
		if o, ok := gateUnclassifiedNatives[key]; ok {
			actionType, operand = o.actionType, o.operand
		}
	}
	kind, evaluable := guard.ClassifyActionType(actionType)
	if !evaluable || operand == tooltax.OperandNone {
		return gateShape{}, false
	}
	s := gateShape{canonical: canonical, kind: kind, actionType: actionType, operand: operand}
	if powerShellNatives[key] {
		s.dialect = policy.DialectPowerShell
	}
	return s, true
}

// operandFields lists, per tooltax operand, the tool-input field spellings
// the vendors use, tried in order.
var operandFields = map[string][]string{
	tooltax.OperandCommand:      {"command", "cmd", "commandLine", "script"},
	tooltax.OperandFilePath:     {"file_path", "filePath", "absolute_path", "target_file", "path", "notebook_path", "filename"},
	tooltax.OperandNotebookPath: {"notebook_path", "file_path"},
	tooltax.OperandPath:         {"path", "dir_path", "directory", "absolute_path", "file_path", "directory_path", "search_path"},
	tooltax.OperandURL:          {"url", "uri"},
	tooltax.OperandQuery:        {"query", "q"},
}

// operandListFields lists, per tooltax operand, the BATCH field spellings a
// tool input may carry instead of one operand: a list of strings, or of
// objects that each carry the operand (Cline CLI's run_commands
// {commands:[...]} and read_files {files:[{path}]}). Every element is judged.
var operandListFields = map[string][]string{
	tooltax.OperandCommand:  {"commands"},
	tooltax.OperandFilePath: {"files", "file_paths", "paths"},
	tooltax.OperandPath:     {"paths", "files"},
}

// operandList returns every operand of the first non-empty batch field for
// operand (exact spelling first, then compared without case or separators),
// or nil. A JSON-encoded list string is decoded.
func operandList(operand string, input map[string]any) []string {
	if len(input) == 0 {
		return nil
	}
	keys := make([]string, 0, len(input))
	for k := range input {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, f := range operandListFields[operand] {
		want := normalizeNative(f)
		for _, k := range keys {
			if k != f && normalizeNative(k) != want {
				continue
			}
			if out := operandListValues(operand, input[k]); len(out) > 0 {
				return out
			}
		}
	}
	return nil
}

// operandListValues reads one batch value: strings are operands as-is, an
// object yields its operand field. A batch field may also hold ONE bare
// operand string (Cline CLI 3.0.68 sent {"commands":"rm -rf ~/x"} live on
// demo node 1 for some models), which is that single operand.
func operandListValues(operand string, v any) []string {
	if s, ok := v.(string); ok {
		var decoded []any
		if json.Unmarshal([]byte(s), &decoded) != nil {
			if strings.TrimSpace(s) == "" {
				return nil
			}
			return []string{s}
		}
		v = decoded
	}
	list, ok := v.([]any)
	if !ok {
		return nil
	}
	var out []string
	for _, el := range list {
		switch t := el.(type) {
		case string:
			if strings.TrimSpace(t) != "" {
				out = append(out, t)
			}
		case map[string]any:
			if s := operandValue(operand, t); s != "" {
				out = append(out, s)
			}
		}
	}
	return out
}

// operandValue returns the first non-empty string input field for operand:
// an exact spelling first, then the same spellings compared without case or
// separators (Antigravity's PascalCase CommandLine / TargetFile /
// AbsolutePath, Cascade's command_line), so one field list serves every
// vendor's casing.
func operandValue(operand string, input map[string]any) string {
	for _, f := range operandFields[operand] {
		if s, ok := input[f].(string); ok && strings.TrimSpace(s) != "" {
			return s
		}
	}
	if len(input) == 0 {
		return ""
	}
	keys := make([]string, 0, len(input))
	for k := range input {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, f := range operandFields[operand] {
		want := normalizeNative(f)
		for _, k := range keys {
			if normalizeNative(k) != want {
				continue
			}
			if s, ok := input[k].(string); ok && strings.TrimSpace(s) != "" {
				return s
			}
		}
	}
	return ""
}

// patchFileHeaders are the file headers of the apply_patch envelope (Codex
// and its rebadges): each names one path the patch touches.
var patchFileHeaders = []string{"Add File:", "Update File:", "Delete File:", "Move File:", "Move to:"}

// patchTargetsIn finds a patch envelope in any string input field and
// returns every path it touches, in order, de-duplicated. Content-shaped
// (the envelope's own markers), never keyed on a tool name.
func patchTargetsIn(input map[string]any) []string {
	keys := make([]string, 0, len(input))
	for k := range input {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var out []string
	seen := map[string]bool{}
	for _, k := range keys {
		s, ok := input[k].(string)
		if !ok || !strings.Contains(s, "*** Begin Patch") {
			continue
		}
		for _, line := range strings.Split(s, "\n") {
			line = strings.TrimSpace(line)
			rest, ok := strings.CutPrefix(line, "*** ")
			if !ok {
				continue
			}
			for _, h := range patchFileHeaders {
				if p, ok := strings.CutPrefix(rest, h); ok {
					if p = strings.TrimSpace(p); p != "" && !seen[p] {
						seen[p] = true
						out = append(out, p)
					}
				}
			}
		}
	}
	return out
}

// firstWorkspaceRoot returns the first workspace root a payload declares
// as a plain string list ("" for any other shape: boundary rules then see
// no project root, as on Claude Code's PreToolUse).
func firstWorkspaceRoot(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var roots []string
	if json.Unmarshal(raw, &roots) != nil {
		return ""
	}
	for _, r := range roots {
		if r = strings.TrimSpace(r); r != "" {
			return r
		}
	}
	return ""
}

func normalizeNative(name string) string {
	key := strings.ToLower(strings.TrimSpace(name))
	for _, sep := range []string{"_", "-", ".", " "} {
		key = strings.ReplaceAll(key, sep, "")
	}
	return key
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}
