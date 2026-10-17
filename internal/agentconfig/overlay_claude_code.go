package agentconfig

// Claude Code overlay. Sources: code.claude.com/docs/en/{settings,
// settings-reference,permissions,managed-settings,env-vars} (fetched
// 2026-10-03; audit section 5 "claude-code").
const (
	ccSettings  = "https://code.claude.com/docs/en/settings"
	ccRef       = "https://code.claude.com/docs/en/settings-reference"
	ccPerms     = "https://code.claude.com/docs/en/permissions"
	ccManaged   = "https://code.claude.com/docs/en/managed-settings"
	ccEnv       = "https://code.claude.com/docs/en/env-vars"
	managedOnly = "Honoured only from the managed settings layer."
)

var managedScope = []Scope{ScopeManaged}

var claudeCodeCatalog = toolCatalog{
	Tool:            "claude-code",
	Map:             claudeCodeMap,
	VerifiedAgainst: "schemastore claude-code-settings.json fetched 2026-10-03 (the schema carries no tool version; the vendor notes it can lag new keys)",
	Overlays: []Overlay{
		{
			ID: "claude-code/permissions.defaultMode", Danger: DangerConfirm, Source: ccPerms,
			Note: "bypassPermissions / auto loosen approvals; both are ignored from project and local files (v2.1.257+).",
		},
		{ID: "claude-code/skipDangerousModePermissionPrompt", Danger: DangerConfirm, Source: ccRef},
		{
			ID: "claude-code/permissions.allow", Type: TypeRuleList, Danger: DangerConfirm, Source: ccPerms,
			Note: "Rules Tool(specifier); a broad rule such as Bash(*) needs a confirmation.",
		},
		{ID: "claude-code/permissions.ask", Type: TypeRuleList, Source: ccPerms},
		{ID: "claude-code/permissions.deny", Type: TypeRuleList, Source: ccPerms},
		{ID: "claude-code/sandbox.enabled", Danger: DangerConfirm, Source: ccRef, Note: "false runs commands unsandboxed."},
		{ID: "claude-code/sandbox.allowUnsandboxedCommands", Danger: DangerConfirm, Source: ccRef},
		{ID: "claude-code/sandbox.autoAllowBashIfSandboxed", Danger: DangerConfirm, Source: ccRef},
		{ID: "claude-code/enableAllProjectMcpServers", Danger: DangerConfirm, Source: ccRef},
		{
			ID: "claude-code/disableAllHooks", Danger: DangerRefuseWhileIntegrated, Source: ccRef,
			Note: "Also silences hooks for every tool that reads Claude settings files (Copilot CLI, VS Code Copilot, Devin CLI, Grok, DeepSeek Harness).",
		},
		{ID: "claude-code/allowManagedHooksOnly", Danger: DangerRefuseWhileIntegrated, Scopes: managedScope, Source: ccManaged, Note: managedOnly},
		{ID: "claude-code/deniedMcpServers", Danger: DangerRefuseWhileIntegrated, Source: ccRef, Note: "Can drop Observer's MCP server."},
		{ID: "claude-code/disabledMcpjsonServers", Danger: DangerRefuseWhileIntegrated, Source: ccRef, Note: "Can drop Observer's MCP server."},
		{ID: "claude-code/allowManagedMcpServersOnly", Danger: DangerRefuseWhileIntegrated, Scopes: managedScope, Source: ccManaged, Note: managedOnly},
		{ID: "claude-code/allowManagedPermissionRulesOnly", Scopes: managedScope, Source: ccManaged, Note: managedOnly},
		{ID: "claude-code/managedMcpServers", Type: TypeMCPMap, Scopes: managedScope, Source: ccManaged, Note: managedOnly},
		{ID: "claude-code/strictPluginOnlyCustomization", Scopes: managedScope, Source: ccManaged, Note: managedOnly},
		{ID: "claude-code/disableSideloadFlags", Scopes: managedScope, Source: ccManaged, Note: managedOnly},
		{ID: "claude-code/forceRemoteSettingsRefresh", Scopes: managedScope, Source: ccManaged, Note: managedOnly},
		{
			ID: "claude-code/policyHelper", Prefix: true, Danger: DangerConfirm, Scopes: managedScope, Source: ccManaged,
			Note: "Runs an executable that computes managed settings. " + managedOnly,
		},
		{ID: "claude-code/sandbox.network.allowManagedDomainsOnly", Scopes: managedScope, Source: ccManaged, Note: managedOnly},
		{ID: "claude-code/sandbox.filesystem.allowManagedReadPathsOnly", Scopes: managedScope, Source: ccManaged, Note: managedOnly},
		{ID: "claude-code/strictKnownMarketplaces", Scopes: managedScope, Source: ccManaged, Note: managedOnly},
		{ID: "claude-code/blockedMarketplaces", Scopes: managedScope, Source: ccManaged, Note: managedOnly},
		{ID: "claude-code/channelsEnabled", Scopes: managedScope, Source: ccManaged, Note: managedOnly},
		{ID: "claude-code/parentSettingsBehavior", Scopes: managedScope, Source: ccManaged, Note: managedOnly},
		{ID: "claude-code/wslInheritsWindowsSettings", Scopes: managedScope, Source: ccManaged, Note: managedOnly},
		{ID: "claude-code/apiKeyHelper", Danger: DangerConfirm, Source: ccRef, Note: "Runs an arbitrary command; the preview shows it."},
		{ID: "claude-code/otelHeadersHelper", Danger: DangerConfirm, Source: ccRef, Note: "Runs an arbitrary command."},
		{ID: "claude-code/awsAuthRefresh", Danger: DangerConfirm, Source: ccRef, Note: "Runs an arbitrary command."},
		{ID: "claude-code/awsCredentialExport", Danger: DangerConfirm, Source: ccRef, Note: "Runs an arbitrary command."},
		{ID: "claude-code/gcpAuthRefresh", Danger: DangerConfirm, Source: ccRef, Note: "Runs an arbitrary command."},
		{ID: "claude-code/statusLine.command", Danger: DangerConfirm, Source: ccRef, Note: "Runs an arbitrary command."},
		{ID: "claude-code/subagentStatusLine.command", Danger: DangerConfirm, Source: ccRef, Note: "Runs an arbitrary command."},
		{ID: "claude-code/fileSuggestion.command", Danger: DangerConfirm, Source: ccRef, Note: "Runs an arbitrary command."},
		{
			ID: "claude-code/hooks", Prefix: true, Source: ccSettings,
			Note: "Observer's hook registration owns its own entries here (stamped as an Observer-owned span in phase P3).",
		},
		{
			ID: "claude-code/env.ANTHROPIC_BASE_URL", Source: ccEnv,
			Note: "Observer's proxy route owns this key while the route is on. A non-Anthropic URL also switches off the claude.ai server-managed settings source (D-114).",
		},
		{ID: "claude-code/env.AWS_BEARER_TOKEN_BEDROCK", Secret: boolp(true), Source: ccEnv},
		{ID: "claude-code/env.CLAUDE_CODE_CLIENT_KEY_PASSPHRASE", Secret: boolp(true), Source: ccEnv},
		{
			ID: "claude-code/theme", Verified: VerifiedLive, Source: "node 1 live probe 2026-10-03 (audit section 6)",
			Note: "Lives in settings.json (confirmed on node 1).",
		},
	},
	Extra: []SettingRow{
		{
			ID: "claude-code/autoCompactWindow", Tool: "claude-code", FileID: "claude-code/settings", Path: KeyPath{"autoCompactWindow"},
			Type: TypeInt, Desc: "Auto-compact window in tokens.", Source: ccRef, Verified: VerifiedDoc, Owner: OwnerUser,
			Note: "Documented in the settings reference; not yet in the published schema.",
		},
		{
			ID: "claude-code/maxEffortLevel", Tool: "claude-code", FileID: "claude-code/settings", Path: KeyPath{"maxEffortLevel"},
			Type: TypeEnum, Enum: []string{"low", "medium", "high", "xhigh", "max"}, Desc: "Highest effort level selectable (adds max).",
			Source: ccRef, Verified: VerifiedDoc, Owner: OwnerUser,
			Note: "Documented in the settings reference; not yet in the published schema.",
		},
		{
			ID: "claude-code/managedSourcesBehavior", Tool: "claude-code", FileID: "claude-code/settings", Path: KeyPath{"managedSourcesBehavior"},
			Type: TypeString, Enum: []string{"merge"}, Desc: "How managed sources combine: by default only the highest source carrying a policy key applies; \"merge\" composes all.",
			Scopes: managedScope, Source: ccManaged, Verified: VerifiedDoc, Owner: OwnerUser, Note: managedOnly,
		},
		{
			ID: "claude-code/env.CLAUDE_CODE_AUTOCOMPACT_PCT_OVERRIDE", Tool: "claude-code", FileID: "claude-code/settings",
			Path: KeyPath{"env", "CLAUDE_CODE_AUTOCOMPACT_PCT_OVERRIDE"}, Type: TypeString,
			Desc: "Auto-compact threshold as a percent of the window (1-100).", Source: ccEnv, Verified: VerifiedDoc, Owner: OwnerUser,
			Note: "An env-backed key the schema omits; a percent, unlike CLAUDE_CODE_AUTO_COMPACT_WINDOW (tokens).",
		},
		{
			ID: "claude-code/env", Tool: "claude-code", FileID: "claude-code/settings", Path: KeyPath{"env"}, Type: TypeStringMap,
			Desc:   "Environment variables applied to every session. The known variables are their own rows (env.<NAME>); this row is the whole map.",
			Source: ccRef, Verified: VerifiedDoc, Owner: OwnerUser,
			Note: "The schema describes env as an object of named properties, so the generator emits one row per variable and none for the map itself.",
		},
		{
			ID: "claude-code/env.CLAUDE_CODE_GATEWAY_HINT_HEADERS", Tool: "claude-code", FileID: "claude-code/settings",
			Path: KeyPath{"env", "CLAUDE_CODE_GATEWAY_HINT_HEADERS"}, Type: TypeEnum, Enum: []string{"0", "1"},
			Desc:   "Set to 1 to send the gateway hint headers (request class, agent type, prompt id) to a custom base URL; off by default there.",
			Source: ccEnv, Verified: VerifiedDoc, Owner: OwnerUser,
			Note: "An env-backed key the schema omits (also https://code.claude.com/docs/en/llm-gateway-protocol#gateway-hint-headers).",
		},
	},
}
