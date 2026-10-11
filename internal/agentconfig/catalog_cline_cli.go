package agentconfig

// Cline CLI, the npm `cline` package (phase P2, doc-authored). JSON files
// under CLINE_DIR (default ~/.cline on every OS) / CLINE_DATA_DIR (default
// ~/.cline/data). providers.json holds OAuth tokens and API keys and is
// never read: the model, provider and base URL live there, so they have no
// rows (`cline auth` is the only writer; Observer's launcher writes
// settings.baseUrl through that verb's file without parsing it for a
// surface). Sources: audit docs/audits/adapter-config-surface-2026-10-03.md
// section 5 "cline-cli", the zod GlobalSettingsSchema in vendor source and
// repo docs/clinecli-adapter.md, re-fetched 2026-10-04.

const (
	clineCLISchemaRef = "https://raw.githubusercontent.com/cline/cline/main/sdk/packages/core/src/services/global-settings.ts"
	clineCLIPathsRef  = "https://raw.githubusercontent.com/cline/cline/main/sdk/packages/shared/src/storage/paths.ts"
)

var clineCLIFiles = []FileRow{
	{
		ID: "cline-cli/global-settings", Tool: "cline-cli", Shape: "cline-cli/global-settings", Kind: KindSettings, Scope: ScopeUser,
		Paths: samePath("~/.cline/data/settings/global-settings.json"), Format: FormatJSON, Precedence: 20, WriteNeeds: PrivUser,
		Grounding: source(clineCLISchemaRef),
		Note:      "CLINE_GLOBAL_SETTINGS_PATH relocates it. Project .cline/ settings override global ones; CLI flags override files. Absent on node 1 (only a marker file under ~/.cline, 2026-10-03).",
	},
	{
		ID: "cline-cli/mcp-settings", Tool: "cline-cli", Shape: "cline-cli/mcp-settings", Kind: KindMCP, Scope: ScopeUser,
		Paths: samePath("~/.cline/data/settings/cline_mcp_settings.json"), Format: FormatJSON, Precedence: 20, WriteNeeds: PrivUser,
		Grounding: source(clineCLIPathsRef),
		Note:      "CLINE_MCP_SETTINGS_PATH relocates it. Usually written by `cline mcp install`; absent on a live 3.0.48 install until then. The Cline VS Code extension on the shared SDK layout reads the same file.",
	},
	{
		ID: "cline-cli/providers", Tool: "cline-cli", Shape: "cline-cli/providers", Kind: KindToolState, Scope: ScopeUser,
		Paths: samePath("~/.cline/data/settings/providers.json"), Format: FormatJSON, Precedence: 0, WriteNeeds: PrivNever,
		Grounding: source(clineCLIPathsRef),
		Note: "Credential file (OAuth tokens and API keys): never read, no rows. It holds the provider, model and settings.baseUrl; " +
			"`cline auth` (and Observer's cline-cli launcher, for baseUrl only) are its writers. CLINE_PROVIDER_SETTINGS_PATH relocates it.",
	},
	{
		ID: "cline-cli/secrets", Tool: "cline-cli", Shape: "cline-cli/secrets", Kind: KindToolState, Scope: ScopeUser,
		Paths: samePath("~/.cline/data/secrets.json"), Format: FormatJSON, Precedence: 0, WriteNeeds: PrivNever,
		Grounding: source(clineCLIPathsRef), Note: "Secrets store: never read or written.",
	},
}

func clineCLIRows(rows []SettingRow) []SettingRow {
	for i := range rows {
		r := &rows[i]
		r.Tool = "cline-cli"
		r.ID = r.Tool + "/" + r.Path.String()
		if r.FileID == "" {
			r.FileID = "cline-cli/global-settings"
		}
		if r.Owner == "" {
			r.Owner = OwnerUser
		}
		if r.Verified == "" {
			r.Verified = VerifiedDoc
		}
		if r.Source == "" {
			r.Source = clineCLISchemaRef
		}
	}
	return rows
}

var clineCLISettings = clineCLIRows([]SettingRow{
	{Path: KeyPath{"telemetryOptOut"}, Type: TypeBool, Default: "false", Desc: "Opt out of telemetry."},
	{Path: KeyPath{"autoUpdateEnabled"}, Type: TypeBool, Default: "true", Desc: "Update the CLI automatically."},
	{
		Path: KeyPath{"compactionStrategy"}, Type: TypeEnum, Enum: []string{"basic", "agentic"}, Default: "\"agentic\"",
		Desc: "Conversation compaction strategy.", Note: "The UI also offers off, which maps to compactionEnabled false.",
	},
	{Path: KeyPath{"compactionEnabled"}, Type: TypeBool, Desc: "Compact the conversation as context fills (no token threshold knob)."},
	{Path: KeyPath{"planActMode"}, Type: TypeEnum, Enum: []string{"plan", "act"}, Desc: "Start in Plan or Act mode."},
	{
		Path: KeyPath{"toolAutoApprove"}, Type: TypeBool, Desc: "Approve tool calls without asking.",
		Danger: DangerConfirm, Note: "Dangerous: true.",
	},
	{Path: KeyPath{"tuiTheme"}, Type: TypeString, Desc: "Terminal UI theme name."},
	{
		Path: KeyPath{"disabledTools"}, Type: TypeStringList, Desc: "Tools the agent may not use.",
		Note: "The audit lists it among the dangerous keys; tool names are the CLI's own.",
	},
	{Path: KeyPath{"tools"}, Type: TypeObject, Desc: "Per-tool settings for configurable model tools (tools.<name>.enabled)."},
	{Path: KeyPath{"disabledPlugins"}, Type: TypeStringList, Desc: "Plugins not loaded."},
	{Path: KeyPath{"disabledAgentPlugins"}, Type: TypeStringList, Desc: "Agent plugins not loaded."},
	{
		FileID: "cline-cli/mcp-settings", Path: KeyPath{"mcpServers"}, Type: TypeMCPMap, Source: "https://docs.cline.bot/getting-started/config.md",
		Desc: "MCP servers by name: {command, args, env, disabled, alwaysAllow, type, url}.",
		Note: "`cline mcp install <name> --transport stdio|sse|http|streamable-http` is the vendor's writer.",
	},
})

var clineCLIMap = []MapRow{
	{Key: KeyTelemetryOff, Tool: "cline-cli", Settings: []string{"cline-cli/telemetryOptOut"}, Transform: TransformIdentity, Source: clineCLISchemaRef},
	{Key: KeyAutoUpdate, Tool: "cline-cli", Settings: []string{"cline-cli/autoUpdateEnabled"}, Transform: TransformIdentity, Source: clineCLISchemaRef},
	{
		Key: KeyMCPServers, Tool: "cline-cli", Settings: []string{"cline-cli/mcpServers"}, Transform: TransformMCPShape,
		Params: TransformParams{MCP: clineMCPShape}, Lossy: true, Source: "https://docs.cline.bot/getting-started/config.md",
		Note: "Local servers only; remote servers are refused. disabled and alwaysAllow are not carried. " + mcpLossyNote,
	},
}

var clineCLICatalog = toolCatalog{
	Tool: "cline-cli",
	VerifiedAgainst: "cline/cline main GlobalSettingsSchema (sdk/packages/core/src/services/global-settings.ts) fetched 2026-10-04 and docs.cline.bot " +
		"(audit section 5); repo docs/clinecli-adapter.md live-grounded cline 3.0.48 / 3.0.68. Node 1 had only a marker file (2026-10-03).",
	Files: clineCLIFiles,
	Extra: clineCLISettings,
	Map:   clineCLIMap,
}
