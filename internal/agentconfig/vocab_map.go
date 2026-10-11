package agentconfig

import "sort"

// MapRow maps one common key onto one tool's catalogue rows (plan section
// 2.4). A canonical value can fan out to several native settings (Codex
// approval_mode -> approval_policy + sandbox_mode); Settings lists them in
// the order Forward returns values. A tool with no row for a key does not
// expose it (the fan-out preview says so); it is never guessed.
type MapRow struct {
	Key  CommonKey
	Tool string
	// Settings are catalogue SettingRow ids (pinned: each resolves).
	Settings  []string
	Transform TransformID
	Params    TransformParams
	// Lossy: read -> write -> read is not the identity for every canonical
	// value (pinned by TestMapRowRoundTrip). Shown in the UI.
	Lossy  bool
	Note   string
	Source string
}

// MapRows returns every map row of every catalogued tool, sorted by key
// then tool.
func MapRows() []MapRow {
	var out []MapRow
	for _, tc := range toolCatalogs {
		for _, m := range tc.Map {
			m.Settings = append([]string(nil), m.Settings...)
			out = append(out, m)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Key != out[j].Key {
			return out[i].Key < out[j].Key
		}
		return out[i].Tool < out[j].Tool
	})
	return out
}

// MapRowsFor returns one common key's rows (one per tool exposing it).
func MapRowsFor(k CommonKey) []MapRow {
	var out []MapRow
	for _, m := range MapRows() {
		if m.Key == k {
			out = append(out, m)
		}
	}
	return out
}

// MapRowFor returns one tool's row for a key.
func MapRowFor(tool string, k CommonKey) (MapRow, bool) {
	for _, m := range MapRowsFor(k) {
		if m.Tool == tool {
			return m, true
		}
	}
	return MapRow{}, false
}

// --- shared grammars and shapes (each grounded where the tool rows cite it)

const (
	claudePermsRef  = "https://code.claude.com/docs/en/permissions"
	geminiToolsRef  = "https://raw.githubusercontent.com/google-gemini/gemini-cli/main/docs/reference/configuration.md"
	geminiMCPRef    = "https://raw.githubusercontent.com/google-gemini/gemini-cli/main/docs/tools/mcp-server.md"
	crushToolsRef   = "https://raw.githubusercontent.com/charmbracelet/crush/main/README.md"
	codexMCPRef     = "https://learn.chatgpt.com/docs/mcp"
	opencodeMCPRef  = "https://opencode.ai/docs/mcp-servers/"
	opencodeModelRf = "https://opencode.ai/docs/models/"
)

// claudeRuleGrammar is Claude Code's permission rule syntax:
// Tool or Tool(specifier); WebFetch(domain:...); mcp__server__tool.
var claudeRuleGrammar = RuleGrammar{
	"shell":  {Name: "Bash", Style: RuleCall},
	"read":   {Name: "Read", Style: RuleCall},
	"edit":   {Name: "Edit", Style: RuleCall},
	"write":  {Name: "Write", Style: RuleCall},
	"fetch":  {Name: "WebFetch", Style: RuleCall, PatternPrefix: "domain:"},
	"search": {Name: "WebSearch", Style: RuleBare},
	"mcp":    {Name: "mcp__", Style: RulePrefix, Sep: "__"},
}

// geminiRuleGrammar is Gemini CLI's tools.allowed / tools.exclude spelling:
// built-in tool names, with a command pattern only on run_shell_command.
var geminiRuleGrammar = RuleGrammar{
	"shell":  {Name: "run_shell_command", Style: RuleCall},
	"read":   {Name: "read_file", Style: RuleBare},
	"edit":   {Name: "replace", Style: RuleBare},
	"write":  {Name: "write_file", Style: RuleBare},
	"fetch":  {Name: "web_fetch", Style: RuleBare},
	"search": {Name: "google_web_search", Style: RuleBare},
}

