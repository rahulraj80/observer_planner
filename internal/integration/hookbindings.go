package integration

import "time"

// Hook bindings for every registry row (hook-parity audit 2026-10-02,
// section 4.2; docs/plans/hook-parity-lane0-brief.md step 4). DATA ONLY.
//
// Each row lists the vendor's documented hook events (Registered and Wired
// false) and the ones Observer registers today (Registered true; Wired
// where a feature acts on it). Grounding:
//
//   - live: an in-repo live capture or recorded live test (Claude Code
//     PreToolUse and UserPromptSubmit, Codex SessionStart and
//     UserPromptSubmit per docs/codex-hook-capture.md);
//   - fixture: an in-repo recorded payload (Cursor hooks-log fixtures);
//   - doc: the vendor page fetched 2026-10-02 (audit section 4.6);
//   - inferred: prime-agent's extension API and Muse's payload schema.
//
// HostTimeout / HostOnFailure come from the vendor doc where it states
// them, else zero / unknown. Most vendor field names were read through a
// fetch summarizer: every gate binding is re-checked against a live payload
// before it flips to Wired (Lane A grounding step).

// hookDocDate is the date every doc-grounded binding below was fetched.
const hookDocDate = "2026-10-02"

// Vendor hook documentation, audit section 4.6.
const (
	hookSrcClaudeCode     = "https://code.claude.com/docs/en/hooks"
	hookSrcCodex          = "https://learn.chatgpt.com/docs/hooks"
	hookSrcCursor         = "https://cursor.com/docs/hooks"
	hookSrcGemini         = "https://geminicli.com/docs/hooks/reference/"
	hookSrcQwen           = "https://qwenlm.github.io/qwen-code-docs/en/users/features/hooks/"
	hookSrcDroid          = "https://docs.factory.com/reference/hooks-reference"
	hookSrcQoder          = "https://docs.qoder.com/en/cli/hooks"
	hookSrcPoolside       = "https://docs.poolside.ai/hooks"
	hookSrcZcode          = "https://zcode.z.ai/en/docs/hooks"
	hookSrcCommandCode    = "https://commandcode.ai/docs/hooks"
	hookSrcCommandCodeMod = "https://commandcode.ai/docs/mods"
	hookSrcDevinCLI       = "https://docs.devin.ai/cli/extensibility/hooks/lifecycle-hooks"
	hookSrcCascade        = "https://docs.devin.ai/desktop/cascade/hooks"
	hookSrcKiro           = "https://kiro.dev/docs/hooks/types/"
	hookSrcKiroCrew       = "https://kiro.dev/docs/crew/capabilities/hooks/"
	hookSrcKimi           = "https://moonshotai.github.io/kimi-code/en/customization/hooks"
	hookSrcGrok           = "https://docs.x.ai/build/features/hooks"
	hookSrcGoose          = "https://goose-docs.ai/docs/guides/context-engineering/hooks/"
	hookSrcCrush          = "https://github.com/charmbracelet/crush/blob/main/docs/hooks/README.md"
	hookSrcAntigravity    = "https://antigravity.google/docs/hooks"
	hookSrcVibe           = "https://docs.mistral.ai/vibe/code/cli/hooks"
	hookSrcMuse           = "https://dev.meta.ai/docs/muse-code/extending.md (schema: https://agenticcontrolplane.com/blog/muse-code-acp-integration, third-party)"
	hookSrcJunie          = "https://junie.jetbrains.com/docs/junie-cli-hooks.html"
	hookSrcDeepSeek       = "https://raw.githubusercontent.com/deepseek-ai/deepseek-harness/master/docs/cookbook/extension-cookbook.md"
	hookSrcOpenInterp     = "https://www.openinterpreter.com/docs/terminal/hooks"
	hookSrcCopilotCLI     = "https://docs.github.com/en/copilot/reference/hooks-reference"
	hookSrcVSCode         = "https://code.visualstudio.com/docs/copilot/customization/hooks"
	hookSrcCline          = "https://raw.githubusercontent.com/cline/cline/main/sdk/packages/core/src/hooks/hook-file-config.ts"
	hookSrcKilo           = "https://kilo.ai/docs/automate/extending/plugins"
	hookSrcOpenCode       = "https://opencode.ai/docs/plugins/"
	hookSrcPi             = "https://raw.githubusercontent.com/badlogic/pi-mono/main/packages/coding-agent/docs/extensions.md"
	hookSrcPrimeAgent     = "PrimeIntellect-ai/prime-agent #872, #2712 (pi-fork extension API, third-party PR)"
	hookSrcOpenClaw       = "https://docs.openclaw.ai/plugins/hooks/reference"
	hookSrcHermes         = "https://raw.githubusercontent.com/NousResearch/hermes-agent/main/hermes_cli/plugins.py"
	hookSrcBrowserExt     = "internal/browserhost/hostfiles/host.js (Observer's own native-messaging host)"
)

// Live grounding records.
const (
	hookLiveClaude     = "docs/plans/hook-parity-lane0-brief.md step 4 (recorded live tests: PreToolUse deny/ask; UserPromptSubmit LIVE CORRECTION 2026-09-07, internal/guard/conformance.go)"
	hookLiveClaudeDate = "2026-09-07"
	hookLiveCodex      = "docs/codex-hook-capture.md (codex 0.129.0 empirical capture)"
	hookLiveCodexDate  = "2026-05-11"
	// hookLiveCodexToolGate: codex 0.160.0 `codex exec` on demo node 1 in a
	// throwaway HOME, hooks written by `observer init`; the deny reason
	// reached the model for a shell command, an apply_patch and a degraded
	// ask (docs/guard.md "Live grounding: hook-parity Lane A").
	hookLiveCodexToolGate = "docs/guard.md \"Live grounding: hook-parity Lane A\" (codex 0.160.0, demo node 1)"
	hookLiveLaneADate     = "2026-10-03"
	// hookLiveCursorToolGate: cursor-agent 2026.10.01 (signed in) on demo
	// node 1 in a throwaway HOME: a Write to ~/.ssh/x was denied on
	// preToolUse and the agent_message reached the model.
	hookLiveCursorToolGate = "docs/guard.md \"Live grounding: hook-parity Lane A\" (cursor-agent 2026.10.01, demo node 1)"
	// hookLiveQoderToolGate: qodercli 1.1.65 on demo node 1 in a throwaway
	// HOME: a deny reached the model; an ask under --yolo was auto-approved.
	hookLiveQoderToolGate = "docs/guard.md \"Live grounding: hook-parity Lane A\" (qodercli 1.1.65, demo node 1)"
	// T-357 live grounding (2026-10-04, demo node 1): the vendor ran in the
	// operator's real HOME on its own sign-in; `observer init` (run with
	// HOME pointed at a scratch project) wrote PROJECT-scoped hooks into
	// that project, and the observer hook child ran on a scratch HOME.
	// hookLiveQwenToolGate: qwen 0.24.7 --yolo: rm -rf ~/x denied and the
	// reason reached the model ("Tool run_shell_command was not run: ...");
	// {} let ls run; an ask was not run headless (the reason was shown).
	hookLiveQwenToolGate = "docs/guard.md \"Live grounding: hook-parity Lane A\" (qwen-code 0.24.7, demo node 1, project-scoped hooks)"
	// hookLiveDroidToolGate: droid 0.233.0 `droid exec --auto high`: rm -rf
	// ~/x denied and the reason reached the model; {} let ls run; an ask
	// under --auto high ran the command, so the binding lists no ask.
	hookLiveDroidToolGate = "docs/guard.md \"Live grounding: hook-parity Lane A\" (droid 0.233.0, demo node 1, project-scoped hooks.json with events at the root)"
	hookLiveT357Date      = "2026-10-04"
	// hookLiveAgyToolGate: agy 1.2.16 on demo node 1 in a throwaway HOME
	// (hook-parity Lane A6): rm -rf ~/x and a write to ~/.ssh/x were denied
	// and the reasons reached the model; an empty stdout let ls run; an
	// empty object {} DENIED the call; force_ask under
	// --dangerously-skip-permissions ran the command.
	hookLiveAgyToolGate = "docs/guard.md \"Live grounding: hook-parity Lane A\" (agy 1.2.16, demo node 1)"
	// hookLiveAgyPreInvocation (T-476, lane LIVE-GROUND-2, 2026-10-06): agy
	// 1.3.0 `agy -p` on demo node 1 in a throwaway HOME, in its documented
	// API-key mode against a loopback mock model with a fake key (no Google
	// sign-in, no OAuth file copied), hooks written by Observer's own
	// writer. PreInvocation fires before every model call (invocationNum 0,
	// then 1 after a tool result); stdin {artifactDirectoryPath,
	// conversationId, initialNumSteps, invocationNum, modelName,
	// transcriptPath, workspacePaths}, no prompt text; transcriptPath names
	// .../brain/<id>/.system_generated/logs/transcript_full.jsonl, which
	// already holds the USER_INPUT step at invocationNum 0. A fake-token
	// prompt got an injectSteps ephemeralMessage at invocationNum 0 and the
	// notice was in the model's next agent request; later invocations of the
	// same prompt replied {}. agy also sends one tool-less side request with
	// the prompt that no hook sees. p95 40 ms. PreToolUse was re-checked on
	// 1.3.0: the deny reached the model ("tool call denied by pre-tool hook:
	// <reason>") and an empty stdout let ls run.
	hookLiveAgyPreInvocation     = "hook-parity live-ground 2026-10-06 (T-476): agy 1.3.0 on demo node 1, API-key mode, throwaway HOME, loopback mock model, hooks written by observer init"
	hookLiveAgyPreInvocationDate = "2026-10-06"
	// hookLiveGrokToolGate: grok 1.0.46 on demo node 1 in a throwaway HOME
	// (hook-parity Lane A6): rm -rf ~/x was denied and the reason reached
	// the model; {"decision":"allow"} let ls run; an ask under
	// --always-approve ran the command.
	hookLiveGrokToolGate = "docs/guard.md \"Live grounding: hook-parity Lane A\" (grok 1.0.46, demo node 1)"
	hookFixtureCursor    = "testdata/cursor/hooks-log"
	// Lane C node-1 live grounding (2026-10-03): one session per vendor
	// with Observer's own writers in a throwaway HOME and
	// OBSERVER_HOOK_SHAPE_LOG; the start payload's session id equalled the
	// transcript's session id on every row marked live.
	hookLiveLaneCDate   = "2026-10-03"
	hookLiveGrokReplay  = "hook-parity Lane C node 1 capture: grok 1.0.46 `grok -p` replaying ~/.claude/settings.json (sessionId == ~/.grok/sessions/<cwd>/<sid>)"
	hookLiveQoderLC     = "hook-parity Lane C node 1 capture: qodercli 1.1.65 -p (SessionStart/Stop/SessionEnd/SubagentStart/SubagentStop; session_id == ~/.qoder/projects/<slug>/<sid>.jsonl, agent_id == subagents/agent-<id>.jsonl)"
	hookLiveCodexLC     = "hook-parity Lane C node 1 capture: codex-cli 0.160.0 exec (SessionEnd, SubagentStart/SubagentStop; session_id == rollout uuid, agent_id == child rollout uuid)"
	hookLiveClaudeLC    = "hook-parity Lane C node 1 capture: Claude Code 2.1.288 -p SessionEnd through the lifecycle spool"
	hookLiveQwenStartLC = "hook-parity Lane C node 1 capture: qwen 0.24.7 SessionStart (session_id == ~/.qwen/projects/<slug>/chats/<sid>.jsonl)"
	// hook-parity Lane D node 1 live tests (deny + allow captured per event).
	hookLivePluginShim     = "docs/guard.md \"Plugin shim gates\" live record (demo node 1: opencode 1.18.31, kilo 7.8.3, pi 1.0.0)"
	hookLivePluginShimDate = "2026-10-03"
	// Vendor SOURCE read from the installed build on demo node 1 (read-only,
	// 2026-10-03, hook-parity Lanes B/E): a doc-kind grounding stronger than
	// a fetched page, still not a live capture.
	hookSrcKimiInstalled  = "kimi-code 2.1.1 bundled source (agent-core-v2 externalHooks runHook.ts, userPrompt.ts) on demo node 1"
	hookSrcQwenInstalled  = "qwen-code 0.24.7 bundled source (todoWrite chunk, hook event descriptions) on demo node 1"
	hookSrcClineInstalled = "cline 3.0.68 bundled source (@cline/core hub daemon entry.js: prompt_submit runs with detached: detachAsyncHooks ?? true) on demo node 1"
	hookSrcLaneDate       = "2026-10-03"
	// hookLiveGooseToolGate / hookLiveClineToolGate: live-ground lane on
	// demo node 1, 2026-10-04, throwaway HOME, hooks written by `observer
	// init --all`. Goose 1.53.0 (nvidia poolside/laguna-xs-2.1): rm -rf ~/x
	// was blocked and the model got "Tool call denied by policy hook
	// `superbased-observer`: ..."; ls ran on the empty-stdout allow. Cline
	// CLI 3.0.68 (-P nvidia): the {"cancel":true,"errorMessage"} reply
	// aborted the run before rm -rf ~/x executed (the fake target
	// survived); ls ran on the empty-stdout allow.
	hookLiveGooseToolGate = "hook-parity live-ground 2026-10-04 (goose 1.53.0, demo node 1)"
	// hookLiveGeminiGates (T-363, lane LIVE-GROUND-2, 2026-10-06): gemini
	// 0.62.0 `gemini -p --yolo` on demo node 1 in a throwaway HOME against
	// a loopback mock model (gemini is not signed in there; a fake key, no
	// credential read or copied), hooks written by `observer init`.
	// BeforeTool: rm -rf ~/x was denied, the reason reached the model as the
	// functionResponse error and the fake target survived; {} let ls run; an
	// enforced ask was a deny naming `observer guard approve`. BeforeAgent:
	// a prompt carrying a fake GitHub token was blocked with ZERO model
	// requests and the developer saw "Agent execution blocked: <reason>";
	// a clean prompt replied {}. p95 BeforeTool deny 100 ms, allow 44 ms,
	// BeforeAgent allow 45 ms.
	hookLiveGeminiGates     = "hook-parity live-ground 2026-10-06 (T-363): gemini-cli 0.62.0 on demo node 1, throwaway HOME, loopback mock model, hooks written by observer init"
	hookLiveGeminiGatesDate = "2026-10-06"
	hookLiveClineToolGate   = "hook-parity live-ground 2026-10-04 (cline CLI 3.0.68 through the shared SDK hook runtime, demo node 1)"
	// LIVE-GROUND-3 (T-378): kimi-code 2.1.1 on demo node 1, throwaway HOME
	// (the operator's own ~/.kimi-code untouched), a loopback mock model
	// through the vendor's KIMI_MODEL_* env override, the [[hooks]] tables
	// written by `observer init` into a config.toml that also held a
	// provider api_key. PreToolUse: rm -rf ~/x was blocked by exit 2, the
	// stderr reason reached the model as the tool error and the fake target
	// survived; ls ran. UserPromptSubmit: a prompt carrying a fake GitHub
	// token was blocked with zero model requests and the developer saw
	// "UserPromptSubmit hook blocked" plus the reason. p95 deny 87 ms, allow
	// 36 ms, prompt allow 37 ms.
	hookLiveKimiGates     = "LIVE-GROUND-3 2026-10-07 (T-378): kimi-code 2.1.1 on demo node 1, throwaway HOME, loopback mock model, hooks written by observer init"
	hookLiveKimiGatesDate = "2026-10-07"
	// hookLiveDevinCLIToolGate is the LIVE-GROUND-3 receipt for the Devin
	// CLI PreToolUse writer (T-478): devin 3000.11.3 on demo node 1 after
	// T-574, a throwaway HOME holding only Observer's hooks key, a loopback
	// mock model. rm -rf ~/x was denied (the CLI reported "Tool rejected"
	// with the U-901 reason) and the fake target survived; ls ran. p95 deny
	// 92 ms, allow 37 ms.
	hookLiveDevinCLIToolGate     = "LIVE-GROUND-3 2026-10-07 (T-478): devin 3000.11.3 on demo node 1, throwaway HOME, loopback mock model, hook written by observer init"
	hookLiveDevinCLIToolGateDate = "2026-10-07"
)

