package agentconfig

// OpenClaw doc-authored catalogue. Sources: docs.openclaw.ai gateway
// configuration pages (configuration, configuration-reference, config-agents,
// config-agents/models, config-agents/sandbox, config-runtime,
// config-extensions, config-tools/tool-policy, config-observability,
// config-secrets-env, config-browser-ui-desktop, telemetry) fetched
// 2026-10-04, plus audit section 5 "openclaw". OpenClaw is not installed on
// node 1: doc-only. The live schema is generated locally
// (`openclaw config schema`); no public URL.
const (
	openclawRef        = "https://docs.openclaw.ai/gateway/configuration-reference.md"
	openclawAgentsRef  = "https://docs.openclaw.ai/gateway/config-agents.md"
	openclawModelsRef  = "https://docs.openclaw.ai/gateway/config-agents/models.md"
	openclawSandboxRef = "https://docs.openclaw.ai/gateway/config-agents/sandbox.md"
	openclawRuntimeRef = "https://docs.openclaw.ai/gateway/config-runtime.md"
	openclawExtRef     = "https://docs.openclaw.ai/gateway/config-extensions.md"
	openclawToolsRef   = "https://docs.openclaw.ai/gateway/config-tools/tool-policy.md"
	openclawObsRef     = "https://docs.openclaw.ai/gateway/config-observability.md"
	openclawEnvRef     = "https://docs.openclaw.ai/gateway/config-secrets-env.md"
	openclawUIRef      = "https://docs.openclaw.ai/gateway/config-browser-ui-desktop.md"
	openclawCompactRef = "https://docs.openclaw.ai/gateway/config-agents/heartbeat-compaction-and-streaming.md"
	openclawShape      = "openclaw/config"
)

// openclawRow fills the tool, shape, owner, verification and id of one row.
func openclawRow(r SettingRow) SettingRow {
	r.Tool = "openclaw"
	if r.FileID == "" {
		r.FileID = openclawShape
	}
	if r.Owner == "" {
		r.Owner = OwnerUser
	}
	if r.Verified == "" {
		r.Verified = VerifiedDoc
	}
	if r.Source == "" {
		r.Source = openclawRef
	}
	r.ID = r.Tool + "/" + r.Path.String()
	return r
}

// openclawDenyGrammar is tools.deny's spelling: bare tool names (bash is an
// alias of exec, group:* names cover sets). MCP tools follow per-server
// names, not a fixed prefix, so mcp rules are refused.
var openclawDenyGrammar = RuleGrammar{
	"shell":  {Name: "exec", Style: RuleBare},
	"read":   {Name: "read", Style: RuleBare},
	"edit":   {Name: "edit", Style: RuleBare},
	"write":  {Name: "write", Style: RuleBare},
	"fetch":  {Name: "web_fetch", Style: RuleBare},
	"search": {Name: "web_search", Style: RuleBare},
}