// crushAllowGrammar is Crush's permissions.allowed_tools spelling: bare
// tool names plus mcp_<server>_<tool>; no patterns.
var crushAllowGrammar = RuleGrammar{
	"shell": {Name: "bash", Style: RuleBare},
	"read":  {Name: "view", Style: RuleBare},
	"edit":  {Name: "edit", Style: RuleBare},
	"write": {Name: "write", Style: RuleBare},
	"fetch": {Name: "fetch", Style: RuleBare},
	"mcp":   {Name: "mcp_", Style: RulePrefix, Sep: "_"},
}

// crushDisableGrammar is options.disabled_tools: built-in tools only.
var crushDisableGrammar = RuleGrammar{
	"shell": {Name: "bash", Style: RuleBare},
	"read":  {Name: "view", Style: RuleBare},
	"edit":  {Name: "edit", Style: RuleBare},
	"write": {Name: "write", Style: RuleBare},
	"fetch": {Name: "fetch", Style: RuleBare},
}

var (
	// mcpCodex: [mcp_servers.<name>] command/args/env, or url for a
	// streamable HTTP server.
	mcpCodex = MCPShape{
		EnvKey: "env", TypeNames: map[string]string{"stdio": "", "http": ""},
		URLKeys: map[string]string{"http": "url"},
	}
	// mcpGemini: mcpServers.<name> command/args/env; httpUrl for streamable
	// HTTP, url for SSE.
	mcpGemini = MCPShape{
		EnvKey: "env", TypeNames: map[string]string{"stdio": "", "http": "", "sse": ""},
		URLKeys: map[string]string{"http": "httpUrl", "sse": "url"},
	}
	// mcpCrush: mcp.<name> with an explicit type stdio | http | sse.
	mcpCrush = MCPShape{
		EnvKey: "env", TypeKey: "type", StdioTypeExplicit: true,
		TypeNames: map[string]string{"stdio": "stdio", "http": "http", "sse": "sse"},
		URLKeys:   map[string]string{"http": "url", "sse": "url"},
	}
	// mcpOpenCode: mcp.<name> {type: local, command: [cmd, ...args],
	// environment} or {type: remote, url}.
	mcpOpenCode = MCPShape{
		CommandArray: true, EnvKey: "environment", TypeKey: "type", StdioTypeExplicit: true,
		TypeNames: map[string]string{"stdio": "local", "http": "remote"},
		URLKeys:   map[string]string{"http": "url"},
	}
)

const mcpLossyNote = "Fields outside {command, args, env, type, url} (timeouts, trust, headers, enabled) are not carried; the writer merges per entry and never drops Observer's own entry (owned span)."

// --- claude-code -----------------------------------------------------------

