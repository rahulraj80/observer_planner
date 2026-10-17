package agentconfig

// Kimi Code CLI doc-authored catalogue. Sources: moonshotai.github.io/kimi-code
// (configuration/config-files, customization/mcp, customization/hooks)
// fetched 2026-10-04, audit section 5 "kimi-code" and the node-1 live key
// names of 2026-10-03.
//
// THE KIMI RULE: ~/.kimi-code/config.toml holds a plaintext provider key and
// is never read by anything. Its rows come from the vendor docs only, its
// file row is PrivNever, and no common key maps onto it. UI preferences live
// in the separate tui.toml (audit section 6 finding 4), which is an ordinary
// user file.
const (
	kimiCodeRef      = "https://moonshotai.github.io/kimi-code/en/configuration/config-files.html"
	kimiCodeMCPRef   = "https://moonshotai.github.io/kimi-code/en/customization/mcp.html"
	kimiCodeHooksRef = "https://moonshotai.github.io/kimi-code/en/customization/hooks.html"
	kimiCodeConfig   = "kimi-code/config"
	kimiCodeTUI      = "kimi-code/tui"
	kimiCodeLive     = "Key name seen on node 1 (2026-10-03, audit section 5 kimi-code); no value was read."
)

// kimiCodeRow fills the tool, shape, owner, verification and id of one row.
func kimiCodeRow(r SettingRow) SettingRow {
	r.Tool = "kimi-code"
	if r.FileID == "" {
		r.FileID = kimiCodeConfig
	}
	if r.Owner == "" {
		r.Owner = OwnerUser
	}
	if r.Verified == "" {
		r.Verified = VerifiedDoc
	}
	if r.Source == "" {
		r.Source = kimiCodeRef
	}
	r.ID = r.Tool + "/" + r.Path.String()
	return r
}

// kimiCodeMCPShape is mcp.json's entry shape: command / args / env for
// stdio; url alone for streamable HTTP. SSE needs transport: "sse", which
// the shared shape cannot add for one remote transport only, so it is
// refused.
var kimiCodeMCPShape = MCPShape{
	EnvKey: "env", TypeNames: map[string]string{"stdio": "", "http": ""},
	URLKeys: map[string]string{"http": "url"},
}