// hgSource is a binding grounded on the vendor's own source as shipped in
// an installed build (GroundingDoc: "the vendor's own documentation or
// source").
func hgSource(src string) Grounding {
	return Grounding{Kind: GroundingDoc, Source: src, Date: hookSrcLaneDate}
}

// Reply dialect ids for the bindings Observer registers. Prompt dialects
// are internal/hook's PromptDialect* keys (spelled here because this
// package imports nothing project-internal); tool-gate and lifecycle
// dialects name the receiver family that speaks them today.
const (
	hookDialectClaudePreTool  = "claude-code-pretool"
	hookDialectClaudeCode     = "claude-code"
	hookDialectCursorPerm     = "cursor-permission"
	hookDialectCursor         = "cursor"
	hookDialectCodex          = "codex"
	hookDialectTopLevelBlock  = "top-level-block"
	hookDialectGemini         = "gemini-cli"
	hookDialectQoder          = "qoder"
	hookDialectPoolside       = "poolside"
	hookDialectZcode          = "zcode"
	hookDialectCascade        = "cascade"
	hookDialectCommandCode    = "command-code"
	hookDialectAgyPreInvoke   = "antigravity-preinvocation"
	hookDialectHermesPlugin   = "hermes-plugin"
	hookDialectClineCLITail   = "cline-cli-jsonl"
	hookDialectBrowserCapture = "browser-capture"
	// hookDialectLifecycle: served by cmd/observer's ONE lifecycle handler
	// (hook-parity Lane C1, internal/hooklifecycle); the reply is the
	// neutral {} every Claude-shaped host and Codex accept.
	hookDialectLifecycle = "lifecycle"
	// Lane B prompt dialects (internal/hook promptsubmit.go).
	hookDialectKimiCode = "kimi-code"
	hookDialectDevinCLI = "devin-cli"
	// Tool-gate dialects (internal/hook toolgate.go ToolGateDialect*).
	hookDialectClaudeJSONPreTool = "claude-json-pretool"
	hookDialectGeminiBeforeTool  = "gemini-beforetool"
	hookDialectCursorPreTool     = "cursor-pretool"
	// Lane A4-A6 tool-gate dialects.
	hookDialectAgyPreTool      = "antigravity-pretool"
	hookDialectGrokPreTool     = "grok-pretool"
	hookDialectCopilotPreTool  = "copilot-cli-pretool"
	hookDialectVibePreTool     = "vibe-pretool"
	hookDialectGoosePreTool    = "goose-pretool"
	hookDialectClinePreTool    = "cline-pretool"
	hookDialectPoolsidePreTool = "poolside-pretool"
	hookDialectJuniePreTool    = "junie-pretool"
	hookDialectCrushPreTool    = "crush-pretool"
	hookDialectCascadePreTool  = "cascade-pretool"
	hookDialectKiroPreTool     = "kiro-pretool"
	hookDialectKimiPreTool     = "kimi-pretool"
	// hookKimiRegisteredTimeout is the "timeout" (seconds) Observer writes
	// on its Kimi Code [[hooks]] tables (the vendor default is also 30).
	hookKimiRegisteredTimeout = 30 * time.Second
	// Host hook timeouts for the Lane A4-A6 tool gates: the value Observer
	// registers where it sets one, else the vendor's documented default.
	hookAgyToolGateTimeout      = 30 * time.Second
	hookGrokRegisteredTimeout   = 10 * time.Second
	hookCopilotCLIHostTimeout   = 30 * time.Second
	hookVibeRegisteredTimeout   = 30 * time.Second
	hookGooseHostTimeout        = 30 * time.Second
	hookClineHostTimeout        = 120 * time.Second
	hookPoolsideHostTimeout     = 60 * time.Second
	hookJunieRegisteredTimeout  = 10 * time.Second
	hookCrushHostTimeout        = 30 * time.Second
	hookZcodeRegisteredTimeout  = 30 * time.Second
	hookCommandCodeShellTimeout = 30 * time.Second
	hookKiroHostTimeout         = 60 * time.Second
	// hookMuseRegisteredTimeout is the "timeout" Observer registers on its
	// Muse handlers.
	hookMuseRegisteredTimeout = 30 * time.Second
	// hookDevinCLIRegisteredTimeout is the timeout Observer registers on
	// its Devin CLI handler (seconds field "timeout").
	hookDevinCLIRegisteredTimeout = 30 * time.Second
	// hookCodexHostTimeout is Codex's default command-hook timeout (600 s:
	// codex-rs/hooks/src/engine/discovery.rs normalize_command_hook, every
	// event except SessionEnd / Interrupt).
	hookCodexHostTimeout = 600 * time.Second
	// hookCursorHostTimeout is the per-hook timeout Cursor logs ("Running
	// hook 1 with timeout 60000ms").
	hookCursorHostTimeout = 60 * time.Second
	// hookGeminiHostTimeout is Gemini CLI's documented default (60000 ms).
	hookGeminiHostTimeout      = 60 * time.Second
	hookClaudeHostTimeout      = 60 * time.Second
	hookGrokHostTimeout        = 5 * time.Second
	hookAgyRegisteredTimeout   = 10 * time.Second
	hookCommandCodeModTimeoutS = 5 * time.Second
	// Plugin-host dialects (internal/hook plugingate.go): the pluginshim TS
	// shim and the Hermes Python plugin.
	hookDialectPluginShim       = "plugin-shim"
	hookDialectPluginShimPrompt = "plugin-shim-prompt"
)

// hbOpt mutates one binding under construction.
type hbOpt func(*HookBinding)

// hb builds a binding. Defaults: no process relation known, host failure
// behaviour unknown, vendor-only (not registered, not wired).
func hb(c HookCap, phase, event string, g Grounding, opts ...hbOpt) HookBinding {
	b := HookBinding{Cap: c, Phase: phase, NativeEvent: event, Grounding: g, ProcRel: ProcNone, HostOnFailure: HostFailureUnknown}
	for _, o := range opts {
		o(&b)
	}
	return b
}

func hgDoc(src string) Grounding {
	return Grounding{Kind: GroundingDoc, Source: src, Date: hookDocDate}
}

func hgInferred(src string) Grounding {
	return Grounding{Kind: GroundingInferred, Source: src, Date: hookDocDate}
}

func hgLive(src, date string) Grounding {
	return Grounding{Kind: GroundingLive, Source: src, Date: date}
}

// hookLiveGroundDate is the 2026-10-04 live-ground lane's capture date.
const hookLiveGroundDate = "2026-10-04"

// hgLiveOpt marks a binding live-grounded by the 2026-10-04 live-ground
// lane.
func hgLiveOpt(src string) hbOpt {
	return func(b *HookBinding) { b.Grounding = hgLive(src, hookLiveGroundDate) }
}

func hgFixture(src string) Grounding {
	return Grounding{Kind: GroundingFixture, Source: src, Date: hookDocDate}
}

// liveAt overrides a binding's grounding with a recorded live test (a
// plugin-host builder fixes the doc grounding for the whole host).
func liveAt(src, date string) hbOpt {
	return func(b *HookBinding) { b.Grounding = hgLive(src, date) }
}

// pluginShimLive is the Lane D node 1 live test of a shim binding.
var pluginShimLive = liveAt(hookLivePluginShim, hookLivePluginShimDate)

func dec(d ...string) hbOpt { return func(b *HookBinding) { b.Decisions = d } }

// reg marks the event as registered by Observer with the given dialect.
func reg(dialect string) hbOpt {
	return func(b *HookBinding) { b.Registered, b.Dialect = true, dialect }
}

// wired marks a feature as acting on the event today.
func wired() hbOpt { return func(b *HookBinding) { b.Wired = true } }

// dialectOnly names the dialect of a binding Observer handles but does not
// write into a vendor config (manual registration, tailer, plugin events).
func dialectOnly(dialect string) hbOpt { return func(b *HookBinding) { b.Dialect = dialect } }

func sess() hbOpt { return func(b *HookBinding) { b.SessionKey = true } }

func proc(p ProcRelation) hbOpt { return func(b *HookBinding) { b.ProcRel = p } }

func host(timeout time.Duration, f FailurePolicy) hbOpt {
	return func(b *HookBinding) { b.HostTimeout, b.HostOnFailure = timeout, f }
}

func note(s string) hbOpt { return func(b *HookBinding) { b.Note = s } }

// laneCLive overrides a binding's grounding with a hook-parity Lane C node-1
// live capture (src names the vendor build and what was confirmed).
func laneCLive(src string) hbOpt {
	return func(b *HookBinding) { b.Grounding = hgLive(src, hookLiveLaneCDate) }
}

func guardChannel(ch string) hbOpt { return func(b *HookBinding) { b.GuardChannel = ch } }

// all applies several options at once (a per-vendor default set).
func all(opts ...hbOpt) hbOpt {
	return func(b *HookBinding) {
		for _, o := range opts {
			o(b)
		}
	}
}

// Decision sets reused below.
var (
	decDeny          = []string{HookDecisionDeny}
	decDenyModify    = []string{HookDecisionDeny, HookDecisionModify}
	decDenyAskModify = []string{HookDecisionDeny, HookDecisionAsk, HookDecisionModify}
	decDenyAsk       = []string{HookDecisionDeny, HookDecisionAsk}
	decDenyContext   = []string{HookDecisionDeny, HookDecisionContext}
	decContext       = []string{HookDecisionContext}
	decModify        = []string{HookDecisionModify}
	decMessage       = []string{HookDecisionMessage}
)

// --- claude-code: shell commands in ~/.claude/settings.json ---------------

var claudeCodeOpts = all(sess(), proc(ProcDescendant), host(hookClaudeHostTimeout, HostProceeds))

func claudeB(c HookCap, phase, event string, opts ...hbOpt) HookBinding {
	return hb(c, phase, event, hgDoc(hookSrcClaudeCode), append([]hbOpt{claudeCodeOpts}, opts...)...)
}

// claudeTaskNote is the honest status of the Claude Code task events
// (hook-parity Lane E, 2026-10-03).
const claudeTaskNote = "capture only: `observer hook claude-code task-created|task-completed` records a task_event action and always approves (exit 0), so the vendor's exit-2 rollback is never taken. Live on demo node 1 (Claude Code 2.1.288 -p, CLAUDE_CODE_ENABLE_TASKS=true, throwaway HOME, hooks written by `observer init --claude-code`): TaskCreate fired TaskCreated and TaskUpdate(status completed) fired TaskCompleted, payload {session_id, cwd, prompt_id, task_id, task_subject, task_description}; four task_event rows landed. p95 hook latency 52 ms"