var openclawCatalog = toolCatalog{
	Tool: "openclaw",
	VerifiedAgainst: "docs.openclaw.ai gateway configuration pages fetched 2026-10-04 (audit section 5 openclaw). Not installed on node 1: no live evidence. " +
		"Exec approvals (the approval mode) live in the state SQLite, not openclaw.json, so they have no rows; mcp.servers is not mapped because an " +
		"unknown key refuses gateway start and the stdio env key is not documented.",
	Files: []FileRow{
		{
			ID: "openclaw/user-config", Tool: "openclaw", Shape: openclawShape, Kind: KindSettings, Scope: ScopeUser,
			Paths: samePath("~/.openclaw/openclaw.json"), Format: FormatJSONC, Precedence: 20, WriteNeeds: PrivUser,
			Grounding: doc("https://docs.openclaw.ai/gateway/configuration.md"),
			Note: "STRICTLY validated: an unknown key, wrong type or invalid value makes the gateway refuse to start (only root $schema is exempt), so every write must be schema-exact " +
				"(`openclaw config schema`). JSON5: unquoted keys or single quotes do not parse with the JSONC engine. Must be a regular file, not a symlink. " +
				"OPENCLAW_CONFIG_PATH / OPENCLAW_STATE_DIR / OPENCLAW_PROFILE relocate it; $include splits it across files.",
		},
	},
	Extra: []SettingRow{
		openclawRow(SettingRow{Path: KeyPath{"$schema"}, Type: TypeString, Desc: "JSON Schema reference (the only key exempt from validation)."}),
		// --- agents
		openclawRow(SettingRow{
			Path: KeyPath{"agents", "defaults", "model"}, Type: TypeString, Source: openclawModelsRef,
			Desc: "Default model: \"provider/model\" or {primary, fallbacks[]}.",
			Note: "Writers save the canonical object form; agents.entries.<id>.model overrides per agent.",
		}),
		openclawRow(SettingRow{
			Path: KeyPath{"agents", "defaults", "utilityModel"}, Type: TypeString, Source: openclawModelsRef,
			Desc: "provider/model for short internal tasks (titles, recaps, narration).",
		}),
		openclawRow(SettingRow{Path: KeyPath{"agents", "defaults", "imageModel"}, Type: TypeString, Source: openclawModelsRef, Desc: "Image generation model."}),
		openclawRow(SettingRow{Path: KeyPath{"agents", "defaults", "pdfModel"}, Type: TypeString, Source: openclawModelsRef, Desc: "PDF model."}),
		openclawRow(SettingRow{
			Path: KeyPath{"agents", "defaults", "thinkingDefault"}, Type: TypeString, Source: openclawModelsRef, Desc: "Default thinking level (e.g. low).",
			Note: "Session /think and per-message choices win over it.",
		}),
		openclawRow(SettingRow{
			Path: KeyPath{"agents", "defaults", "params"}, Type: TypeObject, Source: openclawModelsRef,
			Desc: "Provider parameters for every model (maxTokens, temperature, thinking, ...).",
		}),
		openclawRow(SettingRow{
			Path: KeyPath{"agents", "defaults", "models"}, Type: TypeObject, Source: openclawModelsRef,
			Desc: "Per-model settings keyed \"provider/model\" ({alias, params, ...}).",
		}),
		openclawRow(SettingRow{Path: KeyPath{"agents", "defaults", "timeoutSeconds"}, Type: TypeInt, Source: openclawCompactRef, Desc: "Agent run timeout in seconds."}),
		openclawRow(SettingRow{Path: KeyPath{"agents", "defaults", "workspace"}, Type: TypeString, Source: openclawAgentsRef, Desc: "Agent workspace directory (holds AGENTS.md, SOUL.md, ...)."}),
		openclawRow(SettingRow{Path: KeyPath{"agents", "defaults", "repoRoot"}, Type: TypeString, Source: openclawAgentsRef, Desc: "Repository the agent works in."}),
		openclawRow(SettingRow{Path: KeyPath{"agents", "defaults", "cwd"}, Type: TypeString, Source: openclawAgentsRef, Desc: "Working directory for the agent."}),
		openclawRow(SettingRow{Path: KeyPath{"agents", "defaults", "skipBootstrap"}, Type: TypeBool, Source: openclawAgentsRef, Desc: "Skip the bootstrap workspace files."}),
		openclawRow(SettingRow{Path: KeyPath{"agents", "defaults", "bootstrapMaxChars"}, Type: TypeInt, Source: openclawAgentsRef, Desc: "Per-file bootstrap character cap."}),
		openclawRow(SettingRow{Path: KeyPath{"agents", "defaults", "bootstrapTotalMaxChars"}, Type: TypeInt, Source: openclawAgentsRef, Desc: "Total bootstrap character cap."}),
		openclawRow(SettingRow{
			Path: KeyPath{"agents", "defaults", "compaction"}, Type: TypeObject, Source: openclawCompactRef,
			Desc: "Compaction {mode: default | safeguard, enabled, provider, timeoutSeconds, maxActiveTranscriptBytes, memoryFlush, postCompactionSections}.",
		}),
		openclawRow(SettingRow{
			Path: KeyPath{"agents", "defaults", "contextPruning"}, Type: TypeObject, Source: openclawCompactRef, Desc: "Context pruning {mode: off | cache-ttl, ...}.",
		}),
		openclawRow(SettingRow{
			Path: KeyPath{"agents", "defaults", "sandbox", "mode"}, Type: TypeEnum, Enum: []string{"off", "non-main", "all"}, Default: "\"off\"",
			Source: openclawSandboxRef, Desc: "Which sessions run in a sandbox.", Danger: DangerConfirm, Note: "off (the default) runs every session unsandboxed.",
		}),
		openclawRow(SettingRow{
			Path: KeyPath{"agents", "defaults", "sandbox", "scope"}, Type: TypeEnum, Enum: []string{"session", "agent", "shared"}, Default: "\"agent\"",
			Source: openclawSandboxRef, Desc: "Sandbox sharing scope.",
		}),
		openclawRow(SettingRow{
			Path: KeyPath{"agents", "defaults", "sandbox", "workspaceAccess"}, Type: TypeEnum, Enum: []string{"none", "ro", "rw"}, Default: "\"none\"",
			Source: openclawSandboxRef, Desc: "Sandbox access to the agent workspace.", Danger: DangerConfirm, Note: "rw lets sandboxed commands write the workspace.",
		}),
		openclawRow(SettingRow{
			Path: KeyPath{"agents", "entries"}, Type: TypeObject, Source: openclawAgentsRef,
			Desc: "Per-agent overrides keyed by agent id (model, workspace, tools, params, ...).",
		}),
		// --- tools
		openclawRow(SettingRow{
			Path: KeyPath{"tools", "profile"}, Type: TypeEnum, Enum: []string{"minimal", "coding", "messaging", "full"}, Source: openclawToolsRef,
			Desc: "Base tool allowlist before allow / deny.", Danger: DangerConfirm,
			Note: "full (set by local onboarding) applies no core filtering; it selects tools, not execution permissions.",
		}),
		openclawRow(SettingRow{
			Path: KeyPath{"tools", "allow"}, Type: TypeRuleList, Source: openclawToolsRef, Desc: "Tool names (or group:*) made available on top of the profile.",
			Note: "Availability, not prompt-free approval: approvals are the exec-approvals policy.",
		}),
		openclawRow(SettingRow{Path: KeyPath{"tools", "alsoAllow"}, Type: TypeRuleList, Source: openclawToolsRef, Desc: "Extra tools added to a limited profile."}),
		openclawRow(SettingRow{
			Path: KeyPath{"tools", "deny"}, Type: TypeRuleList, Source: openclawToolsRef, Desc: "Tool names (or group:*) never available.",
			Note: "Group denies override explicit allows.",
		}),
		openclawRow(SettingRow{
			Path: KeyPath{"tools", "elevated"}, Type: TypeObject, Source: openclawToolsRef, Desc: "Elevated exec policy.",
			Danger: DangerConfirm, Note: "Grants elevated command execution.",
		}),
		openclawRow(SettingRow{Path: KeyPath{"tools", "byProvider"}, Type: TypeObject, Source: openclawToolsRef, Desc: "Tool policy per provider."}),
		// --- mcp / skills / plugins
		openclawRow(SettingRow{
			Path: KeyPath{"mcp", "servers"}, Type: TypeMCPMap, Source: openclawExtRef,
			Desc: "MCP servers by name: command, args | url, transport (streamable-http | sse), enabled, requestTimeoutMs, connectionTimeoutMs, auth, oauth, toolFilter, sslVerify, headers.",
			Note: "Managed by openclaw mcp set / unset. enabled: false hides a server; headers may hold credentials (use ${VAR} substitution).",
		}),
		openclawRow(SettingRow{Path: KeyPath{"mcp", "sessionIdleTtlMs"}, Type: TypeInt, Source: openclawExtRef, Desc: "Idle lifetime of session-scoped MCP runtimes."}),
		openclawRow(SettingRow{Path: KeyPath{"skills", "load", "extraDirs"}, Type: TypeStringList, Source: openclawExtRef, Desc: "Extra skill directories (lowest precedence)."}),
		openclawRow(SettingRow{Path: KeyPath{"skills", "entries"}, Type: TypeObject, Source: openclawExtRef, Desc: "Per-skill settings {enabled, apiKey, ...}.", Note: "entries.<key>.apiKey may hold a credential."}),
		openclawRow(SettingRow{Path: KeyPath{"plugins", "allow"}, Type: TypeStringList, Source: openclawExtRef, Desc: "Plugin allowlist."}),
		openclawRow(SettingRow{Path: KeyPath{"plugins", "deny"}, Type: TypeStringList, Source: openclawExtRef, Desc: "Plugin denylist."}),
		openclawRow(SettingRow{Path: KeyPath{"plugins", "load", "paths"}, Type: TypeStringList, Source: openclawExtRef, Desc: "Extra plugin load paths."}),
		openclawRow(SettingRow{
			Path: KeyPath{"plugins", "entries"}, Type: TypeObject, Source: openclawExtRef,
			Desc:   "Per-plugin settings {enabled, config, env, apiKey, hooks.allowConversationAccess, hooks.allowPromptInjection, ...}.",
			Danger: DangerConfirm, Note: "hooks.allowPromptInjection / allowConversationAccess widen what a plugin sees and does; apiKey may hold a credential.",
		}),
		// --- models
		openclawRow(SettingRow{
			Path: KeyPath{"models", "providers"}, Type: TypeObject, Source: openclawRuntimeRef, Danger: DangerConfirm,
			Desc: "Custom providers by id: {baseUrl, api, apiKey, models[], ...}.",
			Note: "baseUrl redirects traffic (the proxy seam); apiKey should be a ${ENV} reference or SecretRef, never a literal.",
		}),
		openclawRow(SettingRow{Path: KeyPath{"models", "mode"}, Type: TypeEnum, Enum: []string{"merge", "replace"}, Source: openclawRuntimeRef, Desc: "How custom providers combine with the catalogue."}),
		// --- env / telemetry / update / ui / hooks
		openclawRow(SettingRow{Path: KeyPath{"env"}, Type: TypeObject, Source: openclawEnvRef, Desc: "Environment block {vars, shellEnv, ...}; does not override existing variables."}),
		openclawRow(SettingRow{
			Path: KeyPath{"env", "vars"}, Type: TypeStringMap, Source: openclawEnvRef, Desc: "Inline environment variables.",
			Note: "Values may be credentials: the secret posture applies; prefer SecretRef.",
		}),
		openclawRow(SettingRow{
			Path: KeyPath{"telemetry", "enabled"}, Type: TypeBool, Source: openclawObsRef, Desc: "Feature statistics sent to OpenClaw.",
			Note: "DO_NOT_TRACK also applies; update checks are separate (update.checkOnStart).",
		}),
		openclawRow(SettingRow{
			Path: KeyPath{"telemetry", "consentedAt"}, Type: TypeString, Source: openclawObsRef, Owner: OwnerToolState,
			Desc: "When the operator answered the statistics prompt.",
		}),
		openclawRow(SettingRow{
			Path: KeyPath{"update", "channel"}, Type: TypeEnum, Enum: []string{"stable", "extended-stable", "beta", "dev"}, Source: openclawRuntimeRef,
			Desc: "Release channel.",
		}),
		openclawRow(SettingRow{Path: KeyPath{"update", "checkOnStart"}, Type: TypeBool, Default: "true", Source: openclawRuntimeRef, Desc: "Check for updates at gateway start and daily."}),
		openclawRow(SettingRow{
			Path: KeyPath{"update", "auto", "enabled"}, Type: TypeBool, Default: "false", Source: openclawRuntimeRef, Desc: "Background auto-update (needs checkOnStart).",
		}),
		openclawRow(SettingRow{
			Path: KeyPath{"ui", "prefs", "theme"}, Type: TypeEnum, Source: openclawUIRef,
			Enum: []string{"claw", "knot", "dash", "absolutely", "tide", "beacon", "phosphor", "crt", "manuscript", "rose", "miami", "custom"},
			Desc: "Control UI theme.",
		}),
		openclawRow(SettingRow{
			Path: KeyPath{"ui", "prefs", "themeMode"}, Type: TypeEnum, Enum: []string{"light", "dark", "system"}, Source: openclawUIRef, Desc: "Control UI light / dark mode.",
		}),
		openclawRow(SettingRow{
			Path: KeyPath{"hooks"}, Type: TypeObject, Source: "https://docs.openclaw.ai/gateway/config-hooks.md",
			Desc: "HTTP / webhook ingress hooks (not tool-call hooks; in-process plugin hooks live under plugins.entries).",
		}),
	},
	Map: []MapRow{
		{
			Key: KeyModel, Tool: "openclaw", Settings: []string{"openclaw/agents.defaults.model"}, Transform: TransformModelAlias,
			Params: TransformParams{Model: ModelSpelling{Qualified: true}}, Source: openclawModelsRef,
			Note: "Written as the string form; the {primary, fallbacks} object form has no canonical reading.",
		},
		{
			Key: KeySmallModel, Tool: "openclaw", Settings: []string{"openclaw/agents.defaults.utilityModel"}, Transform: TransformModelAlias,
			Params: TransformParams{Model: ModelSpelling{Qualified: true}}, Source: openclawModelsRef,
		},
		{
			Key: KeyDeniedTools, Tool: "openclaw", Settings: []string{"openclaw/tools.deny"}, Transform: TransformRuleTranslate,
			Params: TransformParams{Grammar: openclawDenyGrammar}, Source: openclawToolsRef,
			Note: "Tool names only; patterns and mcp rules are refused, group:* names read back as tool-specific.",
		},
		{Key: KeyTelemetryOff, Tool: "openclaw", Settings: []string{"openclaw/telemetry.enabled"}, Transform: TransformBoolInvert, Source: openclawObsRef},
		{Key: KeyAutoUpdate, Tool: "openclaw", Settings: []string{"openclaw/update.auto.enabled"}, Transform: TransformIdentity, Source: openclawRuntimeRef},
		{
			Key: KeyTheme, Tool: "openclaw", Settings: []string{"openclaw/ui.prefs.themeMode"}, Transform: TransformEnumMap,
			Params: TransformParams{Enum: []EnumPair{{"light", []any{"light"}}, {"dark", []any{"dark"}}}}, Source: openclawUIRef,
			Note: "system and the named ui.prefs.theme palettes stay tool-specific.",
		},
		{
			Key: KeyEnv, Tool: "openclaw", Settings: []string{"openclaw/env.vars"}, Transform: TransformIdentity, Source: openclawEnvRef,
			Note: "Values may be credentials; the section 5.3 secret posture applies.",
		},
	},
}
