package agentconfig

// Poolside (pool CLI / ACP agent) catalogue. Sources: docs.poolside.ai
// settings-file-reference (re-fetched 2026-10-04 for the full key table),
// permissions, hooks, mcp-servers, cli/cli-reference, audit section 5
// "poolside"; repo docs/poolside-adapter.md. Poolside was NOT installed on
// demo node 1, so every row is doc-grounded.
const (
	poolsideRef      = "https://docs.poolside.ai/settings-file-reference.md"
	poolsidePermsRef = "https://docs.poolside.ai/permissions.md"
	poolsideHooksRef = "https://docs.poolside.ai/hooks.md"
	poolsideMCPRef   = "https://docs.poolside.ai/mcp-servers.md"
	poolsideSettings = "poolside/settings"
	poolsideAdapter  = "docs/poolside-adapter.md"
	poolsidePersonal = "Read from the personal file only."
	poolsideCombine  = "Lists combine across files (deny wins over allow)."
)

var (
	poolsidePersonalOnly = []Scope{ScopeUser}
	// mcp_servers is read from the personal file and settings.local.yaml.
	poolsideMCPScopes = []Scope{ScopeUser, ScopeProjectLocal}
)

var poolsideCatalog = toolCatalog{
	Tool:            "poolside",
	VerifiedAgainst: "vendor docs fetched 2026-10-03 (audit section 5) and the settings file reference re-fetched 2026-10-04; not installed on node 1, no live keys",
	Files: []FileRow{
		{
			ID: "poolside/personal-settings", Tool: "poolside", Shape: poolsideSettings, Kind: KindSettings, Scope: ScopeUser,
			Paths: samePath("~/.config/poolside/settings.yaml"), Format: FormatYAML, Precedence: 20, WriteNeeds: PrivUser, Grounding: doc(poolsideRef),
			Note: "Honours XDG_CONFIG_HOME; the same XDG-style path on Windows (%USERPROFILE%\\.config\\poolside, repo-grounded). " +
				"Observer's hook entry lives here and its YAML round-trip loses comments and key order. pool acp --settings ranks above every file.",
		},
		{
			ID: "poolside/project-settings", Tool: "poolside", Shape: poolsideSettings, Kind: KindSettings, Scope: ScopeProjectShared,
			Paths: samePath("<project>/.poolside/settings.yaml"), Format: FormatYAML, Precedence: 30, WriteNeeds: PrivUser, Grounding: doc(poolsideRef),
		},
		{
			ID: "poolside/project-local-settings", Tool: "poolside", Shape: poolsideSettings, Kind: KindSettings, Scope: ScopeProjectLocal,
			Paths: samePath("<project>/.poolside/settings.local.yaml"), Format: FormatYAML, Precedence: 40, WriteNeeds: PrivUser, Gitignored: true,
			Grounding: doc(poolsideRef), Note: "Personal and project-specific; the most specific file.",
		},
		{
			ID: "poolside/credentials", Tool: "poolside", Shape: "poolside/credentials", Kind: KindToolState, Scope: ScopeUser,
			Paths: samePath("~/.config/poolside/credentials.json"), Format: FormatJSON, Precedence: 0, WriteNeeds: PrivNever,
			Grounding: Grounding{Kind: GroundAdapterDoc, Source: poolsideAdapter, Date: auditDate},
			Note:      "Saved API token: never touched.",
		},
		{
			ID: "poolside/pool-state", Tool: "poolside", Shape: "poolside/pool-state", Kind: KindToolState, Scope: ScopeUser,
			Paths: samePath("~/.config/poolside/pool.json"), Format: FormatJSON, Precedence: 0, WriteNeeds: PrivNever,
			Grounding: Grounding{Kind: GroundAdapterDoc, Source: poolsideAdapter, Date: auditDate},
			Note:      "Agent-server defaults and saved preferences written by pool (the saved model / approval mode likely live here, key names UNVERIFIED).",
		},
	},
	Extra: []SettingRow{
		// --- pool (personal only)
		{
			ID: "poolside/pool.api_url", Tool: "poolside", FileID: poolsideSettings, Path: KeyPath{"pool", "api_url"}, Scopes: poolsidePersonalOnly, Type: TypeString,
			Desc: "Saved API base URL.", Source: poolsideRef, Verified: VerifiedDoc, Owner: OwnerUser, Danger: DangerConfirm,
			Note: poolsidePersonal + " A URL that is not Observer routes traffic off the proxy (POOLSIDE_STANDALONE_BASE_URL likewise).",
		},
		{
			ID: "poolside/pool.auto_mode_classifier", Tool: "poolside", FileID: poolsideSettings, Path: KeyPath{"pool", "auto_mode_classifier"}, Scopes: poolsidePersonalOnly,
			Type: TypeString, Desc: "Model that judges actions in Auto approval mode.", Source: poolsideRef, Verified: VerifiedDoc, Owner: OwnerUser,
			Danger: DangerConfirm, Note: poolsidePersonal + " Setting it enables model-judged auto approval (POOL_AUTO_MODE_CLASSIFIER_MODEL likewise).",
		},
		{
			ID: "poolside/pool.default_agent_server", Tool: "poolside", FileID: poolsideSettings, Path: KeyPath{"pool", "default_agent_server"}, Scopes: poolsidePersonalOnly,
			Type: TypeString, Desc: "Default agent server entry.", Source: poolsideRef, Verified: VerifiedDoc, Owner: OwnerUser, Note: poolsidePersonal,
		},
		{
			ID: "poolside/pool.worktree_prefix", Tool: "poolside", FileID: poolsideSettings, Path: KeyPath{"pool", "worktree_prefix"}, Scopes: poolsidePersonalOnly,
			Type: TypeString, Desc: "Prefix for git worktree names.", Source: poolsideRef, Verified: VerifiedDoc, Owner: OwnerUser, Note: poolsidePersonal,
		},
		// --- tools and paths
		{
			ID: "poolside/tools", Tool: "poolside", FileID: poolsideSettings, Path: KeyPath{"tools"}, Type: TypeObject,
			Desc: "Per-tool policy: tools.<tool>.allow / deny (glob lists, * only) and disabled.", Source: poolsidePermsRef, Verified: VerifiedDoc,
			Owner: OwnerUser, Danger: DangerConfirm,
			Note: poolsideCombine + " allow: [\"*\"] approves every call of that tool; a tool is off if any file disables it.",
		},
		{
			ID: "poolside/paths.allow", Tool: "poolside", FileID: poolsideSettings, Path: KeyPath{"paths", "allow"}, Type: TypeObject,
			Desc: "Allowed paths {path, write}; read-only unless write is true.", Source: poolsidePermsRef, Verified: VerifiedDoc, Owner: OwnerUser,
			Danger: DangerConfirm,
			Note:   poolsideCombine + " Project paths are relative, personal ones absolute or ~; write: true on ** opens the whole tree.",
		},
		{
			ID: "poolside/paths.deny", Tool: "poolside", FileID: poolsideSettings, Path: KeyPath{"paths", "deny"}, Type: TypeObject,
			Desc: "Blocked paths {path}.", Source: poolsidePermsRef, Verified: VerifiedDoc, Owner: OwnerUser, Note: poolsideCombine,
		},
		// --- secrets
		{
			ID: "poolside/secrets.allow", Tool: "poolside", FileID: poolsideSettings, Path: KeyPath{"secrets", "allow"}, Type: TypeStringList,
			Desc: "Secret NAMES approved for use without a prompt.", Source: poolsideRef, Verified: VerifiedDoc, Owner: OwnerUser, Note: "Names, not values.",
		},
		{
			ID: "poolside/secrets.fallback_redaction_patterns", Tool: "poolside", FileID: poolsideSettings, Path: KeyPath{"secrets", "fallback_redaction_patterns"},
			Type: TypeBool, Default: "true", Desc: "Use the built-in redaction patterns.", Source: poolsideRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "poolside/secrets.redact_patterns", Tool: "poolside", FileID: poolsideSettings, Path: KeyPath{"secrets", "redact_patterns"}, Type: TypeObject,
			Desc: "Extra redaction patterns {name, pattern (RE2)}.", Source: poolsideRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		// --- MCP
		{
			ID: "poolside/mcp_servers", Tool: "poolside", FileID: poolsideSettings, Path: KeyPath{"mcp_servers"}, Scopes: poolsideMCPScopes, Type: TypeMCPMap,
			Desc:   "MCP servers: command, args, cwd, env or transport {type http | sse, url, headers}, enabled_tools, allow, deny, disabled.",
			Source: poolsideMCPRef, Verified: VerifiedDoc, Owner: OwnerUser,
			Note: "Read from the personal file and settings.local.yaml only; snake_case; headers are \"Name: value\" strings. The nested transport block is why there is no common-vocabulary mapping.",
		},
		// --- sandbox
		{
			ID: "poolside/sandbox.image", Tool: "poolside", FileID: poolsideSettings, Path: KeyPath{"sandbox", "image"}, Type: TypeString,
			Desc: "Sandbox container image.", Source: poolsideRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "poolside/sandbox.env_vars", Tool: "poolside", FileID: poolsideSettings, Path: KeyPath{"sandbox", "env_vars"}, Type: TypeStringMap,
			Desc: "Environment variables inside the sandbox.", Source: poolsideRef, Verified: VerifiedDoc, Owner: OwnerUser,
			Note: "Use sandbox.secrets for credentials rather than a literal value here.",
		},
		{
			ID: "poolside/sandbox.secrets", Tool: "poolside", FileID: poolsideSettings, Path: KeyPath{"sandbox", "secrets"}, Type: TypeStringList,
			Desc: "Secret names the sandbox may request.", Source: poolsideRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "poolside/sandbox.filesystem.workspaces.access", Tool: "poolside", FileID: poolsideSettings, Path: KeyPath{"sandbox", "filesystem", "workspaces", "access"},
			Type: TypeEnum, Enum: []string{"read-only", "read-write"}, Desc: "Workspace access inside the sandbox.", Source: poolsideRef,
			Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "poolside/sandbox.filesystem.mounts", Tool: "poolside", FileID: poolsideSettings, Path: KeyPath{"sandbox", "filesystem", "mounts"}, Type: TypeObject,
			Desc: "Extra mounts {host, sandbox, access}.", Source: poolsideRef, Verified: VerifiedDoc, Owner: OwnerUser, Danger: DangerConfirm,
			Note: "A read-write host mount gives the sandbox write access outside the workspace.",
		},
		{
			ID: "poolside/sandbox.network.policy", Tool: "poolside", FileID: poolsideSettings, Path: KeyPath{"sandbox", "network", "policy"}, Type: TypeEnum,
			Enum: []string{"off", "allow-list", "unsafe-allow-all"}, Desc: "Sandbox network policy.", Source: poolsideRef, Verified: VerifiedDoc,
			Owner: OwnerUser, Danger: DangerConfirm, Note: "unsafe-allow-all opens all egress (--sandbox disabled drops the sandbox entirely).",
		},
		{
			ID: "poolside/sandbox.network.egress.allowed_domains", Tool: "poolside", FileID: poolsideSettings, Path: KeyPath{"sandbox", "network", "egress", "allowed_domains"},
			Type: TypeStringList, Desc: "Domains reachable under the allow-list policy.", Source: poolsideRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "poolside/sandbox.network.egress.allowed_cidrs", Tool: "poolside", FileID: poolsideSettings, Path: KeyPath{"sandbox", "network", "egress", "allowed_cidrs"},
			Type: TypeStringList, Desc: "CIDR blocks reachable under the allow-list policy.", Source: poolsideRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		// --- web search
		{
			ID: "poolside/web_search.provider", Tool: "poolside", FileID: poolsideSettings, Path: KeyPath{"web_search", "provider"}, Type: TypeEnum,
			Enum: []string{"parallel", "exa"}, Desc: "Web search provider.", Source: poolsideRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "poolside/web_search.api_key", Tool: "poolside", FileID: poolsideSettings, Path: KeyPath{"web_search", "api_key"}, Scopes: poolsidePersonalOnly,
			Type: TypeString, Desc: "Web search provider API key.", Source: poolsideRef, Verified: VerifiedDoc, Owner: OwnerUser, Secret: true, Note: poolsidePersonal,
		},
		{
			ID: "poolside/web_search.base_url", Tool: "poolside", FileID: poolsideSettings, Path: KeyPath{"web_search", "base_url"}, Type: TypeString,
			Desc: "Web search endpoint override.", Source: poolsideRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "poolside/web_search.summarize_results", Tool: "poolside", FileID: poolsideSettings, Path: KeyPath{"web_search", "summarize_results"}, Type: TypeBool,
			Default: "false", Desc: "Summarise results with the session model.", Source: poolsideRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		// --- hooks
		{
			ID: "poolside/hooks", Tool: "poolside", FileID: poolsideSettings, Path: KeyPath{"hooks"}, Type: TypeObject,
			Desc:   "Hooks by event: [{name, matcher, command, timeout}]; events PreToolUse, PostToolUse, UserPromptSubmit, Stop, PreCompact, SessionStart.",
			Source: poolsideHooksRef, Verified: VerifiedDoc, Owner: OwnerUser,
			Note: "Lists combine across files. Observer's UserPromptSubmit entry is an owned span; overwriting hooks removes it.",
		},
		{
			ID: "poolside/hooks.stop_hook_max_continuations", Tool: "poolside", FileID: poolsideSettings, Path: KeyPath{"hooks", "stop_hook_max_continuations"},
			Type: TypeInt, Desc: "Maximum consecutive continuations a Stop hook may force (unlimited by default).", Source: poolsideRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "poolside/hooks.PreToolUse", Tool: "poolside", FileID: poolsideSettings, Path: KeyPath{"hooks", "PreToolUse"}, Type: TypeObject,
			Desc: "Hooks run before a tool call.", Source: poolsideHooksRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "poolside/hooks.PostToolUse", Tool: "poolside", FileID: poolsideSettings, Path: KeyPath{"hooks", "PostToolUse"}, Type: TypeObject,
			Desc: "Hooks run after a tool call.", Source: poolsideHooksRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "poolside/hooks.UserPromptSubmit", Tool: "poolside", FileID: poolsideSettings, Path: KeyPath{"hooks", "UserPromptSubmit"}, Type: TypeObject,
			Desc: "Hooks run when the user submits a prompt.", Source: poolsideHooksRef, Verified: VerifiedDoc, Owner: OwnerUser,
			Note: "Holds Observer's prompt-submit entry.",
		},
		{
			ID: "poolside/hooks.Stop", Tool: "poolside", FileID: poolsideSettings, Path: KeyPath{"hooks", "Stop"}, Type: TypeObject,
			Desc: "Hooks run when the agent stops.", Source: poolsideHooksRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "poolside/hooks.PreCompact", Tool: "poolside", FileID: poolsideSettings, Path: KeyPath{"hooks", "PreCompact"}, Type: TypeObject,
			Desc: "Hooks run before compaction.", Source: poolsideHooksRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "poolside/hooks.SessionStart", Tool: "poolside", FileID: poolsideSettings, Path: KeyPath{"hooks", "SessionStart"}, Type: TypeObject,
			Desc: "Hooks run at session start.", Source: poolsideHooksRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		// --- subagents, agent servers
		{
			ID: "poolside/subagents.default", Tool: "poolside", FileID: poolsideSettings, Path: KeyPath{"subagents", "default"}, Type: TypeString,
			Desc: "Default subagent name.", Source: poolsideRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "poolside/subagents.agents", Tool: "poolside", FileID: poolsideSettings, Path: KeyPath{"subagents", "agents"}, Type: TypeObject,
			Desc:   "Subagents by name: type (in_process | command | agent_server), description, instructions, command, args, env, agent_server, disabled.",
			Source: poolsideRef, Verified: VerifiedDoc, Owner: OwnerUser, Note: "A command subagent runs an arbitrary executable.",
		},
		{
			ID: "poolside/agent_servers", Tool: "poolside", FileID: poolsideSettings, Path: KeyPath{"agent_servers"}, Scopes: poolsidePersonalOnly, Type: TypeObject,
			Desc: "ACP agent servers by name: type, command or url, headers, args, env, default_config_options.", Source: poolsideRef, Verified: VerifiedDoc,
			Owner: OwnerUser, Secret: true,
			Note: poolsidePersonal + " headers / env can carry credentials, so the map is secret.",
		},
	},
	Map: []MapRow{
		{
			Key: KeySandboxMode, Tool: "poolside", Settings: []string{"poolside/sandbox.filesystem.workspaces.access"}, Transform: TransformEnumMap,
			Params: TransformParams{Enum: []EnumPair{{"read_only", []any{"read-only"}}, {"workspace_write", []any{"read-write"}}}}, Source: poolsideRef,
			Note: "Workspace access inside the sandbox; off (no sandbox) is a launch flag (--sandbox disabled), so it is refused here.",
		},
		{
			Key: KeyBaseURL, Tool: "poolside", Settings: []string{"poolside/pool.api_url"}, Transform: TransformIdentity, Source: poolsideRef,
			Note: "Personal file only.",
		},
	},
}
