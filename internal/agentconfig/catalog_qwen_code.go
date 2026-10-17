package agentconfig

// Qwen Code (QwenLM/qwen-code, a gemini-cli fork) doc-authored catalogue.
// Sources: the vendor's own docs on main (docs/users/configuration/
// settings.md, model-providers.md, auth.md, docs/users/features/hooks.md),
// fetched 2026-10-04, plus audit section 5 "qwen-code" and the node-1 live
// key names of 2026-10-03. No published schema (schemas/settings.schema.json
// is a 404 on main).
const (
	qwenCodeRef       = "https://raw.githubusercontent.com/QwenLM/qwen-code/main/docs/users/configuration/settings.md"
	qwenCodeProviders = "https://raw.githubusercontent.com/QwenLM/qwen-code/main/docs/users/configuration/model-providers.md"
	qwenCodeHooksRef  = "https://raw.githubusercontent.com/QwenLM/qwen-code/main/docs/users/features/hooks.md"
	qwenCodeShape     = "qwen-code/settings"
	qwenCodeLive      = "Seen on node 1 (2026-10-03, audit section 5 qwen-code)."
)

// qwenCodeOperatorScopes is where Qwen reads an operator-only key: User,
// System and SystemDefaults, never the workspace file.
var qwenCodeOperatorScopes = []Scope{ScopeSystem, ScopeUser, ScopeManaged}

// qwenCodeRow fills the tool, shape, owner, verification and id of one row.
func qwenCodeRow(r SettingRow) SettingRow {
	r.Tool = "qwen-code"
	if r.FileID == "" {
		r.FileID = qwenCodeShape
	}
	if r.Owner == "" {
		r.Owner = OwnerUser
	}
	if r.Verified == "" {
		r.Verified = VerifiedDoc
	}
	if r.Source == "" {
		r.Source = qwenCodeRef
	}
	r.ID = r.Tool + "/" + r.Path.String()
	return r
}

// qwenCodeRuleGrammar is Qwen's permissions rule spelling: "Tool" or
// "Tool(specifier)" with Claude-style aliases (Bash, Read, Edit, Write,
// WebFetch). The documented MCP form is server-level only ("mcp__server"),
// so mcp rules are refused rather than guessing a tool-level spelling.
var qwenCodeRuleGrammar = RuleGrammar{
	"shell": {Name: "Bash", Style: RuleCall},
	"read":  {Name: "Read", Style: RuleCall},
	"edit":  {Name: "Edit", Style: RuleCall},
	"write": {Name: "Write", Style: RuleCall},
	"fetch": {Name: "WebFetch", Style: RuleCall},
}