// hookLiveClaudeTasks is the T-322 live capture of the task events.
const (
	hookLiveClaudeTasksDate = "2026-10-04"
	hookLiveClaudeTasks     = "hook-parity live-ground 2026-10-04 (T-322): Claude Code 2.1.288 -p on demo node 1, TaskCreated x2 + TaskCompleted x2 captured, task_event rows in the probe DB"
)

var hookBindingsClaudeCode = []HookBinding{
	claudeB(HookCapSessionLifecycle, HookPhaseStart, "SessionStart", reg(hookDialectClaudeCode), wired()),
	claudeB(HookCapContextInject, HookPhaseStart, "SessionStart", reg(hookDialectClaudeCode), wired(), dec(HookDecisionContext)),
	claudeB(HookCapSessionLifecycle, HookPhaseEnd, "SessionEnd", reg(hookDialectLifecycle), wired(),
		laneCLive(hookLiveClaudeLC),
		note("served by the one lifecycle handler: the session_end row is spooled DB-free (the host's SessionEnd budget is about 1.5 s) and ingested by the daemon")),
	claudeB(HookCapEnvironmentObserve, HookPhaseNone, "Setup", reg(hookDialectClaudeCode), wired()),
	hb(HookCapPromptGate, HookPhasePre, "UserPromptSubmit", hgLive(hookLiveClaude, hookLiveClaudeDate), claudeCodeOpts,
		reg(hookDialectClaudeCode), wired(),
		dec(HookDecisionAllow, HookDecisionAsk, HookDecisionDeny, HookDecisionContext, HookDecisionMessage),
		note("process exit code 2 with NO stdout reply; stderr is the user-visible reason and the prompt is erased (LIVE CORRECTION 2026-09-07: permissionDecision is PreToolUse-only and is silently ignored on this event). Allow/warn reply with the bare hookSpecificOutput envelope (+ additionalContext on warn)")),
	claudeB(HookCapUserMessage, HookPhasePre, "UserPromptSubmit", reg(hookDialectClaudeCode), wired(), dec(HookDecisionMessage)),
	claudeB(HookCapEnvironmentObserve, HookPhaseNone, "UserPromptExpansion", reg(hookDialectClaudeCode), wired()),
	hb(HookCapToolGate, HookPhasePre, "PreToolUse", hgLive(hookLiveClaude, hookLiveClaudeDate), claudeCodeOpts,
		reg(hookDialectClaudePreTool), wired(),
		dec(HookDecisionAllow, HookDecisionAsk, HookDecisionDeny, HookDecisionModify),
		note("native ask via permissionDecision; payload carries cwd but no project root, so boundary rules defer to the watcher")),
	claudeB(HookCapToolRewrite, HookPhasePre, "PreToolUse", reg(hookDialectClaudePreTool), wired(), dec(HookDecisionModify)),
	claudeB(HookCapToolObserve, HookPhasePost, "PostToolUse", reg(hookDialectClaudeCode), wired()),
	claudeB(HookCapToolFailureObserve, HookPhasePost, "PostToolUseFailure", reg(hookDialectClaudeCode), wired()),
	claudeB(HookCapToolObserve, HookPhasePost, "PostToolBatch", reg(hookDialectClaudeCode), wired()),
	claudeB(HookCapPermissionObserve, HookPhasePre, "PermissionRequest", reg(hookDialectClaudeCode), wired()),
	claudeB(HookCapPermissionObserve, HookPhasePost, "PermissionDenied", reg(hookDialectClaudeCode), wired()),
	claudeB(HookCapTurnLifecycle, HookPhaseEnd, "Stop", reg(hookDialectClaudeCode), wired(), dec(HookDecisionDeny)),
	claudeB(HookCapTurnLifecycle, HookPhaseEnd, "StopFailure", reg(hookDialectClaudeCode), wired()),
	claudeB(HookCapCompactLifecycle, HookPhasePre, "PreCompact", reg(hookDialectClaudeCode), wired()),
	claudeB(HookCapCompactLifecycle, HookPhasePost, "PostCompact", reg(hookDialectClaudeCode), wired()),
	claudeB(HookCapSubagentLifecycle, HookPhaseStart, "SubagentStart", reg(hookDialectClaudeCode), wired()),
	claudeB(HookCapSubagentLifecycle, HookPhaseEnd, "SubagentStop", reg(hookDialectClaudeCode), wired()),
	claudeB(HookCapNotificationObserve, HookPhaseNone, "Notification", reg(hookDialectClaudeCode), wired()),
	claudeB(HookCapEnvironmentObserve, HookPhaseNone, "CwdChanged", reg(hookDialectClaudeCode), wired()),
	claudeB(HookCapEnvironmentObserve, HookPhaseNone, "InstructionsLoaded", reg(hookDialectClaudeCode), wired()),
	claudeB(HookCapEnvironmentObserve, HookPhaseNone, "ConfigChange", reg(hookDialectClaudeCode), wired()),
	claudeB(HookCapEnvironmentObserve, HookPhaseNone, "WorktreeRemove", reg(hookDialectClaudeCode), wired()),
	// Vendor-only: WorktreeCreate is blocking and opt-in
	// (docs/claude-worktree-hook.md); the task events have a capture-only
	// receiver but are not registered until live-grounded (hook-parity
	// Lane E); FileChanged needs a per-file matcher.
	claudeB(HookCapEnvironmentObserve, HookPhaseNone, "WorktreeCreate", note("opt-in only (docs/claude-worktree-hook.md): blocking, must echo the worktree path")),
	hb(HookCapTaskObserve, HookPhaseStart, "TaskCreated", hgLive(hookLiveClaudeTasks, hookLiveClaudeTasksDate), claudeCodeOpts, reg(hookDialectClaudeCode), wired(), note(claudeTaskNote)),
	hb(HookCapTaskObserve, HookPhaseEnd, "TaskCompleted", hgLive(hookLiveClaudeTasks, hookLiveClaudeTasksDate), claudeCodeOpts, reg(hookDialectClaudeCode), wired(), note(claudeTaskNote)),
	claudeB(HookCapEnvironmentObserve, HookPhaseNone, "FileChanged", note("matcher takes literal filenames, not the wildcard every registered event uses")),
}

// --- codex: shell commands in ~/.codex/hooks.json --------------------------

var codexOpts = all(sess(), proc(ProcDescendant), host(0, HostProceeds))

func codexB(c HookCap, phase, event string, opts ...hbOpt) HookBinding {
	return hb(c, phase, event, hgDoc(hookSrcCodex), append([]hbOpt{codexOpts}, opts...)...)
}

var hookBindingsCodex = []HookBinding{
	hb(HookCapSessionLifecycle, HookPhaseStart, "SessionStart", hgLive(hookLiveCodex, hookLiveCodexDate), codexOpts, reg(hookDialectCodex), wired()),
	codexB(HookCapContextInject, HookPhaseStart, "SessionStart", reg(hookDialectCodex), dec(HookDecisionContext, HookDecisionMessage)),
	hb(HookCapPromptGate, HookPhasePre, "UserPromptSubmit", hgLive(hookLiveCodex, hookLiveCodexDate), codexOpts,
		reg(hookDialectTopLevelBlock), wired(),
		dec(HookDecisionAllow, HookDecisionAsk, HookDecisionDeny, HookDecisionContext, HookDecisionMessage),
		note("legacy {decision:block,reason} shown to the developer; prompt cannot be modified")),
	hb(HookCapToolGate, HookPhasePre, "PreToolUse", hgLive(hookLiveCodexToolGate, hookLiveLaneADate), codexOpts,
		reg(hookDialectClaudeJSONPreTool), wired(),
		host(hookCodexHostTimeout, HostProceeds),
		dec(HookDecisionAllow, HookDecisionDeny, HookDecisionModify),
		note("hookSpecificOutput.permissionDecision deny with a reason (Codex shows the model \"Command blocked by PreToolUse hook: <reason>\"); covers Bash, apply_patch (every file in the patch) and MCP tools; no ask: Codex rejects permissionDecision:ask and fails open (openai/codex #28437), so an enforced ask is a deny that says how to confirm; unified_exec shell calls are only partly intercepted (#20204) and spawn_agent sub-agents bypass hooks (#49736)")),
	codexB(HookCapPermissionObserve, HookPhasePre, "PermissionRequest", reg(hookDialectCodex), wired()),
	codexB(HookCapToolObserve, HookPhasePost, "PostToolUse", reg(hookDialectCodex), wired()),
	codexB(HookCapTurnLifecycle, HookPhaseEnd, "Stop", reg(hookDialectCodex), wired(), dec(HookDecisionDeny)),
	codexB(HookCapSessionLifecycle, HookPhaseEnd, "SessionEnd", reg(hookDialectLifecycle), wired(), laneCLive(hookLiveCodexLC)),
	codexB(HookCapSubagentLifecycle, HookPhaseStart, "SubagentStart", reg(hookDialectLifecycle), wired(), laneCLive(hookLiveCodexLC),
		note("session_id is the PARENT session; agent_id is the child rollout uuid")),
	codexB(HookCapSubagentLifecycle, HookPhaseEnd, "SubagentStop", reg(hookDialectLifecycle), wired(), laneCLive(hookLiveCodexLC)),
	codexB(HookCapCompactLifecycle, HookPhasePre, "PreCompact", dialectOnly(hookDialectCodex),
		note("routed to the one compaction recorder (hook-parity Lane E); not registered until a live Codex capture grounds it (new events also need [hooks.state] re-trust)")),
	codexB(HookCapCompactLifecycle, HookPhasePost, "PostCompact", dialectOnly(hookDialectCodex),
		note("routed to the one compaction recorder (hook-parity Lane E); not registered until a live Codex capture grounds it")),
	// The config.toml `notify` program, not a hooks.json event: listed in
	// the guard conformance matrix as an observe-only channel.
	codexB(HookCapTurnLifecycle, HookPhaseEnd, "notify", guardChannel("notify"),
		note("observe-only notification channel - no documented deny semantics (Q4); enforcement comes from the native rules dialect once G11 compiles it")),
}

// --- cursor: shell commands in ~/.cursor/hooks.json ------------------------

var cursorOpts = all(sess(), proc(ProcDescendant))

func cursorB(c HookCap, phase, event string, opts ...hbOpt) HookBinding {
	return hb(c, phase, event, hgFixture(hookFixtureCursor), append([]hbOpt{cursorOpts}, opts...)...)
}

var hookBindingsCursor = []HookBinding{
	cursorB(HookCapPromptGate, HookPhasePre, "beforeSubmitPrompt", reg(hookDialectCursor), wired(),
		dec(HookDecisionAllow, HookDecisionAsk, HookDecisionDeny, HookDecisionMessage),
		note("continue:false + user_message (NOT the permission field the shell/MCP/file channels use); prompt cannot be modified")),
	cursorB(HookCapToolGate, HookPhasePre, "beforeShellExecution", reg(hookDialectCursorPerm), wired(),
		dec(HookDecisionAllow, HookDecisionAsk, HookDecisionDeny),
		note("permission JSON allow|deny|ask; payload carries the workspace root, so boundary rules are active pre-execution")),
	cursorB(HookCapToolObserve, HookPhasePost, "afterFileEdit", reg(hookDialectCursor), wired()),
	cursorB(HookCapToolGate, HookPhasePre, "beforeMCPExecution", reg(hookDialectCursorPerm), wired(),
		dec(HookDecisionAllow, HookDecisionAsk, HookDecisionDeny), note("permission JSON allow|deny|ask")),
	cursorB(HookCapTurnLifecycle, HookPhaseEnd, "stop", reg(hookDialectCursor), wired()),
	cursorB(HookCapModelCallObserve, HookPhasePost, "stop", reg(hookDialectCursor), wired()),
	cursorB(HookCapToolGate, HookPhasePre, "beforeReadFile", reg(hookDialectCursorPerm), wired(),
		dec(HookDecisionAllow, HookDecisionAsk, HookDecisionDeny), note("permission JSON allow|deny|ask")),
	cursorB(HookCapToolFailureObserve, HookPhasePost, "postToolUseFailure", reg(hookDialectCursor), wired()),
	cursorB(HookCapSessionLifecycle, HookPhaseStart, "sessionStart", reg(hookDialectCursor), wired()),
	cursorB(HookCapContextInject, HookPhaseStart, "sessionStart", reg(hookDialectCursor), dec(HookDecisionContext)),
	cursorB(HookCapSessionLifecycle, HookPhaseEnd, "sessionEnd", reg(hookDialectCursor), wired()),
	cursorB(HookCapSubagentLifecycle, HookPhaseStart, "subagentStart", reg(hookDialectCursor), wired(), dec(HookDecisionDeny),
		note("observe only: the cursor receiver records a subagent_start row (cursor.BuildToolEvent); the deny reply is not used")),
	cursorB(HookCapSubagentLifecycle, HookPhaseEnd, "subagentStop", reg(hookDialectCursor), wired()),
	cursorB(HookCapCompactLifecycle, HookPhasePre, "preCompact", reg(hookDialectCursor), wired(),
		note("compaction_events capture (hook-parity Lane E): conversation_id + workspace_roots[0]; no post event, so the capture is never reconciled")),
	hb(HookCapToolGate, HookPhasePre, "preToolUse", hgLive(hookLiveCursorToolGate, hookLiveLaneADate), cursorOpts,
		reg(hookDialectCursorPreTool), wired(),
		host(hookCursorHostTimeout, HostFailureUnknown),
		dec(HookDecisionAllow, HookDecisionDeny, HookDecisionModify),
		note("permission allow|deny with user_message + agent_message; judges the classes the per-tool before* events do not (writes, edits, deletes, content search, web); ask is accepted but not enforced on this event (vendor docs), so an enforced ask is a deny that says how to confirm")),
	cursorB(HookCapToolObserve, HookPhasePost, "postToolUse", reg(hookDialectCursor), wired()),
	cursorB(HookCapToolObserve, HookPhasePost, "afterShellExecution", reg(hookDialectCursor), wired()),
	cursorB(HookCapToolObserve, HookPhasePost, "afterMCPExecution", reg(hookDialectCursor), wired()),
	cursorB(HookCapModelCallObserve, HookPhasePost, "afterAgentThought", reg(hookDialectCursor), wired()),
	cursorB(HookCapModelCallObserve, HookPhasePost, "afterAgentResponse", reg(hookDialectCursor), wired()),
}

