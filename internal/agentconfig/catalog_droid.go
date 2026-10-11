package agentconfig

// Factory Droid catalogue. Sources: docs.factory.com cli/configuration/
// settings (re-fetched 2026-10-04 for the full key table),
// enterprise/hierarchical-settings-and-org-control, byok, hooks-reference,
// mcp, user-guides/auto-run (autonomy levels, fetched 2026-10-04), audit
// section 5 "droid" and section 6 finding 2; repo docs/droid-adapter.md.
// Node 1 had droid installed: logoAnimation and trustedFolders were seen in
// ~/.factory/settings.json and the org managed-settings cache keys in
// ~/.factory/org-managed-settings.cache.json on 2026-10-03.
const (
	droidRef       = "https://docs.factory.com/cli/configuration/settings"
	droidOrgRef    = "https://docs.factory.com/enterprise/hierarchical-settings-and-org-control.md"
	droidBYOKRef   = "https://docs.factory.com/cli/configuration/byok.md"
	droidHooksRef  = "https://docs.factory.com/reference/hooks-reference"
	droidMCPRef    = "https://docs.factory.com/cli/configuration/mcp.md"
	droidAutoRef   = "https://docs.factory.com/cli/user-guides/auto-run"
	droidSettings  = "droid/settings"
	droidHooks     = "droid/hooks"
	droidMCP       = "droid/mcp"
	droidOrgCache  = "droid/org-cache"
	droidLiveNote  = "Seen on node 1 2026-10-03 (audit section 5 droid, live)."
	droidCacheNote = "Server-delivered org policy cached locally (audit section 6 finding 2): shown read-only, never written. Type inferred from the key name."
	droidOrgOnly   = "An org / enterprise control: honoured from the managed layer (system settings.json or the org endpoint)."
)

var droidManagedOnly = []Scope{ScopeManaged}

// droidMCPShape: mcpServers.<name> {type stdio, command, args, env} or
// {type http, url} (Claude-compatible).
var droidMCPShape = MCPShape{
	EnvKey: "env", TypeKey: "type", StdioTypeExplicit: true,
	TypeNames: map[string]string{"stdio": "stdio", "http": "http"},
	URLKeys:   map[string]string{"http": "url"},
}

