package agentconfig

// Zed's native agent (phase P2, doc-authored). Everything lives in Zed's
// JSONC settings.json (user) and .zed/settings.json (project); there is no
// managed layer and no hook surface (static tool_permissions only). Agent
// threads sit in a separate SQLite threads.db that is a capture target, not
// config. Sources: audit docs/audits/adapter-config-surface-2026-10-03.md
// section 5 "zed" and the zed.dev pages below, re-fetched 2026-10-04.

const (
	zedAgentRef = "https://zed.dev/docs/ai/agent-settings"
	zedPermRef  = "https://zed.dev/docs/ai/tool-permissions"
	zedMCPRef   = "https://zed.dev/docs/ai/mcp"
	zedConfRef  = "https://zed.dev/docs/configuring-zed"
	zedAllRef   = "https://zed.dev/docs/reference/all-settings"
	zedTelemRef = "https://zed.dev/docs/telemetry"
	zedLLMRef   = "https://zed.dev/docs/ai/llm-providers"
	zedAudit    = "docs/audits/adapter-config-surface-2026-10-03.md section 5 zed (zed.dev docs, 2026-10-03)"
	zedModelNt  = "A {provider, model} object, not a string."
)

var zedFiles = []FileRow{
	{
		ID: "zed/user-settings", Tool: "zed", Shape: "zed/settings", Kind: KindSettings, Scope: ScopeUser,
		Paths:  OSPaths{Linux: "~/.config/zed/settings.json", Darwin: "~/.config/zed/settings.json", Windows: `%APPDATA%\Zed\settings.json`},
		Format: FormatJSONC, Precedence: 20, WriteNeeds: PrivUser, Grounding: doc(zedConfRef),
		Note: "Release-channel override blocks inside the file are applied automatically. Zed was not installed on node 1 (2026-10-03).",
	},
	{
		ID: "zed/project-settings", Tool: "zed", Shape: "zed/settings", Kind: KindSettings, Scope: ScopeProjectShared,
		Paths: samePath("<project>/.zed/settings.json"), Format: FormatJSONC, Precedence: 30, WriteNeeds: PrivUser, Grounding: doc(zedConfRef),
		Note: "Worktree settings: override the user file.",
	},
}

func zedRows(rows []SettingRow) []SettingRow {
	for i := range rows {
		r := &rows[i]
		r.Tool = "zed"
		r.ID = r.Tool + "/" + r.Path.String()
		if r.FileID == "" {
			r.FileID = "zed/settings"
		}
		if r.Owner == "" {
			r.Owner = OwnerUser
		}
		if r.Verified == "" {
			r.Verified = VerifiedDoc
		}
		if r.Source == "" {
			r.Source = zedAgentRef
		}
	}
	return rows
}