var claudeCodeMap = []MapRow{
	{Key: KeyModel, Tool: "claude-code", Settings: []string{"claude-code/model"}, Transform: TransformModelAlias, Source: ccRef},
	{
		Key: KeySmallModel, Tool: "claude-code", Settings: []string{"claude-code/env.ANTHROPIC_DEFAULT_HAIKU_MODEL"},
		Transform: TransformModelAlias, Params: TransformParams{Encoding: EncEnvString}, Source: ccEnv,
		Note: "ANTHROPIC_SMALL_FAST_MODEL is the deprecated spelling.",
	},
	{
		Key: KeyMaxOutputTokens, Tool: "claude-code", Settings: []string{"claude-code/env.CLAUDE_CODE_MAX_OUTPUT_TOKENS"},
		Transform: TransformIdentity, Params: TransformParams{Encoding: EncEnvInt}, Source: ccEnv,
	},
	{
		Key: KeyContextLimit, Tool: "claude-code", Settings: []string{"claude-code/autoCompactWindow"},
		Transform: TransformIdentity, Source: ccRef, Note: "Claude Code budgets compaction against this window.",
	},
	{
		Key: KeyAutoCompact, Tool: "claude-code", Settings: []string{"claude-code/env.CLAUDE_CODE_AUTOCOMPACT_PCT_OVERRIDE"},
		Transform: TransformIdentity, Params: TransformParams{Encoding: EncEnvInt}, Source: ccEnv,
	},
	{
		Key: KeyReasoningEffort, Tool: "claude-code", Settings: []string{"claude-code/effortLevel"}, Transform: TransformEnumMap,
		Params: TransformParams{Enum: []EnumPair{
			{"low", []any{"low"}}, {"medium", []any{"medium"}}, {"high", []any{"high"}}, {"xhigh", []any{"xhigh"}},
		}}, Source: ccRef, Note: "none, minimal and max are refused: max is session-only in Claude Code.",
	},
	{
		Key: KeyApprovalMode, Tool: "claude-code", Settings: []string{"claude-code/permissions.defaultMode"}, Transform: TransformEnumMap,
		Params: TransformParams{Enum: []EnumPair{
			{"plan", []any{"plan"}},
			{"ask_edits", []any{"default"}},
			{"auto_edits", []any{"acceptEdits"}},
			{"full_auto", []any{"bypassPermissions"}},
			{"ask_edits", []any{"manual"}},
		}}, Lossy: true, Source: claudePermsRef,
		Note: "ask (prompt even for reads) is refused; \"manual\" reads back as ask_edits (an alias of \"default\"); auto, dontAsk and delegate have no canonical form.",
	},
	{
		Key: KeySandboxMode, Tool: "claude-code", Settings: []string{"claude-code/sandbox.enabled"}, Transform: TransformEnumMap,
		Params: TransformParams{Enum: []EnumPair{{"workspace_write", []any{true}}, {"off", []any{false}}}},
		Source: "https://code.claude.com/docs/en/sandboxing", Note: "read_only is refused: Claude's sandbox always permits workspace writes.",
	},
	{
		Key: KeyAllowedTools, Tool: "claude-code", Settings: []string{"claude-code/permissions.allow"}, Transform: TransformRuleTranslate,
		Params: TransformParams{Grammar: claudeRuleGrammar}, Source: claudePermsRef,
	},
	{
		Key: KeyDeniedTools, Tool: "claude-code", Settings: []string{"claude-code/permissions.deny"}, Transform: TransformRuleTranslate,
		Params: TransformParams{Grammar: claudeRuleGrammar}, Source: claudePermsRef,
	},
	{
		Key: KeyAdditionalDirs, Tool: "claude-code", Settings: []string{"claude-code/permissions.additionalDirectories"},
		Transform: TransformIdentity, Source: ccRef,
	},
	{Key: KeyEnv, Tool: "claude-code", Settings: []string{"claude-code/env"}, Transform: TransformIdentity, Source: ccRef},
	{
		Key: KeyTelemetryOff, Tool: "claude-code", Settings: []string{"claude-code/env.DISABLE_TELEMETRY"},
		Transform: TransformIdentity, Params: TransformParams{Encoding: EncEnvBool}, Source: ccEnv,
	},
	{
		Key: KeyAutoUpdate, Tool: "claude-code", Settings: []string{"claude-code/env.DISABLE_AUTOUPDATER"},
		Transform: TransformBoolInvert, Params: TransformParams{Encoding: EncEnvBool}, Source: ccEnv,
	},
	{
		Key: KeyTheme, Tool: "claude-code", Settings: []string{"claude-code/theme"}, Transform: TransformEnumMap,
		Params: TransformParams{Enum: []EnumPair{{"light", []any{"light"}}, {"dark", []any{"dark"}}}}, Source: ccRef,
		Note: "auto, the daltonized / ansi variants and custom themes stay tool-specific.",
	},
	{
		Key: KeyAPIKeyRef, Tool: "claude-code", Settings: []string{"claude-code/apiKeyHelper"}, Transform: TransformIdentity, Source: ccRef,
		Note: "A helper command that prints the key; Observer never stores the key.",
	},
	{
		Key: KeyBaseURL, Tool: "claude-code", Settings: []string{"claude-code/env.ANTHROPIC_BASE_URL"},
		Transform: TransformIdentity, Params: TransformParams{Encoding: EncEnvString}, Source: ccEnv,
	},
}