// --- gemini-cli: shell commands in ~/.gemini/settings.json -----------------

var geminiOpts = all(sess(), proc(ProcDescendant))

func geminiB(c HookCap, phase, event string, opts ...hbOpt) HookBinding {
	return hb(c, phase, event, hgDoc(hookSrcGemini), append([]hbOpt{geminiOpts}, opts...)...)
}

var hookBindingsGeminiCLI = []HookBinding{
	geminiB(HookCapPromptGate, HookPhasePre, "BeforeAgent", reg(hookDialectGemini), wired(), liveAt(hookLiveGeminiGates, hookLiveGeminiGatesDate),
		dec(HookDecisionAllow, HookDecisionAsk, HookDecisionDeny, HookDecisionContext, HookDecisionMessage),
		note("decision:deny discards the message; both reason and systemMessage are set (vendor docs call systemMessage user-visible in the terminal, and reason also renders when denying). Live (gemini 0.62.0, demo node 1): the block held with zero model requests and the developer saw \"Agent execution blocked: <reason>\"; because the reply also exits 2, gemini additionally prints \"Hook(s) failed for event BeforeAgent\" beside the block")),
	geminiB(HookCapUserMessage, HookPhasePre, "BeforeAgent", reg(hookDialectGemini), wired(), liveAt(hookLiveGeminiGates, hookLiveGeminiGatesDate), dec(HookDecisionMessage)),
	geminiB(HookCapToolGate, HookPhasePre, "BeforeTool", reg(hookDialectGeminiBeforeTool), wired(), liveAt(hookLiveGeminiGates, hookLiveGeminiGatesDate),
		host(hookGeminiHostTimeout, HostFailureUnknown),
		dec(HookDecisionDeny, HookDecisionModify),
		note("decision:deny with reason (sent to the agent as the tool error) and systemMessage (shown to the developer); ask is implemented but undocumented (google-gemini/gemini-cli #28046): deny-only until probed, so an enforced ask is a deny that says how to confirm")),
	geminiB(HookCapToolObserve, HookPhasePost, "AfterTool"),
	geminiB(HookCapSessionLifecycle, HookPhaseStart, "SessionStart", dec(HookDecisionContext, HookDecisionMessage)),
	geminiB(HookCapSessionLifecycle, HookPhaseEnd, "SessionEnd"),
	geminiB(HookCapTurnLifecycle, HookPhaseEnd, "AfterAgent", dec(HookDecisionDeny)),
	geminiB(HookCapCompactLifecycle, HookPhasePre, "PreCompress", dialectOnly(hookDialectGemini),
		note("routed to the one compaction recorder (hook-parity Lane E); the gemini-cli writer registers BeforeAgent only")),
	geminiB(HookCapModelCallObserve, HookPhasePost, "AfterModel", note("fires per streaming chunk: never bind it for token capture")),
	geminiB(HookCapNotificationObserve, HookPhaseNone, "Notification"),
}

// --- Claude-Code-shaped shell-hook clones: qwen-code, droid, qoder ---------

func cloneB(src string) func(c HookCap, phase, event string, opts ...hbOpt) HookBinding {
	return func(c HookCap, phase, event string, opts ...hbOpt) HookBinding {
		return hb(c, phase, event, hgDoc(src), append([]hbOpt{sess(), proc(ProcDescendant)}, opts...)...)
	}
}

var (
	qwenB  = cloneB(hookSrcQwen)
	droidB = cloneB(hookSrcDroid)
	qoderB = cloneB(hookSrcQoder)
)

var hookBindingsQwenCode = []HookBinding{
	qwenB(HookCapPromptGate, HookPhasePre, "UserPromptSubmit", reg(hookDialectTopLevelBlock), wired(),
		dec(HookDecisionAllow, HookDecisionAsk, HookDecisionDeny, HookDecisionContext, HookDecisionMessage),
		note("documented decision enum is allow|deny|block|ask; observer emits \"block\" (the verb every sibling top-level-block vendor's docs confirm) rather than guessing a semantic split between deny/block/ask the docs don't draw")),
	hb(HookCapToolGate, HookPhasePre, "PreToolUse", hgLive(hookLiveQwenToolGate, hookLiveT357Date), sess(), proc(ProcDescendant),
		reg(hookDialectClaudeJSONPreTool), wired(),
		dec(decDenyAskModify...),
		note("hookSpecificOutput.permissionDecision deny|ask with permissionDecisionReason (Claude-shaped); live (qwen 0.24.7 --yolo): the deny reason reached the model, {} let the call run, and an ask was NOT run headless (qwen showed the reason); the interactive ask prompt is not probed. The payload names the native tool (run_shell_command, read_file, glob, write_file) with tool_input")),
	qwenB(HookCapSessionLifecycle, HookPhaseStart, "SessionStart", reg(hookDialectLifecycle), wired(), dec(decContext...), laneCLive(hookLiveQwenStartLC)),
	// Owed a live run (Lane C node 1: every qwen turn 404'd on the free
	// model, so no end event fired); the lifecycle handler would serve them,
	// but they are not registered until a capture grounds them.
	qwenB(HookCapSessionLifecycle, HookPhaseEnd, "SessionEnd", dialectOnly(hookDialectLifecycle)),
	qwenB(HookCapTurnLifecycle, HookPhaseEnd, "Stop", dialectOnly(hookDialectLifecycle), dec(decDeny...)),
	qwenB(HookCapSubagentLifecycle, HookPhaseStart, "SubagentStart", dialectOnly(hookDialectLifecycle)),
	qwenB(HookCapSubagentLifecycle, HookPhaseEnd, "SubagentStop", dialectOnly(hookDialectLifecycle)),
	qwenB(HookCapCompactLifecycle, HookPhasePre, "PreCompact", dec(decDeny...), dialectOnly(hookDialectTopLevelBlock),
		note("routed to the one compaction recorder (hook-parity Lane E); exit 0 stdout is appended as custom compact instructions, so the reply is the JSON approve, never text; the qwen-code writer registers UserPromptSubmit only")),
	qwenB(HookCapCompactLifecycle, HookPhasePost, "PostCompact", dialectOnly(hookDialectTopLevelBlock),
		note("routed to the one compaction recorder (hook-parity Lane E); output ignored for control")),
	hb(HookCapTaskObserve, HookPhaseStart, "TodoCreated", hgSource(hookSrcQwenInstalled), sess(), proc(ProcDescendant),
		dialectOnly(hookDialectTopLevelBlock), dec(decDeny...),
		note("task_observe capture only (hook-parity Lane E): fires twice per todo, phase \"validation\" (exit 2 / decision block cancels the creation) then \"postWrite\"; Observer records postWrite only and never blocks. Input todo_id, todo_content, todo_status, all_todos, phase")),
	hb(HookCapTaskObserve, HookPhaseEnd, "TodoCompleted", hgSource(hookSrcQwenInstalled), sess(), proc(ProcDescendant),
		dialectOnly(hookDialectTopLevelBlock), dec(decDeny...),
		note("task_observe capture only (hook-parity Lane E): validation then postWrite like TodoCreated; input todo_id, todo_content, previous_status, all_todos, phase")),
}

var hookBindingsDroid = []HookBinding{
	droidB(HookCapPromptGate, HookPhasePre, "UserPromptSubmit", reg(hookDialectTopLevelBlock), wired(),
		dec(HookDecisionAllow, HookDecisionAsk, HookDecisionDeny, HookDecisionContext, HookDecisionMessage),
		note("same legacy {decision:block,reason} shape as Codex; vendor docs state outright \"hook feedback goes to the user, not the agent\". Not live: on node 1 (2026-10-04) `droid exec` 0.233.0 fired PreToolUse from the same hooks.json but never UserPromptSubmit; the interactive TUI is not probed")),
	hb(HookCapToolGate, HookPhasePre, "PreToolUse", hgLive(hookLiveDroidToolGate, hookLiveT357Date), sess(), proc(ProcDescendant),
		reg(hookDialectClaudeJSONPreTool), wired(),
		dec(HookDecisionDeny, HookDecisionModify),
		note("hookSpecificOutput.permissionDecision deny with permissionDecisionReason (Claude-shaped); the deny reached the model live. Ask is NOT listed: the vendor documents permissionDecision ask, but live on droid 0.233.0 an ask under `droid exec --auto high` ran the command, so an enforced ask is a deny that says how to confirm. hooks.json keeps its events at the ROOT (a {\"hooks\":{}} wrapper is ignored by droid; Observer's writer repairs it)")),
	// Droid's lifecycle events stay vendor-only (not registered): the earlier
	// "zero hook events" on node 1 was Observer's wrapped hooks.json, fixed
	// 2026-10-04; they are owed their own live run.
	droidB(HookCapSessionLifecycle, HookPhaseStart, "SessionStart", dec(decContext...)),
	droidB(HookCapSessionLifecycle, HookPhaseEnd, "SessionEnd"),
	droidB(HookCapTurnLifecycle, HookPhaseEnd, "Stop", dec(decDeny...)),
	droidB(HookCapSubagentLifecycle, HookPhaseEnd, "SubagentStop"),
	droidB(HookCapCompactLifecycle, HookPhasePre, "PreCompact", dialectOnly(hookDialectTopLevelBlock),
		note("routed to the one compaction recorder (hook-parity Lane E); the droid writer registers UserPromptSubmit only")),
}

var hookBindingsQoder = []HookBinding{
	qoderB(HookCapPromptGate, HookPhasePre, "UserPromptSubmit", reg(hookDialectQoder), wired(),
		dec(HookDecisionAllow, HookDecisionAsk, HookDecisionDeny, HookDecisionMessage),
		note("Part B item 2, phase-3a - live-fetched 2026-09-07 (docs.qoder.com/en/cli/hooks): exit code 2 blocks, plain-text stderr is the developer-visible reason (no JSON reply at all); ~/.qoder/settings.json shares Claude Code's exact hooks schema, so registration reuses registerGenericSettingsHooks")),
	hb(HookCapToolGate, HookPhasePre, "PreToolUse", hgLive(hookLiveQoderToolGate, hookLiveLaneADate), sess(), proc(ProcDescendant),
		reg(hookDialectClaudeJSONPreTool), wired(),
		dec(HookDecisionDeny, HookDecisionModify, HookDecisionContext),
		note("hookSpecificOutput.permissionDecision deny with permissionDecisionReason (Claude-shaped JSON on this event, unlike the exit-code-only UserPromptSubmit); the deny reached the model live. Ask is NOT listed: the vendor documents permissionDecision ask, but live on qodercli 1.1.65 an ask under --yolo ran the command without asking, so an enforced ask is a deny that says how to confirm")),
	qoderB(HookCapSessionLifecycle, HookPhaseStart, "SessionStart", reg(hookDialectLifecycle), wired(), dec(decContext...), laneCLive(hookLiveQoderLC)),
	qoderB(HookCapSessionLifecycle, HookPhaseEnd, "SessionEnd", reg(hookDialectLifecycle), wired(), laneCLive(hookLiveQoderLC)),
	qoderB(HookCapTurnLifecycle, HookPhaseEnd, "Stop", reg(hookDialectLifecycle), wired(), dec(decDeny...), laneCLive(hookLiveQoderLC)),
	qoderB(HookCapSubagentLifecycle, HookPhaseStart, "SubagentStart", reg(hookDialectLifecycle), wired(), laneCLive(hookLiveQoderLC)),
	qoderB(HookCapSubagentLifecycle, HookPhaseEnd, "SubagentStop", reg(hookDialectLifecycle), wired(), laneCLive(hookLiveQoderLC)),
	qoderB(HookCapCompactLifecycle, HookPhasePre, "PreCompact", dec(decDeny...), dialectOnly(hookDialectQoder),
		note("routed to the one compaction recorder (hook-parity Lane E); the qoder writer registers UserPromptSubmit only")),
	qoderB(HookCapCompactLifecycle, HookPhasePost, "PostCompact", dialectOnly(hookDialectQoder),
		note("routed to the one compaction recorder (hook-parity Lane E)")),
}

// --- poolside: snake_case YAML hooks in ~/.config/poolside/settings.yaml ---

var poolsideB = cloneB(hookSrcPoolside)