var zedSettings = zedRows([]SettingRow{
	// --- models
	{Path: KeyPath{"agent", "default_model"}, Type: TypeObject, Desc: "Default model for agent threads.", Note: zedModelNt},
	{Path: KeyPath{"agent", "default_model", "provider"}, Type: TypeString, Desc: "Provider of the default model."},
	{Path: KeyPath{"agent", "default_model", "model"}, Type: TypeString, Desc: "Model id of the default model."},
	{Path: KeyPath{"agent", "inline_assistant_model"}, Type: TypeObject, Desc: "Model for the inline assistant.", Note: zedModelNt},
	{Path: KeyPath{"agent", "commit_message_model"}, Type: TypeObject, Desc: "Model for git commit messages.", Note: zedModelNt},
	{Path: KeyPath{"agent", "thread_summary_model"}, Type: TypeObject, Desc: "Model for thread summaries.", Note: zedModelNt},
	{Path: KeyPath{"agent", "compaction_model"}, Type: TypeObject, Desc: "Model for context compaction (default: the thread's model).", Note: zedModelNt},
	{Path: KeyPath{"agent", "subagent_model"}, Type: TypeObject, Desc: "Model for subagents.", Note: zedModelNt},
	{Path: KeyPath{"agent", "commit_message_instructions"}, Type: TypeString, Desc: "Extra instructions for generated commit messages."},
	{
		Path: KeyPath{"agent", "model_parameters"}, Type: TypeObject, Desc: "Per provider / model parameters (an array of {provider, model, temperature}).",
		Note: "Temperature is set per entry; typical range 0.0 to 2.0.",
	},

	// --- compaction
	{Path: KeyPath{"agent", "auto_compact"}, Type: TypeObject, Desc: "Automatic context compaction."},
	{Path: KeyPath{"agent", "auto_compact", "enabled"}, Type: TypeBool, Default: "true", Desc: "Run automatic compaction."},
	{
		Path: KeyPath{"agent", "auto_compact", "threshold"}, Type: TypeString, Default: "\"90%\"",
		Desc: "When compaction triggers: a percent string (\"90%\") or an integer token count.", Note: "Accepts a string or an integer.",
	},

	// --- tool permissions (approval)
	{Path: KeyPath{"agent", "tool_permissions"}, Type: TypeObject, Desc: "Tool permission rules.", Source: zedPermRef},
	{
		Path: KeyPath{"agent", "tool_permissions", "default"}, Type: TypeEnum, Enum: []string{"confirm", "allow", "deny"}, Default: "\"confirm\"",
		Desc: "Fallback decision for every tool.", Source: zedPermRef, Danger: DangerConfirm, Note: "Dangerous: \"allow\".",
	},
	{
		Path: KeyPath{"agent", "tool_permissions", "tools"}, Type: TypeObject, Source: zedPermRef,
		Desc:   "Per tool (terminal, edit_file, write_file, delete_path, move_path, copy_path, create_directory, fetch, search_web, skill, mcp:<server>:<tool>): default, always_allow, always_deny, always_confirm ([{pattern, case_sensitive}] regexes).",
		Danger: DangerConfirm, Note: "Dangerous: a per-tool default of \"allow\" (especially terminal) or broad always_allow regexes. Built-in security rules cannot be overridden.",
	},
	{
		Path: KeyPath{"agent", "always_allow_tool_actions"}, Type: TypeBool, Desc: "Allow every tool action without asking (legacy).", Source: zedPermRef,
		Deprecated: true, Danger: DangerConfirm, Note: "Deprecated in Zed v0.224.0 (replaced by tool_permissions.default). Dangerous: true.",
	},
	{
		Path: KeyPath{"agent", "default_profile"}, Type: TypeString, Desc: "Agent profile new threads start with (Write, Ask, Minimal or custom).",
		Source: zedAudit, Note: "Named by the audit (V7); not detailed on the page re-fetched 2026-10-04.",
	},
	{
		Path: KeyPath{"agent", "profiles"}, Type: TypeObject, Source: zedMCPRef,
		Desc: "Agent profiles: tools enable map, enable_all_context_servers, context_servers.<name>.tools.",
	},

	// --- agent panel
	{Path: KeyPath{"agent", "default_width"}, Type: TypeInt, Default: "640", Desc: "Agent panel width.", Source: zedAllRef},
	{Path: KeyPath{"agent", "flexible"}, Type: TypeBool, Default: "true", Desc: "Flexible agent panel layout.", Source: zedAllRef},
	{
		Path: KeyPath{"agent", "threads_sidebar", "position"}, Type: TypeEnum, Enum: []string{"left", "right"}, Default: "\"left\"",
		Desc: "Threads sidebar position.", Source: zedAllRef,
	},
	{
		Path: KeyPath{"agent", "threads_sidebar", "default_width"}, Type: TypeInt, Min: f64(200), Max: f64(800), Default: "300",
		Desc: "Threads sidebar width.", Source: zedAllRef,
	},
	{Path: KeyPath{"agent", "threads_sidebar", "auto_open"}, Type: TypeBool, Default: "true", Desc: "Open the threads sidebar automatically.", Source: zedAllRef},

	// --- MCP and external agents
	{
		Path: KeyPath{"context_servers"}, Type: TypeMCPMap, Source: zedMCPRef,
		Desc: "MCP servers by name: {command, args, env} locally or {url, headers} remotely.",
		Note: "Entries run arbitrary commands. A remote server without an Authorization header triggers OAuth sign-in.",
	},
	{
		Path: KeyPath{"agent_servers"}, Type: TypeObject, Desc: "External ACP agents.", Source: zedAudit, Verified: VerifiedUnverified,
		Note: "UNVERIFIED key in the audit.",
	},

	// --- providers
	{Path: KeyPath{"language_models"}, Type: TypeObject, Desc: "Per-provider settings (anthropic, google, ollama, openai, ...).", Source: zedAllRef},
	{
		Path: KeyPath{"language_models", "anthropic", "api_url"}, Type: TypeString, Desc: "Anthropic API base URL.", Source: zedAllRef,
		Danger: DangerConfirm, Note: "Base URL override. Keys come from ANTHROPIC_API_KEY or the OS keychain, never this file.",
	},
	{
		Path: KeyPath{"language_models", "openai", "api_url"}, Type: TypeString, Desc: "OpenAI API base URL.", Source: zedAllRef,
		Danger: DangerConfirm, Note: "Base URL override. Keys come from OPENAI_API_KEY or the OS keychain.",
	},
	{
		Path: KeyPath{"language_models", "google", "api_url"}, Type: TypeString, Desc: "Google AI API base URL.", Source: zedAllRef,
		Danger: DangerConfirm, Note: "Base URL override.",
	},
	{
		Path: KeyPath{"language_models", "ollama", "api_url"}, Type: TypeString, Desc: "Ollama server URL.", Source: zedAllRef,
		Danger: DangerConfirm, Note: "Base URL override.",
	},
	{
		Path: KeyPath{"language_models", "openai_compatible"}, Type: TypeObject, Desc: "Named OpenAI-compatible providers, each with an api_url.",
		Source: zedLLMRef, Verified: VerifiedUnverified, Danger: DangerConfirm,
		Note: "Named by the audit (V19); the provider page re-fetched 2026-10-04 defers to setup pages. Base URL overrides.",
	},

	// --- general
	{Path: KeyPath{"auto_update"}, Type: TypeBool, Default: "true", Desc: "Check for updates automatically.", Source: zedConfRef},
	{
		Path: KeyPath{"theme"}, Type: TypeString, Desc: "Theme name, or an object {mode, light, dark}.", Source: zedConfRef,
		Note: "A string or an object.",
	},
	{Path: KeyPath{"theme", "mode"}, Type: TypeEnum, Enum: []string{"system", "light", "dark"}, Desc: "Which of theme.light / theme.dark applies.", Source: zedConfRef},
	{Path: KeyPath{"theme", "light"}, Type: TypeString, Desc: "Theme used in light mode.", Source: zedConfRef},
	{Path: KeyPath{"theme", "dark"}, Type: TypeString, Desc: "Theme used in dark mode.", Source: zedConfRef},
	{Path: KeyPath{"telemetry", "diagnostics"}, Type: TypeBool, Desc: "Send crash and diagnostic reports.", Source: zedTelemRef},
	{Path: KeyPath{"telemetry", "metrics"}, Type: TypeBool, Desc: "Send usage metrics.", Source: zedTelemRef},
	{Path: KeyPath{"disable_ai"}, Type: TypeBool, Default: "false", Desc: "Disable every AI feature.", Source: zedConfRef},
	{
		Path: KeyPath{"proxy"}, Type: TypeString, Default: "null", Desc: "Network proxy URL (http, https, socks4, socks4a, socks5, socks5h).",
		Source: zedConfRef,
	},
	{
		Path: KeyPath{"edit_predictions", "provider"}, Type: TypeEnum, Enum: []string{"zed", "copilot", "none"}, Default: "\"zed\"",
		Desc: "Edit prediction provider.", Source: zedAllRef,
	},
})