// --- codex lineage (codex, open-interpreter) --------------------------------

// codexLineageMap is the map set both Codex-config tools share; the
// approval fan-out differs (Open Interpreter keeps "untrusted").
func codexLineageMap(tool, ref string, approval []EnumPair, effort []string) []MapRow {
	id := func(k string) string { return tool + "/" + k }
	efforts := make([]EnumPair, 0, len(effort))
	for _, e := range effort {
		efforts = append(efforts, EnumPair{e, []any{e}})
	}
	return []MapRow{
		{Key: KeyModel, Tool: tool, Settings: []string{id("model")}, Transform: TransformModelAlias, Source: ref},
		{Key: KeyContextLimit, Tool: tool, Settings: []string{id("model_context_window")}, Transform: TransformIdentity, Source: ref},
		{
			Key: KeyReasoningEffort, Tool: tool, Settings: []string{id("model_reasoning_effort")}, Transform: TransformEnumMap,
			Params: TransformParams{Enum: efforts}, Source: ref, Note: "The model advertises its own levels; unsupported ones are refused.",
		},
		{
			Key: KeyApprovalMode, Tool: tool, Settings: []string{id("approval_policy"), id("sandbox_mode")}, Transform: TransformEnumMap,
			Params: TransformParams{Enum: approval}, Lossy: true, Source: ref,
			Note: "Fans out to approval_policy + sandbox_mode; plan is refused (no plan mode in the config file).",
		},
		{
			Key: KeySandboxMode, Tool: tool, Settings: []string{id("sandbox_mode")}, Transform: TransformEnumMap,
			Params: TransformParams{Enum: []EnumPair{
				{"read_only", []any{"read-only"}}, {"workspace_write", []any{"workspace-write"}}, {"off", []any{"danger-full-access"}},
			}}, Source: ref,
		},
		{
			Key: KeyAdditionalDirs, Tool: tool, Settings: []string{id("sandbox_workspace_write.writable_roots")},
			Transform: TransformIdentity, Source: ref, Note: "Extra WRITABLE roots of the workspace-write sandbox.",
		},
		{
			Key: KeyMCPServers, Tool: tool, Settings: []string{id("mcp_servers")}, Transform: TransformMCPShape,
			Params: TransformParams{MCP: mcpCodex}, Lossy: true, Source: codexMCPRef, Note: mcpLossyNote,
		},
		{
			Key: KeyEnv, Tool: tool, Settings: []string{id("shell_environment_policy.set")}, Transform: TransformIdentity, Source: ref,
			Note: "Variables set for the commands the agent runs.",
		},
		{Key: KeyTelemetryOff, Tool: tool, Settings: []string{id("analytics.enabled")}, Transform: TransformBoolInvert, Source: ref},
		{
			Key: KeyAutoUpdate, Tool: tool, Settings: []string{id("check_for_update_on_startup")}, Transform: TransformIdentity, Source: ref,
			Note: "Checks and prompts for updates; it does not install them itself.",
		},
		{
			Key: KeyInstructionsFiles, Tool: tool, Settings: []string{id("project_doc_fallback_filenames")}, Transform: TransformIdentity,
			Source: ref, Note: "Fallback names read when AGENTS.md is absent.",
		},
		{Key: KeyBaseURL, Tool: tool, Settings: []string{id("openai_base_url")}, Transform: TransformIdentity, Source: ref},
	}
}

