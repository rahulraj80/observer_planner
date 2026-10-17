package agentconfig

// Prime Agent (Prime Intellect) catalogue, doc-authored (phase P2). Prime
// Agent is a hard fork of pi-mono and publishes no settings reference: the
// live keys come from node 1 (audit section 6), the rest from pi's settings
// reference transposed to ~/.prime/agent/ (UNVERIFIED for prime-agent, audit
// section 5 "prime-agent").
const (
	primeAgentPiRef   = "https://pi.dev/docs/latest/settings"
	primeAgentLiveSrc = "node 1 live probe 2026-10-03 (audit section 6, prime-agent)"
	primeAgentPiNote  = "A pi parent-schema key (pi.dev settings reference); UNVERIFIED for prime-agent."
)

func primeAgentPiRow(path KeyPath, typ ValueType, def, desc string) SettingRow {
	return SettingRow{
		ID: "prime-agent/" + path.String(), Tool: "prime-agent", FileID: "prime-agent/settings", Path: path,
		Type: typ, Default: def, Desc: desc, Source: primeAgentPiRef, Verified: VerifiedUnverified, Owner: OwnerUser, Note: primeAgentPiNote,
	}
}

var primeAgentCatalog = toolCatalog{
	Tool: "prime-agent",
	VerifiedAgainst: "node 1 live key names 2026-10-03 for defaultModel, defaultProvider, defaultThinkingLevel, agentTraces.enabled, recentModels, onboardingShown and models.json providers " +
		"(audit section 6); pi.dev settings reference (pi parent schema, fetched 2026-10-04) for the UNVERIFIED rows; prime-agent README",
	Files: []FileRow{
		{
			ID: "prime-agent/user-settings", Tool: "prime-agent", Shape: "prime-agent/settings", Kind: KindSettings, Scope: ScopeUser,
			Paths: samePath("~/.prime/agent/settings.json"), Format: FormatJSON, Precedence: 20, WriteNeeds: PrivUser,
			Grounding: live("~/.prime/agent/settings.json carries defaultModel, defaultProvider, defaultThinkingLevel, agentTraces.enabled, recentModels, onboardingShown"),
			Note:      "Same layout on Windows (%USERPROFILE%\\.prime\\agent\\). A project settings file (pi: .pi/settings.json) is UNVERIFIED for prime-agent and not declared.",
		},
		{
			ID: "prime-agent/user-models", Tool: "prime-agent", Shape: "prime-agent/models", Kind: KindSettings, Scope: ScopeUser,
			Paths: samePath("~/.prime/agent/models.json"), Format: FormatJSON, Precedence: 20, WriteNeeds: PrivUser,
			Grounding: live("~/.prime/agent/models.json carries providers.<id>.{api,apiKey,baseUrl,models[]}"),
			Note:      "Custom providers; `observer prime-agent` writes an \"observer\" provider here (baseUrl = Observer's proxy /v1) and launches with --provider observer. A provider apiKey may be a literal key.",
		},
		{
			ID: "prime-agent/mcp-catalog", Tool: "prime-agent", Shape: "prime-agent/mcp-catalog", Kind: KindToolState, Scope: ScopeUser,
			Paths: samePath("~/.prime/agent/mcp/mcp-service-catalog.v2.json"), Format: FormatJSON, Precedence: 0, WriteNeeds: PrivNever,
			Grounding: live("~/.prime/agent/mcp/mcp-service-catalog.v2.json is a downloaded catalogue"),
			Note:      "A downloaded MCP service catalogue, not config. MCP servers are set up through the TUI (/login, MCP Connections).",
		},
		{
			ID: "prime-agent/auth", Tool: "prime-agent", Shape: "prime-agent/auth", Kind: KindToolState, Scope: ScopeUser,
			Paths: samePath("~/.prime/agent/auth.json"), Format: FormatJSON, Precedence: 0, WriteNeeds: PrivNever,
			Grounding: Grounding{Kind: GroundAdapterDoc, Source: "internal/adapter/primeagent/doc.go", Date: auditDate},
			Note:      "Per-provider /login credentials: never read, never written.",
		},
		{
			ID: "prime-agent/prime-config", Tool: "prime-agent", Shape: "prime-agent/prime-config", Kind: KindToolState, Scope: ScopeUser,
			Paths: samePath("~/.prime/config.json"), Format: FormatJSON, Precedence: 0, WriteNeeds: PrivNever,
			Grounding: live("~/.prime/config.json present (credential store, not opened)"),
			Note:      "Prime Intellect account config holding the api_key: never read, never written (also daemon-workers/*/*.json and session-artifacts/*/kernel-state.dill).",
		},
	},
	Extra: []SettingRow{
		// Live on node 1.
		{
			ID: "prime-agent/defaultModel", Tool: "prime-agent", FileID: "prime-agent/settings", Path: KeyPath{"defaultModel"}, Type: TypeString,
			Desc: "Default model id.", Source: primeAgentPiRef, Verified: VerifiedLive, Owner: OwnerUser,
			Note: "Seen on node 1 (audit section 6); pairs with defaultProvider.",
		},
		{
			ID: "prime-agent/defaultProvider", Tool: "prime-agent", FileID: "prime-agent/settings", Path: KeyPath{"defaultProvider"}, Type: TypeString,
			Desc: "Default provider id.", Source: primeAgentPiRef, Verified: VerifiedLive, Owner: OwnerUser,
			Note: "Seen on node 1 (audit section 6).",
		},
		{
			ID: "prime-agent/defaultThinkingLevel", Tool: "prime-agent", FileID: "prime-agent/settings", Path: KeyPath{"defaultThinkingLevel"}, Type: TypeEnum,
			Enum: []string{"off", "minimal", "low", "medium", "high", "xhigh", "max"}, Default: "\"medium\"",
			Desc: "Default thinking level.", Source: primeAgentPiRef, Verified: VerifiedLive, Owner: OwnerUser,
			Note: "Key seen on node 1 (audit section 6); the value list and default are pi's.",
		},
		{
			ID: "prime-agent/agentTraces.enabled", Tool: "prime-agent", FileID: "prime-agent/settings", Path: KeyPath{"agentTraces", "enabled"}, Type: TypeBool,
			Desc: "Agent trace recording switch.", Source: primeAgentLiveSrc, Verified: VerifiedLive, Owner: OwnerUser,
			Note: "Seen on node 1; not in pi's reference and its semantics are undocumented.",
		},
		{
			ID: "prime-agent/recentModels", Tool: "prime-agent", FileID: "prime-agent/settings", Path: KeyPath{"recentModels"}, Type: TypeStringList,
			Desc: "Recently used models (picker history).", Source: primeAgentLiveSrc, Verified: VerifiedLive, Owner: OwnerToolState,
			Note: "Seen on node 1; maintained by the tool.",
		},
		{
			ID: "prime-agent/onboardingShown", Tool: "prime-agent", FileID: "prime-agent/settings", Path: KeyPath{"onboardingShown"}, Type: TypeBool,
			Desc: "Whether onboarding was shown.", Source: primeAgentLiveSrc, Verified: VerifiedLive, Owner: OwnerToolState,
			Note: "Seen on node 1; maintained by the tool.",
		},
		{
			ID: "prime-agent/providers", Tool: "prime-agent", FileID: "prime-agent/models", Path: KeyPath{"providers"}, Type: TypeObject,
			Desc:   "Custom providers by id: api (e.g. openai-completions), apiKey, baseUrl and models[] {id, name, contextWindow, maxTokens, input, reasoning}.",
			Source: primeAgentLiveSrc, Verified: VerifiedLive, Owner: OwnerUser, Secret: true, Danger: DangerConfirm,
			Note: "Shape seen on node 1 (audit section 6): per-model maxTokens and contextWindow, and a provider apiKey that may be a literal key (pi accepts an env var NAME or a literal), so the whole map is secret. " +
				"A baseUrl sends model traffic elsewhere; the \"observer\" entry is written by `observer prime-agent` (removing it drops proxy capture).",
		},
		// pi parent schema (UNVERIFIED for prime-agent).
		primeAgentPiRow(KeyPath{"modelThinkingLevels"}, TypeObject, "", "Per-model thinking levels keyed provider/modelId."),
		primeAgentPiRow(KeyPath{"thinkingBudgets"}, TypeObject, "", "Token budgets for the thinking levels."),
		primeAgentPiRow(KeyPath{"enabledModels"}, TypeStringList, "", "Model patterns offered in the picker."),
		primeAgentPiRow(KeyPath{"hideThinkingBlock"}, TypeBool, "false", "Hide thinking blocks."),
		func() SettingRow {
			r := primeAgentPiRow(KeyPath{"defaultProjectTrust"}, TypeEnum, "\"ask\"", "Trust decision for a new project.")
			r.Enum, r.Danger = []string{"ask", "always", "never"}, DangerConfirm
			r.Note = primeAgentPiNote + " \"always\" trusts every project without asking."
			return r
		}(),
		func() SettingRow {
			r := primeAgentPiRow(KeyPath{"defaultTools"}, TypeStringList, "", "Tools loaded by default (+ / - modifiers).")
			r.Note = primeAgentPiNote + " Prime Agent's only built-in tool is ipython (one persistent kernel), so there is no per-command allow / deny surface."
			return r
		}(),
		primeAgentPiRow(KeyPath{"compaction", "enabled"}, TypeBool, "true", "Automatic context compaction."),
		primeAgentPiRow(KeyPath{"compaction", "reserveTokens"}, TypeInt, "16384", "Tokens reserved before compaction triggers."),
		primeAgentPiRow(KeyPath{"compaction", "keepRecentTokens"}, TypeInt, "20000", "Recent tokens kept verbatim after compaction."),
		primeAgentPiRow(KeyPath{"compaction", "modelOverrides"}, TypeObject, "", "Per-model compaction overrides keyed provider/modelId."),
		primeAgentPiRow(KeyPath{"theme"}, TypeString, "\"system\"", "Theme name."),
		primeAgentPiRow(KeyPath{"enableAnalytics"}, TypeBool, "false", "Usage analytics."),
		primeAgentPiRow(KeyPath{"enableInstallTelemetry"}, TypeBool, "true", "Install telemetry."),
		primeAgentPiRow(KeyPath{"sessionDir"}, TypeString, "", "Session directory."),
		primeAgentPiRow(KeyPath{"skills"}, TypeStringList, "", "Skill paths (glob, + / - patterns)."),
		primeAgentPiRow(KeyPath{"extensions"}, TypeStringList, "", "Extension paths (glob, + / - patterns)."),
		primeAgentPiRow(KeyPath{"prompts"}, TypeStringList, "", "Prompt-template paths (glob, + / - patterns)."),
		primeAgentPiRow(KeyPath{"retry", "enabled"}, TypeBool, "true", "Retry failed provider requests."),
		primeAgentPiRow(KeyPath{"retry", "maxRetries"}, TypeInt, "3", "Maximum retries."),
		func() SettingRow {
			r := primeAgentPiRow(KeyPath{"httpProxy"}, TypeString, "", "HTTP proxy URL for provider traffic.")
			r.Danger = DangerConfirm
			r.Note = primeAgentPiNote + " Routes model traffic through another host."
			return r
		}(),
		func() SettingRow {
			r := primeAgentPiRow(KeyPath{"shellCommandPrefix"}, TypeString, "", "Command prefixed to every shell command.")
			r.Danger = DangerConfirm
			r.Note = primeAgentPiNote + " Runs an arbitrary command."
			return r
		}(),
		primeAgentPiRow(KeyPath{"quietStartup"}, TypeBool, "false", "Quiet startup (pi also accepts \"header\")."),
		func() SettingRow {
			r := primeAgentPiRow(KeyPath{"tuiMode"}, TypeEnum, "\"fullscreen\"", "Terminal UI mode.")
			r.Enum = []string{"regular", "fullscreen"}
			return r
		}(),
		func() SettingRow {
			r := primeAgentPiRow(KeyPath{"transport"}, TypeEnum, "\"auto\"", "Provider transport.")
			r.Enum = []string{"auto", "sse", "websocket", "websocket-cached"}
			return r
		}(),
	},
	Map: []MapRow{
		{
			Key: KeyModel, Tool: "prime-agent", Settings: []string{"prime-agent/defaultModel"}, Transform: TransformModelAlias, Source: primeAgentPiRef,
			Note: "The provider half goes to defaultProvider, which this mapping does not write.",
		},
		{
			Key: KeyReasoningEffort, Tool: "prime-agent", Settings: []string{"prime-agent/defaultThinkingLevel"}, Transform: TransformEnumMap,
			Params: TransformParams{Enum: []EnumPair{
				{"none", []any{"off"}},
				{"minimal", []any{"minimal"}},
				{"low", []any{"low"}},
				{"medium", []any{"medium"}},
				{"high", []any{"high"}},
				{"xhigh", []any{"xhigh"}},
				{"max", []any{"max"}},
			}},
			Source: primeAgentPiRef, Note: "none is spelled \"off\".",
		},
	},
}
