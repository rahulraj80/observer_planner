package agentconfig

// DeepSeek Harness (`@deepseek-ai/dsh`) catalogue, doc-authored (phase P2).
// Config is per PLUGIN (Cordis), runtime-validated only: every key below is
// the vendor's own docs/config-catalog.md entry (re-fetched 2026-10-04), but
// where a key sits inside a profile's YAML is UNVERIFIED (audit section 5
// "deepseek") and the harness was not installed on node 1 (audit section 6),
// so every row is VerifiedUnverified. Path convention: the first segment is
// the plugin name, the rest is the key inside that plugin's config block.
const (
	deepseekCatalogRef = "https://raw.githubusercontent.com/deepseek-ai/deepseek-harness/master/docs/config-catalog.md"
	deepseekDocsRef    = "https://github.com/deepseek-ai/deepseek-harness/tree/master/docs"
	deepseekMCPRef     = "https://composio.dev/blog/how-to-connect-mcp-servers-to-deepseek-harness"
	deepseekPlacement  = "Key from the vendor config catalog; its placement inside the profile YAML is UNVERIFIED (not installed on node 1)."
)

func deepseekRow(shape, plugin string, key KeyPath, typ ValueType, def, desc string) SettingRow {
	path := append(KeyPath{plugin}, key...)
	return SettingRow{
		ID: "deepseek/" + path.String(), Tool: "deepseek", FileID: shape, Path: path,
		Type: typ, Default: def, Desc: desc, Source: deepseekCatalogRef, Verified: VerifiedUnverified, Owner: OwnerUser, Note: deepseekPlacement,
	}
}

func deepseekCordis(plugin string, key KeyPath, typ ValueType, def, desc string) SettingRow {
	return deepseekRow("deepseek/cordis", plugin, key, typ, def, desc)
}

func deepseekWith(r SettingRow, edit func(*SettingRow)) SettingRow {
	edit(&r)
	return r
}

const (
	deepseekPluginModel      = "@deepseek-ai/dsh-agent-default-model"
	deepseekPluginPresetReg  = "@deepseek-ai/dsh-agent-preset-registry"
	deepseekPluginInstr      = "@deepseek-ai/dsh-agent-instructions"
	deepseekPluginPiAI       = "@deepseek-ai/dsh-llm-pi-ai"
	deepseekPluginPerms      = "@deepseek-ai/dsh-permission-presets"
	deepseekPluginCompaction = "@deepseek-ai/dsh-compaction-basic"
	deepseekPluginMCP        = "@deepseek-ai/dsh-mcp-client"
	deepseekPluginHooksCC    = "@deepseek-ai/dsh-hooks-claude-code"
	deepseekPluginHooksCodex = "@deepseek-ai/dsh-hooks-codex"
	deepseekPluginBash       = "@deepseek-ai/dsh-bash-local"
	deepseekPluginWebserver  = "@deepseek-ai/dsh-host-webserver"
)

