package agentconfig

// JetBrains Junie CLI catalogue. Sources: junie.jetbrains.com/docs/
// junie-cli-configuration (re-fetched 2026-10-04 for the full config.json
// key table), junie-cli-hooks, environment-variables, parameters, audit
// section 5 "junie"; repo docs/junie-adapter.md. Junie was NOT installed on
// demo node 1, so every row is doc-grounded. The IDE plugin / JetBrains AI
// Assistant keep their settings in the IDE's options store (UNVERIFIED for
// Junie keys) and IDE (ACP) mode ignores CLI hooks; neither is catalogued.
const (
	junieRef      = "https://junie.jetbrains.com/docs/junie-cli-configuration.html"
	junieHooksRef = "https://junie.jetbrains.com/docs/junie-cli-hooks.html"
	junieEnvRef   = "https://junie.jetbrains.com/docs/environment-variables.html"
	junieConfig   = "junie/config"
	junieMCP      = "junie/mcp"
	junieAdapter  = "docs/junie-adapter.md"
)

var junieCatalog = toolCatalog{
	Tool:            "junie",
	VerifiedAgainst: "vendor docs fetched 2026-10-03 (audit section 5) and the CLI configuration page re-fetched 2026-10-04; not installed on node 1, no live keys",
	Files: []FileRow{
		{
			ID: "junie/user-config", Tool: "junie", Shape: junieConfig, Kind: KindSettings, Scope: ScopeUser,
			Paths: samePath("~/.junie/config.json"), Format: FormatJSON, Precedence: 20, WriteNeeds: PrivUser, Grounding: doc(junieRef),
			Note: "JUNIE_HOME relocates ~/.junie (Windows %USERPROFILE%\\.junie per the docs, unconfirmed live). --config-location / JUNIE_CONFIG_LOCATION add files; CLI flags rank above every file.",
		},
		{
			ID: "junie/project-config", Tool: "junie", Shape: junieConfig, Kind: KindSettings, Scope: ScopeProjectShared,
			Paths: samePath("<project>/.junie/config.json"), Format: FormatJSON, Precedence: 30, WriteNeeds: PrivUser, Grounding: doc(junieRef),
			Note: "Team-shared; loaded only for a trusted project. Project hooks here are ignored for security.",
		},
		{
			ID: "junie/user-settings", Tool: "junie", Shape: "junie/settings", Kind: KindSettings, Scope: ScopeUser,
			Paths: samePath("~/.junie/settings.json"), Format: FormatJSON, Precedence: 20, WriteNeeds: PrivNever,
			Grounding: Grounding{Kind: GroundAdapterDoc, Source: junieAdapter, Date: auditDate},
			Note:      "A separate settings channel (not merged with config.json; precedence across the two is UNVERIFIED). It carries model / provider config that may include credential fields, so it is never read or written.",
		},
		{
			ID: "junie/user-mcp", Tool: "junie", Shape: junieMCP, Kind: KindMCP, Scope: ScopeUser,
			Paths: samePath("~/.junie/mcp/mcp.json"), Format: FormatJSON, Precedence: 20, WriteNeeds: PrivUser, Grounding: doc(junieRef),
			Note: "Default MCP location; mcp-locations adds more.",
		},
		{
			ID: "junie/project-mcp", Tool: "junie", Shape: junieMCP, Kind: KindMCP, Scope: ScopeProjectShared,
			Paths: samePath("<project>/.junie/mcp/mcp.json"), Format: FormatJSON, Precedence: 30, WriteNeeds: PrivUser, Grounding: doc(junieRef),
		},
		{
			ID: "junie/allowlist", Tool: "junie", Shape: "junie/allowlist", Kind: KindPermissions, Scope: ScopeUser,
			Paths: samePath("~/.junie/allowlist.json"), Format: FormatJSON, Precedence: 20, WriteNeeds: PrivNever,
			Grounding: Grounding{Kind: GroundAdapterDoc, Source: junieAdapter, Date: auditDate},
			Note:      "The CLI's approved-command list, managed by Junie itself (shape UNVERIFIED): never read or written. A broad entry approves commands unasked.",
		},
		{
			ID: "junie/secure-credentials", Tool: "junie", Shape: "junie/secure-credentials", Kind: KindToolState, Scope: ScopeUser,
			Paths: samePath("~/.junie/secure_credentials.json"), Format: FormatJSON, Precedence: 0, WriteNeeds: PrivNever,
			Grounding: Grounding{Kind: GroundAdapterDoc, Source: junieAdapter, Date: auditDate},
			Note:      "Credential store: never touched (nor the trust/ directory beside it).",
		},
	},
	Extra: []SettingRow{
		{
			ID: "junie/model", Tool: "junie", FileID: junieConfig, Path: KeyPath{"model"}, Type: TypeString,
			Desc: "Default model: a built-in id, alias or custom profile (JUNIE_MODEL, --model override).", Source: junieRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "junie/effort", Tool: "junie", FileID: junieConfig, Path: KeyPath{"effort"}, Type: TypeString,
			Enum: []string{"low", "medium", "high"}, Desc: "Reasoning effort (JUNIE_EFFORT overrides).", Source: junieRef, Verified: VerifiedDoc, Owner: OwnerUser,
			Note: "The configuration page calls the values model-dependent; low / medium / high are the documented ones.",
		},
		{
			ID: "junie/provider", Tool: "junie", FileID: junieConfig, Path: KeyPath{"provider"}, Type: TypeString,
			Enum: []string{"openai", "anthropic", "google", "xai", "openrouter", "copilot", "litellm"}, Desc: "Default BYOK provider (JUNIE_LLM_PROVIDER overrides).",
			Source: junieEnvRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "junie/brave", Tool: "junie", FileID: junieConfig, Path: KeyPath{"brave"}, Type: TypeBool, Default: "false",
			Desc: "Start in Brave Mode (no approvals, interactive only).", Source: junieRef, Verified: VerifiedDoc, Owner: OwnerUser, Danger: DangerConfirm,
			Note: "true approves every action without asking.",
		},
		{
			ID: "junie/flags", Tool: "junie", FileID: junieConfig, Path: KeyPath{"flags"}, Type: TypeStringList, Default: "[]",
			Desc: "Additional feature flags.", Source: junieRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "junie/auto-update", Tool: "junie", FileID: junieConfig, Path: KeyPath{"auto-update"}, Type: TypeBool, Default: "true",
			Desc: "Check for updates automatically.", Source: junieRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "junie/guidelines-location", Tool: "junie", FileID: junieConfig, Path: KeyPath{"guidelines-location"}, Type: TypeString,
			Desc: "Path of a custom guidelines file (relative to the config file).", Source: junieRef, Verified: VerifiedDoc, Owner: OwnerUser,
			Note: "--guidelines-filename / JUNIE_GUIDELINES_FILENAME override it; a single path, not a list.",
		},
		{
			ID: "junie/byok", Tool: "junie", FileID: junieConfig, Path: KeyPath{"byok"}, Type: TypeStringMap,
			Desc: "BYOK API keys by provider.", Source: junieRef, Verified: VerifiedDoc, Owner: OwnerUser, Secret: true,
			Note: "Holds literal keys: never written by Observer. The JUNIE_*_API_KEY env vars are the alternative.",
		},
		{
			ID: "junie/proxies", Tool: "junie", FileID: junieConfig, Path: KeyPath{"proxies"}, Type: TypeObject, Default: "[]",
			Desc: "Custom LLM routing endpoints.", Source: junieRef, Verified: VerifiedDoc, Owner: OwnerUser, Danger: DangerConfirm,
			Note: "An array whose entry shape is UNVERIFIED; an entry that is not Observer routes traffic off the proxy (JUNIE_LITELLM_URL likewise).",
		},
		{
			ID: "junie/mcp-locations", Tool: "junie", FileID: junieConfig, Path: KeyPath{"mcp-locations"}, Type: TypeStringList, Default: "[]",
			Desc: "Extra folders searched for MCP configuration.", Source: junieRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "junie/mcp-default-locations", Tool: "junie", FileID: junieConfig, Path: KeyPath{"mcp-default-locations"}, Type: TypeBool, Default: "true",
			Desc: "Search the default MCP locations.", Source: junieRef, Verified: VerifiedDoc, Owner: OwnerUser, Danger: DangerRefuseWhileIntegrated,
			Note: "false skips ~/.junie/mcp and .junie/mcp, dropping any MCP server registered there.",
		},
		{
			ID: "junie/skill-locations", Tool: "junie", FileID: junieConfig, Path: KeyPath{"skill-locations"}, Type: TypeStringList, Default: "[]",
			Desc: "Extra folders searched for skills.", Source: junieRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "junie/skill-default-locations", Tool: "junie", FileID: junieConfig, Path: KeyPath{"skill-default-locations"}, Type: TypeBool, Default: "true",
			Desc: "Search the default skill locations.", Source: junieRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "junie/command-locations", Tool: "junie", FileID: junieConfig, Path: KeyPath{"command-locations"}, Type: TypeStringList, Default: "[]",
			Desc: "Extra folders searched for slash commands.", Source: junieRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "junie/command-default-locations", Tool: "junie", FileID: junieConfig, Path: KeyPath{"command-default-locations"}, Type: TypeBool, Default: "true",
			Desc: "Search the default command locations.", Source: junieRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "junie/agent-locations", Tool: "junie", FileID: junieConfig, Path: KeyPath{"agent-locations"}, Type: TypeStringList, Default: "[]",
			Desc: "Extra folders searched for custom agents.", Source: junieRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "junie/agent-default-locations", Tool: "junie", FileID: junieConfig, Path: KeyPath{"agent-default-locations"}, Type: TypeBool, Default: "true",
			Desc: "Search the default agent locations.", Source: junieRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "junie/model-locations", Tool: "junie", FileID: junieConfig, Path: KeyPath{"model-locations"}, Type: TypeStringList, Default: "[]",
			Desc: "Extra folders searched for custom model profiles.", Source: junieRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "junie/model-default-locations", Tool: "junie", FileID: junieConfig, Path: KeyPath{"model-default-locations"}, Type: TypeBool, Default: "true",
			Desc: "Search the default model-profile locations.", Source: junieRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "junie/hooks", Tool: "junie", FileID: junieConfig, Path: KeyPath{"hooks"}, Scopes: []Scope{ScopeUser}, Type: TypeObject, Default: "{}",
			Desc:   "Shell hooks by event (SessionStart, UserPromptSubmit, PreToolUse, Stop, StopFailure, PermissionRequest, SessionEnd), Claude-shaped matcher groups.",
			Source: junieHooksRef, Verified: VerifiedDoc, Owner: OwnerUser,
			Note: "User config only (project hooks are ignored unless passed via --config-location); CLI only, not IDE / ACP mode.",
		},
		{
			ID: "junie/mcpServers", Tool: "junie", FileID: junieMCP, Path: KeyPath{"mcpServers"}, Type: TypeMCPMap,
			Desc: "MCP servers (Claude-style).", Source: junieRef, Verified: VerifiedUnverified, Owner: OwnerUser,
			Note: "The audit marks the mcpServers shape UNVERIFIED (V12); not mapped to the common vocabulary.",
		},
	},
	Map: []MapRow{
		{Key: KeyModel, Tool: "junie", Settings: []string{"junie/model"}, Transform: TransformModelAlias, Source: junieRef},
		{
			Key: KeyReasoningEffort, Tool: "junie", Settings: []string{"junie/effort"}, Transform: TransformEnumMap,
			Params: TransformParams{Enum: []EnumPair{{"low", []any{"low"}}, {"medium", []any{"medium"}}, {"high", []any{"high"}}}}, Source: junieRef,
		},
		{
			Key: KeyApprovalMode, Tool: "junie", Settings: []string{"junie/brave"}, Transform: TransformEnumMap,
			Params: TransformParams{Enum: []EnumPair{{"full_auto", []any{true}}}}, Source: junieRef,
			Note: "Only full_auto (Brave Mode) is expressible; false (the asking default) has no single canonical level.",
		},
		{Key: KeyAutoUpdate, Tool: "junie", Settings: []string{"junie/auto-update"}, Transform: TransformIdentity, Source: junieRef},
	},
}