var (
	codexApproval = []EnumPair{
		{"ask_edits", []any{"on-request", "read-only"}},
		{"auto_edits", []any{"on-request", "workspace-write"}},
		{"full_auto", []any{"never", "danger-full-access"}},
	}
	oiApproval = []EnumPair{
		{"ask", []any{"untrusted", "read-only"}},
		{"ask_edits", []any{"on-request", "read-only"}},
		{"auto_edits", []any{"on-request", "workspace-write"}},
		{"full_auto", []any{"never", "danger-full-access"}},
	}
	codexMap = codexLineageMap("codex", codexRef, codexApproval, []string{"minimal", "low", "medium", "high", "xhigh"})
	oiMap    = codexLineageMap("open-interpreter", oiRef, oiApproval, []string{"minimal", "low", "medium", "high"})
)

// --- gemini-cli --------------------------------------------------------------

var geminiCLIMap = []MapRow{
	{Key: KeyModel, Tool: "gemini-cli", Settings: []string{"gemini-cli/model.name"}, Transform: TransformModelAlias, Source: geminiRef},
	{
		Key: KeyAutoCompact, Tool: "gemini-cli", Settings: []string{"gemini-cli/model.compressionThreshold"}, Transform: TransformScale,
		Params: TransformParams{Factor: 0.01}, Source: geminiRef, Note: "A fraction of the window (percent / 100).",
	},
	{
		Key: KeyApprovalMode, Tool: "gemini-cli", Settings: []string{"gemini-cli/general.defaultApprovalMode"}, Transform: TransformEnumMap,
		Params: TransformParams{Enum: []EnumPair{
			{"plan", []any{"plan"}}, {"ask_edits", []any{"default"}}, {"auto_edits", []any{"auto_edit"}},
		}}, Source: geminiRef, Note: "full_auto (YOLO) is command-line only; ask is refused.",
	},
	{
		Key: KeySandboxMode, Tool: "gemini-cli", Settings: []string{"gemini-cli/tools.sandbox"}, Transform: TransformEnumMap,
		Params: TransformParams{Enum: []EnumPair{{"workspace_write", []any{true}}, {"off", []any{false}}}},
		Source: geminiRef, Note: "A sandbox profile path or command reads back as tool-specific; read_only is refused.",
	},
	{
		Key: KeyAllowedTools, Tool: "gemini-cli", Settings: []string{"gemini-cli/tools.allowed"}, Transform: TransformRuleTranslate,
		Params: TransformParams{Grammar: geminiRuleGrammar}, Source: geminiToolsRef,
	},
	{
		Key: KeyDeniedTools, Tool: "gemini-cli", Settings: []string{"gemini-cli/tools.exclude"}, Transform: TransformRuleTranslate,
		Params: TransformParams{Grammar: geminiRuleGrammar}, Source: geminiToolsRef,
	},
	{Key: KeyAdditionalDirs, Tool: "gemini-cli", Settings: []string{"gemini-cli/context.includeDirectories"}, Transform: TransformIdentity, Source: geminiRef},
	{
		Key: KeyMCPServers, Tool: "gemini-cli", Settings: []string{"gemini-cli/mcpServers"}, Transform: TransformMCPShape,
		Params: TransformParams{MCP: mcpGemini}, Lossy: true, Source: geminiMCPRef, Note: mcpLossyNote,
	},
	{
		Key: KeyTelemetryOff, Tool: "gemini-cli", Settings: []string{"gemini-cli/privacy.usageStatisticsEnabled"}, Transform: TransformBoolInvert,
		Source: geminiRef, Note: "Usage statistics sent to Google; telemetry.enabled is a separate local OpenTelemetry export.",
	},
	{Key: KeyAutoUpdate, Tool: "gemini-cli", Settings: []string{"gemini-cli/general.enableAutoUpdate"}, Transform: TransformIdentity, Source: geminiRef},
	{
		Key: KeyInstructionsFiles, Tool: "gemini-cli", Settings: []string{"gemini-cli/context.fileName"}, Transform: TransformIdentity,
		Source: geminiRef, Note: "Replaces GEMINI.md as the context file name; a single string reads back as a one-item list.",
	},
}