var hookBindingsPoolside = []HookBinding{
	poolsideB(HookCapPromptGate, HookPhasePre, "UserPromptSubmit", reg(hookDialectPoolside), wired(),
		dec(HookDecisionAllow, HookDecisionAsk, HookDecisionDeny, HookDecisionModify, HookDecisionContext),
		note("Part B item 2, phase-3a - live-fetched 2026-09-07 (docs.poolside.ai/hooks): JSON decision shape ({\"decision\":\"block\",\"reason\",\"hook_specific_output\":{\"updated_prompt\",\"additional_context\"}}, snake_case); registered in ~/.config/poolside/settings.yaml. REDACT wired (hook-parity Lane B, 2026-10-03): under [guard.prompt] mode redact a secret-only prompt is answered with hook_specific_output.updated_prompt (the model receives the [REDACTED:type] text, the terminal keeps the typed prompt)")),
	poolsideB(HookCapToolGate, HookPhasePre, "PreToolUse", reg(hookDialectPoolsidePreTool), wired(),
		host(hookPoolsideHostTimeout, HostProceeds), dec(decDenyModify...),
		note("hook_specific_output.permission_decision deny with permission_decision_reason (snake_case); the reason reaches the model as the tool result; the allow is an empty stdout (no allow value is documented); no ask. Runs before Poolside's own approval prompt. Fails open on timeout / bad output (60 s default). Windows: the vendor runs hooks through cmd /c; Observer writes the poolside-windows bridge too. Not installed on demo node 1 (2026-10-07): the vendor installer asks to accept the Poolside EULA on a TTY, an operator step; doc-grounded, `observer init` only")),
	poolsideB(HookCapSessionLifecycle, HookPhaseStart, "SessionStart", dec(decContext...)),
	poolsideB(HookCapTurnLifecycle, HookPhaseEnd, "Stop"),
	poolsideB(HookCapCompactLifecycle, HookPhasePre, "PreCompact", dec(decContext...), dialectOnly(hookDialectPoolside),
		note("routed to the one compaction recorder (hook-parity Lane E); input trigger auto|forced; a block or continue decision is ignored; the poolside writer registers UserPromptSubmit only")),
}

// --- zcode: ~/.zcode/cli/config.json (receiver only, registered by hand) ---

var zcodeB = cloneB(hookSrcZcode)

var hookBindingsZcode = []HookBinding{
	zcodeB(HookCapPromptGate, HookPhasePre, "UserPromptSubmit", reg(hookDialectZcode), wired(),
		dec(HookDecisionAllow, HookDecisionAsk, HookDecisionDeny, HookDecisionContext),
		note("Part B item 2, phase-3a - live-fetched 2026-09-07 (zcode.z.ai/en/docs/hooks): Claude-Code-shaped continue:false reply, ~/.zcode/cli/config.json registration. The dialect/receiver is built and tested; since hook-parity Lane A4 (2026-10-03) `observer init` registers it beside the PreToolUse gate, but zai-org/feedback #841 (open, v3.14.3) reports UserPromptSubmit never firing on the native agent (and #32 reported no hook firing at all), so prompt coverage there is unproven; doc-grounded, never written by `observer start`")),
	zcodeB(HookCapToolGate, HookPhasePre, "PreToolUse", reg(hookDialectClaudeJSONPreTool), wired(),
		host(hookZcodeRegisteredTimeout, HostProceeds), dec(decDenyAskModify...),
		note("hookSpecificOutput.permissionDecision deny|ask with permissionDecisionReason (Claude-shaped); registered in ~/.zcode/cli/config.json hooks.events with hooks.enabled set true (nothing runs without it). Liveness: zai-org/feedback #32 (closed) reported hooks never firing on the native agent; #841 (open, v3.14.3) reports PreToolUse firing for MAIN-session tool calls only, never for sub-agent or workflow-agent calls (main agent only). Windows: not stated by the vendor. Not installable on demo node 1: Z.ai ships no Linux CLI channel (zcode runs inside its desktop app); doc-grounded, `observer init` only")),
	zcodeB(HookCapSessionLifecycle, HookPhaseStart, "SessionStart", dec(decContext...)),
	zcodeB(HookCapTurnLifecycle, HookPhaseEnd, "Stop", dec(decDeny...)),
}

// --- command-code: Mods SDK TS module + shell hooks ------------------------

var hookBindingsCommandCode = []HookBinding{
	hb(HookCapPromptGate, HookPhasePre, "transformInput", hgDoc(hookSrcCommandCodeMod),
		proc(ProcInProcessPID), host(hookCommandCodeModTimeoutS, HostFailureUnknown),
		reg(hookDialectCommandCode), wired(),
		dec(HookDecisionAllow, HookDecisionAsk, HookDecisionDeny, HookDecisionModify),
		note("Part B item 2, phase-3a - live-fetched 2026-09-07 (commandcode.ai/docs/mods): a Mods-SDK TypeScript module (cmd.hooks({transformInput({text}){...}})), not a shell hook - registered as ~/.commandcode/mods/observer-guard.ts (go:embed template, hermesplugin precedent) that shells out to `observer hook command-code transformInput`. The ONLY documented field is `text` - there is no session/conversation id anywhere in the signature, so an ask-once finding here fails closed to a HARD, unconditional block (no resend override) per the engine's own documented empty-session-id rule. REDACT wired (hook-parity Lane B, 2026-10-03): under [guard.prompt] mode redact a secret-only prompt is answered with {action:'transform',text} (masking needs no session key)")),
	hb(HookCapToolGate, HookPhasePre, "beforeToolCall", hgDoc(hookSrcCommandCodeMod), proc(ProcInProcessPID), dec(decDenyAskModify...),
		reg(hookDialectPluginShim), wired(),
		note("the same mod file (the one plugin shim, hook-parity Lane D): {block: true, additionalContext: <reason>} on a deny; the payload is {toolCallId, toolName, input, state}, no session id, and no ask field is documented, so an ask is a deny with a reason")),
	hb(HookCapToolGate, HookPhasePre, "PreToolUse", hgDoc(hookSrcCommandCode), proc(ProcDescendant), sess(), dec(decDenyContext...),
		dialectOnly(hookDialectClaudeJSONPreTool), host(hookCommandCodeShellTimeout, HostProceeds),
		note("the shell-hook layer (~/.commandcode/settings.json), separate from the Mods SDK: hookSpecificOutput.permissionDecision deny with permissionDecisionReason (reason reaches the model and the TUI); no ask. Never both gates: Observer's mod subscribes to the Mods beforeToolCall (the plugin shim, hook-parity Lane D), so the MOD is the gate of record and this shell entry is not written (and is removed if an earlier version wrote it; withCommandCodeShellGate). Windows: not stated by the vendor. Installed on demo node 1 (command-code 1.77.0, 2026-10-07) but not signed in (`cmd auth login`), so nothing fired: doc-grounded")),
	hb(HookCapSessionLifecycle, HookPhaseStart, "SessionStart", hgDoc(hookSrcCommandCode), proc(ProcDescendant), dec(decContext...)),
	hb(HookCapTurnLifecycle, HookPhaseEnd, "Stop", hgDoc(hookSrcCommandCode), proc(ProcDescendant), dec(decDeny...)),
	hb(HookCapModelCallObserve, HookPhasePost, "onTurnEnd", hgDoc(hookSrcCommandCodeMod), proc(ProcInProcessPID)),
}

// --- devin: Devin Desktop / Windsurf Cascade + Devin CLI -------------------

var (
	cascadeB  = cloneB(hookSrcCascade)
	devinCLIB = cloneB(hookSrcDevinCLI)
)

var hookBindingsDevin = []HookBinding{
	cascadeB(HookCapPromptGate, HookPhasePre, "pre_user_prompt", reg(hookDialectCascade), wired(),
		dec(HookDecisionAllow, HookDecisionDeny),
		note("Part B item 2, phase-3a - live-fetched 2026-09-07 (docs.devin.ai/desktop/cascade/hooks): Windsurf/Devin Desktop Cascade's pre_user_prompt, exit code 2 blocks, no JSON reply and no user-visible message channel at all (show_output does not apply to this event, so ask-once degrades to a hard block); no session_id field is documented either - trajectory_id (the conversation identifier) is used as the session-scoping key instead; registered at ~/.codeium/windsurf/hooks.json")),
	cascadeB(HookCapToolGate, HookPhasePre, "pre_run_command", reg(hookDialectCascadePreTool), wired(), host(0, HostProceeds), dec(decDeny...),
		note("Cascade: exit code 2 blocks and stderr (the reason, nothing else) is shown to the developer; no JSON reply, no ask, so an enforced ask is a deny that says how to confirm; any other exit lets the action proceed. The command is tool_info.command_line. Windows: the vendor runs `command` through PowerShell; Observer writes no Windows target for Cascade. Not installed on demo node 1: doc-grounded, `observer init` only")),
	cascadeB(HookCapToolGate, HookPhasePre, "pre_write_code", reg(hookDialectCascadePreTool), wired(), host(0, HostProceeds), dec(decDeny...),
		note("Cascade: judged as an edit of tool_info.file_path; exit 2 + stderr")),
	cascadeB(HookCapToolGate, HookPhasePre, "pre_read_code", reg(hookDialectCascadePreTool), wired(), host(0, HostProceeds), dec(decDeny...),
		note("Cascade: judged as a read of tool_info.file_path; exit 2 + stderr")),
	cascadeB(HookCapToolGate, HookPhasePre, "pre_mcp_tool_use", reg(hookDialectCascadePreTool), wired(), host(0, HostProceeds), dec(decDeny...),
		note("Cascade: judged as the MCP call mcp__<tool_info.mcp_server_name>__<tool_info.mcp_tool_name>; exit 2 + stderr")),
	devinCLIB(HookCapToolGate, HookPhasePre, "PreToolUse", reg(hookDialectClaudeJSONPreTool), wired(),
		host(hookDevinCLIRegisteredTimeout, HostProceeds), dec(decDenyModify...),
		liveAt(hookLiveDevinCLIToolGate, hookLiveDevinCLIToolGateDate),
		note("Devin CLI (T-478): Observer writes one Claude-shaped matcher group under \"hooks\" in the CLI's user config, ~/.config/devin/config.json (the devin folder under %APPDATA% on Windows), only when that directory exists (the CLI is installed); .devin/hooks.v1.json is project-only and never written. The reply is hookSpecificOutput.permissionDecision deny with permissionDecisionReason (Claude-shaped; the vendor also documents exit 2 and decision:block). The CLI can ALSO replay ~/.claude/settings.json (read_config_from.claude, default on), where Observer's Claude Code PreToolUse may be registered. Live on node 1 (devin 3000.11.3, 2026-10-07, after T-574): rm -rf ~/x was denied, the CLI reported \"Tool rejected\" with the rule reason and the fake target survived; ls ran; p95 deny 92 ms, allow 37 ms. Windows: not live-checked")),
	devinCLIB(HookCapPromptGate, HookPhasePre, "UserPromptSubmit", dec(decContext...), dialectOnly(hookDialectDevinCLI),
		note("Devin CLI .devin/hooks.v1.json: input prompt + session_id; the only documented output is hookSpecificOutput.additionalContext (reaches the agent), no block, so the dialect warns the model and every finding toasts (hook-parity Lane B). No Observer writer yet. Node 1 (devin 3000.11.3, 2026-10-03): `devin -p` did NOT run Observer's Claude Code hooks from ~/.claude/settings.json (no hook-events rows; only post-hoc capture-lane flags)")),
	devinCLIB(HookCapSessionLifecycle, HookPhaseStart, "SessionStart", dec(decContext...)),
	devinCLIB(HookCapSessionLifecycle, HookPhaseEnd, "SessionEnd"),
	devinCLIB(HookCapTurnLifecycle, HookPhaseEnd, "Stop", dec(decDeny...)),
	devinCLIB(HookCapCompactLifecycle, HookPhasePost, "PostCompaction"),
}

// --- kiro-cli / kiro-crew ---------------------------------------------------

var (
	kiroB     = cloneB(hookSrcKiro)
	kiroCrewB = func(c HookCap, phase, event string, opts ...hbOpt) HookBinding {
		return hb(c, phase, event, hgDoc(hookSrcKiroCrew), append([]hbOpt{proc(ProcDescendant)}, opts...)...)
	}
)

var hookBindingsKiroCLI = []HookBinding{
	kiroB(HookCapToolGate, HookPhasePre, "preToolUse", dialectOnly(hookDialectKiroPreTool), host(hookKiroHostTimeout, HostProceeds), dec(decDeny...),
		note("exit code 2 blocks and stderr returns to the agent (2.x reference); no JSON decision, no ask. The dialect is built and tested, but Observer writes NO registration: 2.x hooks live inside a per-agent JSON (\"hooks\" field of ~/.kiro/agents/<agent>.json, user path third-party sourced) and the 3.0 global ~/.kiro/hooks/ location is only third-party sourced (CLI 2.13 --v3 early access), so either writer would guess a path. kiro-cli 2.28.0 on demo node 1 (2026-10-07, T-477): the kiro-cli-chat binary names preToolUse, postToolUse, userPromptSubmit, agentSpawn and stop and the global agent directory .kiro/agents/, and none of its three binaries names a .kiro/hooks path, so this build reads 2.x per-agent hooks only; every agent command needs kiro-cli login, so nothing fired and no writer is built. Windows: not stated")),
	kiroB(HookCapPromptGate, HookPhasePre, "userPromptSubmit", dec(decDeny...),
		note("probe_required: a shell-action non-zero exit is documented to block the submission, but the 2.x CLI reference documents exit codes for preToolUse only and no stdin field names, and kirodotdev/Kiro #11050 (open, Kiro IDE 1.0.337) reports the prompt is committed BEFORE the hook runs, so an exit 2 does not stop it. No dialect row until a live capture settles the payload and whether the block holds (hook-parity Lane B, 2026-10-03)")),
	kiroB(HookCapSessionLifecycle, HookPhaseStart, "agentSpawn", dec(decContext...)),
	kiroB(HookCapTurnLifecycle, HookPhaseEnd, "stop"),
}