var droidCatalog = toolCatalog{
	Tool:            "droid",
	VerifiedAgainst: "vendor docs fetched 2026-10-03 (audit section 5) and 2026-10-04; node 1 live key names 2026-10-03 for logoAnimation, trustedFolders and the org-managed-settings cache",
	Files: []FileRow{
		{
			ID: "droid/system-settings", Tool: "droid", Shape: droidSettings, Kind: KindSettings, Scope: ScopeManaged,
			Paths: OSPaths{
				Linux:   "/etc/factory/settings.json",
				Darwin:  "/Library/Application Support/Factory/settings.json",
				Windows: `C:\Program Files\Factory\settings.json`,
			},
			Format: FormatJSON, Precedence: 100, WriteNeeds: PrivAdminRoot, Grounding: doc(droidOrgRef),
			Note: "The authoritative org-level file when present (Linux path also used on WSL). Hard controls cannot be weakened below it; the org endpoint delivers the same keys server-side.",
		},
		{
			ID: "droid/user-settings", Tool: "droid", Shape: droidSettings, Kind: KindSettings, Scope: ScopeUser,
			Paths: samePath("~/.factory/settings.json"), Format: FormatJSON, Precedence: 20, WriteNeeds: PrivUser,
			Grounding: live("~/.factory/settings.json carries logoAnimation, trustedFolders"),
			Note: "Windows: %USERPROFILE%\\.factory\\settings.json. customModels[].apiKey may be a PLAINTEXT key (repo docs/droid-adapter.md): " +
				"the customModels row is secret. The per-session <uuid>.settings.json sidecars under ~/.factory/sessions are not config.",
		},
		{
			ID: "droid/user-local-settings", Tool: "droid", Shape: droidSettings, Kind: KindSettings, Scope: ScopeUser,
			Paths: samePath("~/.factory/settings.local.json"), Format: FormatJSON, Precedence: 25, WriteNeeds: PrivUser, Grounding: doc(droidRef),
			Note: "Merges on top of ~/.factory/settings.json.",
		},
		{
			ID: "droid/project-settings", Tool: "droid", Shape: droidSettings, Kind: KindSettings, Scope: ScopeProjectShared,
			Paths: samePath("<project>/.factory/settings.json"), Format: FormatJSON, Precedence: 30, WriteNeeds: PrivUser, Grounding: doc(droidRef),
			Note: "At the git root; a sub-folder .factory/settings.json is more specific still (folder > project > user for session defaults).",
		},
		{
			ID: "droid/project-local-settings", Tool: "droid", Shape: droidSettings, Kind: KindSettings, Scope: ScopeProjectLocal,
			Paths: samePath("<project>/.factory/settings.local.json"), Format: FormatJSON, Precedence: 40, WriteNeeds: PrivUser, Grounding: doc(droidRef),
			Note: "Merges on top of the project settings.json.",
		},
		{
			ID: "droid/user-hooks", Tool: "droid", Shape: droidHooks, Kind: KindHooks, Scope: ScopeUser,
			Paths: samePath("~/.factory/hooks.json"), Format: FormatJSON, Precedence: 20, WriteNeeds: PrivUser, Grounding: doc(droidHooksRef),
			Note: "The event map sits at the root of this file ({\"PreToolUse\":[...]}); a {\"hooks\":{...}} wrapper is ignored by droid (live-grounded 2026-10-04). Observer's entries are an owned span. Hooks may also sit under settings.json hooks; the legacy .factory/hooks/hooks.json auto-migrates.",
		},
		{
			ID: "droid/project-hooks", Tool: "droid", Shape: droidHooks, Kind: KindHooks, Scope: ScopeProjectShared,
			Paths: samePath("<project>/.factory/hooks.json"), Format: FormatJSON, Precedence: 30, WriteNeeds: PrivUser, Grounding: doc(droidHooksRef),
		},
		{
			ID: "droid/user-mcp", Tool: "droid", Shape: droidMCP, Kind: KindMCP, Scope: ScopeUser,
			Paths: samePath("~/.factory/mcp.json"), Format: FormatJSON, Precedence: 20, WriteNeeds: PrivUser, Grounding: doc(droidMCPRef),
			Note: "Observer's mcpServers.observer entry is an owned span. Org-managed MCP servers take precedence over user and project ones.",
		},
		{
			ID: "droid/project-mcp", Tool: "droid", Shape: droidMCP, Kind: KindMCP, Scope: ScopeProjectShared,
			Paths: samePath("<project>/.factory/mcp.json"), Format: FormatJSON, Precedence: 30, WriteNeeds: PrivUser, Grounding: doc(droidMCPRef),
		},
		{
			ID: "droid/org-managed-settings-cache", Tool: "droid", Shape: droidOrgCache, Kind: KindToolState, Scope: ScopeUser,
			Paths: samePath("~/.factory/org-managed-settings.cache.json"), Format: FormatJSON, Precedence: 0, WriteNeeds: PrivNever,
			Grounding: live("~/.factory/org-managed-settings.cache.json carries managedSettings.modelPolicy, orgId, revision, cachedAt"),
			Note:      "A local cache of the vendor's server-delivered org policy: read-only evidence for the admin, never written. auth.v2.* beside it holds credentials and is never opened.",
		},
		{
			ID: "droid/legacy-config", Tool: "droid", Shape: "droid/legacy-config", Kind: KindToolState, Scope: ScopeUser,
			Paths: samePath("~/.factory/config.json"), Format: FormatJSON, Precedence: 0, WriteNeeds: PrivNever, Grounding: doc(droidBYOKRef),
			Note: "Deprecated customModels location (superseded by settings.json); may hold literal API keys, never read.",
		},
	},
	Extra: []SettingRow{
		// --- model
		{
			ID: "droid/model", Tool: "droid", FileID: droidSettings, Path: KeyPath{"model"}, Type: TypeString,
			Desc: "Default model: a Factory catalogue id or a custom model id.", Source: droidRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "droid/reasoningEffort", Tool: "droid", FileID: droidSettings, Path: KeyPath{"reasoningEffort"}, Type: TypeEnum,
			Enum: []string{"none", "dynamic", "off", "minimal", "low", "medium", "high", "xhigh", "max"},
			Desc: "Reasoning effort (model-dependent default).", Source: droidRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "droid/customModels", Tool: "droid", FileID: droidSettings, Path: KeyPath{"customModels"}, Type: TypeObject,
			Desc:   "BYOK models: model, displayName, baseUrl, apiKey, provider (anthropic | openai | generic-chat-completion-api), maxOutputTokens, extraArgs.",
			Source: droidBYOKRef, Verified: VerifiedDoc, Owner: OwnerUser, Secret: true, Danger: DangerConfirm,
			Note: "An array. apiKey may be a plaintext key (use ${ENV_NAME}); a baseUrl that is not Observer bypasses the proxy.",
		},
		{
			ID: "droid/modelFallbacks", Tool: "droid", FileID: droidSettings, Path: KeyPath{"modelFallbacks"}, Type: TypeStringMap,
			Desc: "Fallback model per model id when one is unavailable.", Source: droidRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "droid/compactionTokenLimit", Tool: "droid", FileID: droidSettings, Path: KeyPath{"compactionTokenLimit"}, Type: TypeInt,
			Desc: "Token count that triggers auto-compaction.", Source: droidRef, Verified: VerifiedDoc, Owner: OwnerUser,
			Note: "An absolute token count, not a percent.",
		},
		{
			ID: "droid/compactionTokenLimitPerModel", Tool: "droid", FileID: droidSettings, Path: KeyPath{"compactionTokenLimitPerModel"}, Type: TypeObject,
			Desc: "Per-model compaction thresholds (model id -> tokens).", Source: droidRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "droid/compactionModel", Tool: "droid", FileID: droidSettings, Path: KeyPath{"compactionModel"}, Type: TypeString, Default: `"same"`,
			Desc: "Model that compacts the conversation (same = the session model).", Source: droidRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "droid/subagentModelSettings", Tool: "droid", FileID: droidSettings, Path: KeyPath{"subagentModelSettings"}, Type: TypeObject,
			Desc:   "Subagent models and efforts by complexity: lightModel, mediumModel, heavyModel (+ *ReasoningEffort); inherit by default.",
			Source: droidRef, Verified: VerifiedDoc, Owner: OwnerUser, Note: "Lockable by the org. lightModel is the closest analogue of a small model.",
		},
		{
			ID: "droid/missionModelSettings", Tool: "droid", FileID: droidSettings, Path: KeyPath{"missionModelSettings"}, Type: TypeObject,
			Desc: "Mission worker and validator models and efforts, skipScrutiny, skipUserTesting.", Source: droidRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "droid/missionOrchestratorModel", Tool: "droid", FileID: droidSettings, Path: KeyPath{"missionOrchestratorModel"}, Type: TypeString,
			Desc: "Mission orchestrator model.", Source: droidRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		// --- session defaults and autonomy
		{
			ID: "droid/sessionDefaultSettings.autonomyLevel", Tool: "droid", FileID: droidSettings, Path: KeyPath{"sessionDefaultSettings", "autonomyLevel"},
			Type: TypeEnum, Enum: []string{"off", "low", "medium", "high"}, Default: `"off"`,
			Desc:   "Default autonomy: off runs reads only, low adds edits, medium adds reversible commands, high runs nearly everything.",
			Source: droidAutoRef, Verified: VerifiedDoc, Owner: OwnerUser, Danger: DangerConfirm,
			Note: "high (and --auto high / --skip-permissions-unsafe) runs commands without asking. Capped by the org's maxAutonomyLevel.",
		},
		{
			ID: "droid/sessionDefaultSettings.interactionMode", Tool: "droid", FileID: droidSettings, Path: KeyPath{"sessionDefaultSettings", "interactionMode"},
			Type: TypeEnum, Enum: []string{"auto", "spec"}, Default: `"auto"`, Desc: "Interaction mode of a new session.", Source: droidRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "droid/sessionDefaultSettings.specModeModel", Tool: "droid", FileID: droidSettings, Path: KeyPath{"sessionDefaultSettings", "specModeModel"},
			Type: TypeString, Desc: "Model used in Spec Mode.", Source: droidRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "droid/sessionDefaultSettings.specModeReasoningEffort", Tool: "droid", FileID: droidSettings, Path: KeyPath{"sessionDefaultSettings", "specModeReasoningEffort"},
			Type: TypeString, Desc: "Reasoning effort in Spec Mode.", Source: droidRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "droid/subagentAutonomyLevel", Tool: "droid", FileID: droidSettings, Path: KeyPath{"subagentAutonomyLevel"}, Type: TypeEnum,
			Enum: []string{"inherit", "off", "low", "medium", "high"}, Default: `"inherit"`, Desc: "Autonomy ceiling for Task-tool subagents.",
			Source: droidRef, Verified: VerifiedDoc, Owner: OwnerUser, Danger: DangerConfirm, Note: "high lets subagents run commands unasked.",
		},
		{
			ID: "droid/permissionRules", Tool: "droid", FileID: droidSettings, Path: KeyPath{"permissionRules"}, Type: TypeObject,
			Desc:   "Versioned shell-command policy (version 1; rules with match prefixes and decision allow | ask | block).",
			Source: droidRef, Verified: VerifiedDoc, Owner: OwnerUser, Danger: DangerConfirm,
			Note: "A broad allow rule runs commands unasked; block > deny > allow. droid rules check validates it; lockable by the org.",
		},
		{
			ID: "droid/commandAllowlist", Tool: "droid", FileID: droidSettings, Path: KeyPath{"commandAllowlist"}, Type: TypeStringList,
			Desc: "Pre-approved commands.", Source: droidRef, Verified: VerifiedDoc, Owner: OwnerUser, Danger: DangerConfirm, Deprecated: true,
			Note: "Deprecated by permissionRules; a wildcard entry approves everything.",
		},
		{
			ID: "droid/commandDenylist", Tool: "droid", FileID: droidSettings, Path: KeyPath{"commandDenylist"}, Type: TypeStringList,
			Desc: "Commands that always need approval.", Source: droidRef, Verified: VerifiedDoc, Owner: OwnerUser, Danger: DangerConfirm, Deprecated: true,
			Note: "Deprecated by permissionRules; emptying it removes the default guard list.",
		},
		{
			ID: "droid/commandBlocklist", Tool: "droid", FileID: droidSettings, Path: KeyPath{"commandBlocklist"}, Type: TypeStringList, Default: "[]",
			Desc: "Commands that never run.", Source: droidRef, Verified: VerifiedDoc, Owner: OwnerUser, Danger: DangerConfirm, Deprecated: true,
			Note: "Deprecated by permissionRules.",
		},
		{
			ID: "droid/trustedFolders", Tool: "droid", FileID: droidSettings, Path: KeyPath{"trustedFolders"}, Type: TypeStringList,
			Desc: "Folders the user has trusted.", Source: "docs/audits/adapter-config-surface-2026-10-03.md#droid (node 1 live; repo docs/droid-adapter.md)", Verified: VerifiedLive,
			Owner: OwnerToolState, Danger: DangerConfirm,
			Note: droidLiveNote + " Written by droid's trust prompt; not on the settings reference page, value type inferred.",
		},
		{
			ID: "droid/enableDroidShield", Tool: "droid", FileID: droidSettings, Path: KeyPath{"enableDroidShield"}, Type: TypeBool, Default: "true",
			Desc: "Secret scanning and git guardrails.", Source: droidRef, Verified: VerifiedDoc, Owner: OwnerUser, Danger: DangerConfirm,
			Note: "false switches secret scanning off.",
		},
		{
			ID: "droid/hooksDisabled", Tool: "droid", FileID: droidSettings, Path: KeyPath{"hooksDisabled"}, Type: TypeBool, Default: "false",
			Desc: "Switch every hook off.", Source: droidRef, Verified: VerifiedDoc, Owner: OwnerUser, Danger: DangerRefuseWhileIntegrated,
			Note: "true silences Observer's UserPromptSubmit hook.",
		},
		{
			ID: "droid/showHookOutput", Tool: "droid", FileID: droidSettings, Path: KeyPath{"showHookOutput"}, Type: TypeBool, Default: "false",
			Desc: "Show hook stdout / stderr in the transcript.", Source: droidRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "droid/disabledSkills", Tool: "droid", FileID: droidSettings, Path: KeyPath{"disabledSkills"}, Type: TypeStringList, Default: "[]",
			Desc: "Skills disabled by name.", Source: droidRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "droid/blockOnMcpLoad", Tool: "droid", FileID: droidSettings, Path: KeyPath{"blockOnMcpLoad"}, Type: TypeBool, Default: "false",
			Desc: "Wait for MCP servers before the first agent turn.", Source: droidRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "droid/cloudSessionSync", Tool: "droid", FileID: droidSettings, Path: KeyPath{"cloudSessionSync"}, Type: TypeBool, Default: "true",
			Desc: "Mirror CLI sessions to the Factory web app.", Source: droidRef, Verified: VerifiedDoc, Owner: OwnerUser,
			Note: "Not a telemetry switch; no single telemetry opt-out key is documented.",
		},
		{
			ID: "droid/includeCoAuthoredByDroid", Tool: "droid", FileID: droidSettings, Path: KeyPath{"includeCoAuthoredByDroid"}, Type: TypeBool, Default: "true",
			Desc: "Append a Droid co-author trailer to commits.", Source: droidRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "droid/ideAutoConnect", Tool: "droid", FileID: droidSettings, Path: KeyPath{"ideAutoConnect"}, Type: TypeBool, Default: "false",
			Desc: "Connect to the IDE from an external terminal.", Source: droidRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "droid/remoteAccessEnabled", Tool: "droid", FileID: droidSettings, Path: KeyPath{"remoteAccessEnabled"}, Type: TypeBool, Default: "false",
			Desc: "Allow remote Droid Computer access.", Source: droidRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "droid/llmRequestTimeout", Tool: "droid", FileID: droidSettings, Path: KeyPath{"llmRequestTimeout"}, Type: TypeInt,
			Desc: "Model request timeout in milliseconds.", Source: droidRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "droid/specSaveDir", Tool: "droid", FileID: droidSettings, Path: KeyPath{"specSaveDir"}, Type: TypeString, Default: `"~/.factory/specs"`,
			Desc: "Where specs are saved.", Source: droidRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "droid/worktreeDirectory", Tool: "droid", FileID: droidSettings, Path: KeyPath{"worktreeDirectory"}, Type: TypeString, Default: `"~/.factory/worktrees"`,
			Desc: "Parent directory of git worktrees.", Source: droidRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "droid/statusLine", Tool: "droid", FileID: droidSettings, Path: KeyPath{"statusLine"}, Type: TypeObject,
			Desc: "Custom status line: command, padding, maxRows.", Source: droidRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		// --- display
		{
			ID: "droid/theme", Tool: "droid", FileID: droidSettings, Path: KeyPath{"theme"}, Type: TypeString,
			Desc: "TUI colour theme id (system theme by default).", Source: droidRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "droid/logoAnimation", Tool: "droid", FileID: droidSettings, Path: KeyPath{"logoAnimation"}, Type: TypeEnum,
			Enum: []string{"once", "always", "off"}, Default: `"once"`, Desc: "Startup logo animation.", Source: droidRef, Verified: VerifiedLive,
			Owner: OwnerUser, Note: droidLiveNote,
		},
		{
			ID: "droid/outputStyle", Tool: "droid", FileID: droidSettings, Path: KeyPath{"outputStyle"}, Type: TypeString,
			Enum: []string{"default", "concise"}, Default: `"default"`, Desc: "Response formatting style (or a custom style name).",
			Source: droidRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "droid/diffMode", Tool: "droid", FileID: droidSettings, Path: KeyPath{"diffMode"}, Type: TypeEnum,
			Enum: []string{"github", "unified"}, Default: `"github"`, Desc: "Diff display format.", Source: droidRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "droid/toolResultDisplay", Tool: "droid", FileID: droidSettings, Path: KeyPath{"toolResultDisplay"}, Type: TypeEnum,
			Enum: []string{"expanded", "compact"}, Default: `"expanded"`, Desc: "Tool result rendering.", Source: droidRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "droid/showThinkingInMainView", Tool: "droid", FileID: droidSettings, Path: KeyPath{"showThinkingInMainView"}, Type: TypeBool, Default: "false",
			Desc: "Show reasoning blocks in the chat.", Source: droidRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "droid/streamAssistantResponses", Tool: "droid", FileID: droidSettings, Path: KeyPath{"streamAssistantResponses"}, Type: TypeBool, Default: "false",
			Desc: "Stream response text progressively.", Source: droidRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "droid/showTokenUsageIndicator", Tool: "droid", FileID: droidSettings, Path: KeyPath{"showTokenUsageIndicator"}, Type: TypeBool, Default: "false",
			Desc: "Show live token usage.", Source: droidRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "droid/nerdFont", Tool: "droid", FileID: droidSettings, Path: KeyPath{"nerdFont"}, Type: TypeBool, Default: "false",
			Desc: "Use Nerd Font glyphs.", Source: droidRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "droid/completionSound", Tool: "droid", FileID: droidSettings, Path: KeyPath{"completionSound"}, Type: TypeString,
			Enum: []string{"off", "bell", "fx-ok01", "fx-ack01"}, Default: `"fx-ok01"`, Desc: "Sound when a response completes (or a file path).",
			Source: droidRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		// --- org / enterprise controls (managed layer)
		{
			ID: "droid/maxAutonomyLevel", Tool: "droid", FileID: droidSettings, Path: KeyPath{"maxAutonomyLevel"}, Scopes: droidManagedOnly, Type: TypeEnum,
			Enum: []string{"off", "low", "medium", "high"}, Default: `"high"`, Desc: "Org ceiling on session autonomy.", Source: droidOrgRef,
			Verified: VerifiedDoc, Owner: OwnerUser, Note: droidOrgOnly,
		},
		{
			ID: "droid/modelPolicy", Tool: "droid", FileID: droidSettings, Path: KeyPath{"modelPolicy"}, Scopes: droidManagedOnly, Type: TypeObject,
			Desc: "Org model allow / block lists: allowedModelIds, blockedModelIds, allowCustomModels, allowedBaseUrls.", Source: droidOrgRef,
			Verified: VerifiedDoc, Owner: OwnerUser, Note: droidOrgOnly,
		},
		{
			ID: "droid/mcpPolicy", Tool: "droid", FileID: droidSettings, Path: KeyPath{"mcpPolicy"}, Scopes: droidManagedOnly, Type: TypeObject,
			Desc: "Org MCP controls: enabled, allowlist.", Source: droidOrgRef, Verified: VerifiedDoc, Owner: OwnerUser,
			Danger: DangerRefuseWhileIntegrated, Note: droidOrgOnly + " Can switch off or exclude Observer's MCP server.",
		},
		{
			ID: "droid/mcpAutonomyOverrides", Tool: "droid", FileID: droidSettings, Path: KeyPath{"mcpAutonomyOverrides"}, Scopes: droidManagedOnly, Type: TypeObject,
			Desc: "Org autonomy defaults per MCP server / tool.", Source: droidOrgRef, Verified: VerifiedDoc, Owner: OwnerUser, Note: droidOrgOnly,
		},
		{
			ID: "droid/mcpAutonomyUrlOverrides", Tool: "droid", FileID: droidSettings, Path: KeyPath{"mcpAutonomyUrlOverrides"}, Scopes: droidManagedOnly, Type: TypeObject,
			Desc: "Org URL-pattern autonomy rules for MCP servers.", Source: droidOrgRef, Verified: VerifiedDoc, Owner: OwnerUser, Note: droidOrgOnly,
		},
		{
			ID: "droid/sandbox", Tool: "droid", FileID: droidSettings, Path: KeyPath{"sandbox"}, Scopes: droidManagedOnly, Type: TypeObject,
			Desc: "Filesystem and network isolation: enabled, mode, filesystem, network.", Source: droidRef, Verified: VerifiedDoc, Owner: OwnerUser,
			Danger: DangerConfirm, Note: droidOrgOnly + " The inner shape is not fully documented (audit V8); enabled false runs commands unsandboxed.",
		},
		{
			ID: "droid/networkPolicy", Tool: "droid", FileID: droidSettings, Path: KeyPath{"networkPolicy"}, Scopes: droidManagedOnly, Type: TypeObject,
			Desc: "Org outbound IP / CIDR allowlist (allowedIps).", Source: droidOrgRef, Verified: VerifiedDoc, Owner: OwnerUser, Note: droidOrgOnly,
		},
		{
			ID: "droid/missionPolicy", Tool: "droid", FileID: droidSettings, Path: KeyPath{"missionPolicy"}, Scopes: droidManagedOnly, Type: TypeObject,
			Desc: "Org Mission access control: restrictedAccess, allowedUserIds.", Source: droidOrgRef, Verified: VerifiedDoc, Owner: OwnerUser, Note: droidOrgOnly,
		},
		{
			ID: "droid/allowManagedHooksOnly", Tool: "droid", FileID: droidSettings, Path: KeyPath{"allowManagedHooksOnly"}, Scopes: droidManagedOnly, Type: TypeBool,
			Default: "false", Desc: "Run only org-managed hooks.", Source: droidOrgRef, Verified: VerifiedDoc, Owner: OwnerUser,
			Danger: DangerRefuseWhileIntegrated, Note: droidOrgOnly + " true makes Observer's user-level hook silently inert.",
		},
		{
			ID: "droid/disableAutoUpdate", Tool: "droid", FileID: droidSettings, Path: KeyPath{"disableAutoUpdate"}, Scopes: droidManagedOnly, Type: TypeBool,
			Default: "false", Desc: "Turn droid's auto-update off.", Source: droidOrgRef, Verified: VerifiedDoc, Owner: OwnerUser, Note: droidOrgOnly,
		},
		{
			ID: "droid/webSearchDisabled", Tool: "droid", FileID: droidSettings, Path: KeyPath{"webSearchDisabled"}, Scopes: droidManagedOnly, Type: TypeBool,
			Default: "false", Desc: "Disable the built-in web search.", Source: droidOrgRef, Verified: VerifiedDoc, Owner: OwnerUser, Note: droidOrgOnly,
		},
		{
			ID: "droid/sessionRetentionDays", Tool: "droid", FileID: droidSettings, Path: KeyPath{"sessionRetentionDays"}, Scopes: droidManagedOnly, Type: TypeInt,
			Min: f64(14), Max: f64(365), Desc: "Org session history retention in days.", Source: droidOrgRef, Verified: VerifiedDoc, Owner: OwnerUser, Note: droidOrgOnly,
		},
		{
			ID: "droid/strictEnabledPlugins", Tool: "droid", FileID: droidSettings, Path: KeyPath{"strictEnabledPlugins"}, Scopes: droidManagedOnly, Type: TypeObject,
			Desc: "Org-locked plugin enablement.", Source: droidOrgRef, Verified: VerifiedDoc, Owner: OwnerUser, Note: droidOrgOnly + " Shape not stated.",
		},
		{
			ID: "droid/strictKnownMarketplaces", Tool: "droid", FileID: droidSettings, Path: KeyPath{"strictKnownMarketplaces"}, Scopes: droidManagedOnly, Type: TypeObject,
			Desc: "Org-locked plugin marketplaces.", Source: droidOrgRef, Verified: VerifiedDoc, Owner: OwnerUser, Note: droidOrgOnly + " Shape not stated.",
		},
		{
			ID: "droid/imageGenerationEnabled", Tool: "droid", FileID: droidSettings, Path: KeyPath{"imageGenerationEnabled"}, Scopes: droidManagedOnly, Type: TypeBool,
			Desc: "Org switch for image generation.", Source: droidOrgRef, Verified: VerifiedDoc, Owner: OwnerUser, Note: droidOrgOnly,
		},
		{
			ID: "droid/imageGenerationAllowedModelIds", Tool: "droid", FileID: droidSettings, Path: KeyPath{"imageGenerationAllowedModelIds"}, Scopes: droidManagedOnly,
			Type: TypeStringList, Desc: "Models allowed for image generation.", Source: droidOrgRef, Verified: VerifiedDoc, Owner: OwnerUser, Note: droidOrgOnly,
		},
		{
			ID: "droid/factoryRouterRules", Tool: "droid", FileID: droidSettings, Path: KeyPath{"factoryRouterRules"}, Scopes: droidManagedOnly, Type: TypeObject,
			Desc: "Org conditional rules for the Factory model router.", Source: droidRef, Verified: VerifiedDoc, Owner: OwnerUser, Note: droidOrgOnly,
		},
		{
			ID: "droid/factoryRouterGuidance", Tool: "droid", FileID: droidSettings, Path: KeyPath{"factoryRouterGuidance"}, Scopes: droidManagedOnly, Type: TypeString,
			Desc: "Org guidance text for the Factory model router.", Source: droidRef, Verified: VerifiedDoc, Owner: OwnerUser, Note: droidOrgOnly,
		},
		{
			ID: "droid/apiKeyHelper", Tool: "droid", FileID: droidSettings, Path: KeyPath{"apiKeyHelper"}, Scopes: droidManagedOnly, Type: TypeString,
			Desc: "Command that prints the API key for custom models.", Source: droidBYOKRef, Verified: VerifiedDoc, Owner: OwnerUser,
			Danger: DangerConfirm, Note: droidOrgOnly + " Runs an executable; it prints a key, it is not one.",
		},
		// --- hooks.json: the event map sits at the ROOT of the file (only
		// settings.json nests it under "hooks"). Live-grounded 2026-10-04 on
		// droid 0.233.0: a {"hooks":{...}} wrapper in hooks.json is ignored
		// ("Ignoring unknown hook event keys", keys ["hooks"]).
		{
			ID: "droid/PreToolUse", Tool: "droid", FileID: droidHooks, Path: KeyPath{"PreToolUse"}, Type: TypeObject,
			Desc: "Matcher groups run before a tool call.", Source: droidHooksRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "droid/PostToolUse", Tool: "droid", FileID: droidHooks, Path: KeyPath{"PostToolUse"}, Type: TypeObject,
			Desc: "Matcher groups run after a tool call.", Source: droidHooksRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "droid/UserPromptSubmit", Tool: "droid", FileID: droidHooks, Path: KeyPath{"UserPromptSubmit"}, Type: TypeObject,
			Desc: "Matcher groups run when the user submits a prompt.", Source: droidHooksRef, Verified: VerifiedDoc, Owner: OwnerUser,
			Note: "Holds Observer's prompt-submit entry.",
		},
		{
			ID: "droid/Notification", Tool: "droid", FileID: droidHooks, Path: KeyPath{"Notification"}, Type: TypeObject,
			Desc: "Matcher groups run on a notification.", Source: droidHooksRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "droid/Stop", Tool: "droid", FileID: droidHooks, Path: KeyPath{"Stop"}, Type: TypeObject,
			Desc: "Matcher groups run when the agent stops.", Source: droidHooksRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "droid/SubagentStop", Tool: "droid", FileID: droidHooks, Path: KeyPath{"SubagentStop"}, Type: TypeObject,
			Desc: "Matcher groups run when a subagent stops.", Source: droidHooksRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "droid/PreCompact", Tool: "droid", FileID: droidHooks, Path: KeyPath{"PreCompact"}, Type: TypeObject,
			Desc: "Matcher groups run before compaction.", Source: droidHooksRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "droid/SessionStart", Tool: "droid", FileID: droidHooks, Path: KeyPath{"SessionStart"}, Type: TypeObject,
			Desc: "Matcher groups run at session start.", Source: droidHooksRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "droid/SessionEnd", Tool: "droid", FileID: droidHooks, Path: KeyPath{"SessionEnd"}, Type: TypeObject,
			Desc: "Matcher groups run at session end.", Source: droidHooksRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		// --- mcp.json
		{
			ID: "droid/mcpServers", Tool: "droid", FileID: droidMCP, Path: KeyPath{"mcpServers"}, Type: TypeMCPMap,
			Desc: "MCP servers: type, command, args, env or url, headers, plus disabled, disabledTools, timeout.", Source: droidMCPRef,
			Verified: VerifiedDoc, Owner: OwnerUser,
			Note: "disabled: true on Observer's entry drops its MCP server; Observer's mcpServers.observer entry is an owned span.",
		},
		// --- org-managed-settings.cache.json (server cache, read-only)
		{
			ID: "droid/managedSettings", Tool: "droid", FileID: droidOrgCache, Path: KeyPath{"managedSettings"}, Type: TypeObject,
			Desc: "The cached org managed settings.", Source: droidOrgRef, Verified: VerifiedLive, Owner: OwnerServerCache, Note: droidCacheNote,
		},
		{
			ID: "droid/managedSettings.modelPolicy.allowAllFactoryModels", Tool: "droid", FileID: droidOrgCache,
			Path: KeyPath{"managedSettings", "modelPolicy", "allowAllFactoryModels"}, Type: TypeBool,
			Desc: "Cached org policy: every Factory model allowed.", Source: droidOrgRef, Verified: VerifiedLive, Owner: OwnerServerCache, Note: droidCacheNote,
		},
		{
			ID: "droid/managedSettings.modelPolicy.allowCustomModels", Tool: "droid", FileID: droidOrgCache,
			Path: KeyPath{"managedSettings", "modelPolicy", "allowCustomModels"}, Type: TypeBool,
			Desc: "Cached org policy: BYOK custom models allowed.", Source: droidOrgRef, Verified: VerifiedLive, Owner: OwnerServerCache, Note: droidCacheNote,
		},
		{
			ID: "droid/managedSettings.modelPolicy.isFastModelsAllowed", Tool: "droid", FileID: droidOrgCache,
			Path: KeyPath{"managedSettings", "modelPolicy", "isFastModelsAllowed"}, Type: TypeBool,
			Desc: "Cached org policy: fast models allowed.", Source: droidOrgRef, Verified: VerifiedLive, Owner: OwnerServerCache, Note: droidCacheNote,
		},
		{
			ID: "droid/orgId", Tool: "droid", FileID: droidOrgCache, Path: KeyPath{"orgId"}, Type: TypeString,
			Desc: "Org the cached policy belongs to.", Source: droidOrgRef, Verified: VerifiedLive, Owner: OwnerServerCache, Note: droidCacheNote,
		},
		{
			ID: "droid/revision", Tool: "droid", FileID: droidOrgCache, Path: KeyPath{"revision"}, Type: TypeInt,
			Desc: "Revision of the cached org policy.", Source: droidOrgRef, Verified: VerifiedLive, Owner: OwnerServerCache, Note: droidCacheNote,
		},
		{
			ID: "droid/cachedAt", Tool: "droid", FileID: droidOrgCache, Path: KeyPath{"cachedAt"}, Type: TypeString,
			Desc: "When the org policy was cached.", Source: droidOrgRef, Verified: VerifiedLive, Owner: OwnerServerCache, Note: droidCacheNote,
		},
	},
	Map: []MapRow{
		{Key: KeyModel, Tool: "droid", Settings: []string{"droid/model"}, Transform: TransformModelAlias, Source: droidRef},
		{
			Key: KeyReasoningEffort, Tool: "droid", Settings: []string{"droid/reasoningEffort"}, Transform: TransformEnumMap,
			Params: TransformParams{Enum: []EnumPair{
				{"none", []any{"none"}},
				{"minimal", []any{"minimal"}},
				{"low", []any{"low"}},
				{"medium", []any{"medium"}},
				{"high", []any{"high"}},
				{"xhigh", []any{"xhigh"}},
				{"max", []any{"max"}},
			}}, Source: droidRef, Note: "dynamic and off have no canonical form.",
		},
		{
			Key: KeyApprovalMode, Tool: "droid", Settings: []string{"droid/sessionDefaultSettings.autonomyLevel"}, Transform: TransformEnumMap,
			Params: TransformParams{Enum: []EnumPair{{"ask_edits", []any{"off"}}, {"auto_edits", []any{"low"}}, {"full_auto", []any{"high"}}}},
			Source: droidAutoRef,
			Note:   "off runs reads only, low adds edits, high runs commands; medium (reversible commands) has no canonical form; plan and ask are refused.",
		},
		{
			Key: KeyMCPServers, Tool: "droid", Settings: []string{"droid/mcpServers"}, Transform: TransformMCPShape,
			Params: TransformParams{MCP: droidMCPShape}, Lossy: true, Source: droidMCPRef, Note: mcpLossyNote,
		},
	},
}
