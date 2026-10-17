package agentconfig

// goose (aaif-goose/goose, formerly block/goose) doc-authored catalogue.
// Sources: the vendor docs on main (documentation/docs/guides/
// config-files.md, environment-variables.md, managing-tools/
// goose-permissions.md) fetched 2026-10-04 and audit section 5 "goose".
// goose is not installed on node 1: doc-only. Root keys of config.yaml are
// the GOOSE_* environment variable names; environment variables win.
const (
	gooseRef      = "https://raw.githubusercontent.com/aaif-goose/goose/main/documentation/docs/guides/config-files.md"
	gooseEnvRef   = "https://raw.githubusercontent.com/aaif-goose/goose/main/documentation/docs/guides/environment-variables.md"
	goosePermsRef = "https://raw.githubusercontent.com/aaif-goose/goose/main/documentation/docs/guides/managing-tools/goose-permissions.md"
	gooseShape    = "goose/config"
	gooseEnvOnly  = "Documented as an environment variable; the config-files page says other variables MAY also be read from config.yaml, so the root key is UNVERIFIED."
)

// gooseRow fills the tool, shape, owner, verification and id of one row.
func gooseRow(r SettingRow) SettingRow {
	r.Tool = "goose"
	if r.FileID == "" {
		r.FileID = gooseShape
	}
	if r.Owner == "" {
		r.Owner = OwnerUser
	}
	if r.Verified == "" {
		r.Verified = VerifiedDoc
	}
	if r.Source == "" {
		r.Source = gooseRef
	}
	r.ID = r.Tool + "/" + r.Path.String()
	return r
}

var gooseConfigPaths = OSPaths{
	Linux:   "~/.config/goose/config.yaml",
	Darwin:  "~/.config/goose/config.yaml",
	Windows: `%APPDATA%\Block\goose\config\config.yaml`,
}