var hookBindingsKiroCrew = []HookBinding{
	kiroCrewB(HookCapToolGate, HookPhasePre, "PreToolUse", dec(decDeny...), note("shares .kiro/hooks/ with kiro-cli: a project hook may fire under both")),
	kiroCrewB(HookCapPromptGate, HookPhasePre, "UserPromptSubmit", dec(decDeny...)),
	kiroCrewB(HookCapSessionLifecycle, HookPhaseStart, "SessionStart"),
	kiroCrewB(HookCapTurnLifecycle, HookPhaseEnd, "Stop"),
}

// --- kimi-code: ~/.kimi-code/config.toml -----------------------------------

var kimiB = cloneB(hookSrcKimi)

var hookBindingsKimiCode = []HookBinding{
	kimiB(HookCapToolGate, HookPhasePre, "PreToolUse", reg(hookDialectKimiPreTool), wired(),
		host(hookKimiRegisteredTimeout, HostProceeds), dec(decDeny...), liveAt(hookLiveKimiGates, hookLiveKimiGatesDate),
		note("exit 2 blocks and its stderr is the reason the model sees; exit 0 allows and any stdout is added to the model context, so the allow is silence (the JSON hookSpecificOutput.permissionDecision deny is the vendor's other documented block). Other exit codes, a timeout or a crash allow (fail-open by the vendor). "+
			"T-378: registered as a [[hooks]] table {event, command, timeout = 30} in ~/.kimi-code/config.toml ($KIMI_CODE_HOME), a file that also holds plaintext provider keys, under the D-270 credential-file exception (only Observer's own tables are edited). No ask. Windows: implied by the vendor, shell not stated")),
	hb(HookCapPromptGate, HookPhasePre, "UserPromptSubmit", hgSource(hookSrcKimiInstalled), sess(), proc(ProcDescendant),
		host(hookKimiRegisteredTimeout, HostProceeds), reg(hookDialectKimiCode), wired(), dec(decDenyContext...),
		liveAt(hookLiveKimiGates, hookLiveKimiGatesDate),
		note("dialect built from the installed 2.1.1 source (hook-parity Lane B) and live-captured (T-378): stdin {hook_event_name, session_id, cwd, client_type, prompt, is_steer} where prompt is the turn's content parts [{type: text, text}] (not a string); exit 2 blocks and its stderr is appended to the conversation as a <hook_result> message the developer sees; exit-0 stdout is appended to the model context, so the allow is an empty stdout; timeout or crash is an allow. "+
			"T-378: registered beside the PreToolUse gate in ~/.kimi-code/config.toml under the D-270 credential-file exception")),
	kimiB(HookCapSessionLifecycle, HookPhaseStart, "SessionStart"),
	kimiB(HookCapSessionLifecycle, HookPhaseEnd, "SessionEnd"),
	kimiB(HookCapTurnLifecycle, HookPhaseEnd, "Stop", dec(decDeny...)),
	kimiB(HookCapSubagentLifecycle, HookPhaseStart, "SubagentStart"),
	kimiB(HookCapSubagentLifecycle, HookPhaseEnd, "SubagentStop"),
	hb(HookCapCompactLifecycle, HookPhasePre, "PreCompact", hgSource(hookSrcKimiInstalled), sess(), proc(ProcDescendant),
		dialectOnly(hookDialectKimiCode), note("routed to the one compaction recorder (hook-parity Lane E): input trigger, token_count; return value ignored")),
	hb(HookCapCompactLifecycle, HookPhasePost, "PostCompact", hgSource(hookSrcKimiInstalled), sess(), proc(ProcDescendant),
		dialectOnly(hookDialectKimiCode), note("routed to the one compaction recorder (hook-parity Lane E): input trigger, estimated_token_count; fire-and-forget")),
}

// --- grok: ~/.grok/hooks/*.json (also replays .claude / .cursor files) -----

var grokB = func(c HookCap, phase, event string, opts ...hbOpt) HookBinding {
	return hb(c, phase, event, hgDoc(hookSrcGrok), append([]hbOpt{sess(), proc(ProcDescendant), host(hookGrokHostTimeout, HostFailureUnknown)}, opts...)...)
}

// grokReplay marks a grok lifecycle binding Observer acts on WITHOUT writing
// a grok config: Grok Build runs ~/.claude/settings.json hooks, and the
// claude-code receiver dispatches a payload it identifies as Grok's
// (internal/hook foreignHostSignatures) to these bindings (Lane C3).
func grokReplay() hbOpt {
	return all(dialectOnly(hookDialectLifecycle), wired(), func(b *HookBinding) {
		laneCLive(hookLiveGrokReplay)(b)
		b.ReplayedVia = "claude-code"
		b.Note = "reached through the Claude Code settings replay (grok 1.0.46 runs ~/.claude/settings.json hooks), so only where Observer's Claude Code hooks are registered; dispatched by the claude-code receiver to the one lifecycle handler under tool grok"
	})
}

var hookBindingsGrok = []HookBinding{
	grokB(HookCapToolGate, HookPhasePre, "PreToolUse", liveAt(hookLiveGrokToolGate, hookLiveLaneADate), reg(hookDialectGrokPreTool), wired(),
		host(hookGrokRegisteredTimeout, HostProceeds), dec(decDenyModify...),
		note("top-level decision deny with reason, in ~/.grok/hooks/superbased-observer.json (always trusted); the deny reason reached the model live (grok 1.0.46, demo node 1). Ask is NOT listed: the vendor documents ask, but live an ask under --always-approve ran the command, so an enforced ask is a deny that says how to confirm. The allow {\"decision\":\"allow\"} only means not blocked (live, ls ran). Observer registers a 10 s timeout (the vendor default is 5 s); every hook failure fails open on the host side. Grok also replays ~/.claude/settings.json and ~/.cursor/hooks.json (audit section 4.4). Windows: not stated")),
	grokB(HookCapPromptGate, HookPhasePre, "UserPromptSubmit", note("observe only: no block, no message, so no dialect row; prompt coverage is the proxy route (when routed) or the post-hoc capture lane (hook-parity Lane B)")),
	grokB(HookCapSessionLifecycle, HookPhaseStart, "SessionStart", grokReplay()),
	grokB(HookCapSessionLifecycle, HookPhaseEnd, "SessionEnd", grokReplay()),
	grokB(HookCapTurnLifecycle, HookPhaseEnd, "Stop", grokReplay()),
	grokB(HookCapSubagentLifecycle, HookPhaseStart, "SubagentStart"),
	grokB(HookCapSubagentLifecycle, HookPhaseEnd, "SubagentStop"),
	grokB(HookCapCompactLifecycle, HookPhasePre, "PreCompact", dialectOnly(hookDialectTopLevelBlock),
		note("routed to the one compaction recorder (hook-parity Lane E); Grok reads Claude-shaped hook files, so the reply is the Claude-shaped approve (inferred). No Observer writer yet")),
	grokB(HookCapCompactLifecycle, HookPhasePost, "PostCompact", dialectOnly(hookDialectTopLevelBlock),
		note("routed to the one compaction recorder (hook-parity Lane E). No Observer writer yet")),
}

// --- goose / crush / mistral-code ------------------------------------------

var (
	gooseB = cloneB(hookSrcGoose)
	crushB = cloneB(hookSrcCrush)
	vibeB  = cloneB(hookSrcVibe)
)

var hookBindingsGoose = []HookBinding{
	gooseB(HookCapToolGate, HookPhasePre, "PreToolUse", hgLiveOpt(hookLiveGooseToolGate), reg(hookDialectGoosePreTool), wired(),
		host(hookGooseHostTimeout, HostProceeds), dec(decDeny...),
		note("{\"decision\":\"block\",\"reason\"} from Observer's Open Plugin (~/.agents/plugins/superbased-observer: plugin.json + hooks/hooks.json, no trust step); the model gets \"Tool call denied by policy hook\"; the allow is an empty stdout; no ask. The cwd is working_dir. Host default on_failure is allow (fail open); no sub-agent events. Windows: not stated (commands run through sh -c)")),
	gooseB(HookCapPromptGate, HookPhasePre, "UserPromptSubmit", note("observe only: the vendor states hooks cannot inject, so no dialect row; prompt coverage is the proxy route (opt-in) or the post-hoc capture lane (hook-parity Lane B)")),
	gooseB(HookCapSessionLifecycle, HookPhaseStart, "SessionStart"),
	gooseB(HookCapSessionLifecycle, HookPhaseEnd, "SessionEnd"),
	gooseB(HookCapTurnLifecycle, HookPhaseEnd, "Stop", dec(decDeny...)),
}

var hookBindingsCrush = []HookBinding{
	crushB(HookCapToolGate, HookPhasePre, "PreToolUse", reg(hookDialectCrushPreTool), wired(),
		host(hookCrushHostTimeout, HostProceeds), dec(HookDecisionDeny, HookDecisionModify, HookDecisionContext),
		note("the ONE crush.json hook event (global ~/.config/crush/crush.json): {\"decision\":\"deny\",\"reason\"}; the allow is an empty object because decision:allow would skip Crush's permission prompt; exit 49 halts the whole turn (never used); no ask; sub-agents bypass hooks by design (main agent only). Timeouts and other exits fail open. Windows: yes (embedded POSIX shell). Installed on demo node 1 (crush v0.97.1, 2026-10-07) but it has no provider configured and exposes no base-URL env knob, so a run needs a provider key or a hand-written crush.json: doc-grounded, `observer init` only")),
}

var hookBindingsMistralCode = []HookBinding{
	vibeB(HookCapToolGate, HookPhasePre, "pre_tool", reg(hookDialectVibePreTool), wired(),
		host(hookVibeRegisteredTimeout, HostProceeds), dec(HookDecisionDeny, HookDecisionModify, HookDecisionMessage),
		note("JSON decision only ({\"decision\":\"deny\",\"reason\"}, the reason becomes the tool error the model sees): exit 2 is NOT a block, it is a hook failure (a warning unless strict = true). Registered in ~/.vibe/hooks.toml with match \"*\", strict false and a 30 s timeout; no ask. Windows: not stated")),
	vibeB(HookCapToolObserve, HookPhasePost, "post_tool"),
	vibeB(HookCapTurnLifecycle, HookPhaseEnd, "post_agent", dec(decDeny...)),
}

// --- antigravity-cli / antigravity (agy hooks.json) ------------------------

var agyB = func(c HookCap, phase, event string, opts ...hbOpt) HookBinding {
	return hb(c, phase, event, hgDoc(hookSrcAntigravity), append([]hbOpt{sess(), proc(ProcDescendant)}, opts...)...)
}

var hookBindingsAntigravityCLI = []HookBinding{
	agyB(HookCapPromptGate, HookPhasePre, "PreInvocation", reg(hookDialectAgyPreInvoke), wired(),
		liveAt(hookLiveAgyPreInvocation, hookLiveAgyPreInvocationDate),
		host(hookAgyToolGateTimeout, HostFailureUnknown), dec(decContext...),
		note("WARN-ONLY (restored by T-476): the stdin carries no prompt text, so the receiver reads the latest USER_INPUT step from the transcriptPath file (transcript.jsonl on agy 1.2.16, transcript_full.jsonl on 1.3.0) and replies injectSteps with one ephemeralMessage telling the model about the finding; it never blocks, judges each prompt once, and an empty object is the allow. Live on agy 1.3.0 the prompt was already in the transcript at invocationNum 0 and the notice reached the model before its first agent reply. On agy 1.2.16 (2026-10-03) the transcript held the prompt only after the reply, so the seam could not warn; an older agy degrades to {} (no warn). agy also sends one tool-less side request with the prompt that no hook sees")),
	agyB(HookCapToolGate, HookPhasePre, "PreToolUse", liveAt(hookLiveAgyToolGate, hookLiveLaneADate), reg(hookDialectAgyPreTool), wired(),
		host(hookAgyToolGateTimeout, HostFailureUnknown), dec(HookDecisionDeny, HookDecisionModify),
		note("top-level decision deny with reason; the deny reached the model live (agy 1.2.16, demo node 1, a shell command and a write to ~/.ssh). The allow is an EMPTY stdout: live, an empty object {} DENIED the call (decision is required), while an empty stdout leaves the call to agy's own permission flow (and decision:allow is documented to auto-approve past the developer's prompt). Ask is NOT listed: force_ask under --dangerously-skip-permissions ran the command (headless default mode auto-denied it), so an enforced ask is a deny that says how to confirm. camelCase payload: toolCall.{name,args} (args keys PascalCase, e.g. CommandLine), conversationId, workspacePaths; no cwd. The desktop app and the IDE read the same ~/.gemini/config/hooks.json. Windows: the vendor runs hooks through cmd /c; Observer writes no Windows target")),
	agyB(HookCapToolObserve, HookPhasePost, "PostToolUse"),
	agyB(HookCapModelCallObserve, HookPhasePost, "PostInvocation"),
	agyB(HookCapTurnLifecycle, HookPhaseEnd, "Stop"),
}

var hookBindingsAntigravity = []HookBinding{
	agyB(HookCapToolGate, HookPhasePre, "PreToolUse", dec(decDenyAsk...),
		note("desktop 2.0 and the IDE honour the agy hooks.json, so the antigravity-cli registration (`observer hook antigravity-cli PreToolUse`) gates them too; its verdicts are recorded under antigravity-cli")),
	agyB(HookCapContextInject, HookPhasePre, "PreInvocation", dec(decContext...)),
	agyB(HookCapTurnLifecycle, HookPhaseEnd, "Stop"),
}

// --- open-interpreter (Codex-shaped, own home) ------------------------------

var oiB = cloneB(hookSrcOpenInterp)