// zedMCPShape: context_servers.<name> {command, args, env} or {url}.
var zedMCPShape = MCPShape{
	EnvKey: "env", TypeNames: map[string]string{"stdio": "", "http": ""},
	URLKeys: map[string]string{"http": "url"},
}

var zedMap = []MapRow{
	{Key: KeyAutoUpdate, Tool: "zed", Settings: []string{"zed/auto_update"}, Transform: TransformIdentity, Source: zedConfRef},
	{
		Key: KeyTelemetryOff, Tool: "zed", Settings: []string{"zed/telemetry.diagnostics", "zed/telemetry.metrics"}, Transform: TransformEnumMap,
		Params: TransformParams{Enum: []EnumPair{{true, []any{false, false}}, {false, []any{true, true}}}}, Source: zedTelemRef,
		Note: "Fans out to both telemetry switches; a mixed pair has no canonical form.",
	},
	{
		Key: KeyApprovalMode, Tool: "zed", Settings: []string{"zed/agent.tool_permissions.default"}, Transform: TransformEnumMap,
		Params: TransformParams{Enum: []EnumPair{{"ask_edits", []any{"confirm"}}, {"full_auto", []any{"allow"}}}}, Source: zedPermRef,
		Note: "Reads need no permission in Zed, so confirm is ask_edits. deny (block every tool) has no canonical form; plan, ask and auto_edits are refused.",
	},
	{
		Key: KeyMCPServers, Tool: "zed", Settings: []string{"zed/context_servers"}, Transform: TransformMCPShape,
		Params: TransformParams{MCP: zedMCPShape}, Lossy: true, Source: zedMCPRef,
		Note: "Remote servers carry url (and headers); sse is refused. " + mcpLossyNote,
	},
}

var zedCatalog = toolCatalog{
	Tool: "zed",
	VerifiedAgainst: "zed.dev agent-settings, tool-permissions, MCP, configuring-zed, all-settings and telemetry pages fetched 2026-10-04 " +
		"(audit section 5); Zed was not installed on node 1.",
	Files: zedFiles,
	Extra: zedSettings,
	Map:   zedMap,
}
