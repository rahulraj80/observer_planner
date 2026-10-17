package agentconfig

// Meta Muse Code CLI catalogue, doc-authored (phase P2). Sources: the Muse
// configuration and extending pages (configuration re-fetched 2026-10-04),
// audit docs/audits/adapter-config-surface-2026-10-03.md section 5 "muse" and
// section 6 finding 5 (a used install writes only model / provider /
// schema_version).
const (
	museRef     = "https://dev.meta.ai/docs/muse-code/configuration.md"
	museExtRef  = "https://dev.meta.ai/docs/muse-code/extending.md"
	museSummary = "https://innfactory.ai/en/ai-harness/muse/"
	museLiveSrc = "node 1 live probe 2026-10-03 (audit section 6, muse)"
)

var museCatalog = toolCatalog{
	Tool: "muse",
	VerifiedAgainst: "dev.meta.ai Muse Code configuration + extending pages (audit section 5, fetched 2026-10-03; configuration re-fetched 2026-10-04); " +
		"node 1 live key names 2026-10-03 for model, provider, schema_version",
	Files: []FileRow{
		{
			ID: "muse/user-settings", Tool: "muse", Shape: "muse/settings", Kind: KindSettings, Scope: ScopeUser,
			Paths: OSPaths{
				Linux:   "~/.config/muse/settings.json",
				Darwin:  "~/.config/muse/settings.json",
				Windows: `%APPDATA%\muse\settings.json`,
			},
			Format: FormatJSON, Precedence: 20, WriteNeeds: PrivUser,
			Grounding: live("~/.config/muse/settings.json carries model, provider, schema_version only"),
			Note:      "XDG_CONFIG_HOME relocates it (macOS is XDG-shaped too, not ~/Library). MUST keep \"schema_version\": 1 or Muse refuses to start. No project settings file exists.",
		},
		{
			ID: "muse/project-hooks", Tool: "muse", Shape: "muse/project-hooks", Kind: KindHooks, Scope: ScopeProjectShared,
			Paths: samePath("<project>/.muse/hooks.json"), Format: FormatJSON, Precedence: 30, WriteNeeds: PrivUser,
			Grounding: doc(museExtRef),
			Note:      "Project hooks run only in a trusted project. The payload schema is unpublished, so the file carries no rows.",
		},
		{
			ID: "muse/trust", Tool: "muse", Shape: "muse/trust", Kind: KindToolState, Scope: ScopeUser,
			Paths:  OSPaths{Linux: "~/.config/muse/trust.json", Darwin: "~/.config/muse/trust.json", Windows: `%APPDATA%\muse\trust.json`},
			Format: FormatJSON, Precedence: 0, WriteNeeds: PrivNever,
			Grounding: live("~/.config/muse/trust.json present (not opened)"),
			Note:      "Project trust decisions: never read, never written.",
		},
		{
			ID: "muse/auth", Tool: "muse", Shape: "muse/auth", Kind: KindToolState, Scope: ScopeUser,
			Paths:  OSPaths{Linux: "~/.config/muse/auth.json", Darwin: "~/.config/muse/auth.json", Windows: `%APPDATA%\muse\auth.json`},
			Format: FormatJSON, Precedence: 0, WriteNeeds: PrivNever,
			Grounding: live("~/.config/muse/auth.json present (not opened)"),
			Note:      "OAuth state and the minted Model API key: never read, never written (nor its *.lock sibling).",
		},
	},
	Extra: []SettingRow{
		{
			ID: "muse/schema_version", Tool: "muse", FileID: "muse/settings", Path: KeyPath{"schema_version"}, Type: TypeInt,
			Min: f64(1), Max: f64(1), Default: "1", Desc: "Settings file format version; required.",
			Source: museRef, Verified: VerifiedLive, Owner: OwnerUser, Danger: DangerConfirm,
			Note: "Seen on node 1 (an int). Removing it or setting another value makes Muse refuse to start (malformed settings error); every writer must keep it.",
		},
		{
			ID: "muse/model", Tool: "muse", FileID: "muse/settings", Path: KeyPath{"model"}, Type: TypeString, Default: "\"muse-spark-1.2\"",
			Desc: "Default model.", Source: museRef, Verified: VerifiedLive, Owner: OwnerUser,
			Note: "Seen on node 1 (audit section 6).",
		},
		{
			ID: "muse/provider", Tool: "muse", FileID: "muse/settings", Path: KeyPath{"provider"}, Type: TypeString,
			Desc: "Model provider the default model is served by.", Source: museLiveSrc, Verified: VerifiedLive, Owner: OwnerUser,
			Note: "Seen on node 1; the docs do not describe its values. The endpoint / base URL key that pairs with it is UNVERIFIED (the CLI's --base-url is the documented route).",
		},
		{
			ID: "muse/reasoning_effort", Tool: "muse", FileID: "muse/settings", Path: KeyPath{"reasoning_effort"}, Type: TypeEnum,
			Enum:    []string{"none", "minimal", "low", "medium", "high", "xhigh", "max", "ultra"},
			Default: "\"high\"", Desc: "Reasoning effort (trades latency for depth).", Source: museRef, Verified: VerifiedDoc, Owner: OwnerUser,
			Note: "\"ultra\" is unique to Muse.",
		},
		{
			ID: "muse/permissions.default_profile", Tool: "muse", FileID: "muse/settings", Path: KeyPath{"permissions", "default_profile"}, Type: TypeString,
			Desc: "Default permission profile id.", Source: museRef, Verified: VerifiedUnverified, Owner: OwnerUser, Danger: DangerConfirm,
			Note: "Profile names are UNVERIFIED (audit); a full-access profile removes approvals. The OS sandbox covers shell only, not MCP.",
		},
		{
			ID: "muse/tui.terminal_background", Tool: "muse", FileID: "muse/settings", Path: KeyPath{"tui", "terminal_background"}, Type: TypeString,
			Desc: "Terminal theme id (set by /theme).", Source: museRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "muse/tui", Tool: "muse", FileID: "muse/settings", Path: KeyPath{"tui"}, Type: TypeObject,
			Desc: "Terminal UI preferences.", Source: museRef, Verified: VerifiedUnverified, Owner: OwnerUser,
			Note: "Only tui.terminal_background is documented.",
		},
		{
			ID: "muse/hooks", Tool: "muse", FileID: "muse/settings", Path: KeyPath{"hooks"}, Type: TypeObject,
			Desc:   "Hook definitions (15 events incl. PreToolUse, UserPromptSubmit, SessionStart, SessionEnd, Stop, Subagent*, Pre/PostCompact).",
			Source: museExtRef, Verified: VerifiedDoc, Owner: OwnerUser, Danger: DangerConfirm,
			Note: "Hook commands run arbitrary code; the payload schema is unpublished.",
		},
		{
			ID: "muse/managed_hooks_path", Tool: "muse", FileID: "muse/settings", Path: KeyPath{"managed_hooks_path"}, Type: TypeString,
			Desc: "Path to centrally administered hook files.", Source: museRef, Verified: VerifiedDoc, Owner: OwnerUser, Danger: DangerConfirm,
			Note: "Managed hooks bypass project trust; whether it silently disables user-level hooks is inferred, UNVERIFIED (hook-parity audit).",
		},
		{
			ID: "muse/mcp_servers", Tool: "muse", FileID: "muse/settings", Path: KeyPath{"mcp_servers"}, Type: TypeObject,
			Desc:   "MCP servers: an ARRAY of entries, transports stdio and streamable_http, ${VAR} interpolation.",
			Source: museRef, Verified: VerifiedUnverified, Owner: OwnerUser, Danger: DangerConfirm,
			Note: "An array, not a name-keyed map; the entry shape (name / url / command) is UNVERIFIED, so it has no common mapping.",
		},
		{
			ID: "muse/telemetry", Tool: "muse", FileID: "muse/settings", Path: KeyPath{"telemetry"}, Type: TypeObject,
			Desc: "Telemetry options.", Source: museRef, Verified: VerifiedUnverified, Owner: OwnerUser,
			Note: "Its keys are not documented (audit: UNVERIFIED).",
		},
		{
			ID: "muse/runtime_capabilities", Tool: "muse", FileID: "muse/settings", Path: KeyPath{"runtime_capabilities"}, Type: TypeObject,
			Desc: "Capability toggles (e.g. observer agents).", Source: museRef, Verified: VerifiedUnverified, Owner: OwnerUser,
			Note: "Named on the configuration page (fetched 2026-10-04); its keys are not documented.",
		},
		{
			ID: "muse/agents.execution_capacity", Tool: "muse", FileID: "muse/settings", Path: KeyPath{"agents", "execution_capacity"}, Type: TypeInt,
			Min: f64(1), Max: f64(64), Default: "8", Desc: "Maximum concurrent subagents.",
			Source: museSummary, Verified: VerifiedUnverified, Owner: OwnerUser,
			Note: "From third-party search summaries (audit section 5), not the vendor page.",
		},
	},
	Map: []MapRow{
		{Key: KeyModel, Tool: "muse", Settings: []string{"muse/model"}, Transform: TransformModelAlias, Source: museRef},
		{
			Key: KeyReasoningEffort, Tool: "muse", Settings: []string{"muse/reasoning_effort"}, Transform: TransformEnumMap,
			Params: TransformParams{Enum: []EnumPair{
				{"none", []any{"none"}},
				{"minimal", []any{"minimal"}},
				{"low", []any{"low"}},
				{"medium", []any{"medium"}},
				{"high", []any{"high"}},
				{"xhigh", []any{"xhigh"}},
				{"max", []any{"max"}},
			}},
			Source: museRef, Note: "\"ultra\" reads back as tool-specific.",
		},
	},
}
