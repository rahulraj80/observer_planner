package agentconfig

// Qoder CLI catalogue. Sources: docs.qoder.com/en/cli/settings-reference
// (re-fetched 2026-10-04 for the full key table), cli/settings,
// cli/permissions (tool names and modes re-fetched 2026-10-04), cli/hooks,
// audit section 5 "qoder"; repo docs/qoder-adapter.md. Node 1 had Qoder
// installed: model.name, permissions.trustDirectories and
// security.auth.selectedType were seen in ~/.qoder/settings.json on
// 2026-10-03. The Qoder IDE's own VS Code-style settings path is UNVERIFIED
// and not catalogued.
const (
	qoderRef      = "https://docs.qoder.com/en/cli/settings-reference"
	qoderPermsRef = "https://docs.qoder.com/en/cli/permissions"
	qoderHooksRef = "https://docs.qoder.com/en/cli/hooks"
	qoderSettings = "qoder/settings"
	qoderLiveNote = "Seen on node 1 2026-10-03 (audit section 5 qoder, live)."
	qoderRestart  = "Takes effect after a restart."
)

// qoderRuleGrammar is Qoder's permissions rule syntax (Claude-style):
// Tool or Tool(content), mcp__server__tool. The pattern semantics of
// WebFetch / WebSearch are not documented, so they take no pattern.
var qoderRuleGrammar = RuleGrammar{
	"shell":  {Name: "Bash", Style: RuleCall},
	"read":   {Name: "Read", Style: RuleCall},
	"edit":   {Name: "Edit", Style: RuleCall},
	"write":  {Name: "Write", Style: RuleCall},
	"fetch":  {Name: "WebFetch", Style: RuleBare},
	"search": {Name: "WebSearch", Style: RuleBare},
	"mcp":    {Name: "mcp__", Style: RulePrefix, Sep: "__"},
}