var qwenCodeCatalog = toolCatalog{
	Tool:            "qwen-code",
	VerifiedAgainst: "vendor docs on QwenLM/qwen-code main fetched 2026-10-04 (settings.md, model-providers.md, hooks.md; audit section 5 qwen-code); node 1 live key names 2026-10-03 for model.name, model.baseUrl, modelProviders, security.auth.selectedType, tools.webSearch.model, env",
	Files: []FileRow{
		{
			ID: "qwen-code/system-defaults", Tool: "qwen-code", Shape: qwenCodeShape, Kind: KindSettings, Scope: ScopeSystem,
			Paths: OSPaths{
				Linux:   "/etc/qwen-code/system-defaults.json",
				Darwin:  "/Library/Application Support/QwenCode/system-defaults.json",
				Windows: `C:\ProgramData\qwen-code\system-defaults.json`,
			},
			Format: FormatJSONC, Precedence: 10, WriteNeeds: PrivAdminRoot, Grounding: doc(qwenCodeRef),
			Note: "Lowest file layer; QWEN_CODE_SYSTEM_DEFAULTS_PATH relocates it. Comments are tolerated by the gemini-cli lineage loader (not restated in Qwen's docs).",
		},
		{
			ID: "qwen-code/user-settings", Tool: "qwen-code", Shape: qwenCodeShape, Kind: KindSettings, Scope: ScopeUser,
			Paths: samePath("~/.qwen/settings.json"), Format: FormatJSONC, Precedence: 20, WriteNeeds: PrivUser,
			Grounding: live("~/.qwen/settings.json carries model, modelProviders, providerMetadata, security.auth, tools.webSearch, ui and an env block"),
			Note: "QWEN_HOME relocates ~/.qwen. Observer writes hooks here and its proxy route writes model.baseUrl plus the openai modelProviders entries. " +
				"On node 1 the env block held a provider-key-named entry with a literal value: the secret posture applies to existing files, and no value is ever read or copied. ~/.qwen/.env is a separate dotenv file, not catalogued.",
		},
		{
			ID: "qwen-code/project-settings", Tool: "qwen-code", Shape: qwenCodeShape, Kind: KindSettings, Scope: ScopeProjectShared,
			Paths: samePath("<project>/.qwen/settings.json"), Format: FormatJSONC, Precedence: 30, WriteNeeds: PrivUser,
			Grounding: doc(qwenCodeRef), Note: "Operator-only keys (tools.executionSandbox, security.allowPrivateNetworkHooks, review policy) are ignored here.",
		},
		{
			ID: "qwen-code/system-settings", Tool: "qwen-code", Shape: qwenCodeShape, Kind: KindSettings, Scope: ScopeManaged,
			Paths: OSPaths{
				Linux:   "/etc/qwen-code/settings.json",
				Darwin:  "/Library/Application Support/QwenCode/settings.json",
				Windows: `C:\ProgramData\qwen-code\settings.json`,
			},
			Format: FormatJSONC, Precedence: 100, WriteNeeds: PrivAdminRoot, Grounding: doc(qwenCodeRef),
			Note: "System settings override user and project files (only env vars and CLI flags rank higher). QWEN_CODE_SYSTEM_SETTINGS_PATH is user-settable, so this is lever B, not a lock.",
		},
	},
	Extra: []SettingRow{
		// --- general
		qwenCodeRow(SettingRow{Path: KeyPath{"general", "preferredEditor"}, Type: TypeString, Desc: "Editor used to open files."}),
		qwenCodeRow(SettingRow{Path: KeyPath{"general", "outputStyle"}, Type: TypeString, Desc: "Named output style that shapes how responses are written."}),
		qwenCodeRow(SettingRow{Path: KeyPath{"general", "vimMode"}, Type: TypeBool, Default: "false", Desc: "Vim keybindings in the prompt."}),
		qwenCodeRow(SettingRow{
			Path: KeyPath{"general", "enableAutoUpdate"}, Type: TypeBool, Default: "true", Desc: "Check for and install updates on startup.",
			Note: "Replaces the legacy disableAutoUpdate + disableUpdateNag pair (migrated with inversion).",
		}),
		qwenCodeRow(SettingRow{Path: KeyPath{"general", "gitCoAuthor", "commit"}, Type: TypeBool, Default: "true", Desc: "Add a Co-authored-by trailer and an attribution note to commits."}),
		qwenCodeRow(SettingRow{Path: KeyPath{"general", "gitCoAuthor", "pr"}, Type: TypeBool, Default: "true", Desc: "Append an attribution line to pull request descriptions."}),
		qwenCodeRow(SettingRow{
			Path: KeyPath{"general", "defaultFileEncoding"}, Type: TypeEnum, Enum: []string{"utf-8", "utf-8-bom"}, Default: "\"utf-8\"",
			Desc: "Encoding for new files.",
		}),
		qwenCodeRow(SettingRow{Path: KeyPath{"general", "cleanupPeriodDays"}, Type: TypeInt, Default: "30", Desc: "Days to keep file-history backups and session debug logs."}),
		qwenCodeRow(SettingRow{Path: KeyPath{"general", "language"}, Type: TypeString, Default: "\"auto\"", Desc: "UI language (auto or a language code)."}),
		qwenCodeRow(SettingRow{Path: KeyPath{"general", "outputLanguage"}, Type: TypeString, Default: "\"auto\"", Desc: "Language for model output."}),
		qwenCodeRow(SettingRow{Path: KeyPath{"general", "terminalBell"}, Type: TypeBool, Default: "true", Desc: "Ring the terminal bell when a response completes or needs approval."}),
		qwenCodeRow(SettingRow{Path: KeyPath{"general", "preventSystemSleep"}, Type: TypeBool, Default: "true", Desc: "Keep the system awake while streaming or running tools."}),
		qwenCodeRow(SettingRow{
			Path: KeyPath{"general", "chatRecording"}, Type: TypeBool, Default: "true", Desc: "Save chat history to disk (needed by --continue / --resume).",
			Danger: DangerRefuseWhileIntegrated,
			Note:   "false stops the on-disk session recording; Observer's qwen-code adapter captures from Qwen's session JSONL, so turning it off would stop capture (inference from the adapter's source, not stated by the vendor).",
		}),
		// --- output / ui / ide / privacy
		qwenCodeRow(SettingRow{Path: KeyPath{"output", "format"}, Type: TypeString, Default: "\"text\"", Desc: "CLI output format (text, json, stream-json)."}),
		qwenCodeRow(SettingRow{Path: KeyPath{"ui", "theme"}, Type: TypeString, Default: "\"Qwen Dark\"", Desc: "Named color theme."}),
		qwenCodeRow(SettingRow{Path: KeyPath{"ui", "customThemes"}, Type: TypeObject, Desc: "Custom theme definitions."}),
		qwenCodeRow(SettingRow{
			Path: KeyPath{"ui", "statusLine"}, Type: TypeObject, Desc: "Custom status line (command, refreshInterval, ...).",
			Danger: DangerConfirm, Note: "statusLine.command runs an arbitrary shell command on every refresh.",
		}),
		qwenCodeRow(SettingRow{Path: KeyPath{"ui", "hideWindowTitle"}, Type: TypeBool, Default: "false", Desc: "Hide the window title bar."}),
		qwenCodeRow(SettingRow{Path: KeyPath{"ui", "hideTips"}, Type: TypeBool, Default: "false", Desc: "Hide startup and post-response tips."}),
		qwenCodeRow(SettingRow{Path: KeyPath{"ui", "hideBanner"}, Type: TypeBool, Default: "false", Desc: "Hide the startup logo and info panel."}),
		qwenCodeRow(SettingRow{Path: KeyPath{"ui", "showLineNumbers"}, Type: TypeBool, Default: "true", Desc: "Line numbers in code blocks."}),
		qwenCodeRow(SettingRow{Path: KeyPath{"ui", "showCitations"}, Type: TypeBool, Default: "false", Desc: "Show citations for generated text."}),
		qwenCodeRow(SettingRow{Path: KeyPath{"ui", "accessibility", "screenReader"}, Type: TypeBool, Default: "false", Desc: "Screen reader friendly TUI."}),
		qwenCodeRow(SettingRow{Path: KeyPath{"ui", "accessibility", "enableLoadingPhrases"}, Type: TypeBool, Default: "true", Desc: "Show loading phrases."}),
		qwenCodeRow(SettingRow{Path: KeyPath{"ui", "enableUserFeedback"}, Type: TypeBool, Default: "true", Desc: "Show the optional post-conversation feedback dialog."}),
		qwenCodeRow(SettingRow{Path: KeyPath{"ide", "enabled"}, Type: TypeBool, Default: "false", Desc: "IDE integration mode."}),
		qwenCodeRow(SettingRow{
			Path: KeyPath{"ide", "hasSeenNudge"}, Type: TypeBool, Default: "false", Desc: "Whether the IDE integration nudge was shown.",
			Owner: OwnerToolState,
		}),
		qwenCodeRow(SettingRow{
			Path: KeyPath{"privacy", "usageStatisticsEnabled"}, Type: TypeBool, Default: "true", Desc: "Send usage statistics.",
			Note: "QWEN_USAGE_STATISTICS_ENABLED overrides it. telemetry.enabled is the separate OpenTelemetry export.",
		}),
		// --- model
		qwenCodeRow(SettingRow{
			Path: KeyPath{"model", "name"}, Type: TypeString, Desc: "Model for conversations.", Verified: VerifiedLive,
			Note: qwenCodeLive + " Must match a modelProviders entry id when providers are defined; OPENAI_MODEL (alias QWEN_MODEL) overrides it.",
		}),
		qwenCodeRow(SettingRow{
			Path: KeyPath{"model", "reasoningEffort"}, Type: TypeEnum, Enum: []string{"low", "medium", "high", "xhigh", "max"},
			Desc: "Reasoning effort across providers (clamped per provider).",
		}),
		qwenCodeRow(SettingRow{
			Path: KeyPath{"model", "baseUrl"}, Type: TypeString, Desc: "Base URL the model picker persists to tell same-id provider entries apart.",
			Verified: VerifiedLive, Danger: DangerConfirm,
			Note: qwenCodeLive + " Not meant to be hand-set; a stale value redirects traffic off Observer's proxy.",
		}),
		qwenCodeRow(SettingRow{Path: KeyPath{"model", "sessionTokenLimit"}, Type: TypeInt, Default: "-1", Desc: "Prompt token ceiling per session (-1 unlimited)."}),
		qwenCodeRow(SettingRow{Path: KeyPath{"model", "maxSessionTurns"}, Type: TypeInt, Default: "-1", Desc: "Turns kept in a session (-1 unlimited)."}),
		qwenCodeRow(SettingRow{Path: KeyPath{"model", "maxToolCallsPerTurn"}, Type: TypeInt, Default: "100", Desc: "Per-turn tool-call cap."}),
		qwenCodeRow(SettingRow{
			Path: KeyPath{"model", "generationConfig"}, Type: TypeObject, Desc: "Advanced content-generator overrides (timeout, retries, customHeaders, extra_body, samplingParams, ...).",
			Note: "For a model defined in modelProviders, set generationConfig inside that provider entry instead.",
		}),
		qwenCodeRow(SettingRow{Path: KeyPath{"model", "generationConfig", "contextWindowSize"}, Type: TypeInt, Desc: "Context window in tokens.", Source: qwenCodeRef}),
		qwenCodeRow(SettingRow{
			Path: KeyPath{"model", "generationConfig", "samplingParams", "max_tokens"}, Type: TypeInt, Desc: "Fixed output-token limit per response.",
			Note: "Wins over QWEN_CODE_MAX_OUTPUT_TOKENS; an explicit limit disables automatic output escalation.",
		}),
		qwenCodeRow(SettingRow{Path: KeyPath{"model", "generationConfig", "samplingParams", "temperature"}, Type: TypeFloat, Desc: "Sampling temperature."}),
		qwenCodeRow(SettingRow{Path: KeyPath{"model", "skipNextSpeakerCheck"}, Type: TypeBool, Default: "true", Desc: "Skip the next-speaker check."}),
		qwenCodeRow(SettingRow{Path: KeyPath{"model", "skipLoopDetection"}, Type: TypeBool, Default: "true", Desc: "Skip streaming loop detection."}),
		qwenCodeRow(SettingRow{
			Path: KeyPath{"model", "enableOpenAILogging"}, Type: TypeBool, Default: "false", Desc: "Log OpenAI API requests and responses to disk.",
			Note: "The logs hold full request and response bodies.",
		}),
		qwenCodeRow(SettingRow{Path: KeyPath{"model", "openAILoggingDir"}, Type: TypeString, Desc: "Directory for the OpenAI API logs."}),
		qwenCodeRow(SettingRow{Path: KeyPath{"fastModel"}, Type: TypeString, Default: "\"\"", Desc: "Model for prompt suggestions and speculative execution (empty = main model)."}),
		qwenCodeRow(SettingRow{Path: KeyPath{"advisorModel"}, Type: TypeString, Default: "\"\"", Desc: "Model used by /advisor second-opinion reviews."}),
		qwenCodeRow(SettingRow{Path: KeyPath{"visionModel"}, Type: TypeString, Default: "\"\"", Desc: "Image-capable model bridged in for text-only main models."}),
		qwenCodeRow(SettingRow{Path: KeyPath{"compactionModel"}, Type: TypeString, Default: "\"\"", Desc: "Model for auto-compaction (empty = main model)."}),
		qwenCodeRow(SettingRow{Path: KeyPath{"imageModel"}, Type: TypeString, Default: "\"\"", Desc: "Model for the image_gen tool."}),
		qwenCodeRow(SettingRow{Path: KeyPath{"modelFallbacks"}, Type: TypeString, Default: "\"\"", Desc: "Comma-separated fallback model ids (max 3) on capacity errors."}),
		qwenCodeRow(SettingRow{
			Path: KeyPath{"modelProviders"}, Type: TypeObject, Source: qwenCodeProviders, Verified: VerifiedLive,
			Desc:   "Models per provider id: arrays of {id, name, baseUrl, envKey, generationConfig, ...}.",
			Danger: DangerConfirm,
			Note: qwenCodeLive + " envKey names the variable holding the key (a reference, never the key). A baseUrl that is not Observer's proxy takes the session off the proxy; " +
				"Observer's route retargets only the openai-lane entries it recognises (owned span).",
		}),
		qwenCodeRow(SettingRow{
			Path: KeyPath{"providerProtocol"}, Type: TypeStringMap, Source: qwenCodeProviders,
			Desc: "Maps a custom provider id to a built-in protocol (openai, anthropic, gemini, vertex-ai).",
		}),
		// --- context
		qwenCodeRow(SettingRow{
			Path: KeyPath{"context", "fileName"}, Type: TypeStringList, Desc: "Context file name(s) to load instead of QWEN.md.",
			Note: "Accepts a single string or an array.",
		}),
		qwenCodeRow(SettingRow{
			Path: KeyPath{"context", "autoCompactThreshold"}, Type: TypeFloat, Min: f64(0), ExclusiveMin: true, Max: f64(1),
			Desc: "Fraction of the context window at which auto-compaction triggers.",
			Note: "Unset uses an internal 0.85. Replaces the removed model.chatCompression.contextPercentageThreshold.",
		}),
		qwenCodeRow(SettingRow{Path: KeyPath{"context", "importFormat"}, Type: TypeString, Desc: "Format used when importing memory."}),
		qwenCodeRow(SettingRow{Path: KeyPath{"context", "includeDirectories"}, Type: TypeStringList, Default: "[]", Desc: "Extra directories included in the workspace context."}),
		qwenCodeRow(SettingRow{Path: KeyPath{"context", "loadFromIncludeDirectories"}, Type: TypeBool, Default: "false", Desc: "Load QWEN.md files from the include directories on /memory refresh."}),
		qwenCodeRow(SettingRow{Path: KeyPath{"context", "fileFiltering", "respectGitIgnore"}, Type: TypeBool, Default: "true", Desc: "Respect .gitignore when searching."}),
		qwenCodeRow(SettingRow{Path: KeyPath{"context", "fileFiltering", "respectQwenIgnore"}, Type: TypeBool, Default: "true", Desc: "Respect .qwenignore and the custom ignore files."}),
		qwenCodeRow(SettingRow{Path: KeyPath{"context", "fileFiltering", "enableFuzzySearch"}, Type: TypeBool, Default: "true", Desc: "Fuzzy file search for @ completions."}),
		// --- tools
		qwenCodeRow(SettingRow{
			Path: KeyPath{"tools", "sandbox"}, Type: TypeBool, Desc: "Sandbox for tool execution (also accepts a docker / podman / profile string).",
			Danger: DangerConfirm, Note: "false runs commands unsandboxed. QWEN_SANDBOX overrides it.",
		}),
		qwenCodeRow(SettingRow{Path: KeyPath{"tools", "sandboxImage"}, Type: TypeString, Desc: "Docker / Podman sandbox image."}),
		qwenCodeRow(SettingRow{
			Path: KeyPath{"tools", "executionSandbox"}, Type: TypeObject, Scopes: qwenCodeOperatorScopes,
			Desc: "Linux execution sandbox policy {filesystem: read-only | workspace-write, network: open | closed, backend: auto | bwrap | landlock}.",
			Note: "Operator-only: ignored in workspace settings; a System value replaces the whole object.",
		}),
		qwenCodeRow(SettingRow{
			Path: KeyPath{"tools", "approvalMode"}, Type: TypeEnum, Enum: []string{"plan", "default", "auto-edit", "auto", "yolo"}, Default: "\"auto\"",
			Desc: "Default approval mode for tool calls.", Danger: DangerConfirm,
			Note: "yolo approves every call; auto lets an LLM classifier approve safe actions. Hyphenated auto-edit (gemini-cli spells it auto_edit).",
		}),
		qwenCodeRow(SettingRow{Path: KeyPath{"tools", "core"}, Type: TypeStringList, Desc: "Restricts the core tool set.", Deprecated: true, Note: "Use tools.eager plus permissions.deny."}),
		qwenCodeRow(SettingRow{Path: KeyPath{"tools", "exclude"}, Type: TypeStringList, Desc: "Tool names excluded from discovery.", Deprecated: true, Note: "Use permissions.deny; still honoured at startup."}),
		qwenCodeRow(SettingRow{
			Path: KeyPath{"tools", "allowed"}, Type: TypeStringList, Desc: "Tool names that bypass confirmation.", Deprecated: true,
			Danger: DangerConfirm, Note: "Use permissions.allow; still honoured at startup.",
		}),
		qwenCodeRow(SettingRow{Path: KeyPath{"tools", "disabled"}, Type: TypeStringList, Desc: "Tool names hidden from the registry (merged across scopes)."}),
		qwenCodeRow(SettingRow{Path: KeyPath{"tools", "visible"}, Type: TypeStringList, Desc: "Deferred tools made visible at startup (merged across scopes)."}),
		qwenCodeRow(SettingRow{
			Path: KeyPath{"tools", "discoveryCommand"}, Type: TypeString, Desc: "Command run to discover extra tools.",
			Danger: DangerConfirm, Note: "Runs an arbitrary command at startup.",
		}),
		qwenCodeRow(SettingRow{
			Path: KeyPath{"tools", "callCommand"}, Type: TypeString, Desc: "Command run to call a discovered tool.",
			Danger: DangerConfirm, Note: "Runs an arbitrary command per discovered-tool call.",
		}),
		qwenCodeRow(SettingRow{Path: KeyPath{"tools", "useRipgrep"}, Type: TypeBool, Default: "true", Desc: "Use ripgrep for content search."}),
		qwenCodeRow(SettingRow{Path: KeyPath{"tools", "truncateToolOutputThreshold"}, Type: TypeInt, Default: "25000", Desc: "Truncate tool output above this many characters."}),
		qwenCodeRow(SettingRow{Path: KeyPath{"tools", "truncateToolOutputLines"}, Type: TypeInt, Default: "1000", Desc: "Lines kept when truncating tool output."}),
		qwenCodeRow(SettingRow{
			Path: KeyPath{"tools", "webSearch", "model"}, Type: TypeString, Desc: "Model used by the web search tool.", Verified: VerifiedLive,
			Note: qwenCodeLive + " Not on the settings reference page; the key name was seen live and its value type was not recorded beyond a model id.",
		}),
		// --- permissions
		qwenCodeRow(SettingRow{
			Path: KeyPath{"permissions", "allow"}, Type: TypeRuleList, Desc: "Rules auto-approved without a prompt (merged across scopes).",
			Danger: DangerConfirm, Note: "Rules \"Tool\" or \"Tool(specifier)\"; a broad rule such as \"Bash\" needs a confirmation.",
		}),
		qwenCodeRow(SettingRow{Path: KeyPath{"permissions", "ask"}, Type: TypeRuleList, Desc: "Rules that always prompt (beats allow)."}),
		qwenCodeRow(SettingRow{Path: KeyPath{"permissions", "deny"}, Type: TypeRuleList, Desc: "Rules that are always blocked (beats ask and allow at any scope)."}),
		qwenCodeRow(SettingRow{Path: KeyPath{"slashCommands", "disabled"}, Type: TypeStringList, Desc: "Slash commands hidden and refused (union across scopes)."}),
		qwenCodeRow(SettingRow{Path: KeyPath{"skills", "disabled"}, Type: TypeStringList, Desc: "Hard-disabled skill names (union across scopes)."}),
		qwenCodeRow(SettingRow{Path: KeyPath{"skills", "enabled"}, Type: TypeStringList, Desc: "Explicit skill opt-ins."}),
		// --- mcp
		qwenCodeRow(SettingRow{
			Path: KeyPath{"mcp", "serverCommand"}, Type: TypeString, Desc: "Command that starts an MCP server.",
			Danger: DangerConfirm, Note: "Runs an arbitrary command.",
		}),
		qwenCodeRow(SettingRow{
			Path: KeyPath{"mcp", "allowed"}, Type: TypeStringList, Desc: "MCP server allowlist.",
			Note: "Can drop servers; Observer registers no MCP entry for Qwen Code today, so nothing of Observer's is at risk.",
		}),
		qwenCodeRow(SettingRow{Path: KeyPath{"mcp", "excluded"}, Type: TypeStringList, Desc: "MCP server denylist (wins over allowed)."}),
		qwenCodeRow(SettingRow{
			Path: KeyPath{"mcpServers"}, Type: TypeMCPMap,
			Desc:   "MCP servers by name: command, args, env, cwd, url (SSE), httpUrl, headers, timeout, trust, includeTools, excludeTools.",
			Danger: DangerConfirm, Note: "trust: true bypasses the server's tool confirmations; headers and env may hold credentials.",
		}),
		// --- security
		qwenCodeRow(SettingRow{
			Path: KeyPath{"security", "folderTrust", "enabled"}, Type: TypeBool, Default: "false", Desc: "Folder trust gate for project settings and hooks.",
			Danger: DangerConfirm, Note: "false (the default) leaves the trust gate off.",
		}),
		qwenCodeRow(SettingRow{
			Path: KeyPath{"security", "auth", "selectedType"}, Type: TypeString, Desc: "Selected authentication type.", Verified: VerifiedLive, Note: qwenCodeLive,
		}),
		qwenCodeRow(SettingRow{Path: KeyPath{"security", "auth", "enforcedType"}, Type: TypeString, Desc: "Required auth type (an operator lock)."}),
		qwenCodeRow(SettingRow{Path: KeyPath{"security", "auth", "useExternal"}, Type: TypeBool, Desc: "Use an external authentication flow."}),
		qwenCodeRow(SettingRow{
			Path: KeyPath{"security", "auth", "apiKey"}, Type: TypeString, Secret: true, Deprecated: true,
			Desc: "Literal API key for OpenAI-compatible auth.", Note: "A plaintext credential; migrate to modelProviders with envKey.",
		}),
		qwenCodeRow(SettingRow{
			Path: KeyPath{"security", "auth", "baseUrl"}, Type: TypeString, Deprecated: true, Danger: DangerConfirm,
			Desc: "Base URL for OpenAI-compatible auth.", Note: "Redirects traffic off Observer's proxy; migrate to modelProviders.",
		}),
		qwenCodeRow(SettingRow{
			Path: KeyPath{"security", "allowPrivateNetworkHooks"}, Type: TypeBool, Scopes: qwenCodeOperatorScopes, Source: qwenCodeHooksRef,
			Desc: "Let HTTP hooks reach private-network addresses.", Danger: DangerConfirm, Note: "Operator-only; ignored in workspace settings.",
		}),
		qwenCodeRow(SettingRow{
			Path: KeyPath{"security", "allowedHttpHookUrls"}, Type: TypeStringList, Source: qwenCodeHooksRef,
			Desc: "Whitelist of URLs HTTP hooks may call (empty = all).",
		}),
		// --- advanced / telemetry / env / hooks
		qwenCodeRow(SettingRow{Path: KeyPath{"advanced", "excludedEnvVars"}, Type: TypeStringList, Default: "[\"DEBUG\",\"DEBUG_MODE\"]", Desc: "Variables excluded from project .env files."}),
		qwenCodeRow(SettingRow{Path: KeyPath{"advanced", "dnsResolutionOrder"}, Type: TypeString, Desc: "DNS resolution order."}),
		qwenCodeRow(SettingRow{Path: KeyPath{"telemetry", "enabled"}, Type: TypeBool, Desc: "OpenTelemetry export."}),
		qwenCodeRow(SettingRow{Path: KeyPath{"telemetry", "otlpEndpoint"}, Type: TypeString, Desc: "OTLP exporter endpoint."}),
		qwenCodeRow(SettingRow{Path: KeyPath{"telemetry", "otlpProtocol"}, Type: TypeString, Desc: "OTLP protocol (grpc or http)."}),
		qwenCodeRow(SettingRow{
			Path: KeyPath{"telemetry", "logPrompts"}, Type: TypeBool, Desc: "Include prompt and request / response text in telemetry logs.",
			Note: "Exports prompt content.",
		}),
		qwenCodeRow(SettingRow{Path: KeyPath{"telemetry", "outfile"}, Type: TypeString, Desc: "File to write telemetry to (overrides OTLP)."}),
		qwenCodeRow(SettingRow{
			Path: KeyPath{"telemetry", "includeSensitiveSpanAttributes"}, Type: TypeBool, Default: "false",
			Desc: "Attach verbatim prompts, tool I/O and responses to spans.",
		}),
		qwenCodeRow(SettingRow{
			Path: KeyPath{"env"}, Type: TypeStringMap, Verified: VerifiedLive,
			Desc: "Environment variables loaded into the session (some, such as NODE_OPTIONS, are always rejected).",
			Note: qwenCodeLive + " Values may be credentials: node 1 held a provider-key-named entry with a literal value. The secret posture applies to every value; nothing reads or copies them.",
		}),
		qwenCodeRow(SettingRow{
			Path: KeyPath{"hooks"}, Type: TypeObject, Source: qwenCodeHooksRef,
			Desc: "Hooks by event: <Event>[] {matcher, hooks: [{type, command, timeout}]} (PreToolUse, UserPromptSubmit, Stop, ...).",
			Note: "Observer registers its UserPromptSubmit hook here; its own entries are an owned span, the rest stay the user's.",
		}),
		qwenCodeRow(SettingRow{
			Path: KeyPath{"disableAllHooks"}, Type: TypeBool, Source: qwenCodeHooksRef, Desc: "Turn every hook off.",
			Danger: DangerRefuseWhileIntegrated, Note: "true disables Observer's hooks.",
		}),
	},
	Map: []MapRow{
		{Key: KeyModel, Tool: "qwen-code", Settings: []string{"qwen-code/model.name"}, Transform: TransformModelAlias, Source: qwenCodeRef},
		{
			Key: KeySmallModel, Tool: "qwen-code", Settings: []string{"qwen-code/fastModel"}, Transform: TransformModelAlias, Source: qwenCodeRef,
			Note: "fastModel drives suggestions and speculation; compactionModel, advisorModel and visionModel stay tool-specific.",
		},
		{
			Key: KeyReasoningEffort, Tool: "qwen-code", Settings: []string{"qwen-code/model.reasoningEffort"}, Transform: TransformEnumMap,
			Params: TransformParams{Enum: []EnumPair{
				{"low", []any{"low"}}, {"medium", []any{"medium"}}, {"high", []any{"high"}}, {"xhigh", []any{"xhigh"}}, {"max", []any{"max"}},
			}}, Source: qwenCodeRef, Note: "none and minimal are refused; each provider clamps the level.",
		},
		{
			Key: KeyApprovalMode, Tool: "qwen-code", Settings: []string{"qwen-code/tools.approvalMode"}, Transform: TransformEnumMap,
			Params: TransformParams{Enum: []EnumPair{
				{"plan", []any{"plan"}}, {"ask_edits", []any{"default"}}, {"auto_edits", []any{"auto-edit"}}, {"full_auto", []any{"yolo"}},
			}}, Source: qwenCodeRef, Note: "ask is refused; auto (classifier approval) has no canonical form.",
		},
		{
			Key: KeySandboxMode, Tool: "qwen-code", Settings: []string{"qwen-code/tools.sandbox"}, Transform: TransformEnumMap,
			Params: TransformParams{Enum: []EnumPair{{"workspace_write", []any{true}}, {"off", []any{false}}}}, Source: qwenCodeRef,
			Note: "A docker / podman / profile string reads back as tool-specific; read_only is refused (tools.executionSandbox is operator-only).",
		},
		{
			Key: KeyAllowedTools, Tool: "qwen-code", Settings: []string{"qwen-code/permissions.allow"}, Transform: TransformRuleTranslate,
			Params: TransformParams{Grammar: qwenCodeRuleGrammar}, Source: qwenCodeRef,
			Note: "search and mcp rules are refused (only the server-level mcp__server form is documented).",
		},
		{
			Key: KeyDeniedTools, Tool: "qwen-code", Settings: []string{"qwen-code/permissions.deny"}, Transform: TransformRuleTranslate,
			Params: TransformParams{Grammar: qwenCodeRuleGrammar}, Source: qwenCodeRef,
		},
		{Key: KeyAdditionalDirs, Tool: "qwen-code", Settings: []string{"qwen-code/context.includeDirectories"}, Transform: TransformIdentity, Source: qwenCodeRef},
		{
			Key: KeyMCPServers, Tool: "qwen-code", Settings: []string{"qwen-code/mcpServers"}, Transform: TransformMCPShape,
			Params: TransformParams{MCP: mcpGemini}, Lossy: true, Source: qwenCodeRef,
			Note: "Gemini-lineage entry shape (httpUrl for streamable HTTP, url for SSE). " + mcpLossyNote,
		},
		{
			Key: KeyEnv, Tool: "qwen-code", Settings: []string{"qwen-code/env"}, Transform: TransformIdentity, Source: qwenCodeRef,
			Note: "Values may be credentials; the section 5.3 secret posture applies.",
		},
		{Key: KeyTelemetryOff, Tool: "qwen-code", Settings: []string{"qwen-code/privacy.usageStatisticsEnabled"}, Transform: TransformBoolInvert, Source: qwenCodeRef},
		{Key: KeyAutoUpdate, Tool: "qwen-code", Settings: []string{"qwen-code/general.enableAutoUpdate"}, Transform: TransformIdentity, Source: qwenCodeRef},
		{
			Key: KeyInstructionsFiles, Tool: "qwen-code", Settings: []string{"qwen-code/context.fileName"}, Transform: TransformIdentity, Source: qwenCodeRef,
			Note: "Replaces QWEN.md; a single string reads back as a one-item list.",
		},
		{
			Key: KeyMaxOutputTokens, Tool: "qwen-code", Settings: []string{"qwen-code/model.generationConfig.samplingParams.max_tokens"},
			Transform: TransformIdentity, Source: qwenCodeRef,
		},
		{
			Key: KeyContextLimit, Tool: "qwen-code", Settings: []string{"qwen-code/model.generationConfig.contextWindowSize"},
			Transform: TransformIdentity, Source: qwenCodeRef, Note: "For a modelProviders model the window lives in that entry's generationConfig.",
		},
		{
			Key: KeyAutoCompact, Tool: "qwen-code", Settings: []string{"qwen-code/context.autoCompactThreshold"}, Transform: TransformScale,
			Params: TransformParams{Factor: 0.01}, Source: qwenCodeRef, Note: "A fraction of the window (percent / 100).",
		},
		{
			Key: KeyTemperature, Tool: "qwen-code", Settings: []string{"qwen-code/model.generationConfig.samplingParams.temperature"},
			Transform: TransformIdentity, Source: qwenCodeRef,
		},
		{
			Key: KeyBaseURL, Tool: "qwen-code", Settings: []string{"qwen-code/model.baseUrl"}, Transform: TransformIdentity, Source: qwenCodeRef,
			Note: "Observer's proxy route writes model.baseUrl and retargets the matching modelProviders entry.",
		},
	},
}
