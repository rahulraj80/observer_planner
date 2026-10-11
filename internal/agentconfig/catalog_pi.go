package agentconfig

// pi coding agent catalogue (repo badlogic/pi-mono, now earendil-works/pi).
// Sources: packages/coding-agent/docs/{settings,models,mcp,configuration}.md
// (fetched 2026-10-03 for the audit, settings + models re-fetched
// 2026-10-04), audit section 5 "pi". Node 1 had pi installed: the keys
// defaultModel, defaultProvider and lastChangelogVersion were seen live in
// ~/.pi/agent/settings.json on 2026-10-03.
const (
	piSettingsRef = "https://raw.githubusercontent.com/earendil-works/pi/main/packages/coding-agent/docs/settings.md"
	piModelsRef   = "https://raw.githubusercontent.com/earendil-works/pi/main/packages/coding-agent/docs/models.md"
	piMCPRef      = "https://raw.githubusercontent.com/earendil-works/pi/main/packages/coding-agent/docs/mcp.md"
	piConfigRef   = "https://raw.githubusercontent.com/earendil-works/pi/main/packages/coding-agent/docs/configuration.md"
	piSettings    = "pi/settings"
	piModels      = "pi/models"
	piMCP         = "pi/mcp"
	piLiveNote    = "Seen on node 1 2026-10-03 (audit section 5 pi, live)."
	piGlobalOnly  = "Read from the agent-dir settings only (global-only key)."
)

var piUserOnly = []Scope{ScopeUser}

// piMCPShape: mcpServers.<name> {command, args, env} or {type http, url};
// the type key is optional (stdio when absent).
var piMCPShape = MCPShape{
	EnvKey: "env", TypeKey: "type",
	TypeNames: map[string]string{"stdio": "stdio", "http": "http"},
	URLKeys:   map[string]string{"http": "url"},
}