var gooseCatalog = toolCatalog{
	Tool: "goose",
	VerifiedAgainst: "aaif-goose/goose docs on main fetched 2026-10-04 (config-files.md, environment-variables.md, goose-permissions.md; audit section 5 goose). " +
		"Not installed on node 1: no live evidence. extensions (MCP) is not mapped: goose spells the command cmd and the remote URL uri.",
	Files: []FileRow{
		{
			ID: "goose/user-config", Tool: "goose", Shape: gooseShape, Kind: KindSettings, Scope: ScopeUser,
			Paths: gooseConfigPaths, Format: FormatYAML, Precedence: 20, WriteNeeds: PrivUser, Grounding: doc(gooseRef),
			Note: "Shared by goose CLI and Desktop. GOOSE_PATH_ROOT relocates every goose root; environment variables override the file. Provider API keys placed here are ignored.",
		},
		{
			ID: "goose/user-permissions", Tool: "goose", Shape: "goose/permissions", Kind: KindPermissions, Scope: ScopeUser,
			Paths: OSPaths{
				Linux:   "~/.config/goose/permission.yaml",
				Darwin:  "~/.config/goose/permission.yaml",
				Windows: `%APPDATA%\Block\goose\config\permission.yaml`,
			},
			Format: FormatYAML, Precedence: 20, WriteNeeds: PrivUser, Grounding: doc(gooseRef),
			Note: "Per-tool permission levels written by goose configure; the key shape is not documented (UNVERIFIED), so it has no rows.",
		},
		{
			ID: "goose/user-secrets", Tool: "goose", Shape: "goose/secrets", Kind: KindToolState, Scope: ScopeUser,
			Paths: OSPaths{
				Linux:   "~/.config/goose/secrets.yaml",
				Darwin:  "~/.config/goose/secrets.yaml",
				Windows: `%APPDATA%\Block\goose\config\secrets.yaml`,
			},
			Format: FormatYAML, Precedence: 20, WriteNeeds: PrivNever, Grounding: doc(gooseRef),
			Note: "Plaintext API keys when the keyring is unavailable or GOOSE_DISABLE_KEYRING is set: never read, never written.",
		},
		{
			ID: "goose/tool-permissions-state", Tool: "goose", Shape: "goose/tool-permissions-state", Kind: KindToolState, Scope: ScopeUser,
			Paths: OSPaths{
				Linux:   "~/.config/goose/permissions/tool_permissions.json",
				Darwin:  "~/.config/goose/permissions/tool_permissions.json",
				Windows: `%APPDATA%\Block\goose\config\permissions\tool_permissions.json`,
			},
			Format: FormatJSON, Precedence: 20, WriteNeeds: PrivNever, Grounding: doc(gooseRef),
			Note: "Runtime permission decisions, auto-managed by goose.",
		},
	},
	Extra: []SettingRow{
		gooseRow(SettingRow{Path: KeyPath{"active_provider"}, Type: TypeString, Desc: "Provider in use; its model is providers.<name>.model."}),
		gooseRow(SettingRow{
			Path: KeyPath{"providers"}, Type: TypeObject, Desc: "Per-provider settings {enabled, model, configured}.",
			Note: "Legacy flat GOOSE_PROVIDER / GOOSE_MODEL root keys are read and migrated; the env vars of those names override the file.",
		}),
		gooseRow(SettingRow{Path: KeyPath{"GOOSE_PROVIDER"}, Type: TypeString, Deprecated: true, Desc: "Legacy flat provider key."}),
		gooseRow(SettingRow{Path: KeyPath{"GOOSE_MODEL"}, Type: TypeString, Deprecated: true, Desc: "Legacy flat model key."}),
		gooseRow(SettingRow{Path: KeyPath{"GOOSE_TEMPERATURE"}, Type: TypeFloat, Min: f64(0), Max: f64(1), Desc: "Sampling temperature (default model-specific)."}),
		gooseRow(SettingRow{Path: KeyPath{"GOOSE_MAX_TOKENS"}, Type: TypeInt, Min: f64(1), Desc: "Maximum tokens per model response (default model-specific)."}),
		gooseRow(SettingRow{Path: KeyPath{"GOOSE_CACHE_TTL"}, Type: TypeEnum, Enum: []string{"5m", "1h"}, Default: "\"5m\"", Desc: "Anthropic prompt-cache TTL."}),
		gooseRow(SettingRow{
			Path: KeyPath{"GOOSE_MODE"}, Type: TypeEnum, Enum: []string{"auto", "approve", "smart_approve", "chat"}, Default: "\"auto\"",
			Source: goosePermsRef, Desc: "Tool execution mode.", Danger: DangerConfirm,
			Note: "auto (the default) runs every tool without approval; smart_approve auto-approves low-risk calls; chat disables tools.",
		}),
		gooseRow(SettingRow{Path: KeyPath{"GOOSE_MAX_TURNS"}, Type: TypeInt, Default: "1000", Desc: "Turns allowed without user input."}),
		gooseRow(SettingRow{
			Path: KeyPath{"GOOSE_TOOLSHIM"}, Type: TypeBool, Default: "false", Desc: "Interpret text-based tool calls through a shim model.",
			Danger: DangerConfirm, Note: "true changes how tool calls are interpreted.",
		}),
		gooseRow(SettingRow{Path: KeyPath{"GOOSE_TOOLSHIM_OLLAMA_MODEL"}, Type: TypeString, Desc: "Ollama model for the tool shim."}),
		gooseRow(SettingRow{Path: KeyPath{"GOOSE_INPUT_LIMIT"}, Type: TypeInt, Desc: "Ollama input token limit (num_ctx)."}),
		gooseRow(SettingRow{Path: KeyPath{"GOOSE_CLI_MIN_PRIORITY"}, Type: TypeFloat, Min: f64(0), Max: f64(1), Default: "0", Desc: "Tool output verbosity filter."}),
		gooseRow(SettingRow{Path: KeyPath{"GOOSE_CLI_THEME"}, Type: TypeEnum, Enum: []string{"light", "dark", "ansi"}, Default: "\"ansi\"", Desc: "CLI markdown theme."}),
		gooseRow(SettingRow{Path: KeyPath{"GOOSE_CLI_LIGHT_THEME"}, Type: TypeString, Default: "\"GitHub\"", Desc: "bat syntax theme in light mode."}),
		gooseRow(SettingRow{Path: KeyPath{"GOOSE_CLI_DARK_THEME"}, Type: TypeString, Default: "\"zenburn\"", Desc: "bat syntax theme in dark mode."}),
		gooseRow(SettingRow{Path: KeyPath{"GOOSE_CLI_SHOW_COST"}, Type: TypeBool, Default: "false", Desc: "Show estimated token cost in the CLI."}),
		gooseRow(SettingRow{Path: KeyPath{"GOOSE_CLI_BELL"}, Type: TypeBool, Default: "false", Desc: "Ring the bell when a turn ends or approval is needed."}),
		gooseRow(SettingRow{
			Path: KeyPath{"GOOSE_ALLOWLIST"}, Type: TypeString, Desc: "URL of the allowed-extensions YAML.", Danger: DangerConfirm,
			Note: "Unset allows any extension install; the docs suggest a shell-profile variable, so a file value is not a lock.",
		}),
		gooseRow(SettingRow{Path: KeyPath{"GOOSE_DOCS_ROOT"}, Type: TypeString, Desc: "Documentation root for goose-doc-guide."}),
		gooseRow(SettingRow{Path: KeyPath{"GOOSE_RECIPE_GITHUB_REPO"}, Type: TypeString, Desc: "GitHub repository for shared recipes (org/repo)."}),
		gooseRow(SettingRow{
			Path: KeyPath{"GOOSE_AUTO_COMPACT_THRESHOLD"}, Type: TypeFloat, Min: f64(0), Max: f64(1), Default: "0.8",
			Desc: "Fraction of the context at which goose compacts (0 disables).",
		}),
		gooseRow(SettingRow{Path: KeyPath{"SECURITY_PROMPT_ENABLED"}, Type: TypeBool, Default: "false", Desc: "Prompt-injection detection."}),
		gooseRow(SettingRow{Path: KeyPath{"SECURITY_PROMPT_THRESHOLD"}, Type: TypeFloat, Min: f64(0.01), Max: f64(1), Default: "0.8", Desc: "Prompt-injection sensitivity."}),
		gooseRow(SettingRow{Path: KeyPath{"SECURITY_PROMPT_CLASSIFIER_ENABLED"}, Type: TypeBool, Default: "false", Desc: "ML-based prompt-injection classifier."}),
		gooseRow(SettingRow{Path: KeyPath{"SECURITY_PROMPT_CLASSIFIER_ENDPOINT"}, Type: TypeString, Desc: "Classifier endpoint URL."}),
		gooseRow(SettingRow{
			Path: KeyPath{"SECURITY_PROMPT_CLASSIFIER_TOKEN"}, Type: TypeString, Secret: true, Desc: "Token for the classifier endpoint.",
			Note: "A credential stored as a plaintext root key.",
		}),
		gooseRow(SettingRow{Path: KeyPath{"GOOSE_TELEMETRY_ENABLED"}, Type: TypeBool, Default: "false", Desc: "Anonymous usage data (opt-in)."}),
		gooseRow(SettingRow{Path: KeyPath{"GOOSE_SEARCH_PATHS"}, Type: TypeStringList, Desc: "Extra command search paths for extensions (not workspace directories)."}),
		gooseRow(SettingRow{Path: KeyPath{"otel_exporter_otlp_endpoint"}, Type: TypeString, Desc: "OTLP endpoint for OpenTelemetry export."}),
		gooseRow(SettingRow{Path: KeyPath{"otel_exporter_otlp_timeout"}, Type: TypeInt, Default: "10000", Desc: "OTLP export timeout in milliseconds."}),
		gooseRow(SettingRow{
			Path: KeyPath{"extensions"}, Type: TypeMCPMap,
			Desc: "Extensions (MCP servers) by name: {type: builtin | platform | stdio | streamable_http, name, enabled, cmd, args, uri, headers, envs, env_keys, timeout, available_tools, bundled, display_name}.",
			Note: "cmd not command, uri not url; SSE is unsupported. headers and envs may hold credentials.",
		}),
		gooseRow(SettingRow{Path: KeyPath{"slash_commands"}, Type: TypeObject, Desc: "Recipe slash commands: [{command, recipe_path}]."}),
		gooseRow(SettingRow{
			Path: KeyPath{"GOOSE_CONTEXT_LIMIT"}, Type: TypeInt, Source: gooseEnvRef, Verified: VerifiedUnverified,
			Desc: "Context limit for the main model in tokens.", Note: gooseEnvOnly,
		}),
		gooseRow(SettingRow{
			Path: KeyPath{"GOOSE_PROVIDER__HOST"}, Type: TypeString, Source: gooseEnvRef, Verified: VerifiedUnverified, Danger: DangerConfirm,
			Desc: "Custom API endpoint for the provider.", Note: gooseEnvOnly + " Redirects traffic off Observer's opt-in proxy route (which injects OPENAI_HOST).",
		}),
	},
	Map: []MapRow{
		{
			Key: KeyApprovalMode, Tool: "goose", Settings: []string{"goose/GOOSE_MODE"}, Transform: TransformEnumMap,
			Params: TransformParams{Enum: []EnumPair{{"ask", []any{"approve"}}, {"full_auto", []any{"auto"}}}}, Source: goosePermsRef,
			Note: "plan, ask_edits and auto_edits are refused; smart_approve and chat (no tools) have no canonical form.",
		},
		{Key: KeyMaxOutputTokens, Tool: "goose", Settings: []string{"goose/GOOSE_MAX_TOKENS"}, Transform: TransformIdentity, Source: gooseRef},
		{
			Key: KeyAutoCompact, Tool: "goose", Settings: []string{"goose/GOOSE_AUTO_COMPACT_THRESHOLD"}, Transform: TransformScale,
			Params: TransformParams{Factor: 0.01}, Source: gooseRef, Note: "A fraction of the window (percent / 100); 0 disables.",
		},
		{Key: KeyTemperature, Tool: "goose", Settings: []string{"goose/GOOSE_TEMPERATURE"}, Transform: TransformIdentity, Source: gooseRef},
		{Key: KeyTelemetryOff, Tool: "goose", Settings: []string{"goose/GOOSE_TELEMETRY_ENABLED"}, Transform: TransformBoolInvert, Source: gooseRef},
		{
			Key: KeyTheme, Tool: "goose", Settings: []string{"goose/GOOSE_CLI_THEME"}, Transform: TransformEnumMap,
			Params: TransformParams{Enum: []EnumPair{{"light", []any{"light"}}, {"dark", []any{"dark"}}}}, Source: gooseRef,
			Note: "ansi stays tool-specific.",
		},
	},
}