var deepseekCatalog = toolCatalog{
	Tool: "deepseek",
	VerifiedAgainst: "deepseek-harness docs/config-catalog.md (audit section 5, fetched 2026-10-03; re-fetched 2026-10-04) and the composio MCP guide; " +
		"not installed on node 1, so every row is unverified live (YAML placement UNVERIFIED)",
	Files: []FileRow{
		{
			ID: "deepseek/web-profile-cordis", Tool: "deepseek", Shape: "deepseek/cordis", Kind: KindSettings, Scope: ScopeUser,
			Paths: samePath("~/.dsh/profiles/web/cordis.yml"), Format: FormatYAML, Precedence: 20, WriteNeeds: PrivUser,
			Grounding: doc(deepseekDocsRef),
			Note:      "Per-profile plugin config of the web GUI profile; other profiles live at ~/.dsh/profiles/<name>/cordis.yml with the same shape. DSH_HOME relocates ~/.dsh (fixed on every OS, Windows %USERPROFILE%\\.dsh). Layering: plugin defaults < cordis.yml < UI-edited Volatile settings (persisted to the profile) < env vars. No project-level settings file.",
		},
		{
			ID: "deepseek/web-profile-patch", Tool: "deepseek", Shape: "deepseek/cordis-patch", Kind: KindMCP, Scope: ScopeUser,
			Paths: samePath("~/.dsh/profiles/web/cordis.patch.yml"), Format: FormatYAML, Precedence: 25, WriteNeeds: PrivUser,
			Grounding: doc(deepseekMCPRef),
			Note:      "A list of patch operations (- insert: [{id, name: '@deepseek-ai/dsh-mcp-client', config: {...}}]); one mcp-client plugin instance per MCP server. Rows name the keys of that config block.",
		},
		{
			ID: "deepseek/user-settings", Tool: "deepseek", Shape: "deepseek/user-settings", Kind: KindSettings, Scope: ScopeUser,
			Paths: samePath("~/.dsh/settings.yaml"), Format: FormatYAML, Precedence: 20, WriteNeeds: PrivNever,
			Grounding: Grounding{Kind: GroundAdapterDoc, Source: "docs/deepseek-adapter.md", Date: auditDate},
			Note:      "User settings that may hold credentials: never read, never written (Observer's adapter marks it off-limits). Ollama's launcher keeps its own copy at ~/.ollama/launch/dsh/settings.yaml.",
		},
		{
			ID: "deepseek/credentials", Tool: "deepseek", Shape: "deepseek/credentials", Kind: KindToolState, Scope: ScopeUser,
			Paths: samePath("~/.dsh/.credentials.yaml"), Format: FormatYAML, Precedence: 0, WriteNeeds: PrivNever,
			Grounding: doc(deepseekCatalogRef),
			Note:      "API keys (hot-reloaded, watch: true): never read, never written.",
		},
	},
	Extra: []SettingRow{
		// Model selection.
		deepseekCordis(deepseekPluginModel, KeyPath{"provider"}, TypeString, "", "Default provider id."),
		deepseekCordis(deepseekPluginModel, KeyPath{"model"}, TypeString, "", "Default model id (provider-owned)."),
		deepseekWith(deepseekCordis(deepseekPluginModel, KeyPath{"reasoningEffort"}, TypeString, "", "Default reasoning effort."), func(r *SettingRow) {
			r.Note = deepseekPlacement + " Values are adapter-owned ids (UNVERIFIED)."
		}),
		deepseekCordis(deepseekPluginPresetReg, KeyPath{"default"}, TypeString, "", "Default agent preset."),
		deepseekCordis(deepseekPluginPresetReg, KeyPath{"selectedDefault"}, TypeString, "", "Preset selected in the UI (a Volatile setting)."),
		deepseekCordis("@deepseek-ai/dsh-agent-loop", KeyPath{"maxParallelToolCalls"}, TypeInt, "", "Maximum tool calls run in parallel."),
		deepseekWith(deepseekCordis("@deepseek-ai/dsh-agent-tool-presentation", KeyPath{"mode"}, TypeEnum, "", "How tools are presented to the model."), func(r *SettingRow) {
			r.Enum = []string{"native", "ptc", "both"}
		}),
		// Instructions.
		deepseekCordis(deepseekPluginInstr, KeyPath{"dshHome"}, TypeString, "", "Harness home holding the user-global AGENTS.md (default $DSH_HOME or ~/.dsh)."),
		deepseekCordis(deepseekPluginInstr, KeyPath{"projectRootMarkers"}, TypeStringList, "", "Files that mark a project root."),
		deepseekCordis(deepseekPluginInstr, KeyPath{"maxBytes"}, TypeInt, "", "Byte cap on loaded instructions."),
		deepseekCordis(deepseekPluginInstr, KeyPath{"maxSourceBytes"}, TypeInt, "", "Byte cap per instruction source."),
		deepseekWith(deepseekCordis(deepseekPluginInstr, KeyPath{"instructionFileCandidates"}, TypeStringList, "", "Ordered project instruction file names."), func(r *SettingRow) {
			r.Note = deepseekPlacement + " The default list includes AGENTS.md; the full default is UNVERIFIED."
		}),
		deepseekCordis(deepseekPluginInstr, KeyPath{"localInstructionFileCandidates"}, TypeStringList, "", "Personal instruction file names overlaid on the project ones."),
		// Providers.
		deepseekWith(deepseekCordis("@deepseek-ai/dsh-llm-deepseek-api-key", KeyPath{"apiKeyEnv"}, TypeString, "\"DEEPSEEK_API_KEY\"", "Env var NAME holding the DeepSeek API key."), func(r *SettingRow) {
			r.Note = deepseekPlacement + " A reference, never the key; literal keys live in .credentials.yaml."
		}),
		deepseekWith(deepseekCordis(deepseekPluginPiAI, KeyPath{"providers"}, TypeObject, "", "Provider profiles by id: apiKeyEnv, api, baseURL, models, modelOverrides, headers, defaultContextWindow (262144), defaultMaxTokens (32768), reasoning, timeouts."), func(r *SettingRow) {
			r.Danger = DangerConfirm
			r.Note = deepseekPlacement + " A baseURL (capital URL) redirects model traffic; headers may carry credentials (secret posture applies)."
		}),
		// Permission presets.
		deepseekWith(deepseekCordis(deepseekPluginPerms, KeyPath{"presets"}, TypeObject, "", "Permission presets by name: {sandbox, approval, name, description}."), func(r *SettingRow) {
			r.Danger = DangerConfirm
			r.Note = deepseekPlacement + " SandboxMode and ApprovalPolicy values are UNVERIFIED; a full-access preset bypasses approvals."
		}),
		deepseekWith(deepseekCordis(deepseekPluginPerms, KeyPath{"defaultPreset"}, TypeString, "", "Permission preset in force (a Volatile setting)."), func(r *SettingRow) {
			r.Danger = DangerConfirm
			r.Note = deepseekPlacement + " Selecting a full-access preset removes approvals."
		}),
		// Compaction.
		deepseekCordis(deepseekPluginCompaction, KeyPath{"auto"}, TypeBool, "true", "Automatic compaction."),
		deepseekWith(deepseekCordis(deepseekPluginCompaction, KeyPath{"thresholdRatio"}, TypeFloat, "0.8", "Context fill ratio that triggers compaction."), func(r *SettingRow) {
			r.Min, r.Max = f64(0), f64(1)
			r.Note = deepseekPlacement + " A ratio, not a percent (0.8 = 80%)."
		}),
		deepseekCordis(deepseekPluginCompaction, KeyPath{"headroomTokens"}, TypeInt, "65536", "Tokens kept free for the response."),
		deepseekCordis(deepseekPluginCompaction, KeyPath{"retainRatio"}, TypeFloat, "0.16", "Share of context retained after compaction."),
		deepseekCordis(deepseekPluginCompaction, KeyPath{"retainTokens"}, TypeInt, "", "Tokens retained after compaction (mutually exclusive with retainRatio)."),
		deepseekCordis(deepseekPluginCompaction, KeyPath{"summarizationProvider"}, TypeString, "", "Provider for compaction summaries."),
		deepseekCordis(deepseekPluginCompaction, KeyPath{"summarizationModel"}, TypeString, "", "Model for compaction summaries."),
		deepseekCordis(deepseekPluginCompaction, KeyPath{"maxTokens"}, TypeInt, "", "Summary token cap (default: headroomTokens)."),
		deepseekCordis(deepseekPluginCompaction, KeyPath{"compactionRetries"}, TypeInt, "1", "Compaction retries."),
		deepseekCordis("@deepseek-ai/dsh-compaction-tool-result-pruner", KeyPath{"thresholdChars"}, TypeInt, "8192", "Tool results longer than this are pruned."),
		// Shell.
		deepseekCordis(deepseekPluginBash, KeyPath{"timeoutMs"}, TypeInt, "", "Default shell command timeout (ms)."),
		deepseekCordis(deepseekPluginBash, KeyPath{"maxTimeoutMs"}, TypeInt, "", "Cap on requested shell timeouts (ms)."),
		deepseekCordis(deepseekPluginBash, KeyPath{"maxOutputBytes"}, TypeInt, "", "Captured shell output cap."),
		// Hook replay.
		deepseekWith(deepseekCordis(deepseekPluginHooksCC, KeyPath{"configPath"}, TypeString, "", "Claude Code settings file whose hooks dsh replays."), func(r *SettingRow) {
			r.Danger = DangerConfirm
			r.Note = deepseekPlacement + " Runs that file's hook commands, Observer's Claude Code hooks among them (foreign replay, UNVERIFIED live); repointing or removing it changes which hooks fire."
		}),
		deepseekCordis(deepseekPluginHooksCC, KeyPath{"defaultTimeoutMs"}, TypeInt, "600000", "Default hook timeout (ms)."),
		deepseekWith(deepseekCordis(deepseekPluginHooksCodex, KeyPath{"configPath"}, TypeString, "", "Codex hooks file whose hooks dsh replays."), func(r *SettingRow) {
			r.Danger = DangerConfirm
			r.Note = deepseekPlacement + " Runs that file's hook commands, Observer's Codex hooks among them (foreign replay, UNVERIFIED live)."
		}),
		deepseekCordis(deepseekPluginHooksCodex, KeyPath{"defaultTimeoutMs"}, TypeInt, "600000", "Default hook timeout (ms)."),
		// Client, UI, telemetry.
		deepseekWith(deepseekCordis("@deepseek-ai/dsh-client-product-analytics", KeyPath{"enabled"}, TypeBool, "", "Product analytics (a Volatile setting)."), func(r *SettingRow) {
			r.Note = deepseekPlacement + " The audit's telemetry switch; set from the UI."
		}),
		deepseekWith(deepseekCordis("@deepseek-ai/dsh-client-ui-plugin-manager", KeyPath{"registryProbeEnabled"}, TypeBool, "", "Probe the plugin registry for updates (the update-check dialog)."), func(r *SettingRow) {
			r.Note = deepseekPlacement + " Not a real auto-updater (dsh runs through npx)."
		}),
		deepseekCordis("@deepseek-ai/dsh-client-ui-theme", KeyPath{"preference"}, TypeString, "", "Theme preference (a Volatile setting)."),
		deepseekCordis("@deepseek-ai/dsh-client-ui-theme", KeyPath{"fontSize"}, TypeInt, "", "UI font size (a Volatile setting)."),
		deepseekWith(deepseekCordis("@deepseek-ai/dsh-client-connection", KeyPath{"trustedHosts"}, TypeStringList, "", "Hosts the web client trusts."), func(r *SettingRow) {
			r.Danger = DangerConfirm
			r.Note = deepseekPlacement + " Widens who may drive the local harness."
		}),
		deepseekWith(deepseekCordis(deepseekPluginWebserver, KeyPath{"host"}, TypeEnum, "", "Address the web GUI listens on."), func(r *SettingRow) {
			r.Enum, r.Danger = []string{"127.0.0.1", "0.0.0.0"}, DangerConfirm
			r.Note = deepseekPlacement + " \"0.0.0.0\" exposes the agent GUI to the network."
		}),
		deepseekCordis(deepseekPluginWebserver, KeyPath{"port"}, TypeInt, "", "Port the web GUI listens on."),
		deepseekCordis("@deepseek-ai/dsh-credentials-local", KeyPath{"path"}, TypeString, "", "Credentials file (default .credentials.yaml under the harness home)."),
		deepseekWith(deepseekCordis("@deepseek-ai/dsh-experimental-browser-use-stagehand-native", KeyPath{"model", "apiKey"}, TypeString, "", "API key for the Stagehand browser-use model."), func(r *SettingRow) {
			r.Secret = true
			r.Note = deepseekPlacement + " A credential: never written by Observer."
		}),
		// MCP servers (cordis.patch.yml, one mcp-client instance per server).
		deepseekRow("deepseek/cordis-patch", deepseekPluginMCP, KeyPath{"serverName"}, TypeString, "", "MCP server name (tools surface as mcp__<server>__<tool>)."),
		deepseekWith(deepseekRow("deepseek/cordis-patch", deepseekPluginMCP, KeyPath{"transport"}, TypeEnum, "", "MCP transport."), func(r *SettingRow) {
			r.Enum, r.Source = []string{"stdio", "streamable-http"}, deepseekMCPRef
		}),
		deepseekWith(deepseekRow("deepseek/cordis-patch", deepseekPluginMCP, KeyPath{"command"}, TypeString, "", "Command for a stdio server."), func(r *SettingRow) {
			r.Danger = DangerConfirm
			r.Note = deepseekPlacement + " Runs an arbitrary command."
		}),
		deepseekRow("deepseek/cordis-patch", deepseekPluginMCP, KeyPath{"args"}, TypeStringList, "", "Arguments for a stdio server."),
		deepseekWith(deepseekRow("deepseek/cordis-patch", deepseekPluginMCP, KeyPath{"env"}, TypeStringMap, "", "Environment for a stdio server."), func(r *SettingRow) {
			r.Note = deepseekPlacement + " Values can reference process env (!!js process.env.NAME); the secret posture applies."
		}),
		deepseekRow("deepseek/cordis-patch", deepseekPluginMCP, KeyPath{"cwd"}, TypeString, "", "Working directory for a stdio server."),
		deepseekRow("deepseek/cordis-patch", deepseekPluginMCP, KeyPath{"url"}, TypeString, "", "URL of a streamable-http server."),
		deepseekWith(deepseekRow("deepseek/cordis-patch", deepseekPluginMCP, KeyPath{"headers"}, TypeStringMap, "", "HTTP headers for a remote server."), func(r *SettingRow) {
			r.Note = deepseekPlacement + " Headers often carry credentials; the secret posture applies."
		}),
		deepseekRow("deepseek/cordis-patch", deepseekPluginMCP, KeyPath{"toolCallTimeoutMs"}, TypeInt, "", "Tool call timeout (ms)."),
		deepseekRow("deepseek/cordis-patch", deepseekPluginMCP, KeyPath{"failOnStartupError"}, TypeBool, "", "Fail the session when the server does not start."),
		deepseekRow("deepseek/cordis-patch", deepseekPluginMCP, KeyPath{"reconnect", "enabled"}, TypeBool, "true", "Reconnect after a dropped connection."),
	},
}