var hookBindingsOpenInterpreter = []HookBinding{
	oiB(HookCapToolGate, HookPhasePre, "PreToolUse", reg(hookDialectClaudeJSONPreTool), wired(), host(0, HostProceeds), dec(decDenyModify...),
		note("Codex shape under its OWN home: ~/.openinterpreter/hooks.json, hookSpecificOutput.permissionDecision deny. Ask is not listed (the vendor does not state it and Codex itself rejects ask). Per-definition trust like Codex: the entry runs only after the developer reviews it with /hooks, and [features] hooks = true must be set in ~/.openinterpreter/config.toml, which Observer never reads or rewrites (it can hold plaintext provider keys). Windows: not stated. Installed on demo node 1 (open-interpreter 0.0.56, 2026-10-07) and run against a loopback mock: the hooks.json Observer wrote did not fire, even with --enable hooks, because the entry was never trusted in /hooks (a TTY step); the vendor's own exec policy refused rm -rf on its own: doc-grounded, `observer init` only")),
	oiB(HookCapPromptGate, HookPhasePre, "UserPromptSubmit",
		note("probe_required - open-interpreter is a rebadged Codex CLI Rust build (codex.NewOpenInterpreter retag); it plausibly inherits Codex's UserPromptSubmit hook under INTERPRETER_HOME, unconfirmed")),
	oiB(HookCapSessionLifecycle, HookPhaseStart, "SessionStart", dec(decContext...)),
	oiB(HookCapTurnLifecycle, HookPhaseEnd, "Stop"),
	oiB(HookCapSubagentLifecycle, HookPhaseStart, "SubagentStart"),
	oiB(HookCapSubagentLifecycle, HookPhaseEnd, "SubagentStop"),
	oiB(HookCapCompactLifecycle, HookPhasePre, "PreCompact"),
	oiB(HookCapCompactLifecycle, HookPhasePost, "PostCompact"),
}

// --- muse (payload schema unpublished: inferred) ---------------------------

var museB = func(c HookCap, phase, event string, opts ...hbOpt) HookBinding {
	return hb(c, phase, event, hgInferred(hookSrcMuse), append([]hbOpt{proc(ProcDescendant)}, opts...)...)
}

// hookLiveMuse is the T-479 live capture (lane LIVE-GROUND-2, 2026-10-06):
// muse-bin 1.4.3 `muse exec` on demo node 1 with a throwaway HOME and XDG
// dirs, provider "meta" pointed at a loopback mock with a fake key (no Meta
// sign-in; auth.json never read). PreToolUse stdin: {cwd, hook_event_name,
// model, model_provider, permission_mode, session_id, tool_input{command,
// description, workdir}, tool_name, tool_use_id, transcript_path (null),
// turn_id}; a hookSpecificOutput.permissionDecision deny reached the model
// as "tool blocked by hook: <reason>". UserPromptSubmit stdin adds prompt;
// {"decision":"block"} and exit 2 both cancelled the run with no model
// request.
const (
	hookLiveMuse     = "hook-parity live-ground 2026-10-06 (T-479): muse-bin 1.4.3 exec on demo node 1, throwaway HOME and XDG dirs, loopback mock model, hooks written by observer init (U-901 deny reached the model, a ghp_ prompt was cancelled with no model request)"
	hookLiveMuseDate = "2026-10-06"
)

var hookBindingsMuse = []HookBinding{
	museB(HookCapToolGate, HookPhasePre, "PreToolUse", reg(hookDialectClaudeJSONPreTool), wired(), liveAt(hookLiveMuse, hookLiveMuseDate),
		host(hookMuseRegisteredTimeout, HostFailureUnknown), dec(decDenyModify...),
		note("Observer writes one Claude-shaped matcher group under \"hooks\" in ~/.config/muse/settings.json (and ensures \"schema_version\": 1); the reply is hookSpecificOutput.permissionDecision deny with permissionDecisionReason, which reached the model live as \"tool blocked by hook: <reason>\". Ask is not listed (not probed headless). Muse's own approval reviewer runs separately and can still refuse a call the hook allowed. Plugin bundles (.muse-plugin, argv commands) are the other hook source; Observer writes none. No Windows build exists")),
	museB(HookCapPromptGate, HookPhasePre, "UserPromptSubmit", reg(hookDialectTopLevelBlock), wired(), liveAt(hookLiveMuse, hookLiveMuseDate),
		host(hookMuseRegisteredTimeout, HostFailureUnknown), dec(HookDecisionAllow, HookDecisionDeny),
		note("the top-level-block dialect (decision block with a reason): live, the run ended \"Cancelled\" before any model request (exit 2 did the same); muse exec printed no reason, so the developer-visible message is unproven and an enforced ask is a block")),
	museB(HookCapSessionLifecycle, HookPhaseStart, "SessionStart"),
	museB(HookCapSessionLifecycle, HookPhaseEnd, "SessionEnd"),
	museB(HookCapTurnLifecycle, HookPhaseEnd, "Stop"),
	museB(HookCapSubagentLifecycle, HookPhaseStart, "SubagentStart"),
	museB(HookCapSubagentLifecycle, HookPhaseEnd, "SubagentStop"),
	museB(HookCapCompactLifecycle, HookPhasePre, "PreCompact"),
	museB(HookCapCompactLifecycle, HookPhasePost, "PostCompact"),
}

// --- junie (CLI hooks only; not in ACP/IDE mode) ---------------------------

var junieB = cloneB(hookSrcJunie)

var hookBindingsJunie = []HookBinding{
	junieB(HookCapToolGate, HookPhasePre, "PreToolUse", reg(hookDialectJuniePreTool), wired(),
		host(hookJunieRegisteredTimeout, HostProceeds), dec(HookDecisionDeny, HookDecisionAsk, HookDecisionModify, HookDecisionContext),
		note("CLI only: hooks do not run in ACP/IDE mode. Top-level decision deny|ask with reason in ~/.junie/config.json \"hooks\" (the reason reaches the model as an error); Observer registers a 10 s timeout because the PreToolUse default is 2 s; a timeout or crash lets the tool continue. Windows: yes (cmd /c). Installed on demo node 1 (junie 3579.5, 2026-10-07) but it needs a JetBrains account or JUNIE_API_KEY, and it refuses to start with HOME pointed at a throwaway dir: doc-grounded, `observer init` only")),
	junieB(HookCapPromptGate, HookPhasePre, "UserPromptSubmit", dec(decDenyContext...)),
	junieB(HookCapSessionLifecycle, HookPhaseStart, "SessionStart", dec(decMessage...)),
	junieB(HookCapSessionLifecycle, HookPhaseEnd, "SessionEnd"),
	junieB(HookCapTurnLifecycle, HookPhaseEnd, "Stop", dec(decDeny...)),
}

// --- deepseek (in-process Cordis plugins; field names inferred) -------------

var deepseekB = func(c HookCap, phase, event string, opts ...hbOpt) HookBinding {
	return hb(c, phase, event, hgDoc(hookSrcDeepSeek), append([]hbOpt{proc(ProcInProcessPID)}, opts...)...)
}

var hookBindingsDeepSeek = []HookBinding{
	deepseekB(HookCapToolGate, HookPhasePre, "tools/pre-execute", dec(decDenyAsk...), pluginShimRendered,
		note("in-process TS (Cordis) plugin; payload field names inferred. The plugin shim renders {kind: \"deny\"} / next(), but no user plugin directory is documented (profile layers, cordis.patch.yml), so Observer does not register it")),
	deepseekB(HookCapSessionLifecycle, HookPhaseStart, "agent/created"),
	deepseekB(HookCapTurnLifecycle, HookPhaseEnd, "agent/turn-stopping"),
	deepseekB(HookCapModelCallObserve, HookPhasePre, "agent/pre-step"),
}

// --- copilot-cli / copilot (VS Code agent hooks) ----------------------------

var (
	copilotCLIB = cloneB(hookSrcCopilotCLI)
	vscodeB     = cloneB(hookSrcVSCode)
)

// copilot 1.0.91 live record (T-325): a deny reached the model as "Denied by
// preToolUse hook: <reason>" and the fake target survived; {} let ls run; an
// ask was refused headless as "unable to ask user for confirmation".
const (
	hookLiveCopilotCLIToolGate     = "hook-parity live-ground 2026-10-06 (T-325): copilot 1.0.91 -p on demo node 1, BYOK offline mode against a loopback mock, throwaway HOME, hooks written by observer init"
	hookLiveCopilotCLIToolGateDate = "2026-10-06"
)

var hookBindingsCopilotCLI = []HookBinding{
	copilotCLIB(HookCapToolGate, HookPhasePre, "preToolUse", reg(hookDialectCopilotPreTool), wired(),
		liveAt(hookLiveCopilotCLIToolGate, hookLiveCopilotCLIToolGateDate),
		dec(decDenyAskModify...), host(hookCopilotCLIHostTimeout, HostBlocks),
		note("top-level permissionDecision deny|ask with permissionDecisionReason, in ~/.copilot/hooks/superbased-observer.json (version 1, the command in \"bash\"); toolArgs may arrive as a JSON-encoded string and is decoded. The allow is an empty object because permissionDecision:allow skips Copilot's own prompt. A crash or non-zero exit DENIES on the host side (a timeout fails open). A deny-not-blocking report exists (github/copilot-cli #3874, closed); camelCase and PascalCase event names are both accepted. Windows: yes via the handler's powershell field, which Observer does not write")),
	copilotCLIB(HookCapPromptGate, HookPhasePre, "userPromptSubmitted",
		note("observe only for Observer: modifiedPrompt is honoured ONLY for SDK programmatic hooks; command and HTTP config-file userPromptSubmitted hooks have their output dropped (docs.github.com hooks-reference, fetched 2026-10-03), and no block exists, so there is no redact or block seam for a shell hook; prompt coverage is the proxy lane (hook-parity Lane B)")),
	copilotCLIB(HookCapSessionLifecycle, HookPhaseStart, "sessionStart", dec(decContext...)),
	copilotCLIB(HookCapSessionLifecycle, HookPhaseEnd, "sessionEnd"),
	copilotCLIB(HookCapTurnLifecycle, HookPhaseEnd, "agentStop", dec(decDeny...)),
	copilotCLIB(HookCapSubagentLifecycle, HookPhaseStart, "subagentStart", dec(decContext...)),
	copilotCLIB(HookCapSubagentLifecycle, HookPhaseEnd, "subagentStop", dec(decDeny...)),
	copilotCLIB(HookCapCompactLifecycle, HookPhasePre, "preCompact"),
}

var hookBindingsCopilot = []HookBinding{
	vscodeB(HookCapToolGate, HookPhasePre, "PreToolUse", dec(decDenyAskModify...),
		note("VS Code agent hooks (Preview, chat.useHooks on by default) also replay ~/.claude/settings.json (audit section 4.4)")),
	vscodeB(HookCapPromptGate, HookPhasePre, "UserPromptSubmit", dec(decContext...), note("context only; block unstated")),
	vscodeB(HookCapSessionLifecycle, HookPhaseStart, "SessionStart", dec(decContext...)),
	vscodeB(HookCapTurnLifecycle, HookPhaseEnd, "Stop"),
	vscodeB(HookCapSubagentLifecycle, HookPhaseStart, "SubagentStart"),
	vscodeB(HookCapSubagentLifecycle, HookPhaseEnd, "SubagentStop"),
	vscodeB(HookCapCompactLifecycle, HookPhasePre, "PreCompact"),
}

// --- cline / cline-cli (shared SDK hook runtime) ---------------------------

var clineB = cloneB(hookSrcCline)

// hookNoteClineGate is the shared vendor fact about the SDK hook runtime's
// PreToolUse file, stated once for both rows that see it.
const hookNoteClineGate = "file-per-event executables: the SDK hook runtime reads ~/.cline/hooks/PreToolUse. A block is {\"cancel\":true,\"errorMessage\"}, which STOPS THE RUN (no per-call deny exists); exit codes are ignored; the allow is an empty stdout; no ask (review has no effect). Timeout 120 s, fail open. macOS/Linux only (Windows .ps1 detached hooks broken, cline/cline #14767): Observer writes no Windows target"

var hookBindingsCline = []HookBinding{
	// T-342 (2026-10-04): the registration moved to the cline-cli row, the
	// surface live-verified to read ~/.cline/hooks. Whether the VS Code
	// extension reads that directory is unconfirmed, so this row keeps the
	// vendor surface only.
	clineB(HookCapToolGate, HookPhasePre, "PreToolUse", host(hookClineHostTimeout, HostProceeds), dec(decDenyModify...),
		note("vendor doc only: the extension shares the SDK hook runtime, but whether it reads ~/.cline/hooks is unconfirmed, so Observer registers this gate on the cline-cli row and records its verdicts under cline-cli. "+hookNoteClineGate)),
	clineB(HookCapPromptGate, HookPhasePre, "UserPromptSubmit",
		note("probe_required: the installed cline 3.0.68 SDK (demo node 1, 2026-10-03) runs prompt_submit hooks DETACHED (detached: detachAsyncHooks ?? true) and also fires them after the user message is added, with payload {hookName:\"prompt_submit\", userPromptSubmit:{prompt, attachments}}; a cancel reply cannot stop the prompt there, so no dialect row (the VS Code extension's detach setting is unconfirmed). Observe-only until a host is shown to wait on the hook (hook-parity Lane B)")),
	clineB(HookCapSessionLifecycle, HookPhaseStart, "TaskStart"),
	clineB(HookCapSessionLifecycle, HookPhaseStart, "TaskResume"),
	clineB(HookCapSessionLifecycle, HookPhaseEnd, "SessionShutdown"),
	clineB(HookCapTurnLifecycle, HookPhaseEnd, "TaskComplete"),
	clineB(HookCapTurnLifecycle, HookPhaseEnd, "TaskError"),
	clineB(HookCapTurnLifecycle, HookPhaseEnd, "TaskCancel"),
	clineB(HookCapCompactLifecycle, HookPhasePre, "PreCompact", note("never fires")),
}