var qoderCatalog = toolCatalog{
	Tool:            "qoder",
	VerifiedAgainst: "vendor docs fetched 2026-10-03 (audit section 5) and 2026-10-04; node 1 live key names 2026-10-03 for model.name, permissions.trustDirectories, security.auth.selectedType",
	Files: []FileRow{
		{
			ID: "qoder/user-settings", Tool: "qoder", Shape: qoderSettings, Kind: KindSettings, Scope: ScopeUser,
			Paths: samePath("~/.qoder/settings.json"), Format: FormatJSON, Precedence: 20, WriteNeeds: PrivUser,
			Grounding: live("~/.qoder/settings.json carries model.name, permissions.trustDirectories, security.auth.selectedType"),
			Note: "QODER_CONFIG_DIR relocates ~/.qoder (Windows %USERPROFILE%\\.qoder assumed). Also holds provider / auth selection: Observer writes only its hook entry and never reads credentials from it. " +
				"--settings <path> and CLI flags rank above every file.",
		},
		{
			ID: "qoder/project-settings", Tool: "qoder", Shape: qoderSettings, Kind: KindSettings, Scope: ScopeProjectShared,
			Paths: samePath("<project>/.qoder/settings.json"), Format: FormatJSON, Precedence: 30, WriteNeeds: PrivUser, Grounding: doc(qoderRef),
		},
		{
			ID: "qoder/project-local-settings", Tool: "qoder", Shape: qoderSettings, Kind: KindSettings, Scope: ScopeProjectLocal,
			Paths: samePath("<project>/.qoder/settings.local.json"), Format: FormatJSON, Precedence: 40, WriteNeeds: PrivUser, Gitignored: true,
			Grounding: doc("https://docs.qoder.com/en/cli/settings"),
		},
		{
			ID: "qoder/auth", Tool: "qoder", Shape: "qoder/auth", Kind: KindToolState, Scope: ScopeUser,
			Paths: samePath("~/.qoder/.auth/"), Format: FormatJSON, Precedence: 0, WriteNeeds: PrivNever,
			Grounding: Grounding{Kind: GroundAdapterDoc, Source: "docs/qoder-adapter.md", Date: auditDate},
			Note:      "Credential store: never touched.",
		},
	},
	Extra: []SettingRow{
		// --- model
		{
			ID: "qoder/model.name", Tool: "qoder", FileID: qoderSettings, Path: KeyPath{"model", "name"}, Type: TypeString,
			Desc: "Conversational model (QODER_MODEL overrides).", Source: qoderRef, Verified: VerifiedLive, Owner: OwnerUser,
			Note: qoderLiveNote + " Resolved server-side.",
		},
		{
			ID: "qoder/model.reasoningEffort", Tool: "qoder", FileID: qoderSettings, Path: KeyPath{"model", "reasoningEffort"}, Type: TypeEnum,
			Enum: []string{"disabled", "off", "none", "low", "medium", "high", "xhigh", "max"}, Desc: "Reasoning effort.", Source: qoderRef,
			Verified: VerifiedDoc, Owner: OwnerUser,
			Note: "The settings page lists low / medium / high only; the reference lists the full set (none appears in the audit's fetch only).",
		},
		{
			ID: "qoder/model.contextWindow", Tool: "qoder", FileID: qoderSettings, Path: KeyPath{"model", "contextWindow"}, Type: TypeInt,
			Desc: "Context window cap in tokens.", Source: qoderRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "qoder/model.summarizeToolOutput", Tool: "qoder", FileID: qoderSettings, Path: KeyPath{"model", "summarizeToolOutput"}, Type: TypeObject,
			Desc: "Summary token budget per tool.", Source: qoderRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "qoder/modelConfigs.customAliases", Tool: "qoder", FileID: qoderSettings, Path: KeyPath{"modelConfigs", "customAliases"}, Type: TypeObject,
			Desc: "Custom model aliases.", Source: qoderRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "qoder/modelConfigs.customOverrides", Tool: "qoder", FileID: qoderSettings, Path: KeyPath{"modelConfigs", "customOverrides"}, Type: TypeObject,
			Desc: "Custom model configuration overrides.", Source: qoderRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "qoder/agent", Tool: "qoder", FileID: qoderSettings, Path: KeyPath{"agent"}, Type: TypeString,
			Desc: "Agent used for the main thread.", Source: qoderRef, Verified: VerifiedDoc, Owner: OwnerUser, Note: qoderRestart,
		},
		{
			ID: "qoder/agents.overrides", Tool: "qoder", FileID: qoderSettings, Path: KeyPath{"agents", "overrides"}, Type: TypeObject,
			Desc: "Per-agent overrides by name.", Source: qoderRef, Verified: VerifiedDoc, Owner: OwnerUser, Note: qoderRestart,
		},
		{
			ID: "qoder/language", Tool: "qoder", FileID: qoderSettings, Path: KeyPath{"language"}, Type: TypeString,
			Desc: "Preferred response language.", Source: qoderRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "qoder/outputStyle", Tool: "qoder", FileID: qoderSettings, Path: KeyPath{"outputStyle"}, Type: TypeString,
			Desc: "Active output style.", Source: qoderRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "qoder/vpcInstanceName", Tool: "qoder", FileID: qoderSettings, Path: KeyPath{"vpcInstanceName"}, Type: TypeString,
			Desc: "VPC private-deployment instance name.", Source: qoderRef, Verified: VerifiedDoc, Owner: OwnerUser, Note: qoderRestart,
		},
		// --- general
		{
			ID: "qoder/general.defaultPermissionMode", Tool: "qoder", FileID: qoderSettings, Path: KeyPath{"general", "defaultPermissionMode"}, Type: TypeEnum,
			Enum: []string{"default", "accept_edits", "plan", "auto", "bypass_permissions", "yolo", "dont_ask"}, Default: `"default"`,
			Desc: "Default permission mode (QODER_PERMISSION_MODE overrides).", Source: qoderPermsRef, Verified: VerifiedDoc, Owner: OwnerUser,
			Danger: DangerConfirm, Note: "bypass_permissions (alias yolo) and auto run without per-action prompts. snake_case, unlike Claude's camelCase.",
		},
		{
			ID: "qoder/general.enableAutoUpdate", Tool: "qoder", FileID: qoderSettings, Path: KeyPath{"general", "enableAutoUpdate"}, Type: TypeBool, Default: "true",
			Desc: "Update automatically.", Source: qoderRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "qoder/general.enableNotifications", Tool: "qoder", FileID: qoderSettings, Path: KeyPath{"general", "enableNotifications"}, Type: TypeBool,
			Default: "false", Desc: "Run notifications.", Source: qoderRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "qoder/general.preferredEditor", Tool: "qoder", FileID: qoderSettings, Path: KeyPath{"general", "preferredEditor"}, Type: TypeString,
			Desc: "Editor for opening files.", Source: qoderRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "qoder/general.vimMode", Tool: "qoder", FileID: qoderSettings, Path: KeyPath{"general", "vimMode"}, Type: TypeBool, Default: "false",
			Desc: "Vim key bindings.", Source: qoderRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "qoder/general.maxAttempts", Tool: "qoder", FileID: qoderSettings, Path: KeyPath{"general", "maxAttempts"}, Type: TypeInt,
			Max: f64(10), Default: "10", Desc: "Maximum retry attempts.", Source: qoderRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "qoder/general.fileCheckpointing.enabled", Tool: "qoder", FileID: qoderSettings, Path: KeyPath{"general", "fileCheckpointing", "enabled"},
			Type: TypeBool, Default: "true", Desc: "File checkpoints.", Source: qoderRef, Verified: VerifiedDoc, Owner: OwnerUser, Note: qoderRestart,
		},
		{
			ID: "qoder/general.plan.enabled", Tool: "qoder", FileID: qoderSettings, Path: KeyPath{"general", "plan", "enabled"}, Type: TypeBool,
			Default: "true", Desc: "Plan Mode.", Source: qoderRef, Verified: VerifiedDoc, Owner: OwnerUser, Note: qoderRestart,
		},
		{
			ID: "qoder/general.plan.modelRouting", Tool: "qoder", FileID: qoderSettings, Path: KeyPath{"general", "plan", "modelRouting"}, Type: TypeBool,
			Default: "true", Desc: "Switch models between plan phases.", Source: qoderRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "qoder/general.sessionRetention.enabled", Tool: "qoder", FileID: qoderSettings, Path: KeyPath{"general", "sessionRetention", "enabled"},
			Type: TypeBool, Default: "true", Desc: "Clean up old sessions automatically.", Source: qoderRef, Verified: VerifiedDoc, Owner: OwnerUser,
			Note: "Pruned sessions are no longer there for Observer's watcher to backfill.",
		},
		{
			ID: "qoder/general.sessionRetention.maxAge", Tool: "qoder", FileID: qoderSettings, Path: KeyPath{"general", "sessionRetention", "maxAge"},
			Type: TypeString, Default: `"30d"`, Desc: "Delete sessions older than this.", Source: qoderRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "qoder/output.format", Tool: "qoder", FileID: qoderSettings, Path: KeyPath{"output", "format"}, Type: TypeEnum,
			Enum: []string{"text", "json"}, Default: `"text"`, Desc: "CLI output format.", Source: qoderRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "qoder/autoMemoryEnabled", Tool: "qoder", FileID: qoderSettings, Path: KeyPath{"autoMemoryEnabled"}, Type: TypeBool, Default: "false",
			Desc: "Auto-memory for sessions.", Source: qoderRef, Verified: VerifiedDoc, Owner: OwnerUser, Note: qoderRestart,
		},
		// --- UI
		{
			ID: "qoder/ui.theme", Tool: "qoder", FileID: qoderSettings, Path: KeyPath{"ui", "theme"}, Type: TypeString,
			Desc: "Colour theme name.", Source: qoderRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "qoder/ui.autoThemeSwitching", Tool: "qoder", FileID: qoderSettings, Path: KeyPath{"ui", "autoThemeSwitching"}, Type: TypeBool, Default: "true",
			Desc: "Follow the terminal's light / dark background.", Source: qoderRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "qoder/ui.customThemes", Tool: "qoder", FileID: qoderSettings, Path: KeyPath{"ui", "customThemes"}, Type: TypeObject,
			Desc: "Custom theme definitions.", Source: qoderRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "qoder/ui.inlineThinkingMode", Tool: "qoder", FileID: qoderSettings, Path: KeyPath{"ui", "inlineThinkingMode"}, Type: TypeEnum,
			Enum: []string{"off", "full"}, Default: `"full"`, Desc: "How reasoning is shown inline.", Source: qoderRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "qoder/ui.maxTurns", Tool: "qoder", FileID: qoderSettings, Path: KeyPath{"ui", "maxTurns"}, Type: TypeString,
			Enum: []string{"1000", "2000", "3000", "unlimited"}, Default: `"1000"`, Desc: "Maximum turns per session.", Source: qoderRef,
			Verified: VerifiedDoc, Owner: OwnerUser, Note: "The reference lists numeric and unlimited choices; the on-disk spelling of the numbers is not stated.",
		},
		{
			ID: "qoder/ui.hideBanner", Tool: "qoder", FileID: qoderSettings, Path: KeyPath{"ui", "hideBanner"}, Type: TypeBool, Default: "false",
			Desc: "Hide the startup banner.", Source: qoderRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "qoder/ui.hideTips", Tool: "qoder", FileID: qoderSettings, Path: KeyPath{"ui", "hideTips"}, Type: TypeBool, Default: "false",
			Desc: "Hide tips.", Source: qoderRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "qoder/ui.showLineNumbers", Tool: "qoder", FileID: qoderSettings, Path: KeyPath{"ui", "showLineNumbers"}, Type: TypeBool, Default: "true",
			Desc: "Show line numbers.", Source: qoderRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "qoder/ui.loadingPhrases", Tool: "qoder", FileID: qoderSettings, Path: KeyPath{"ui", "loadingPhrases"}, Type: TypeEnum,
			Enum: []string{"tips", "witty", "all", "off"}, Default: `"off"`, Desc: "Loading-line content.", Source: qoderRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "qoder/ui.errorVerbosity", Tool: "qoder", FileID: qoderSettings, Path: KeyPath{"ui", "errorVerbosity"}, Type: TypeEnum,
			Enum: []string{"low", "full"}, Default: `"low"`, Desc: "Error detail.", Source: qoderRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "qoder/ui.accessibility.screenReader", Tool: "qoder", FileID: qoderSettings, Path: KeyPath{"ui", "accessibility", "screenReader"}, Type: TypeBool,
			Default: "false", Desc: "Screen-reader mode.", Source: qoderRef, Verified: VerifiedDoc, Owner: OwnerUser, Note: qoderRestart,
		},
		{
			ID: "qoder/statusLine.command", Tool: "qoder", FileID: qoderSettings, Path: KeyPath{"statusLine", "command"}, Type: TypeString, Default: `""`,
			Desc: "Shell command that renders the status line.", Source: qoderRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "qoder/spellcheck.enabled", Tool: "qoder", FileID: qoderSettings, Path: KeyPath{"spellcheck", "enabled"}, Type: TypeBool, Default: "false",
			Desc: "Spell checking of the prompt.", Source: qoderRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		// --- context
		{
			ID: "qoder/context.fileName", Tool: "qoder", FileID: qoderSettings, Path: KeyPath{"context", "fileName"}, Type: TypeStringList,
			Default: `["AGENTS.md"]`, Desc: "Context (memory) file names to load: a string or a list.", Source: qoderRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "qoder/context.importFormat", Tool: "qoder", FileID: qoderSettings, Path: KeyPath{"context", "importFormat"}, Type: TypeEnum,
			Enum: []string{"tree", "flat"}, Desc: "How imported context is laid out.", Source: qoderRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "qoder/context.discoveryMaxDirs", Tool: "qoder", FileID: qoderSettings, Path: KeyPath{"context", "discoveryMaxDirs"}, Type: TypeInt, Default: "200",
			Desc: "Maximum directories searched for context files.", Source: qoderRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "qoder/context.memoryBoundaryMarkers", Tool: "qoder", FileID: qoderSettings, Path: KeyPath{"context", "memoryBoundaryMarkers"}, Type: TypeStringList,
			Default: `[".git"]`, Desc: "Markers that stop the upward context search.", Source: qoderRef, Verified: VerifiedDoc, Owner: OwnerUser, Note: qoderRestart,
		},
		{
			ID: "qoder/context.loadMemoryFromIncludeDirectories", Tool: "qoder", FileID: qoderSettings, Path: KeyPath{"context", "loadMemoryFromIncludeDirectories"},
			Type: TypeBool, Default: "false", Desc: "Load context files from trusted extra directories too.", Source: qoderRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "qoder/context.fileFiltering.respectGitIgnore", Tool: "qoder", FileID: qoderSettings, Path: KeyPath{"context", "fileFiltering", "respectGitIgnore"},
			Type: TypeBool, Default: "true", Desc: "Honour .gitignore in file search.", Source: qoderRef, Verified: VerifiedDoc, Owner: OwnerUser, Note: qoderRestart,
		},
		{
			ID: "qoder/agentsMdExcludes", Tool: "qoder", FileID: qoderSettings, Path: KeyPath{"agentsMdExcludes"}, Type: TypeStringList, Default: "[]",
			Desc: "Globs of context files to skip.", Source: qoderRef, Verified: VerifiedDoc, Owner: OwnerUser, Note: qoderRestart,
		},
		// --- tools
		{
			ID: "qoder/tools.core", Tool: "qoder", FileID: qoderSettings, Path: KeyPath{"tools", "core"}, Type: TypeStringList,
			Desc: "Allowlist of built-in tools (enablement, not approval).", Source: qoderRef, Verified: VerifiedDoc, Owner: OwnerUser, Note: qoderRestart,
		},
		{
			ID: "qoder/tools.exclude", Tool: "qoder", FileID: qoderSettings, Path: KeyPath{"tools", "exclude"}, Type: TypeStringList,
			Desc: "Tool names removed from the agent.", Source: qoderRef, Verified: VerifiedDoc, Owner: OwnerUser, Note: qoderRestart,
		},
		{
			ID: "qoder/tools.shell.inactivityTimeout", Tool: "qoder", FileID: qoderSettings, Path: KeyPath{"tools", "shell", "inactivityTimeout"}, Type: TypeInt,
			Default: "300", Desc: "Shell inactivity timeout in seconds.", Source: qoderRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "qoder/tools.useRipgrep", Tool: "qoder", FileID: qoderSettings, Path: KeyPath{"tools", "useRipgrep"}, Type: TypeBool, Default: "true",
			Desc: "Search with ripgrep.", Source: qoderRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		// --- MCP
		{
			ID: "qoder/mcpServers", Tool: "qoder", FileID: qoderSettings, Path: KeyPath{"mcpServers"}, Type: TypeMCPMap, Default: "{}",
			Desc: "MCP servers by name.", Source: qoderRef, Verified: VerifiedDoc, Owner: OwnerUser,
			Note: qoderRestart + " Entry shape is Claude-like but not documented in detail (audit V12), so there is no common-vocabulary mapping.",
		},
		{
			ID: "qoder/mcp.allowed", Tool: "qoder", FileID: qoderSettings, Path: KeyPath{"mcp", "allowed"}, Type: TypeStringList,
			Desc: "Only these MCP servers load.", Source: qoderRef, Verified: VerifiedDoc, Owner: OwnerUser, Danger: DangerRefuseWhileIntegrated,
			Note: qoderRestart + " An allow-list that omits Observer's MCP server drops it.",
		},
		{
			ID: "qoder/mcp.excluded", Tool: "qoder", FileID: qoderSettings, Path: KeyPath{"mcp", "excluded"}, Type: TypeStringList,
			Desc: "MCP servers that never load.", Source: qoderRef, Verified: VerifiedDoc, Owner: OwnerUser, Danger: DangerRefuseWhileIntegrated,
			Note: qoderRestart + " Can exclude Observer's MCP server.",
		},
		{
			ID: "qoder/mcp.enableAllProjectMcpServers", Tool: "qoder", FileID: qoderSettings, Path: KeyPath{"mcp", "enableAllProjectMcpServers"}, Type: TypeBool,
			Default: "false", Desc: "Approve every project MCP server automatically.", Source: qoderRef, Verified: VerifiedDoc, Owner: OwnerUser,
			Danger: DangerConfirm, Note: qoderRestart + " true starts any server a repo declares.",
		},
		{
			ID: "qoder/mcp.enabledProjectMcpServers", Tool: "qoder", FileID: qoderSettings, Path: KeyPath{"mcp", "enabledProjectMcpServers"}, Type: TypeStringList,
			Default: "[]", Desc: "Approved project MCP servers.", Source: qoderRef, Verified: VerifiedDoc, Owner: OwnerUser, Note: qoderRestart,
		},
		{
			ID: "qoder/mcp.lazyLoad", Tool: "qoder", FileID: qoderSettings, Path: KeyPath{"mcp", "lazyLoad"}, Type: TypeBool, Default: "false",
			Desc: "Load MCP tools lazily.", Source: qoderRef, Verified: VerifiedDoc, Owner: OwnerUser, Note: qoderRestart,
		},
		{
			ID: "qoder/enabledPlugins", Tool: "qoder", FileID: qoderSettings, Path: KeyPath{"enabledPlugins"}, Type: TypeObject, Default: "{}",
			Desc: "Plugin enablement map.", Source: qoderRef, Verified: VerifiedDoc, Owner: OwnerUser, Note: qoderRestart,
		},
		// --- security and permissions
		{
			ID: "qoder/security.folderTrust.enabled", Tool: "qoder", FileID: qoderSettings, Path: KeyPath{"security", "folderTrust", "enabled"}, Type: TypeBool,
			Default: "true", Desc: "Require folder trust.", Source: qoderRef, Verified: VerifiedDoc, Owner: OwnerUser, Danger: DangerConfirm,
			Note: qoderRestart + " false trusts every folder.",
		},
		{
			ID: "qoder/security.disableYoloMode", Tool: "qoder", FileID: qoderSettings, Path: KeyPath{"security", "disableYoloMode"}, Type: TypeBool,
			Default: "false", Desc: "Forbid bypass_permissions (YOLO).", Source: qoderRef, Verified: VerifiedDoc, Owner: OwnerUser, Danger: DangerConfirm,
			Note: qoderRestart + " Setting it back to false re-allows YOLO.",
		},
		{
			ID: "qoder/security.blockGitExtensions", Tool: "qoder", FileID: qoderSettings, Path: KeyPath{"security", "blockGitExtensions"}, Type: TypeBool,
			Default: "false", Desc: "Block extension installs from git.", Source: qoderRef, Verified: VerifiedDoc, Owner: OwnerUser, Note: qoderRestart,
		},
		{
			ID: "qoder/security.allowedExtensions", Tool: "qoder", FileID: qoderSettings, Path: KeyPath{"security", "allowedExtensions"}, Type: TypeStringList,
			Default: "[]", Desc: "Allowed extension sources.", Source: qoderRef, Verified: VerifiedDoc, Owner: OwnerUser, Note: qoderRestart,
		},
		{
			ID: "qoder/security.environmentVariableRedaction.enabled", Tool: "qoder", FileID: qoderSettings, Path: KeyPath{"security", "environmentVariableRedaction", "enabled"},
			Type: TypeBool, Default: "false", Desc: "Mask environment variables in tool output.", Source: qoderRef, Verified: VerifiedDoc, Owner: OwnerUser, Note: qoderRestart,
		},
		{
			ID: "qoder/security.enableConseca", Tool: "qoder", FileID: qoderSettings, Path: KeyPath{"security", "enableConseca"}, Type: TypeBool, Default: "false",
			Desc: "Context-aware safety checks.", Source: qoderRef, Verified: VerifiedDoc, Owner: OwnerUser, Note: qoderRestart,
		},
		{
			ID: "qoder/security.auth.selectedType", Tool: "qoder", FileID: qoderSettings, Path: KeyPath{"security", "auth", "selectedType"}, Type: TypeString,
			Desc: "Selected sign-in method.", Source: "docs/audits/adapter-config-surface-2026-10-03.md#qoder (node 1 live)", Verified: VerifiedLive,
			Owner: OwnerToolState, Note: qoderLiveNote + " Not on the settings reference; written by Qoder's sign-in flow.",
		},
		{
			ID: "qoder/permissions.allow", Tool: "qoder", FileID: qoderSettings, Path: KeyPath{"permissions", "allow"}, Type: TypeRuleList, Default: "[]",
			Desc: "Rules that run without a prompt (Bash(npm run test:*), Read(/src/**), mcp__srv__*).", Source: qoderPermsRef, Verified: VerifiedDoc,
			Owner: OwnerUser, Danger: DangerConfirm, Note: "A broad rule such as Bash or * runs everything unasked.",
		},
		{
			ID: "qoder/permissions.ask", Tool: "qoder", FileID: qoderSettings, Path: KeyPath{"permissions", "ask"}, Type: TypeRuleList, Default: "[]",
			Desc: "Rules that always ask.", Source: qoderPermsRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "qoder/permissions.deny", Tool: "qoder", FileID: qoderSettings, Path: KeyPath{"permissions", "deny"}, Type: TypeRuleList, Default: "[]",
			Desc: "Rules that never run (evaluated first).", Source: qoderPermsRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "qoder/permissions.additionalDirectories", Tool: "qoder", FileID: qoderSettings, Path: KeyPath{"permissions", "additionalDirectories"},
			Type: TypeStringList, Default: "[]", Desc: "Extra directories the agent may use.", Source: qoderRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "qoder/permissions.trustDirectories", Tool: "qoder", FileID: qoderSettings, Path: KeyPath{"permissions", "trustDirectories"}, Type: TypeStringList,
			Default: "[]", Desc: "Explicitly trusted directories.", Source: qoderRef, Verified: VerifiedLive, Owner: OwnerUser, Danger: DangerConfirm,
			Note: qoderLiveNote + " Trusting a directory loads its project settings and hooks.",
		},
		{
			ID: "qoder/allowManagedPermissionRulesOnly", Tool: "qoder", FileID: qoderSettings, Path: KeyPath{"allowManagedPermissionRulesOnly"}, Type: TypeBool,
			Default: "false", Desc: "Use only managed permission rules.", Source: qoderRef, Verified: VerifiedDoc, Owner: OwnerUser,
			Note: "No managed settings file is documented for the CLI (audit), so where the managed rules come from is UNVERIFIED.",
		},
		{
			ID: "qoder/autoMode", Tool: "qoder", FileID: qoderSettings, Path: KeyPath{"autoMode"}, Type: TypeObject, Default: "{}",
			Desc: "Rules for the auto-mode classifier.", Source: qoderRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		// --- telemetry
		{
			ID: "qoder/privacy.usageStatisticsEnabled", Tool: "qoder", FileID: qoderSettings, Path: KeyPath{"privacy", "usageStatisticsEnabled"}, Type: TypeBool,
			Default: "true", Desc: "Share usage data (false = Privacy mode).", Source: qoderRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "qoder/aiCodeStatistics.enabled", Tool: "qoder", FileID: qoderSettings, Path: KeyPath{"aiCodeStatistics", "enabled"}, Type: TypeBool,
			Default: "true", Desc: "Record AI-generated code statistics.", Source: qoderRef, Verified: VerifiedDoc, Owner: OwnerUser, Note: qoderRestart,
		},
		{
			ID: "qoder/advanced.excludedEnvVars", Tool: "qoder", FileID: qoderSettings, Path: KeyPath{"advanced", "excludedEnvVars"}, Type: TypeStringList,
			Default: `["DEBUG","DEBUG_MODE"]`, Desc: "Environment variables not passed through.", Source: qoderRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		// --- hooks
		{
			ID: "qoder/hooks", Tool: "qoder", FileID: qoderSettings, Path: KeyPath{"hooks"}, Type: TypeObject, Default: "{}",
			Desc: "Hooks by event (23 events, Claude-shaped: event -> matcher groups -> command).", Source: qoderHooksRef, Verified: VerifiedDoc, Owner: OwnerUser,
			Note: "Observer's UserPromptSubmit entry is an owned span; overwriting hooks with {} removes it.",
		},
		{
			ID: "qoder/hooks.UserPromptSubmit", Tool: "qoder", FileID: qoderSettings, Path: KeyPath{"hooks", "UserPromptSubmit"}, Type: TypeObject,
			Desc: "Matcher groups run when the user submits a prompt.", Source: qoderHooksRef, Verified: VerifiedDoc, Owner: OwnerUser,
			Note: "Holds Observer's prompt-submit entry.",
		},
		{
			ID: "qoder/hooks.PreToolUse", Tool: "qoder", FileID: qoderSettings, Path: KeyPath{"hooks", "PreToolUse"}, Type: TypeObject,
			Desc: "Matcher groups run before a tool call.", Source: qoderHooksRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "qoder/hooks.PostToolUse", Tool: "qoder", FileID: qoderSettings, Path: KeyPath{"hooks", "PostToolUse"}, Type: TypeObject,
			Desc: "Matcher groups run after a tool call.", Source: qoderHooksRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "qoder/hooks.SessionStart", Tool: "qoder", FileID: qoderSettings, Path: KeyPath{"hooks", "SessionStart"}, Type: TypeObject,
			Desc: "Matcher groups run at session start.", Source: qoderHooksRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "qoder/hooks.Stop", Tool: "qoder", FileID: qoderSettings, Path: KeyPath{"hooks", "Stop"}, Type: TypeObject,
			Desc: "Matcher groups run when the agent stops.", Source: qoderHooksRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
	},
	Map: []MapRow{
		{Key: KeyModel, Tool: "qoder", Settings: []string{"qoder/model.name"}, Transform: TransformModelAlias, Source: qoderRef},
		{Key: KeyContextLimit, Tool: "qoder", Settings: []string{"qoder/model.contextWindow"}, Transform: TransformIdentity, Source: qoderRef},
		{
			Key: KeyReasoningEffort, Tool: "qoder", Settings: []string{"qoder/model.reasoningEffort"}, Transform: TransformEnumMap,
			Params: TransformParams{Enum: []EnumPair{
				{"none", []any{"off"}},
				{"low", []any{"low"}},
				{"medium", []any{"medium"}},
				{"high", []any{"high"}},
				{"xhigh", []any{"xhigh"}},
				{"max", []any{"max"}},
				{"none", []any{"disabled"}},
				{"none", []any{"none"}},
			}}, Lossy: true, Source: qoderRef,
			Note: "none is written as off; disabled and none read back as none. minimal is refused.",
		},
		{
			Key: KeyApprovalMode, Tool: "qoder", Settings: []string{"qoder/general.defaultPermissionMode"}, Transform: TransformEnumMap,
			Params: TransformParams{Enum: []EnumPair{
				{"plan", []any{"plan"}},
				{"ask_edits", []any{"default"}},
				{"auto_edits", []any{"accept_edits"}},
				{"full_auto", []any{"bypass_permissions"}},
				{"full_auto", []any{"yolo"}},
			}}, Lossy: true, Source: qoderPermsRef,
			Note: "yolo reads back as full_auto (an alias of bypass_permissions); ask is refused; auto (model-judged) and dont_ask have no canonical form.",
		},
		{
			Key: KeyAllowedTools, Tool: "qoder", Settings: []string{"qoder/permissions.allow"}, Transform: TransformRuleTranslate,
			Params: TransformParams{Grammar: qoderRuleGrammar}, Source: qoderPermsRef, Note: "fetch and search rules take no pattern.",
		},
		{
			Key: KeyDeniedTools, Tool: "qoder", Settings: []string{"qoder/permissions.deny"}, Transform: TransformRuleTranslate,
			Params: TransformParams{Grammar: qoderRuleGrammar}, Source: qoderPermsRef,
		},
		{Key: KeyAdditionalDirs, Tool: "qoder", Settings: []string{"qoder/permissions.additionalDirectories"}, Transform: TransformIdentity, Source: qoderRef},
		{Key: KeyTelemetryOff, Tool: "qoder", Settings: []string{"qoder/privacy.usageStatisticsEnabled"}, Transform: TransformBoolInvert, Source: qoderRef},
		{Key: KeyAutoUpdate, Tool: "qoder", Settings: []string{"qoder/general.enableAutoUpdate"}, Transform: TransformIdentity, Source: qoderRef},
		{
			Key: KeyInstructionsFiles, Tool: "qoder", Settings: []string{"qoder/context.fileName"}, Transform: TransformIdentity, Source: qoderRef,
			Note: "Replaces AGENTS.md as the context file name; a single string reads back as a one-item list.",
		},
	},
}