var piCatalog = toolCatalog{
	Tool:            "pi",
	VerifiedAgainst: "vendor docs fetched 2026-10-03 (audit section 5) and 2026-10-04; node 1 live key names 2026-10-03 for defaultModel, defaultProvider, lastChangelogVersion",
	Files: []FileRow{
		{
			ID: "pi/user-settings", Tool: "pi", Shape: piSettings, Kind: KindSettings, Scope: ScopeUser,
			Paths: samePath("~/.pi/agent/settings.json"), Format: FormatJSON, Precedence: 20, WriteNeeds: PrivUser,
			Grounding: live("~/.pi/agent/settings.json carries defaultModel, defaultProvider, lastChangelogVersion"),
			Note:      "PI_CODING_AGENT_DIR relocates ~/.pi/agent; the Windows path (%USERPROFILE%\\.pi\\agent) is inferred. /reload re-reads it.",
		},
		{
			ID: "pi/project-settings", Tool: "pi", Shape: piSettings, Kind: KindSettings, Scope: ScopeProjectShared,
			Paths: samePath("<project>/.pi/settings.json"), Format: FormatJSON, Precedence: 30, WriteNeeds: PrivUser,
			Grounding: doc(piConfigRef),
			Note:      "Loaded only after project trust is granted (sessionDir is read before trust). Resource lists combine with the agent-dir ones.",
		},
		{
			ID: "pi/user-models", Tool: "pi", Shape: piModels, Kind: KindSettings, Scope: ScopeUser,
			Paths: samePath("~/.pi/agent/models.json"), Format: FormatJSON, Precedence: 20, WriteNeeds: PrivUser,
			Grounding: doc(piModelsRef),
			Note: "pi's provider registry. A provider apiKey may be a literal key (or $NAME / ${NAME} / !command): those rows are secret. " +
				"Observer's launcher writes providers.observer here (owned span).",
		},
		{
			ID: "pi/user-mcp", Tool: "pi", Shape: piMCP, Kind: KindMCP, Scope: ScopeUser,
			Paths: samePath("~/.pi/agent/mcp.json"), Format: FormatJSON, Precedence: 20, WriteNeeds: PrivUser, Grounding: doc(piMCPRef),
		},
		{
			ID: "pi/project-mcp", Tool: "pi", Shape: piMCP, Kind: KindMCP, Scope: ScopeProjectShared,
			Paths: samePath("<project>/.pi/mcp.json"), Format: FormatJSON, Precedence: 30, WriteNeeds: PrivUser, Grounding: doc(piMCPRef),
			Note: "Trust-gated. An entry without command / url only overrides enabled and exposure of the same server.",
		},
		{
			ID: "pi/keybindings", Tool: "pi", Shape: "pi/keybindings", Kind: KindSettings, Scope: ScopeUser,
			Paths: samePath("~/.pi/agent/keybindings.json"), Format: FormatJSON, Precedence: 20, WriteNeeds: PrivUser, Grounding: doc(piConfigRef),
			Note: "Key bindings; not catalogued key by key.",
		},
		{
			ID: "pi/auth", Tool: "pi", Shape: "pi/auth", Kind: KindToolState, Scope: ScopeUser,
			Paths: samePath("~/.pi/agent/auth.json"), Format: FormatJSON, Precedence: 0, WriteNeeds: PrivNever, Grounding: doc(piConfigRef),
			Note: "API keys and OAuth tokens from /login: never read.",
		},
		{
			ID: "pi/trust", Tool: "pi", Shape: "pi/trust", Kind: KindToolState, Scope: ScopeUser,
			Paths: samePath("~/.pi/agent/trust.json"), Format: FormatJSON, Precedence: 0, WriteNeeds: PrivNever, Grounding: doc(piConfigRef),
			Note: "Saved project-trust decisions; writing it would trust a project (and its extensions) on the user's behalf.",
		},
		{
			ID: "pi/managed-install", Tool: "pi", Shape: "pi/managed-install", Kind: KindToolState, Scope: ScopeUser,
			Paths: samePath("~/.pi/agent/install/managed-install.json"), Format: FormatJSON, Precedence: 0, WriteNeeds: PrivNever,
			Grounding: live("~/.pi/agent/install/managed-install.json present (installer state)"),
			Note:      "Installer state, not configuration.",
		},
	},
	Extra: []SettingRow{
		// --- model and thinking
		{
			ID: "pi/defaultProvider", Tool: "pi", FileID: piSettings, Path: KeyPath{"defaultProvider"}, Type: TypeString,
			Desc: "Provider selected at startup.", Source: piSettingsRef, Verified: VerifiedLive, Owner: OwnerUser, Note: piLiveNote,
		},
		{
			ID: "pi/defaultModel", Tool: "pi", FileID: piSettings, Path: KeyPath{"defaultModel"}, Type: TypeString,
			Desc: "Model id selected at startup (within defaultProvider).", Source: piSettingsRef, Verified: VerifiedLive, Owner: OwnerUser,
			Note: piLiveNote + " Ctrl+S in /model saves it; --model overrides it.",
		},
		{
			ID: "pi/lastChangelogVersion", Tool: "pi", FileID: piSettings, Path: KeyPath{"lastChangelogVersion"}, Type: TypeString,
			Desc: "Last version whose changelog was shown.", Source: piSettingsRef, Verified: VerifiedLive, Owner: OwnerToolState,
			Note: piLiveNote + " Maintained by pi.",
		},
		{
			ID: "pi/defaultThinkingLevel", Tool: "pi", FileID: piSettings, Path: KeyPath{"defaultThinkingLevel"}, Type: TypeEnum,
			Enum: []string{"off", "minimal", "low", "medium", "high", "xhigh", "max"}, Default: `"medium"`,
			Desc: "Reasoning level at launch.", Source: piSettingsRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "pi/modelThinkingLevels", Tool: "pi", FileID: piSettings, Path: KeyPath{"modelThinkingLevels"}, Type: TypeStringMap,
			Desc: "Per-model reasoning level, keyed provider/modelId.", Source: piSettingsRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "pi/thinkingBudgets", Tool: "pi", FileID: piSettings, Path: KeyPath{"thinkingBudgets"}, Type: TypeObject,
			Desc: "Token budget per reasoning level.", Source: piSettingsRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "pi/enabledModels", Tool: "pi", FileID: piSettings, Path: KeyPath{"enabledModels"}, Type: TypeStringList,
			Desc: "Model patterns offered for selection and cycling.", Source: piSettingsRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "pi/hideThinkingBlock", Tool: "pi", FileID: piSettings, Path: KeyPath{"hideThinkingBlock"}, Type: TypeBool, Default: "false",
			Desc: "Hide reasoning blocks.", Source: piSettingsRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "pi/cacheWarming", Tool: "pi", FileID: piSettings, Path: KeyPath{"cacheWarming"}, Scopes: piUserOnly, Type: TypeEnum,
			Enum: []string{"off", "streaming", "idle"}, Default: `"streaming"`, Desc: "Keep the provider prompt cache warm.",
			Source: piSettingsRef, Verified: VerifiedDoc, Owner: OwnerUser, Note: piGlobalOnly,
		},
		{
			ID: "pi/showCacheMissNotices", Tool: "pi", FileID: piSettings, Path: KeyPath{"showCacheMissNotices"}, Type: TypeBool, Default: "false",
			Desc: "Show prompt-cache miss notices.", Source: piSettingsRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		// --- tools, trust, resources
		{
			ID: "pi/defaultTools", Tool: "pi", FileID: piSettings, Path: KeyPath{"defaultTools"}, Type: TypeStringList,
			Desc:   "Tools active at startup (read, bash, powershell, edit, write, grep, find, ls, codemode, tool_search); +name / -name modify the inherited set.",
			Source: piSettingsRef, Verified: VerifiedDoc, Owner: OwnerUser, Danger: DangerConfirm,
			Note: "pi has no per-call approval gate: adding bash or powershell lets the agent run commands unasked.",
		},
		{
			ID: "pi/defaultProjectTrust", Tool: "pi", FileID: piSettings, Path: KeyPath{"defaultProjectTrust"}, Scopes: piUserOnly, Type: TypeEnum,
			Enum: []string{"ask", "always", "never"}, Default: `"ask"`, Desc: "Trust decision for a project with no saved decision.",
			Source: piSettingsRef, Verified: VerifiedDoc, Owner: OwnerUser, Danger: DangerConfirm,
			Note: piGlobalOnly + " always auto-loads project extensions (arbitrary code).",
		},
		{
			ID: "pi/extensions", Tool: "pi", FileID: piSettings, Path: KeyPath{"extensions"}, Type: TypeStringList,
			Desc:   "Extension paths / globs; -builtin:<name> disables a built-in (mcp, llama.cpp, codemode, tool-search).",
			Source: piSettingsRef, Verified: VerifiedDoc, Owner: OwnerUser, Danger: DangerRefuseWhileIntegrated,
			Note: "-builtin:mcp switches off every MCP server, Observer's included; extensions run in-process (the hook surface).",
		},
		{
			ID: "pi/packages", Tool: "pi", FileID: piSettings, Path: KeyPath{"packages"}, Type: TypeObject,
			Desc:   "External packages (npm, git or local sources) that bring extensions, skills, prompts and themes.",
			Source: piSettingsRef, Verified: VerifiedDoc, Owner: OwnerUser, Danger: DangerConfirm, Note: "A package can ship executable extensions.",
		},
		{
			ID: "pi/skills", Tool: "pi", FileID: piSettings, Path: KeyPath{"skills"}, Type: TypeStringList,
			Desc: "Extra skill paths / globs.", Source: piSettingsRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "pi/prompts", Tool: "pi", FileID: piSettings, Path: KeyPath{"prompts"}, Type: TypeStringList,
			Desc: "Extra prompt-template paths / globs.", Source: piSettingsRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "pi/themes", Tool: "pi", FileID: piSettings, Path: KeyPath{"themes"}, Type: TypeStringList,
			Desc: "Extra theme paths / globs.", Source: piSettingsRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "pi/enableSkillCommands", Tool: "pi", FileID: piSettings, Path: KeyPath{"enableSkillCommands"}, Type: TypeBool, Default: "true",
			Desc: "Register /skill:name commands.", Source: piSettingsRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "pi/sessionDir", Tool: "pi", FileID: piSettings, Path: KeyPath{"sessionDir"}, Type: TypeString,
			Desc: "Where sessions are stored.", Source: piSettingsRef, Verified: VerifiedDoc, Owner: OwnerUser,
			Note: "Read before project trust; moving it moves the transcripts Observer captures.",
		},
		{
			ID: "pi/codemode.mode", Tool: "pi", FileID: piSettings, Path: KeyPath{"codemode", "mode"}, Type: TypeEnum,
			Enum: []string{"on", "only"}, Default: `"on"`, Desc: "How the codemode tool is presented.", Source: piSettingsRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "pi/codemode.inlineBudget", Tool: "pi", FileID: piSettings, Path: KeyPath{"codemode", "inlineBudget"}, Type: TypeInt, Min: f64(0),
			Default: "3000", Desc: "Tokens spent on inline tool declarations.", Source: piSettingsRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		// --- compaction
		{
			ID: "pi/compaction.enabled", Tool: "pi", FileID: piSettings, Path: KeyPath{"compaction", "enabled"}, Type: TypeBool, Default: "true",
			Desc: "Compact the conversation automatically.", Source: piSettingsRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "pi/compaction.reserveTokens", Tool: "pi", FileID: piSettings, Path: KeyPath{"compaction", "reserveTokens"}, Type: TypeInt, Min: f64(0),
			Default: "16384", Desc: "Tokens reserved for the response; compaction triggers when the context would eat into them.",
			Source: piSettingsRef, Verified: VerifiedDoc, Owner: OwnerUser, Note: "Token-based; there is no percent threshold.",
		},
		{
			ID: "pi/compaction.keepRecentTokens", Tool: "pi", FileID: piSettings, Path: KeyPath{"compaction", "keepRecentTokens"}, Type: TypeInt, Min: f64(0),
			Default: "20000", Desc: "Recent tokens kept verbatim through compaction.", Source: piSettingsRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "pi/compaction.modelOverrides", Tool: "pi", FileID: piSettings, Path: KeyPath{"compaction", "modelOverrides"}, Type: TypeObject,
			Desc: "Per-model compaction settings, keyed provider/modelId.", Source: piSettingsRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "pi/branchSummary.reserveTokens", Tool: "pi", FileID: piSettings, Path: KeyPath{"branchSummary", "reserveTokens"}, Type: TypeInt, Min: f64(0),
			Default: "16384", Desc: "Tokens reserved when summarising a branch.", Source: piSettingsRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "pi/branchSummary.skipPrompt", Tool: "pi", FileID: piSettings, Path: KeyPath{"branchSummary", "skipPrompt"}, Type: TypeBool, Default: "false",
			Desc: "Skip the branch summary prompt.", Source: piSettingsRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		// --- UI
		{
			ID: "pi/theme", Tool: "pi", FileID: piSettings, Path: KeyPath{"theme"}, Type: TypeString, Default: `"system"`,
			Desc: "Colour theme name.", Source: piSettingsRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "pi/quietStartup", Tool: "pi", FileID: piSettings, Path: KeyPath{"quietStartup"}, Type: TypeString,
			Enum: []string{"true", "false", "header"}, Desc: "Reduce startup output (a bool or \"header\").",
			Source: piSettingsRef, Verified: VerifiedDoc, Owner: OwnerUser, Note: "A JSON bool or the string header; default false.",
		},
		{
			ID: "pi/tuiMode", Tool: "pi", FileID: piSettings, Path: KeyPath{"tuiMode"}, Type: TypeEnum,
			Enum: []string{"regular", "fullscreen"}, Default: `"fullscreen"`, Desc: "Terminal UI mode.", Source: piSettingsRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "pi/steeringMode", Tool: "pi", FileID: piSettings, Path: KeyPath{"steeringMode"}, Type: TypeEnum,
			Enum: []string{"all", "one-at-a-time"}, Default: `"one-at-a-time"`, Desc: "How queued steering messages are delivered.",
			Source: piSettingsRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "pi/followUpMode", Tool: "pi", FileID: piSettings, Path: KeyPath{"followUpMode"}, Type: TypeEnum,
			Enum: []string{"all", "one-at-a-time"}, Default: `"one-at-a-time"`, Desc: "How queued follow-up messages are delivered.",
			Source: piSettingsRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "pi/doubleEscapeAction", Tool: "pi", FileID: piSettings, Path: KeyPath{"doubleEscapeAction"}, Type: TypeEnum,
			Enum: []string{"tree", "fork", "none"}, Default: `"tree"`, Desc: "What a double Escape does.", Source: piSettingsRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "pi/treeFilterMode", Tool: "pi", FileID: piSettings, Path: KeyPath{"treeFilterMode"}, Type: TypeEnum,
			Enum: []string{"default", "no-tools", "user-only", "labeled-only", "all"}, Default: `"default"`, Desc: "Initial /tree filter.",
			Source: piSettingsRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "pi/externalEditor", Tool: "pi", FileID: piSettings, Path: KeyPath{"externalEditor"}, Type: TypeString,
			Desc: "External editor command ($VISUAL / $EDITOR when unset).", Source: piSettingsRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "pi/autocompleteMaxVisible", Tool: "pi", FileID: piSettings, Path: KeyPath{"autocompleteMaxVisible"}, Type: TypeInt,
			Min: f64(3), Max: f64(20), Default: "5", Desc: "Autocomplete entries shown.", Source: piSettingsRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "pi/collapseChangelog", Tool: "pi", FileID: piSettings, Path: KeyPath{"collapseChangelog"}, Type: TypeBool, Default: "false",
			Desc: "Show a condensed changelog after updates.", Source: piSettingsRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "pi/markdown.mermaid", Tool: "pi", FileID: piSettings, Path: KeyPath{"markdown", "mermaid"}, Type: TypeEnum,
			Enum: []string{"off", "final", "streaming"}, Default: `"streaming"`, Desc: "Mermaid diagram rendering.", Source: piSettingsRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "pi/terminal.showImages", Tool: "pi", FileID: piSettings, Path: KeyPath{"terminal", "showImages"}, Type: TypeBool, Default: "true",
			Desc: "Render inline images.", Source: piSettingsRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "pi/images.blockImages", Tool: "pi", FileID: piSettings, Path: KeyPath{"images", "blockImages"}, Type: TypeBool, Default: "false",
			Desc: "Keep images away from the model.", Source: piSettingsRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		// --- network, shell
		{
			ID: "pi/transport", Tool: "pi", FileID: piSettings, Path: KeyPath{"transport"}, Type: TypeEnum,
			Enum: []string{"auto", "sse", "websocket", "websocket-cached"}, Default: `"auto"`, Desc: "Transport to the AI provider.",
			Source: piSettingsRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "pi/httpProxy", Tool: "pi", FileID: piSettings, Path: KeyPath{"httpProxy"}, Scopes: piUserOnly, Type: TypeString,
			Desc: "HTTP proxy for provider traffic.", Source: piSettingsRef, Verified: VerifiedDoc, Owner: OwnerUser, Danger: DangerConfirm,
			Note: piGlobalOnly + " Redirects all provider traffic.",
		},
		{
			ID: "pi/httpIdleTimeoutMs", Tool: "pi", FileID: piSettings, Path: KeyPath{"httpIdleTimeoutMs"}, Type: TypeInt, Min: f64(0),
			Default: "300000", Desc: "HTTP idle timeout in milliseconds.", Source: piSettingsRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "pi/retry.enabled", Tool: "pi", FileID: piSettings, Path: KeyPath{"retry", "enabled"}, Type: TypeBool, Default: "true",
			Desc: "Retry transient provider errors.", Source: piSettingsRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "pi/retry.maxRetries", Tool: "pi", FileID: piSettings, Path: KeyPath{"retry", "maxRetries"}, Type: TypeInt, Min: f64(0), Default: "3",
			Desc: "Maximum retry attempts.", Source: piSettingsRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "pi/shellPath", Tool: "pi", FileID: piSettings, Path: KeyPath{"shellPath"}, Type: TypeString,
			Desc: "Shell executable for commands.", Source: piSettingsRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "pi/shellCommandPrefix", Tool: "pi", FileID: piSettings, Path: KeyPath{"shellCommandPrefix"}, Type: TypeString,
			Desc: "Text prepended to every shell command.", Source: piSettingsRef, Verified: VerifiedDoc, Owner: OwnerUser, Danger: DangerConfirm,
			Note: "Rewrites every command the agent runs.",
		},
		{
			ID: "pi/npmCommand", Tool: "pi", FileID: piSettings, Path: KeyPath{"npmCommand"}, Type: TypeStringList,
			Desc: "npm command and arguments used to install packages.", Source: piSettingsRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		// --- telemetry
		{
			ID: "pi/enableInstallTelemetry", Tool: "pi", FileID: piSettings, Path: KeyPath{"enableInstallTelemetry"}, Type: TypeBool, Default: "true",
			Desc: "Report installs and updates.", Source: piSettingsRef, Verified: VerifiedDoc, Owner: OwnerUser,
			Note: "PI_TELEMETRY=0 also turns it off; the version check is separate (PI_SKIP_VERSION_CHECK, PI_OFFLINE).",
		},
		{
			ID: "pi/enableAnalytics", Tool: "pi", FileID: piSettings, Path: KeyPath{"enableAnalytics"}, Type: TypeBool, Default: "false",
			Desc: "Opt in to analytics sharing.", Source: piSettingsRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "pi/warnings.anthropicExtraUsage", Tool: "pi", FileID: piSettings, Path: KeyPath{"warnings", "anthropicExtraUsage"}, Type: TypeBool,
			Default: "true", Desc: "Warn before paid extra usage on Anthropic.", Source: piSettingsRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		// --- models.json
		{
			ID: "pi/providers", Tool: "pi", FileID: piModels, Path: KeyPath{"providers"}, Type: TypeObject,
			Desc:   "Custom providers by id: baseUrl, api (openai-completions, ...), apiKey, headers, models[] (id, name, contextWindow, maxTokens, input, reasoning, cost).",
			Source: piModelsRef, Verified: VerifiedDoc, Owner: OwnerUser, Secret: true, Danger: DangerConfirm,
			Note: "apiKey and header values may be literal credentials ($NAME / ${NAME} / !command are the safe forms), so the map is secret. " +
				"A baseUrl that is not Observer bypasses the proxy; Observer's providers.observer entry is an owned span.",
		},
		{
			ID: "pi/modelOverrides", Tool: "pi", FileID: piModels, Path: KeyPath{"modelOverrides"}, Type: TypeObject,
			Desc: "Metadata overrides for built-in models.", Source: piModelsRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		// --- mcp.json
		{
			ID: "pi/mcpServers", Tool: "pi", FileID: piMCP, Path: KeyPath{"mcpServers"}, Type: TypeMCPMap,
			Desc:   "MCP servers: command / args / env / cwd or url / headers / oauth, type stdio | http | streamable-http, enabled, exposure.",
			Source: piMCPRef, Verified: VerifiedDoc, Owner: OwnerUser, Note: "pi mcp add [--local] edits it.",
		},
	},
	// No model map row: pi splits the model across defaultProvider + defaultModel,
	// which one model_alias write cannot express cleanly.
	Map: []MapRow{
		{
			Key: KeyReasoningEffort, Tool: "pi", Settings: []string{"pi/defaultThinkingLevel"}, Transform: TransformEnumMap,
			Params: TransformParams{Enum: []EnumPair{
				{"none", []any{"off"}},
				{"minimal", []any{"minimal"}},
				{"low", []any{"low"}},
				{"medium", []any{"medium"}},
				{"high", []any{"high"}},
				{"xhigh", []any{"xhigh"}},
				{"max", []any{"max"}},
			}}, Source: piSettingsRef, Note: "pi spells none as off.",
		},
		{
			Key: KeyTelemetryOff, Tool: "pi", Settings: []string{"pi/enableInstallTelemetry"}, Transform: TransformBoolInvert, Source: piSettingsRef,
			Note: "Install / update reporting; enableAnalytics is a separate opt-in.",
		},
		{
			Key: KeyMCPServers, Tool: "pi", Settings: []string{"pi/mcpServers"}, Transform: TransformMCPShape,
			Params: TransformParams{MCP: piMCPShape}, Lossy: true, Source: piMCPRef, Note: mcpLossyNote,
		},
	},
}