var kimiCodeCatalog = toolCatalog{
	Tool: "kimi-code",
	VerifiedAgainst: "moonshotai.github.io/kimi-code docs fetched 2026-10-04 (config-files, mcp, hooks; audit section 5 kimi-code); " +
		"node 1 live key NAMES 2026-10-03 for default_model, providers, services, thinking, models (config.toml, names only, never values) and theme, tui_mode (tui.toml)",
	Files: []FileRow{
		{
			ID: "kimi-code/user-config", Tool: "kimi-code", Shape: kimiCodeConfig, Kind: KindSettings, Scope: ScopeUser,
			Paths: samePath("~/.kimi-code/config.toml"), Format: FormatTOML, Precedence: 20, WriteNeeds: PrivNever,
			Grounding: doc(kimiCodeRef),
			Note: "Holds a plaintext provider api_key: never read by anything, rows come from the vendor docs only. " +
				"Observer's proxy route writes providers.openai.base_url without reading the file into any surface. KIMI_CODE_HOME relocates ~/.kimi-code; credentials/ beside it is never opened. " +
				"The one other exception is Observer's hook registration (T-378), which adds and removes only its own [[hooks]] tables under the declared " +
				"credential-file exception (D-268 / D-270).",
		},
		{
			ID: "kimi-code/user-tui", Tool: "kimi-code", Shape: kimiCodeTUI, Kind: KindSettings, Scope: ScopeUser,
			Paths: samePath("~/.kimi-code/tui.toml"), Format: FormatTOML, Precedence: 20, WriteNeeds: PrivUser,
			Grounding: live("~/.kimi-code/tui.toml carries theme, editor, notifications, upgrade, tui_mode"),
			Note:      "Terminal-UI preferences, apart from config.toml; /config, /theme and /editor write it.",
		},
		{
			ID: "kimi-code/user-mcp", Tool: "kimi-code", Shape: "kimi-code/mcp", Kind: KindMCP, Scope: ScopeUser,
			Paths: samePath("~/.kimi-code/mcp.json"), Format: FormatJSON, Precedence: 20, WriteNeeds: PrivUser,
			Grounding: doc(kimiCodeMCPRef), Note: "Moves with KIMI_CODE_HOME.",
		},
		{
			ID: "kimi-code/project-mcp", Tool: "kimi-code", Shape: "kimi-code/mcp", Kind: KindMCP, Scope: ScopeProjectShared,
			Paths: samePath("<project>/.kimi-code/mcp.json"), Format: FormatJSON, Precedence: 30, WriteNeeds: PrivUser,
			Grounding: doc(kimiCodeMCPRef), Note: "A project entry overrides a user entry of the same name; stdio entries run commands at session start.",
		},
		{
			ID: "kimi-code/project-local", Tool: "kimi-code", Shape: "kimi-code/local", Kind: KindSettings, Scope: ScopeProjectLocal,
			Paths: samePath("<project>/.kimi-code/local.toml"), Format: FormatTOML, Precedence: 40, WriteNeeds: PrivUser, Gitignored: true,
			Grounding: doc(kimiCodeRef), Note: "Written by /add-dir; the docs recommend gitignoring it (machine-specific absolute paths).",
		},
	},
	Extra: []SettingRow{
		// --- config.toml (doc-only: the file is never read)
		kimiCodeRow(SettingRow{
			Path: KeyPath{"default_model"}, Type: TypeString, Desc: "Default model alias (a key of [models]).", Verified: VerifiedLive, Note: kimiCodeLive,
		}),
		kimiCodeRow(SettingRow{
			Path: KeyPath{"default_permission_mode"}, Type: TypeEnum, Enum: []string{"manual", "yolo", "auto"}, Default: "\"manual\"",
			Desc: "Permission mode for new sessions.", Danger: DangerConfirm, Note: "yolo (and auto) approve tool calls without asking.",
		}),
		kimiCodeRow(SettingRow{Path: KeyPath{"default_plan_mode"}, Type: TypeBool, Default: "false", Desc: "Start new sessions in Plan mode."}),
		kimiCodeRow(SettingRow{Path: KeyPath{"merge_all_available_skills"}, Type: TypeBool, Default: "true", Desc: "Merge skills from every skill directory."}),
		kimiCodeRow(SettingRow{Path: KeyPath{"extra_skill_dirs"}, Type: TypeStringList, Desc: "Extra skill directories."}),
		kimiCodeRow(SettingRow{Path: KeyPath{"extra_agent_dirs"}, Type: TypeStringList, Desc: "Extra custom-agent directories."}),
		kimiCodeRow(SettingRow{Path: KeyPath{"builtin_product_skills"}, Type: TypeBool, Default: "true", Desc: "Offer the built-in skills that document Kimi Code."}),
		kimiCodeRow(SettingRow{Path: KeyPath{"telemetry"}, Type: TypeBool, Default: "true", Desc: "Anonymous telemetry (off only when explicitly false)."}),
		kimiCodeRow(SettingRow{Path: KeyPath{"auto_session_title"}, Type: TypeBool, Default: "true", Desc: "Let clients generate session titles."}),
		kimiCodeRow(SettingRow{
			Path: KeyPath{"providers"}, Type: TypeObject, Verified: VerifiedLive, Danger: DangerConfirm,
			Desc: "API providers by name: {type, api_key, api_key_env, base_url, oauth, env, custom_headers}.",
			Note: kimiCodeLive + " api_key is a plaintext credential (api_key_env names a variable instead); base_url redirects traffic. " +
				"Observer's route writes providers.openai.base_url (owned span).",
		}),
		kimiCodeRow(SettingRow{
			Path: KeyPath{"models"}, Type: TypeObject, Verified: VerifiedLive,
			Desc: "Model aliases: {provider, model, max_context_size, max_output_size, capabilities, support_efforts, default_effort, off_effort, ...}.",
			Note: kimiCodeLive + " An alias containing a dot is a quoted TOML key; [models.\"<alias>\".overrides] survives provider refreshes.",
		}),
		kimiCodeRow(SettingRow{Path: KeyPath{"thinking", "enabled"}, Type: TypeBool, Default: "true", Desc: "Thinking on for new sessions."}),
		kimiCodeRow(SettingRow{
			Path: KeyPath{"thinking", "effort"}, Type: TypeEnum, Enum: []string{"low", "medium", "high", "xhigh", "max"}, Desc: "Default thinking effort.",
			Note: "Falls back to the model default when the model does not list the level.",
		}),
		kimiCodeRow(SettingRow{Path: KeyPath{"thinking", "keep"}, Type: TypeString, Default: "\"all\"", Desc: "Preserved-thinking passthrough."}),
		kimiCodeRow(SettingRow{Path: KeyPath{"secondary_model", "default_model"}, Type: TypeString, Desc: "Default model alias for subagents."}),
		kimiCodeRow(SettingRow{Path: KeyPath{"secondary_model", "models"}, Type: TypeStringMap, Desc: "Subagent model pool: alias to selection hint."}),
		kimiCodeRow(SettingRow{Path: KeyPath{"secondary_model", "force"}, Type: TypeBool, Default: "false", Desc: "Pin every subagent to default_model."}),
		kimiCodeRow(SettingRow{Path: KeyPath{"secondary_model", "default_effort"}, Type: TypeString, Desc: "Thinking effort for every spawned subagent."}),
		kimiCodeRow(SettingRow{Path: KeyPath{"loop_control", "max_steps_per_turn"}, Type: TypeInt, Desc: "Steps per turn (unset or 0 = unlimited)."}),
		kimiCodeRow(SettingRow{Path: KeyPath{"loop_control", "max_attempts_per_step"}, Type: TypeInt, Default: "10", Desc: "Attempts for a failing step."}),
		kimiCodeRow(SettingRow{
			Path: KeyPath{"loop_control", "reserved_context_size"}, Type: TypeInt, Desc: "Tokens reserved for output; compaction triggers when the remaining window falls below it.",
		}),
		kimiCodeRow(SettingRow{Path: KeyPath{"loop_control", "compaction_max_attempts"}, Type: TypeInt, Default: "5", Desc: "Attempts for a failing compaction."}),
		kimiCodeRow(SettingRow{
			Path: KeyPath{"token_counting", "strategy"}, Type: TypeEnum, Enum: []string{"measured+estimated", "measured", "estimated"},
			Default: "\"measured+estimated\"", Desc: "Which context token count is reported.",
		}),
		kimiCodeRow(SettingRow{Path: KeyPath{"background", "max_running_tasks"}, Type: TypeInt, Desc: "Concurrent background tasks."}),
		kimiCodeRow(SettingRow{Path: KeyPath{"background", "keep_alive_on_exit"}, Type: TypeBool, Default: "false", Desc: "Keep background tasks running when the session closes."}),
		kimiCodeRow(SettingRow{Path: KeyPath{"background", "bash_task_timeout_s"}, Type: TypeInt, Default: "600", Desc: "Default background Bash timeout in seconds (0 = none)."}),
		kimiCodeRow(SettingRow{Path: KeyPath{"subagent", "timeout_ms"}, Type: TypeInt, Default: "7200000", Desc: "Wall-clock limit for one Agent subagent."}),
		kimiCodeRow(SettingRow{Path: KeyPath{"swarm", "timeout_ms"}, Type: TypeInt, Default: "7200000", Desc: "Wall-clock limit for one AgentSwarm subagent."}),
		kimiCodeRow(SettingRow{Path: KeyPath{"mcp", "startup_timeout_ms"}, Type: TypeInt, Default: "30000", Desc: "Default MCP connection timeout."}),
		kimiCodeRow(SettingRow{Path: KeyPath{"mcp", "tool_timeout_ms"}, Type: TypeInt, Default: "60000", Desc: "Default MCP tool-call timeout."}),
		kimiCodeRow(SettingRow{Path: KeyPath{"identity", "name"}, Type: TypeString, Desc: "Name the agent calls itself."}),
		kimiCodeRow(SettingRow{Path: KeyPath{"identity", "slug"}, Type: TypeString, Desc: "Protocol identifier (derived from name when unset)."}),
		kimiCodeRow(SettingRow{
			Path: KeyPath{"tools", "enabled"}, Type: TypeStringList, Desc: "Global tool allowlist (empty = no constraint; MCP tools by glob such as mcp__srv__*).",
		}),
		kimiCodeRow(SettingRow{Path: KeyPath{"tools", "disabled"}, Type: TypeStringList, Desc: "Global tool denylist, applied after enabled."}),
		kimiCodeRow(SettingRow{Path: KeyPath{"read", "default_max_chars"}, Type: TypeInt, Default: "100000", Desc: "Read tool budget when a call omits max_chars."}),
		kimiCodeRow(SettingRow{Path: KeyPath{"read", "max_chars"}, Type: TypeInt, Default: "500000", Desc: "Largest Read budget a call may request."}),
		kimiCodeRow(SettingRow{Path: KeyPath{"image", "max_edge_px"}, Type: TypeInt, Default: "2000", Desc: "Longest image edge sent to the model."}),
		kimiCodeRow(SettingRow{Path: KeyPath{"watch", "enabled"}, Type: TypeBool, Default: "true", Desc: "Watch config, AGENTS.md, skills and MCP files for changes."}),
		kimiCodeRow(SettingRow{
			Path: KeyPath{"services"}, Type: TypeObject, Verified: VerifiedLive,
			Desc: "Built-in web search (moonshot_search) and fetch (moonshot_fetch) services: {base_url, api_key, oauth, custom_headers}.",
			Note: kimiCodeLive,
		}),
		kimiCodeRow(SettingRow{Path: KeyPath{"services", "moonshot_search", "base_url"}, Type: TypeString, Desc: "Web search service URL."}),
		kimiCodeRow(SettingRow{
			Path: KeyPath{"services", "moonshot_search", "api_key"}, Type: TypeString, Secret: true, Desc: "Web search service key.",
			Note: "A plaintext credential; KIMI_WEB_SEARCH_API_KEY overrides it.",
		}),
		kimiCodeRow(SettingRow{Path: KeyPath{"services", "moonshot_fetch", "base_url"}, Type: TypeString, Desc: "Web fetch service URL."}),
		kimiCodeRow(SettingRow{
			Path: KeyPath{"services", "moonshot_fetch", "api_key"}, Type: TypeString, Secret: true, Desc: "Web fetch service key.",
			Note: "A plaintext credential; KIMI_WEB_FETCH_API_KEY overrides it.",
		}),
		kimiCodeRow(SettingRow{
			Path: KeyPath{"permission", "rules"}, Type: TypeObject, Danger: DangerConfirm,
			Desc: "Ordered [[permission.rules]] tables {decision: allow | deny | ask, pattern, scope, reason}; the first match wins.",
			Note: "pattern is Tool or Tool(arg-pattern); an allow rule with a broad pattern (Bash, mcp__*) needs a confirmation.",
		}),
		kimiCodeRow(SettingRow{
			Path: KeyPath{"permission", "dangerous_command_guard"}, Type: TypeBool, Default: "true", Desc: "Built-in dangerous-command confirmation policy.",
			Danger: DangerConfirm, Note: "false turns the guard off (KIMI_CODE_DANGEROUS_COMMAND_GUARD=false does the same).",
		}),
		kimiCodeRow(SettingRow{
			Path: KeyPath{"hooks"}, Type: TypeObject, Source: kimiCodeHooksRef,
			Desc: "[[hooks]] tables {event, matcher, command, timeout (1-600 s, default 30)}; extra fields fail the load.",
			Note: "Observer registers its own UserPromptSubmit and PreToolUse tables here (`observer init` and `observer start`, T-378, live 2026-10-07); every other table is the user's.",
		}),
		// --- tui.toml
		kimiCodeRow(SettingRow{
			Path: KeyPath{"theme"}, FileID: kimiCodeTUI, Type: TypeString, Enum: []string{"auto", "dark", "light"}, Default: "\"auto\"",
			Desc: "Color theme (or a custom theme name).", Verified: VerifiedLive, Note: kimiCodeLive,
		}),
		kimiCodeRow(SettingRow{
			Path: KeyPath{"tui_mode"}, FileID: kimiCodeTUI, Type: TypeEnum, Enum: []string{"regular", "fullscreen"}, Default: "\"regular\"",
			Desc: "UI layout (fullscreen is experimental).", Verified: VerifiedLive, Note: kimiCodeLive,
		}),
		kimiCodeRow(SettingRow{Path: KeyPath{"render_latex"}, FileID: kimiCodeTUI, Type: TypeBool, Default: "true", Desc: "Render LaTeX as Unicode text."}),
		kimiCodeRow(SettingRow{Path: KeyPath{"disable_paste_burst"}, FileID: kimiCodeTUI, Type: TypeBool, Default: "false", Desc: "Disable the paste-burst fallback."}),
		kimiCodeRow(SettingRow{Path: KeyPath{"cache_expiry_hint"}, FileID: kimiCodeTUI, Type: TypeBool, Default: "true", Desc: "Warn when the context cache may have expired."}),
		kimiCodeRow(SettingRow{Path: KeyPath{"disable_feedback_survey"}, FileID: kimiCodeTUI, Type: TypeBool, Default: "false", Desc: "Hide the session rating prompt."}),
		kimiCodeRow(SettingRow{Path: KeyPath{"editor", "command"}, FileID: kimiCodeTUI, Type: TypeString, Default: "\"\"", Desc: "External editor command (empty = $VISUAL / $EDITOR)."}),
		kimiCodeRow(SettingRow{Path: KeyPath{"notifications", "enabled"}, FileID: kimiCodeTUI, Type: TypeBool, Default: "true", Desc: "Desktop notifications."}),
		kimiCodeRow(SettingRow{
			Path: KeyPath{"notifications", "notification_condition"}, FileID: kimiCodeTUI, Type: TypeEnum, Enum: []string{"unfocused", "always"},
			Default: "\"unfocused\"", Desc: "When to notify.",
		}),
		kimiCodeRow(SettingRow{Path: KeyPath{"upgrade", "auto_install"}, FileID: kimiCodeTUI, Type: TypeBool, Default: "true", Desc: "Install new versions automatically."}),
		kimiCodeRow(SettingRow{Path: KeyPath{"status_line", "items"}, FileID: kimiCodeTUI, Type: TypeStringList, Default: "[]", Desc: "Built-in footer slots and their order."}),
		kimiCodeRow(SettingRow{
			Path: KeyPath{"status_line", "command"}, FileID: kimiCodeTUI, Type: TypeString, Default: "\"\"", Desc: "Custom status line command.",
			Danger: DangerConfirm, Note: "Runs an arbitrary command (throttled to once per second).",
		}),
		// --- mcp.json / local.toml
		kimiCodeRow(SettingRow{
			Path: KeyPath{"mcpServers"}, FileID: "kimi-code/mcp", Type: TypeMCPMap, Source: kimiCodeMCPRef,
			Desc: "MCP servers by name: command, args, env, cwd | url, transport (sse), headers, bearerTokenEnvVar, enabled, deferred, timeouts, enabledTools, disabledTools.",
			Note: "headers may hold credentials (bearerTokenEnvVar is the reference form); enabled: false disables a server.",
		}),
		kimiCodeRow(SettingRow{
			Path: KeyPath{"workspace", "additional_dir"}, FileID: "kimi-code/local", Type: TypeStringList, Desc: "Extra workspace directories (absolute paths) for this project.",
		}),
	},
	Map: []MapRow{
		{
			Key: KeyTheme, Tool: "kimi-code", Settings: []string{"kimi-code/theme"}, Transform: TransformEnumMap,
			Params: TransformParams{Enum: []EnumPair{{"light", []any{"light"}}, {"dark", []any{"dark"}}}}, Source: kimiCodeRef,
			Note: "auto and custom themes stay tool-specific.",
		},
		{Key: KeyAutoUpdate, Tool: "kimi-code", Settings: []string{"kimi-code/upgrade.auto_install"}, Transform: TransformIdentity, Source: kimiCodeRef},
		{
			Key: KeyMCPServers, Tool: "kimi-code", Settings: []string{"kimi-code/mcpServers"}, Transform: TransformMCPShape,
			Params: TransformParams{MCP: kimiCodeMCPShape}, Lossy: true, Source: kimiCodeMCPRef,
			Note: "SSE (transport: \"sse\") is refused. " + mcpLossyNote,
		},
		{
			Key: KeyAdditionalDirs, Tool: "kimi-code", Settings: []string{"kimi-code/workspace.additional_dir"}, Transform: TransformIdentity, Source: kimiCodeRef,
			Note: "Project-local only (.kimi-code/local.toml); absolute paths.",
		},
	},
}
