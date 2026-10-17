package agentconfig

// Kiro Crew catalogue. Crew drives kiro-cli as its engine, but nothing is
// shared implicitly: these are Crew's OWN documented files only (kiro-cli's
// steering, permissions and managed-settings rails may also govern the
// driven sessions; that coverage is UNVERIFIED and lives in the kiro-cli
// catalogue). Sources: kiro.dev/docs/crew/configuration (re-fetched
// 2026-10-04 for the full key table), crew/capabilities/hooks, audit section
// 5 "kiro-crew"; repo docs/kiro-crew-adapter.md. Not installed on node 1.
const (
	kiroCrewRef      = "https://kiro.dev/docs/crew/configuration.md"
	kiroCrewHooksRef = "https://kiro.dev/docs/crew/capabilities/hooks/"
	kiroCrewAdapter  = "docs/kiro-crew-adapter.md"
	kiroCrewConfig   = "kiro-crew/config"
	kiroCrewMCP      = "kiro-crew/mcp"
	kiroCrewHooks    = "kiro-crew/hooks"
)

// kiroCrewMCPShape: Crew's own mcp.json uses the standard mcpServers shape;
// only local servers are grounded.
var kiroCrewMCPShape = MCPShape{EnvKey: "env", TypeNames: map[string]string{"stdio": ""}}