// --- opencode lineage (opencode, kilo-code-cli, kilo-code) ------------------

func opencodeLineageMap(tool, ref string) []MapRow {
	id := func(k string) string { return tool + "/" + k }
	return []MapRow{
		{
			Key: KeyModel, Tool: tool, Settings: []string{id("model")}, Transform: TransformModelAlias,
			Params: TransformParams{Model: ModelSpelling{Qualified: true}}, Source: opencodeModelRf,
		},
		{
			Key: KeySmallModel, Tool: tool, Settings: []string{id("small_model")}, Transform: TransformModelAlias,
			Params: TransformParams{Model: ModelSpelling{Qualified: true}}, Source: opencodeModelRf,
		},
		{
			Key: KeyApprovalMode, Tool: tool, Settings: []string{id("permission")}, Transform: TransformEnumMap,
			Params: TransformParams{Enum: []EnumPair{{"ask", []any{"ask"}}, {"full_auto", []any{"allow"}}}}, Source: ref,
			Note: "The scalar form applies to every tool; the per-tool object form stays in the tool form. plan, ask_edits and auto_edits are refused.",
		},
		{
			Key: KeyMCPServers, Tool: tool, Settings: []string{id("mcp")}, Transform: TransformMCPShape,
			Params: TransformParams{MCP: mcpOpenCode}, Lossy: true, Source: opencodeMCPRef, Note: mcpLossyNote,
		},
		{
			Key: KeyAutoUpdate, Tool: tool, Settings: []string{id("autoupdate")}, Transform: TransformEnumMap,
			Params: TransformParams{Enum: []EnumPair{{true, []any{true}}, {false, []any{false}}}}, Source: ref,
			Note: "\"notify\" (tell, do not install) has no canonical form.",
		},
		{Key: KeyInstructionsFiles, Tool: tool, Settings: []string{id("instructions")}, Transform: TransformIdentity, Source: ref},
	}
}

var (
	opencodeMap    = opencodeLineageMap("opencode", opencodeRef)
	kiloCodeCLIMap = opencodeLineageMap("kilo-code-cli", kiloRef)
	kiloCodeMap    = opencodeLineageMap("kilo-code", kiloRef)
)

// --- crush -----------------------------------------------------------------

var crushMap = []MapRow{
	{
		Key: KeyAllowedTools, Tool: "crush", Settings: []string{"crush/permissions.allowed_tools"}, Transform: TransformRuleTranslate,
		Params: TransformParams{Grammar: crushAllowGrammar}, Lossy: true, Source: crushToolsRef,
		Note: "Tool names only; patterns are refused. An MCP server or tool name containing \"_\" does not read back exactly (mcp_<server>_<tool>).",
	},
	{
		Key: KeyDeniedTools, Tool: "crush", Settings: []string{"crush/options.disabled_tools"}, Transform: TransformRuleTranslate,
		Params: TransformParams{Grammar: crushDisableGrammar}, Source: crushRef, Note: "Built-in tools only; patterns and MCP rules are refused.",
	},
	{
		Key: KeyMCPServers, Tool: "crush", Settings: []string{"crush/mcp"}, Transform: TransformMCPShape,
		Params: TransformParams{MCP: mcpCrush}, Lossy: true, Source: crushRef, Note: mcpLossyNote,
	},
	{Key: KeyEnv, Tool: "crush", Settings: []string{"crush/env"}, Transform: TransformIdentity, Source: crushRef},
	{Key: KeyTelemetryOff, Tool: "crush", Settings: []string{"crush/options.disable_metrics"}, Transform: TransformIdentity, Source: crushRef},
	{Key: KeyInstructionsFiles, Tool: "crush", Settings: []string{"crush/options.context_paths"}, Transform: TransformIdentity, Source: crushRef},
}