// cline-cli: Observer writes the SDK runtime's ~/.cline/hooks/PreToolUse
// (the tool gate, T-342) and TAILS its hooks.jsonl (the lifecycle and tool
// result rows below, served by the clinecli adapter, not by `observer hook`).
var clineCLITailB = func(c HookCap, phase, event string, opts ...hbOpt) HookBinding {
	return hb(c, phase, event, hgFixture("testdata/clinecli (hooks.jsonl tailer, internal/adapter/clinecli/hook.go)"),
		append([]hbOpt{sess(), dialectOnly(hookDialectClineCLITail), wired()}, opts...)...)
}

var hookBindingsClineCLI = []HookBinding{
	// T-342 / D-230: the live grounding of fix/live-ground-2026-10-04
	// (e951c73e0, 32c46db65, 0764620f2) was taken with Cline CLI 3.0.68, so it
	// sits on this row; the extension's cline row stays vendor doc.
	clineB(HookCapToolGate, HookPhasePre, "PreToolUse", hgLiveOpt(hookLiveClineToolGate), reg(hookDialectClinePreTool), wired(),
		host(hookClineHostTimeout, HostProceeds), dec(decDenyModify...),
		note("Observer writes ~/.cline/hooks/PreToolUse, a bash script that execs `observer hook cline-cli PreToolUse`, so Cline CLI gate verdicts are recorded under cline-cli (T-342: the CLI reads this directory, live 2026-10-04; a script an earlier version wrote for `observer hook cline` is recognised and rewritten). "+
			"Live, Cline CLI 3.0.68 names tools in the CLI vocabulary (run_commands with a list or a bare string in commands, read_files with files[]); the cancel reply aborted the run before rm -rf ~/x ran and the CLI printed \"[abort] aborted by another client\". "+hookNoteClineGate)),
	clineCLITailB(HookCapSessionLifecycle, HookPhaseStart, "agent_start"),
	clineCLITailB(HookCapSessionLifecycle, HookPhaseEnd, "session_shutdown"),
	clineCLITailB(HookCapTurnLifecycle, HookPhaseEnd, "agent_end"),
	clineCLITailB(HookCapToolObserve, HookPhasePost, "tool_result"),
}

// --- in-process TS plugin hosts: kilo-code(-cli), opencode, pi, prime-agent,
// openclaw --------------------------------------------------------------------

func pluginB(g Grounding) func(c HookCap, phase, event string, opts ...hbOpt) HookBinding {
	return func(c HookCap, phase, event string, opts ...hbOpt) HookBinding {
		return hb(c, phase, event, g, append([]hbOpt{proc(ProcInProcessPID)}, opts...)...)
	}
}

var (
	kiloB      = pluginB(hgDoc(hookSrcKilo))
	opencodeB  = pluginB(hgDoc(hookSrcOpenCode))
	piB        = pluginB(hgDoc(hookSrcPi))
	primeB     = pluginB(hgInferred(hookSrcPrimeAgent))
	openclawB  = pluginB(hgDoc(hookSrcOpenClaw))
	hermesDocB = pluginB(hgDoc(hookSrcHermes))
)

// Plugin-shim bindings (hook-parity Lane D): the internal/hook/pluginshim
// TS shim subscribes to a host's Registered bindings and speaks the plugin
// dialects (internal/hook plugingate.go). The shim passes the host's
// session id and its own pid.
var (
	// pluginShimGate: the tool gate through the shim.
	pluginShimGate = all(reg(hookDialectPluginShim), wired(), sess())
	// pluginShimPrompt: OpenCode / Kilo chat.message. The seam is
	// modify-only; the shim delivers a block by replacing the message text
	// before the model sees it, so the reply can express deny and modify.
	pluginShimPrompt = all(reg(hookDialectPluginShimPrompt), wired(), sess(),
		dec(HookDecisionAllow, HookDecisionDeny, HookDecisionModify))
	// pluginShimRendered: the shim renders this host's gate (dialect set)
	// but Observer does not place it: the host's plugin install location is
	// not grounded, so the binding is neither registered nor wired.
	pluginShimRendered = dialectOnly(hookDialectPluginShim)
)

const (
	notePluginChatMessage  = "modify-only seam: the plugin shim replaces the message text with the block reason before the model sees it, and tells the developer with the host's TUI toast (client.tui.showToast, @opencode-ai/sdk 1.18.31 / Kilo CLI 7.8.3; a headless `run` has no TUI, so no toast; the dashboard post-hoc toast still applies); the session id keys resend-to-confirm"
	notePluginOpenCodeTool = "plugin shim: throw = deny; no native ask (an ask is a deny with a reason); sub-agent coverage disputed (#5894); the Desktop app never dispatches plugin hooks (#38604)"
)

// kiloBindings returns Kilo's plugin bindings. The CLI row gets the shim
// (wired); the legacy IDE extension row keeps the vendor surface only.
func kiloBindings(shim bool) []HookBinding {
	gate, prompt := []hbOpt{dec(decDenyModify...), note("in-process plugin: throw = deny (OpenCode contract)")}, []hbOpt{dec(decModify...), note("modify only")}
	if shim {
		gate = []hbOpt{dec(decDenyModify...), pluginShimGate, pluginShimLive, note("plugin shim at ~/.config/kilo/plugin/: throw = deny (OpenCode contract); no native ask")}
		prompt = []hbOpt{pluginShimPrompt, pluginShimLive, note(notePluginChatMessage)}
	}
	return []HookBinding{
		kiloB(HookCapToolGate, HookPhasePre, "tool.execute.before", gate...),
		kiloB(HookCapPromptGate, HookPhasePre, "chat.message", prompt...),
		kiloB(HookCapSessionLifecycle, HookPhaseStart, "session.created"),
		kiloB(HookCapTurnLifecycle, HookPhaseEnd, "session.idle"),
		kiloB(HookCapCompactLifecycle, HookPhasePre, "experimental.session.compacting"),
		kiloB(HookCapContextInject, HookPhasePre, "experimental.chat.system.transform", dec(decContext...)),
	}
}

var hookBindingsOpenCode = []HookBinding{
	opencodeB(HookCapToolGate, HookPhasePre, "tool.execute.before", dec(decDenyModify...), pluginShimGate, pluginShimLive,
		note(notePluginOpenCodeTool)),
	opencodeB(HookCapPromptGate, HookPhasePre, "chat.message", pluginShimPrompt, pluginShimLive, note(notePluginChatMessage)),
	opencodeB(HookCapSessionLifecycle, HookPhaseStart, "session.created"),
	opencodeB(HookCapSessionLifecycle, HookPhaseEnd, "session.deleted"),
	opencodeB(HookCapTurnLifecycle, HookPhaseEnd, "session.idle"),
	opencodeB(HookCapCompactLifecycle, HookPhasePre, "experimental.session.compacting"),
	opencodeB(HookCapCompactLifecycle, HookPhasePost, "session.compacted"),
	opencodeB(HookCapContextInject, HookPhasePre, "experimental.chat.system.transform", dec(decContext...)),
}

var hookBindingsPi = []HookBinding{
	piB(HookCapToolGate, HookPhasePre, "tool_call", dec(decDenyModify...), pluginShimGate, pluginShimLive,
		note("plugin shim at ~/.pi/agent/extensions/: {block, reason}; a handler failure blocks the tool (pi fail-safe); ask only via the TUI confirm, so an ask is a deny with a reason")),
	piB(HookCapPromptGate, HookPhasePre, "input", dec(decDenyModify...)),
	piB(HookCapSessionLifecycle, HookPhaseStart, "session_start"),
	piB(HookCapSessionLifecycle, HookPhaseEnd, "session_shutdown"),
	piB(HookCapTurnLifecycle, HookPhaseEnd, "turn_end"),
	piB(HookCapTurnLifecycle, HookPhaseEnd, "agent_end"),
	piB(HookCapCompactLifecycle, HookPhasePre, "session_before_compact"),
	piB(HookCapContextInject, HookPhaseStart, "before_agent_start", dec(decContext...)),
}

var hookBindingsPrimeAgent = []HookBinding{
	primeB(HookCapToolGate, HookPhasePre, "tool_call", dec(decDeny...), note("pi fork; hooks issue #872 closed not planned; the only tool is ipython")),
	primeB(HookCapPromptGate, HookPhasePre, "input"),
}

var hookBindingsOpenClaw = []HookBinding{
	openclawB(HookCapToolGate, HookPhasePre, "before_tool_call", dec(decDenyAskModify...), host(15*time.Second, HostBlocks), pluginShimRendered,
		note("runner-scoped: skips MCP under claude-cli (#155946) and Codex exec (#111254); fails closed on timeout (15 s default). The plugin shim renders {block, blockReason} / requireApproval, but OpenClaw loads a plugin only after `openclaw plugins install --link`, so Observer does not register it")),
	openclawB(HookCapPromptGate, HookPhasePre, "before_agent_run", dec(decDeny...), note("embedded / CLI runners only")),
	openclawB(HookCapSessionLifecycle, HookPhaseStart, "session_start"),
	openclawB(HookCapSessionLifecycle, HookPhaseEnd, "session_end"),
	openclawB(HookCapTurnLifecycle, HookPhaseEnd, "agent_end"),
	openclawB(HookCapSubagentLifecycle, HookPhaseStart, "subagent_spawned"),
	openclawB(HookCapSubagentLifecycle, HookPhaseEnd, "subagent_ended"),
	openclawB(HookCapCompactLifecycle, HookPhasePre, "before_compaction"),
	openclawB(HookCapCompactLifecycle, HookPhasePost, "after_compaction"),
	openclawB(HookCapContextInject, HookPhasePre, "before_prompt_build", dec(decContext...)),
}

// --- hermes: Observer's embedded Python plugin ------------------------------

var hermesPluginB = func(c HookCap, phase, event string, opts ...hbOpt) HookBinding {
	return hb(c, phase, event, hgFixture("internal/hook/hermesplugin/__init__.py + testdata/hermes"),
		append([]hbOpt{sess(), proc(ProcInProcessPID), reg(hookDialectHermesPlugin), wired()}, opts...)...)
}

var hookBindingsHermes = []HookBinding{
	hermesPluginB(HookCapSessionLifecycle, HookPhaseStart, "on_session_start"),
	hermesPluginB(HookCapSessionLifecycle, HookPhaseEnd, "on_session_end"),
	hermesPluginB(HookCapToolObserve, HookPhasePost, "post_tool_call", guardChannel("plugin"),
		note("observe-only audit-log plugin bridge - no documented deny semantics (Q4)")),
	hermesPluginB(HookCapModelCallObserve, HookPhasePost, "post_api_request"),
	hermesPluginB(HookCapSubagentLifecycle, HookPhaseEnd, "subagent_stop"),
	hermesDocB(HookCapToolGate, HookPhasePre, "pre_tool_call", dec(decDenyAskModify...), host(30*time.Second, HostBlocks),
		sess(), reg(hookDialectPluginShim), wired(),
		note("Observer's plugin subscribes (Lane A7, operator decision 2026-10-02): {\"action\": \"block\", \"message\"} denies, {\"action\": \"approve\", \"message\"} raises Hermes' own approval prompt; Hermes fails a timed-out pre_tool_call CLOSED at plugins.hook_callback_timeout (30 s default)")),
	hermesDocB(HookCapContextInject, HookPhasePre, "pre_llm_call", dec(decContext...)),
	hermesDocB(HookCapTurnLifecycle, HookPhaseEnd, "agent_loop_stopped"),
	hermesDocB(HookCapSubagentLifecycle, HookPhaseStart, "subagent_start"),
	hermesDocB(HookCapCompactLifecycle, HookPhasePost, "session:compress", note("gateway hook; `hermes serve` skips shell hooks (#102504)")),
}

// --- *-web: Observer's own MV3 extension (native messaging) ----------------

func browserBindings() []HookBinding {
	return []HookBinding{
		hb(HookCapTurnLifecycle, HookPhaseEnd, "capture", hgFixture(hookSrcBrowserExt), dialectOnly(hookDialectBrowserCapture), wired(),
			note("the extension relays each captured turn; content-script submit interception is per-site unverified")),
	}
}

// Honest-zero notes for rows with no hook surface Observer could bind.
const (
	hookNoteGrokbot = "no hook surface: Grok Bot runs tools on hosted VMs (https://docs.x.ai/grok-bot/security)"
	hookNoteZed     = "no hook surface: only static tool_permissions regex rules (https://zed.dev/docs/ai/tool-permissions), a native-compile target, not a hook"
	hookNoteAider   = "no hook surface: only post-hoc --lint-cmd / --test-cmd / --notifications-command options (https://aider.chat/docs/config/options.html)"
	hookNoteCowork  = "no hook surface: Cowork's VM never runs hooks (anthropics/claude-code #63360, #27398, #63047); Claude Code coverage does not extend to Cowork sessions"
	hookNoteFreebuf = "no hook surface: the SDK handleEvent callback is observe-only (https://www.codebuff.com/docs/advanced/sdk)"
)