var kiroCrewCatalog = toolCatalog{
	Tool:            "kiro-crew",
	VerifiedAgainst: "vendor docs fetched 2026-10-03 (audit section 5) and the configuration reference re-fetched 2026-10-04; repo docs/kiro-crew-adapter.md; not installed on node 1, no live keys",
	Files: []FileRow{
		{
			ID: "kiro-crew/user-config", Tool: "kiro-crew", Shape: kiroCrewConfig, Kind: KindSettings, Scope: ScopeUser,
			Paths: samePath("~/.kiro/crew/config.json"), Format: FormatJSON, Precedence: 20, WriteNeeds: PrivUser, Grounding: doc(kiroCrewRef),
			Note: "Shipped defaults fill missing keys. KIROCREW_HOME relocates ~/.kiro/crew; the Windows / macOS location is presumed the same (UNVERIFIED).",
		},
		{
			ID: "kiro-crew/user-local-config", Tool: "kiro-crew", Shape: kiroCrewConfig, Kind: KindSettings, Scope: ScopeUser,
			Paths: samePath("~/.kiro/crew/config.local.json"), Format: FormatJSON, Precedence: 25, WriteNeeds: PrivUser, Grounding: doc(kiroCrewRef),
			Note: "Written by --local; overrides config.json and survives updates.",
		},
		{
			ID: "kiro-crew/user-mcp", Tool: "kiro-crew", Shape: kiroCrewMCP, Kind: KindMCP, Scope: ScopeUser,
			Paths: samePath("~/.kiro/crew/mcp.json"), Format: FormatJSON, Precedence: 20, WriteNeeds: PrivUser,
			Grounding: Grounding{Kind: GroundAdapterDoc, Source: kiroCrewAdapter, Date: auditDate},
			Note:      "Crew's own servers, separate from kiro-cli's ~/.kiro/settings/mcp.json.",
		},
		{
			ID: "kiro-crew/user-hooks", Tool: "kiro-crew", Shape: kiroCrewHooks, Kind: KindHooks, Scope: ScopeUser,
			Paths: samePath("~/.kiro/crew/hooks/"), Format: FormatJSON, Precedence: 20, WriteNeeds: PrivUser, Grounding: doc(kiroCrewHooksRef),
			Note: "Global JSON hook files (an install showed hooks.json = {\"hooks\": []}, repo adapter doc).",
		},
		{
			ID: "kiro-crew/project-hooks", Tool: "kiro-crew", Shape: kiroCrewHooks, Kind: KindHooks, Scope: ScopeProjectShared,
			Paths: samePath("<project>/.kiro/hooks/"), Format: FormatJSON, Precedence: 30, WriteNeeds: PrivUser, Grounding: doc(kiroCrewHooksRef),
			Note: "The SAME directory kiro-cli reads: one project hook fires under both tools.",
		},
		{
			ID: "kiro-crew/env", Tool: "kiro-crew", Shape: "kiro-crew/env", Kind: KindToolState, Scope: ScopeUser,
			Paths: samePath("~/.kiro/crew/.env"), Format: FormatScript, Precedence: 0, WriteNeeds: PrivNever, Grounding: doc(kiroCrewRef),
			Note: "Channel credentials (Slack bot tokens): never read or written.",
		},
		{
			ID: "kiro-crew/agent-model-state", Tool: "kiro-crew", Shape: "kiro-crew/agent-model-state", Kind: KindToolState, Scope: ScopeUser,
			Paths: samePath("~/.kiro/crew/agent_model_state.json"), Format: FormatJSON, Precedence: 0, WriteNeeds: PrivNever,
			Grounding: Grounding{Kind: GroundAdapterDoc, Source: kiroCrewAdapter, Date: auditDate},
			Note:      "Model-picker UI state (crons.json, folders.json, tags.json and session_map.json beside it are app state too): never written.",
		},
	},
	Extra: []SettingRow{
		// --- agent
		{
			ID: "kiro-crew/agent.approval_mode", Tool: "kiro-crew", FileID: kiroCrewConfig, Path: KeyPath{"agent", "approval_mode"}, Type: TypeString,
			Enum: []string{"auto"}, Default: `"auto"`, Desc: "Agent approval behaviour.", Source: kiroCrewRef, Verified: VerifiedDoc, Owner: OwnerUser,
			Danger: DangerConfirm, Note: "Only auto is documented; other values are UNVERIFIED.",
		},
		{
			ID: "kiro-crew/agent.sandbox", Tool: "kiro-crew", FileID: kiroCrewConfig, Path: KeyPath{"agent", "sandbox"}, Type: TypeString,
			Enum: []string{"auto"}, Default: `"auto"`, Desc: "Sandbox posture for agent subprocesses.", Source: kiroCrewRef, Verified: VerifiedDoc,
			Owner: OwnerUser, Danger: DangerConfirm, Note: "Only auto is documented; other values are UNVERIFIED.",
		},
		{
			ID: "kiro-crew/agent.sandbox_allow_unsandboxed_exec", Tool: "kiro-crew", FileID: kiroCrewConfig, Path: KeyPath{"agent", "sandbox_allow_unsandboxed_exec"},
			Type: TypeBool, Default: "false", Desc: "Let the agent run commands outside the sandbox.", Source: kiroCrewRef, Verified: VerifiedDoc,
			Owner: OwnerUser, Danger: DangerConfirm, Note: "true escapes the sandbox.",
		},
		{
			ID: "kiro-crew/agent.chat_turn_timeout_secs", Tool: "kiro-crew", FileID: kiroCrewConfig, Path: KeyPath{"agent", "chat_turn_timeout_secs"}, Type: TypeInt,
			Min: f64(0), Default: "14400", Desc: "Wall-clock limit per chat turn, in seconds.", Source: kiroCrewRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "kiro-crew/agent.subagent_timeout_secs", Tool: "kiro-crew", FileID: kiroCrewConfig, Path: KeyPath{"agent", "subagent_timeout_secs"}, Type: TypeInt,
			Min: f64(0), Default: "10800", Desc: "Wall-clock limit per subagent, in seconds.", Source: kiroCrewRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "kiro-crew/agent.subagent_max_turns", Tool: "kiro-crew", FileID: kiroCrewConfig, Path: KeyPath{"agent", "subagent_max_turns"}, Type: TypeInt,
			Min: f64(0), Max: f64(1000), Default: "1000", Desc: "Tool-call budget per subagent.", Source: kiroCrewRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "kiro-crew/agent.session_control", Tool: "kiro-crew", FileID: kiroCrewConfig, Path: KeyPath{"agent", "session_control"}, Type: TypeBool,
			Default: "true", Desc: "Let the agent manage peer sessions.", Source: kiroCrewRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "kiro-crew/agent.refusal_fallback_model", Tool: "kiro-crew", FileID: kiroCrewConfig, Path: KeyPath{"agent", "refusal_fallback_model"}, Type: TypeString,
			Default: `""`, Desc: "Model used to retry after a content-filter refusal.", Source: kiroCrewRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		// --- session, orchestrator, decisions
		{
			ID: "kiro-crew/session.timeout_secs", Tool: "kiro-crew", FileID: kiroCrewConfig, Path: KeyPath{"session", "timeout_secs"}, Type: TypeInt,
			Min: f64(0), Default: "3600", Desc: "Idle-session timeout in seconds (0 disables).", Source: kiroCrewRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "kiro-crew/session.autocompact_pct", Tool: "kiro-crew", FileID: kiroCrewConfig, Path: KeyPath{"session", "autocompact_pct"}, Type: TypeInt,
			Min: f64(0), Max: f64(100), Default: "70", Desc: "Context use, in percent, that triggers auto-compaction.", Source: kiroCrewRef,
			Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "kiro-crew/orchestrator.max_plan_duration_seconds", Tool: "kiro-crew", FileID: kiroCrewConfig, Path: KeyPath{"orchestrator", "max_plan_duration_seconds"},
			Type: TypeInt, Min: f64(0), Default: "7200", Desc: "Limit on an unattended plan run, in seconds (0 removes it).", Source: kiroCrewRef,
			Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "kiro-crew/decisions.model_route.simple", Tool: "kiro-crew", FileID: kiroCrewConfig, Path: KeyPath{"decisions", "model_route", "simple"},
			Type: TypeString, Default: `""`, Desc: "Model for simple messages.", Source: kiroCrewRef, Verified: VerifiedDoc, Owner: OwnerUser,
			Note: "The closest analogue of a small model; there is no single default-model key.",
		},
		{
			ID: "kiro-crew/decisions.model_route.medium", Tool: "kiro-crew", FileID: kiroCrewConfig, Path: KeyPath{"decisions", "model_route", "medium"},
			Type: TypeString, Default: `""`, Desc: "Model for medium messages.", Source: kiroCrewRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "kiro-crew/decisions.model_route.complex", Tool: "kiro-crew", FileID: kiroCrewConfig, Path: KeyPath{"decisions", "model_route", "complex"},
			Type: TypeString, Default: `""`, Desc: "Model for complex messages.", Source: kiroCrewRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "kiro-crew/decisions.bucket", Tool: "kiro-crew", FileID: kiroCrewConfig, Path: KeyPath{"decisions", "bucket"}, Type: TypeInt,
			Min: f64(0), Max: f64(100), Default: "100", Desc: "Percent of sessions sampled for routing decisions.", Source: kiroCrewRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "kiro-crew/decisions.history_budget_chars", Tool: "kiro-crew", FileID: kiroCrewConfig, Path: KeyPath{"decisions", "history_budget_chars"}, Type: TypeInt,
			Min: f64(0), Default: "0", Desc: "Prior-conversation characters given to routing decisions.", Source: kiroCrewRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		// --- dashboard, skills, misc
		{
			ID: "kiro-crew/dashboard.default_memory_mode", Tool: "kiro-crew", FileID: kiroCrewConfig, Path: KeyPath{"dashboard", "default_memory_mode"}, Type: TypeEnum,
			Enum: []string{"persistent", "incognito", "temporary"}, Default: `"persistent"`, Desc: "Memory mode for new chats.", Source: kiroCrewRef,
			Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "kiro-crew/dashboard.bot_name", Tool: "kiro-crew", FileID: kiroCrewConfig, Path: KeyPath{"dashboard", "bot_name"}, Type: TypeString,
			Desc: "Agent display name.", Source: kiroCrewRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "kiro-crew/dashboard.model_picker_hidden_models", Tool: "kiro-crew", FileID: kiroCrewConfig, Path: KeyPath{"dashboard", "model_picker_hidden_models"},
			Type: TypeStringList, Default: "[]", Desc: "Models hidden from the picker.", Source: kiroCrewRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "kiro-crew/dashboard.link_patterns", Tool: "kiro-crew", FileID: kiroCrewConfig, Path: KeyPath{"dashboard", "link_patterns"}, Type: TypeObject,
			Desc: "Regex / URL pairs that turn text into links.", Source: kiroCrewRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "kiro-crew/dashboard.terminal.completion.enabled", Tool: "kiro-crew", FileID: kiroCrewConfig, Path: KeyPath{"dashboard", "terminal", "completion", "enabled"},
			Type: TypeBool, Default: "true", Desc: "Terminal command completion.", Source: kiroCrewRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "kiro-crew/skills.max_triggered", Tool: "kiro-crew", FileID: kiroCrewConfig, Path: KeyPath{"skills", "max_triggered"}, Type: TypeInt,
			Min: f64(0), Default: "0", Desc: "Maximum skills injected per message.", Source: kiroCrewRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "kiro-crew/wakatime.enabled", Tool: "kiro-crew", FileID: kiroCrewConfig, Path: KeyPath{"wakatime", "enabled"}, Type: TypeBool, Default: "false",
			Desc: "Report coding hours to WakaTime.", Source: kiroCrewRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "kiro-crew/telemetry.enabled", Tool: "kiro-crew", FileID: kiroCrewConfig, Path: KeyPath{"telemetry", "enabled"}, Type: TypeBool, Default: "false",
			Desc: "Collect local metrics.", Source: kiroCrewRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "kiro-crew/auto_update", Tool: "kiro-crew", FileID: kiroCrewConfig, Path: KeyPath{"auto_update"}, Type: TypeBool, Default: "true",
			Desc: "Check for updates automatically.", Source: kiroCrewRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "kiro-crew/instances.enabled", Tool: "kiro-crew", FileID: kiroCrewConfig, Path: KeyPath{"instances", "enabled"}, Type: TypeBool, Default: "false",
			Desc: "Multi-instance management.", Source: kiroCrewRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "kiro-crew/instances.warm_set_cap", Tool: "kiro-crew", FileID: kiroCrewConfig, Path: KeyPath{"instances", "warm_set_cap"}, Type: TypeInt,
			Min: f64(0), Default: "0", Desc: "Warm-tunnel limit (0 sizes it automatically).", Source: kiroCrewRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "kiro-crew/capabilities.publish", Tool: "kiro-crew", FileID: kiroCrewConfig, Path: KeyPath{"capabilities", "publish"}, Type: TypeString,
			Desc: "Governance of public artifact publishing (disabled by default).", Source: kiroCrewRef, Verified: VerifiedDoc, Owner: OwnerUser,
			Danger: DangerConfirm, Note: "Enabling it lets the agent publish artifacts publicly. Values not listed in the doc.",
		},
		{
			ID: "kiro-crew/publish.allowed_destinations", Tool: "kiro-crew", FileID: kiroCrewConfig, Path: KeyPath{"publish", "allowed_destinations"}, Type: TypeStringList,
			Default: "[]", Desc: "Approved publishing destinations.", Source: kiroCrewRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		// --- mcp.json, hooks
		{
			ID: "kiro-crew/mcpServers", Tool: "kiro-crew", FileID: kiroCrewMCP, Path: KeyPath{"mcpServers"}, Type: TypeMCPMap,
			Desc: "Crew's own MCP servers (standard mcpServers shape).", Source: kiroCrewRef, Verified: VerifiedDoc, Owner: OwnerUser,
			Note: "Shape grounded by the repo adapter doc (docs/kiro-crew-adapter.md).",
		},
		{
			ID: "kiro-crew/hooks", Tool: "kiro-crew", FileID: kiroCrewHooks, Path: KeyPath{"hooks"}, Type: TypeObject,
			Desc: "Hooks: trigger, matcher regex, action (a command whose exit 2 blocks, or a prompt injection).", Source: kiroCrewHooksRef,
			Verified: VerifiedDoc, Owner: OwnerUser,
		},
	},
	Map: []MapRow{
		{Key: KeyAutoCompact, Tool: "kiro-crew", Settings: []string{"kiro-crew/session.autocompact_pct"}, Transform: TransformIdentity, Source: kiroCrewRef},
		{Key: KeyTelemetryOff, Tool: "kiro-crew", Settings: []string{"kiro-crew/telemetry.enabled"}, Transform: TransformBoolInvert, Source: kiroCrewRef},
		{Key: KeyAutoUpdate, Tool: "kiro-crew", Settings: []string{"kiro-crew/auto_update"}, Transform: TransformIdentity, Source: kiroCrewRef},
		{
			Key: KeyMCPServers, Tool: "kiro-crew", Settings: []string{"kiro-crew/mcpServers"}, Transform: TransformMCPShape,
			Params: TransformParams{MCP: kiroCrewMCPShape}, Lossy: true, Source: kiroCrewRef,
			Note: "Local servers only (remote transports are not grounded). " + mcpLossyNote,
		},
	},
}
