package agentconfig

// Hermes Agent (Nous Research) catalogue. Sources: the vendor configuration
// guide and the repo's reference file cli-config.yaml.example (fetched
// 2026-10-03 for the audit, re-fetched 2026-10-04 for the key list), the
// hooks guide (2026-10-02), audit section 5 "hermes". Hermes was NOT
// installed on demo node 1, so every row is doc-grounded.
const (
	hermesRef      = "https://hermes-agent.nousresearch.com/docs/user-guide/configuration"
	hermesExample  = "https://raw.githubusercontent.com/NousResearch/hermes-agent/main/cli-config.yaml.example"
	hermesHooksRef = "https://hermes-agent.nousresearch.com/docs/user-guide/features/hooks"
	hermesCfg      = "hermes/config"
)

// hermesMCPShape: mcp_servers.<name> {command, args, env} or {url} for a
// remote server (the transport is inferred from the url key).
var hermesMCPShape = MCPShape{
	EnvKey: "env", TypeNames: map[string]string{"stdio": "", "http": ""},
	URLKeys: map[string]string{"http": "url"},
}

var hermesCatalog = toolCatalog{
	Tool:            "hermes",
	VerifiedAgainst: "vendor docs + cli-config.yaml.example (_config_version 49) fetched 2026-10-03 (audit section 5) and 2026-10-04; not installed on node 1, no live keys",
	Files: []FileRow{
		{
			ID: "hermes/user-config", Tool: "hermes", Shape: hermesCfg, Kind: KindSettings, Scope: ScopeUser,
			Paths:  OSPaths{Linux: "~/.hermes/config.yaml", Darwin: "~/.hermes/config.yaml", Windows: `%LOCALAPPDATA%\hermes\config.yaml`},
			Format: FormatYAML, Precedence: 20, WriteNeeds: PrivUser, Grounding: doc(hermesRef),
			Note: "HERMES_HOME, --profile and the sticky active_profile relocate the home (profiles live under <root>/profiles/<n>). " +
				"Windows: Observer's resolver uses %LOCALAPPDATA%\\hermes (internal/hook/hermes.go) while the vendor web doc says %USERPROFILE%\\.hermes; live-check before writing. " +
				"Observer writes plugins.enabled, mcp_servers.observer and providers.observer here; its YAML round-trip loses comments and key order.",
		},
		{
			ID: "hermes/user-env", Tool: "hermes", Shape: "hermes/env", Kind: KindToolState, Scope: ScopeUser,
			Paths:  OSPaths{Linux: "~/.hermes/.env", Darwin: "~/.hermes/.env", Windows: `%LOCALAPPDATA%\hermes\.env`},
			Format: FormatScript, Precedence: 15, WriteNeeds: PrivNever, Grounding: doc(hermesRef),
			Note: "dotenv file of provider API keys (config.yaml > .env > defaults): holds secrets, never read or written.",
		},
		{
			ID: "hermes/auth", Tool: "hermes", Shape: "hermes/auth", Kind: KindToolState, Scope: ScopeUser,
			Paths:  OSPaths{Linux: "~/.hermes/auth.json", Darwin: "~/.hermes/auth.json", Windows: `%LOCALAPPDATA%\hermes\auth.json`},
			Format: FormatJSON, Precedence: 0, WriteNeeds: PrivNever, Grounding: doc(hermesRef),
			Note: "OAuth credentials: never read.",
		},
		{
			ID: "hermes/shell-hooks-allowlist", Tool: "hermes", Shape: "hermes/shell-hooks-allowlist", Kind: KindToolState, Scope: ScopeUser,
			Paths:  OSPaths{Linux: "~/.hermes/shell-hooks-allowlist.json", Darwin: "~/.hermes/shell-hooks-allowlist.json", Windows: `%LOCALAPPDATA%\hermes\shell-hooks-allowlist.json`},
			Format: FormatJSON, Precedence: 0, WriteNeeds: PrivNever, Grounding: doc(hermesHooksRef),
			Note: "The shell hooks the user accepted; written by Hermes on acceptance. Writing it would accept a hook on the user's behalf.",
		},
		{
			ID: "hermes/observer-plugin", Tool: "hermes", Shape: "hermes/observer-plugin", Kind: KindHooks, Scope: ScopeUser,
			Paths:  OSPaths{Linux: "~/.hermes/plugins/superbased-observer/", Darwin: "~/.hermes/plugins/superbased-observer/", Windows: `%LOCALAPPDATA%\hermes\plugins\superbased-observer\`},
			Format: FormatScript, Precedence: 20, WriteNeeds: PrivNever,
			Grounding: Grounding{Kind: GroundAdapterDoc, Source: "docs/hermes-adapter.md", Date: auditDate},
			Note:      "Observer's own Python plugin (internal/hook/hermesplugin, written by observer init --hermes): edited only through the hook verbs.",
		},
	},
	Extra: []SettingRow{
		{
			ID: "hermes/_config_version", Tool: "hermes", FileID: hermesCfg, Path: KeyPath{"_config_version"}, Type: TypeInt,
			Desc: "Config schema version stamp maintained by hermes config migrate.", Source: hermesExample, Verified: VerifiedDoc, Owner: OwnerToolState,
			Note: "49 in the reference file.",
		},
		// --- model
		{
			ID: "hermes/model.default", Tool: "hermes", FileID: hermesCfg, Path: KeyPath{"model", "default"}, Type: TypeString,
			Desc: "Default model as provider/model (OpenRouter style).", Source: hermesRef, Verified: VerifiedDoc, Owner: OwnerUser,
			Note: "Overridden by --model and HERMES_INFERENCE_MODEL.",
		},
		{
			ID: "hermes/model.provider", Tool: "hermes", FileID: hermesCfg, Path: KeyPath{"model", "provider"}, Type: TypeString,
			Enum: []string{"auto", "openrouter", "nous", "anthropic", "openai-codex", "custom"}, Default: `"auto"`,
			Desc: "Inference provider for the default model.", Source: hermesExample, Verified: VerifiedDoc, Owner: OwnerUser,
			Note: "The enum lists the documented examples; more providers exist. Observer's route selects --provider observer at launch.",
		},
		{
			ID: "hermes/model.base_url", Tool: "hermes", FileID: hermesCfg, Path: KeyPath{"model", "base_url"}, Type: TypeString,
			Desc: "Endpoint for the custom provider.", Source: hermesRef, Verified: VerifiedDoc, Owner: OwnerUser, Danger: DangerConfirm,
			Note: "Honoured only for the custom provider; named providers hardcode their endpoint. A non-Observer URL bypasses the proxy route.",
		},
		{
			ID: "hermes/model.api_key", Tool: "hermes", FileID: hermesCfg, Path: KeyPath{"model", "api_key"}, Type: TypeString,
			Desc: "API key for the default provider (a ${VAR} reference is preferred over a literal).", Source: hermesExample,
			Verified: VerifiedDoc, Owner: OwnerUser, Secret: true, Note: "The vendor discourages a literal key here.",
		},
		{
			ID: "hermes/model.max_tokens", Tool: "hermes", FileID: hermesCfg, Path: KeyPath{"model", "max_tokens"}, Type: TypeInt,
			Desc: "Maximum output tokens per response.", Source: hermesRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "hermes/model.context_length", Tool: "hermes", FileID: hermesCfg, Path: KeyPath{"model", "context_length"}, Type: TypeInt,
			Desc: "Context window in tokens (auto-detected when unset).", Source: hermesExample, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "hermes/model.temperature", Tool: "hermes", FileID: hermesCfg, Path: KeyPath{"model", "temperature"}, Type: TypeFloat,
			Desc: "Sampling temperature.", Source: hermesRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "hermes/model.streaming", Tool: "hermes", FileID: hermesCfg, Path: KeyPath{"model", "streaming"}, Type: TypeBool, Default: "true",
			Desc: "Stream model responses.", Source: hermesExample, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "hermes/model.default_headers", Tool: "hermes", FileID: hermesCfg, Path: KeyPath{"model", "default_headers"}, Type: TypeStringMap,
			Desc: "Extra HTTP headers sent with every model request.", Source: hermesExample, Verified: VerifiedDoc, Owner: OwnerUser, Secret: true,
			Note: "Headers can carry an authorization value, so the map is treated as secret.",
		},
		{
			ID: "hermes/model.reasoning_effort", Tool: "hermes", FileID: hermesCfg, Path: KeyPath{"model", "reasoning_effort"}, Type: TypeString,
			Enum: []string{"low", "medium", "high"}, Desc: "Reasoning effort (web doc location).", Source: hermesRef,
			Verified: VerifiedUnverified, Owner: OwnerUser,
			Note: "The web doc puts it under model, the reference file under agent; which one wins is UNVERIFIED (audit V5).",
		},
		{
			ID: "hermes/providers", Tool: "hermes", FileID: hermesCfg, Path: KeyPath{"providers"}, Type: TypeObject,
			Desc:   "Named providers: base_url, api_key or key_env, key_cmd, api_mode, timeouts, extra_headers, per-model overrides.",
			Source: hermesExample, Verified: VerifiedDoc, Owner: OwnerUser, Secret: true, Danger: DangerConfirm,
			Note: "An entry may hold a literal api_key, so the map is secret; key_env (an env var NAME) is the safe reference. " +
				"A base_url that is not Observer bypasses the proxy; Observer's own providers.observer entry is an owned span.",
		},
		{
			ID: "hermes/provider_routing.sort", Tool: "hermes", FileID: hermesCfg, Path: KeyPath{"provider_routing", "sort"}, Type: TypeEnum,
			Enum: []string{"price", "throughput", "latency"}, Desc: "OpenRouter provider ordering.", Source: hermesExample, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "hermes/provider_routing.only", Tool: "hermes", FileID: hermesCfg, Path: KeyPath{"provider_routing", "only"}, Type: TypeStringList,
			Desc: "Only route to these OpenRouter provider slugs.", Source: hermesExample, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "hermes/provider_routing.ignore", Tool: "hermes", FileID: hermesCfg, Path: KeyPath{"provider_routing", "ignore"}, Type: TypeStringList,
			Desc: "Never route to these OpenRouter provider slugs.", Source: hermesExample, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "hermes/provider_routing.data_collection", Tool: "hermes", FileID: hermesCfg, Path: KeyPath{"provider_routing", "data_collection"}, Type: TypeEnum,
			Enum: []string{"allow", "deny"}, Desc: "Whether OpenRouter may route to providers that retain data.", Source: hermesExample, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "hermes/auxiliary", Tool: "hermes", FileID: hermesCfg, Path: KeyPath{"auxiliary"}, Type: TypeObject,
			Desc:   "Per-task side models (vision, web_extract, title_generation, session_search, compression, background_review): provider, model, timeout.",
			Source: hermesExample, Verified: VerifiedDoc, Owner: OwnerUser, Note: "There is no single small/fast model slot.",
		},
		// --- compression
		{
			ID: "hermes/compression.enabled", Tool: "hermes", FileID: hermesCfg, Path: KeyPath{"compression", "enabled"}, Type: TypeBool, Default: "true",
			Desc: "Compress the conversation when it nears the context limit.", Source: hermesExample, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "hermes/compression.threshold", Tool: "hermes", FileID: hermesCfg, Path: KeyPath{"compression", "threshold"}, Type: TypeFloat,
			Min: f64(0.1), Max: f64(1), Default: "0.5", Desc: "Fraction of model.context_length at which compression starts.",
			Source: hermesExample, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "hermes/compression.target_ratio", Tool: "hermes", FileID: hermesCfg, Path: KeyPath{"compression", "target_ratio"}, Type: TypeFloat,
			Min: f64(0.1), Max: f64(0.8), Default: "0.2", Desc: "Target size of the compressed history as a fraction.",
			Source: hermesExample, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "hermes/compression.protect_last_n", Tool: "hermes", FileID: hermesCfg, Path: KeyPath{"compression", "protect_last_n"}, Type: TypeInt,
			Default: "20", Desc: "Most recent messages never compressed.", Source: hermesExample, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "hermes/compression.threshold_tokens", Tool: "hermes", FileID: hermesCfg, Path: KeyPath{"compression", "threshold_tokens"}, Type: TypeInt,
			Desc: "Absolute token threshold that overrides the fraction when set.", Source: hermesExample, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "hermes/compression.idle_compact_after_seconds", Tool: "hermes", FileID: hermesCfg, Path: KeyPath{"compression", "idle_compact_after_seconds"}, Type: TypeInt,
			Default: "0", Desc: "Compact after this many idle seconds (0 = off).", Source: hermesExample, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "hermes/context_file_max_chars", Tool: "hermes", FileID: hermesCfg, Path: KeyPath{"context_file_max_chars"}, Type: TypeInt,
			Desc: "Truncation limit for injected context files (AGENTS.md, .hermes.md, CLAUDE.md, .cursorrules).", Source: hermesRef,
			Verified: VerifiedDoc, Owner: OwnerUser, Note: "Context file names are fixed; there is no rename knob.",
		},
		// --- agent
		{
			ID: "hermes/agent.reasoning_effort", Tool: "hermes", FileID: hermesCfg, Path: KeyPath{"agent", "reasoning_effort"}, Type: TypeString,
			Enum: []string{"low", "medium", "high", "xhigh"}, Default: `"medium"`, Desc: "Reasoning effort (reference-file location).",
			Source: hermesExample, Verified: VerifiedDoc, Owner: OwnerUser,
			Note: "The web doc names model.reasoning_effort instead; which wins is UNVERIFIED (audit V5).",
		},
		{
			ID: "hermes/agent.reasoning_overrides", Tool: "hermes", FileID: hermesCfg, Path: KeyPath{"agent", "reasoning_overrides"}, Type: TypeStringMap,
			Desc: "Per-model reasoning effort overrides.", Source: hermesExample, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "hermes/agent.max_turns", Tool: "hermes", FileID: hermesCfg, Path: KeyPath{"agent", "max_turns"}, Type: TypeInt, Default: "500",
			Desc: "Maximum agent turns per run.", Source: hermesExample, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "hermes/agent.service_tier", Tool: "hermes", FileID: hermesCfg, Path: KeyPath{"agent", "service_tier"}, Type: TypeString,
			Enum: []string{"", "normal", "fast", "auto", "cold"}, Desc: "Provider service tier.", Source: hermesExample, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "hermes/agent.personalities", Tool: "hermes", FileID: hermesCfg, Path: KeyPath{"agent", "personalities"}, Type: TypeObject,
			Desc: "Named personalities (a system prompt or a config map each).", Source: hermesExample, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		// --- approvals, terminal, security
		{
			ID: "hermes/approvals.mode", Tool: "hermes", FileID: hermesCfg, Path: KeyPath{"approvals", "mode"}, Type: TypeEnum,
			Enum: []string{"manual", "smart", "off"}, Desc: "Approval of dangerous commands: manual prompts, smart lets a model judge, off never asks.",
			Source: hermesRef, Verified: VerifiedDoc, Owner: OwnerUser, Danger: DangerConfirm,
			Note: "off (and smart) loosen approvals; --yolo / HERMES_YOLO_MODE=1 do the same per session.",
		},
		{
			ID: "hermes/approvals.command_allowlist", Tool: "hermes", FileID: hermesCfg, Path: KeyPath{"approvals", "command_allowlist"}, Type: TypeStringList,
			Desc: "Regexes of commands that run without approval.", Source: hermesRef, Verified: VerifiedDoc, Owner: OwnerUser, Danger: DangerConfirm,
			Note: "A broad regex (.*) approves every command.",
		},
		{
			ID: "hermes/terminal.backend", Tool: "hermes", FileID: hermesCfg, Path: KeyPath{"terminal", "backend"}, Type: TypeEnum,
			Enum:    []string{"local", "docker", "ssh", "modal", "daytona", "vercel_sandbox", "singularity"},
			Default: `"local"`, Desc: "Where commands run.", Source: hermesRef, Verified: VerifiedDoc, Owner: OwnerUser, Danger: DangerConfirm,
			Note: "local (the default) has no sandbox.",
		},
		{
			ID: "hermes/terminal.cwd", Tool: "hermes", FileID: hermesCfg, Path: KeyPath{"terminal", "cwd"}, Type: TypeString, Default: `"."`,
			Desc: "Working directory for commands.", Source: hermesExample, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "hermes/terminal.timeout", Tool: "hermes", FileID: hermesCfg, Path: KeyPath{"terminal", "timeout"}, Type: TypeInt, Default: "180",
			Desc: "Command timeout in seconds.", Source: hermesExample, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "hermes/terminal.docker_image", Tool: "hermes", FileID: hermesCfg, Path: KeyPath{"terminal", "docker_image"}, Type: TypeString,
			Desc: "Image for the docker backend.", Source: hermesExample, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "hermes/terminal.docker_mount_cwd_to_workspace", Tool: "hermes", FileID: hermesCfg, Path: KeyPath{"terminal", "docker_mount_cwd_to_workspace"},
			Type: TypeBool, Default: "false", Desc: "Mount the host working directory into the container workspace.", Source: hermesRef,
			Verified: VerifiedDoc, Owner: OwnerUser, Danger: DangerConfirm, Note: "true gives the container write access to the host tree.",
		},
		{
			ID: "hermes/terminal.sudo_password", Tool: "hermes", FileID: hermesCfg, Path: KeyPath{"terminal", "sudo_password"}, Type: TypeString,
			Desc: "Password Hermes supplies to sudo.", Source: hermesExample, Verified: VerifiedDoc, Owner: OwnerUser, Secret: true,
		},
		{
			ID: "hermes/terminal.container_memory", Tool: "hermes", FileID: hermesCfg, Path: KeyPath{"terminal", "container_memory"}, Type: TypeInt,
			Default: "5120", Desc: "Container memory limit in MB.", Source: hermesExample, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "hermes/security.tirith_enabled", Tool: "hermes", FileID: hermesCfg, Path: KeyPath{"security", "tirith_enabled"}, Type: TypeBool,
			Desc: "Run the tirith command scanner before commands.", Source: hermesExample, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "hermes/security.tirith_fail_open", Tool: "hermes", FileID: hermesCfg, Path: KeyPath{"security", "tirith_fail_open"}, Type: TypeBool,
			Default: "true", Desc: "Let a command run when the scanner fails.", Source: hermesExample, Verified: VerifiedDoc, Owner: OwnerUser,
			Danger: DangerConfirm, Note: "true runs commands unscanned on a scanner failure.",
		},
		{
			ID: "hermes/security.protected_instruction_files", Tool: "hermes", FileID: hermesCfg, Path: KeyPath{"security", "protected_instruction_files"},
			Type: TypeBool, Default: "true", Desc: "Gate agent writes to instruction files, even under --yolo.", Source: hermesRef,
			Verified: VerifiedDoc, Owner: OwnerUser, Danger: DangerConfirm, Note: "false lets the agent rewrite its own instructions.",
		},
		{
			ID: "hermes/security.protected_instruction_extra_patterns", Tool: "hermes", FileID: hermesCfg, Path: KeyPath{"security", "protected_instruction_extra_patterns"},
			Type: TypeStringList, Desc: "Extra fnmatch patterns treated as protected instruction files.", Source: hermesExample, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "hermes/auth.adopt_external_logins", Tool: "hermes", FileID: hermesCfg, Path: KeyPath{"auth", "adopt_external_logins"}, Type: TypeBool,
			Desc: "Borrow Codex / Claude Code logins.", Source: hermesRef, Verified: VerifiedDoc, Owner: OwnerUser, Danger: DangerConfirm,
			Note: "true reuses another tool's credentials.",
		},
		{
			ID: "hermes/delegation.subagent_auto_approve", Tool: "hermes", FileID: hermesCfg, Path: KeyPath{"delegation", "subagent_auto_approve"}, Type: TypeString,
			Enum: []string{"false", "true", "once"}, Desc: "Auto-approve subagent actions (false, true or once).", Source: hermesExample,
			Verified: VerifiedDoc, Owner: OwnerUser, Danger: DangerConfirm, Note: "A YAML bool or the string once; true approves every subagent action.",
		},
		{
			ID: "hermes/delegation.max_spawn_depth", Tool: "hermes", FileID: hermesCfg, Path: KeyPath{"delegation", "max_spawn_depth"}, Type: TypeInt,
			Min: f64(1), Max: f64(3), Default: "1", Desc: "How deep subagents may spawn subagents.", Source: hermesExample, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "hermes/delegation.model", Tool: "hermes", FileID: hermesCfg, Path: KeyPath{"delegation", "model"}, Type: TypeString,
			Desc: "Model for subagents (empty = inherit).", Source: hermesExample, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "hermes/tool_loop_guardrails.hard_stop_enabled", Tool: "hermes", FileID: hermesCfg, Path: KeyPath{"tool_loop_guardrails", "hard_stop_enabled"},
			Type: TypeBool, Default: "false", Desc: "Stop a run stuck in a failing tool loop.", Source: hermesExample, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "hermes/lsp.trusted_workspaces", Tool: "hermes", FileID: hermesCfg, Path: KeyPath{"lsp", "trusted_workspaces"}, Type: TypeStringList,
			Desc: "Workspaces where language servers may start.", Source: hermesExample, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "hermes/platform_toolsets", Tool: "hermes", FileID: hermesCfg, Path: KeyPath{"platform_toolsets"}, Type: TypeObject,
			Desc: "Toolsets enabled per platform (cli: [hermes-cli], ...).", Source: hermesRef, Verified: VerifiedDoc, Owner: OwnerUser,
			Note: "There is no deny list: drop a toolset from the platform's list instead.",
		},
		{
			ID: "hermes/toolsets", Tool: "hermes", FileID: hermesCfg, Path: KeyPath{"toolsets"}, Type: TypeStringList,
			Desc: "Legacy top-level toolset list.", Source: hermesRef, Verified: VerifiedDoc, Owner: OwnerUser, Deprecated: true,
			Note: "Deprecated and ignored; use platform_toolsets.",
		},
		// --- MCP, plugins, skills, hooks
		{
			ID: "hermes/mcp_servers", Tool: "hermes", FileID: hermesCfg, Path: KeyPath{"mcp_servers"}, Type: TypeMCPMap,
			Desc: "MCP servers by name: command / args / env or url, plus headers, timeouts, lazy and sampling.", Source: hermesExample,
			Verified: VerifiedDoc, Owner: OwnerUser, Note: "Observer's mcp_servers.observer entry is an owned span.",
		},
		{
			ID: "hermes/plugins.enabled", Tool: "hermes", FileID: hermesCfg, Path: KeyPath{"plugins", "enabled"}, Type: TypeStringList,
			Desc: "Python plugins under plugins/ that load.", Source: hermesHooksRef, Verified: VerifiedDoc, Owner: OwnerUser,
			Note: "Observer's superbased-observer entry is an owned span; removing it drops capture.",
		},
		{
			ID: "hermes/plugins.clone_timeout_seconds", Tool: "hermes", FileID: hermesCfg, Path: KeyPath{"plugins", "clone_timeout_seconds"}, Type: TypeInt,
			Min: f64(1), Max: f64(3600), Default: "300", Desc: "Timeout for cloning a plugin repository.", Source: hermesExample, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "hermes/skills.external_dirs", Tool: "hermes", FileID: hermesCfg, Path: KeyPath{"skills", "external_dirs"}, Type: TypeStringList,
			Desc: "Read-only shared skill directories (e.g. ~/.agents/skills).", Source: hermesExample, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "hermes/skills.auto_load", Tool: "hermes", FileID: hermesCfg, Path: KeyPath{"skills", "auto_load"}, Type: TypeObject,
			Desc: "Skills loaded automatically.", Source: hermesRef, Verified: VerifiedDoc, Owner: OwnerUser, Note: "Shape not stated in the doc.",
		},
		{
			ID: "hermes/skills.write_approval", Tool: "hermes", FileID: hermesCfg, Path: KeyPath{"skills", "write_approval"}, Type: TypeBool,
			Desc: "Ask before the agent writes a skill.", Source: hermesRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "hermes/skills.guard_agent_created", Tool: "hermes", FileID: hermesCfg, Path: KeyPath{"skills", "guard_agent_created"}, Type: TypeBool,
			Desc: "Scan agent-created skills before they load.", Source: hermesRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "hermes/hooks", Tool: "hermes", FileID: hermesCfg, Path: KeyPath{"hooks"}, Type: TypeObject,
			Desc: "Shell hooks by event (hooks.<event>[] {command, ...}); events mirror the plugin VALID_HOOKS list.", Source: hermesHooksRef,
			Verified: VerifiedDoc, Owner: OwnerUser, Note: "Shell hooks need acceptance (shell-hooks-allowlist.json); hermes serve skips them.",
		},
		{
			ID: "hermes/hooks_auto_accept", Tool: "hermes", FileID: hermesCfg, Path: KeyPath{"hooks_auto_accept"}, Type: TypeBool,
			Desc: "Accept shell hooks without a prompt in non-interactive runs.", Source: hermesHooksRef, Verified: VerifiedDoc, Owner: OwnerUser,
			Danger: DangerConfirm, Note: "true runs any configured shell hook unreviewed (also HERMES_ACCEPT_HOOKS=1).",
		},
		// --- telemetry, updates, display, memory
		{
			ID: "hermes/telemetry.shared_metrics.enabled", Tool: "hermes", FileID: hermesCfg, Path: KeyPath{"telemetry", "shared_metrics", "enabled"},
			Type: TypeBool, Default: "false", Desc: "Opt in to shared usage metrics.", Source: hermesRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "hermes/telemetry.shared_metrics.send", Tool: "hermes", FileID: hermesCfg, Path: KeyPath{"telemetry", "shared_metrics", "send"},
			Type: TypeBool, Default: "false", Desc: "Send the collected shared metrics.", Source: hermesRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "hermes/updates.check", Tool: "hermes", FileID: hermesCfg, Path: KeyPath{"updates", "check"}, Type: TypeBool,
			Desc: "Check for updates (hermes update installs them manually).", Source: hermesRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "hermes/updates.pre_update_backup", Tool: "hermes", FileID: hermesCfg, Path: KeyPath{"updates", "pre_update_backup"}, Type: TypeBool,
			Desc: "Back up the home before an update.", Source: hermesRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "hermes/updates.backup_keep", Tool: "hermes", FileID: hermesCfg, Path: KeyPath{"updates", "backup_keep"}, Type: TypeInt,
			Desc: "How many pre-update backups to keep.", Source: hermesRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "hermes/display.skin", Tool: "hermes", FileID: hermesCfg, Path: KeyPath{"display", "skin"}, Type: TypeString, Default: `"default"`,
			Desc: "Named UI skin.", Source: hermesRef, Verified: VerifiedDoc, Owner: OwnerUser, Note: "Named skins only; no light / dark switch.",
		},
		{
			ID: "hermes/display.tool_progress", Tool: "hermes", FileID: hermesCfg, Path: KeyPath{"display", "tool_progress"}, Type: TypeEnum,
			Enum: []string{"off", "new", "all", "verbose", "log"}, Default: `"all"`, Desc: "How much tool progress is shown.",
			Source: hermesExample, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "hermes/display.show_reasoning", Tool: "hermes", FileID: hermesCfg, Path: KeyPath{"display", "show_reasoning"}, Type: TypeBool,
			Desc: "Show reasoning blocks.", Source: hermesExample, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "hermes/memory.memory_enabled", Tool: "hermes", FileID: hermesCfg, Path: KeyPath{"memory", "memory_enabled"}, Type: TypeBool, Default: "true",
			Desc: "Keep persistent agent memory.", Source: hermesExample, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "hermes/sessions.retention_days", Tool: "hermes", FileID: hermesCfg, Path: KeyPath{"sessions", "retention_days"}, Type: TypeInt, Default: "90",
			Desc: "Days of session history kept by auto-prune.", Source: hermesExample, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "hermes/prompt_caching.cache_ttl", Tool: "hermes", FileID: hermesCfg, Path: KeyPath{"prompt_caching", "cache_ttl"}, Type: TypeEnum,
			Enum: []string{"5m", "1h"}, Default: `"5m"`, Desc: "Prompt-cache lifetime requested from the provider.", Source: hermesExample, Verified: VerifiedDoc, Owner: OwnerUser,
		},
	},
	Map: []MapRow{
		{
			Key: KeyModel, Tool: "hermes", Settings: []string{"hermes/model.default"}, Transform: TransformModelAlias,
			Params: TransformParams{Model: ModelSpelling{Qualified: true}}, Source: hermesRef, Note: "model.provider is a separate key.",
		},
		{Key: KeyMaxOutputTokens, Tool: "hermes", Settings: []string{"hermes/model.max_tokens"}, Transform: TransformIdentity, Source: hermesRef},
		{Key: KeyContextLimit, Tool: "hermes", Settings: []string{"hermes/model.context_length"}, Transform: TransformIdentity, Source: hermesExample},
		{
			Key: KeyAutoCompact, Tool: "hermes", Settings: []string{"hermes/compression.threshold"}, Transform: TransformScale,
			Params: TransformParams{Factor: 0.01}, Source: hermesExample, Note: "A fraction of model.context_length (percent / 100).",
		},
		{
			Key: KeyReasoningEffort, Tool: "hermes", Settings: []string{"hermes/agent.reasoning_effort"}, Transform: TransformEnumMap,
			Params: TransformParams{Enum: []EnumPair{{"low", []any{"low"}}, {"medium", []any{"medium"}}, {"high", []any{"high"}}, {"xhigh", []any{"xhigh"}}}},
			Source: hermesExample, Note: "Written at the reference-file location; none, minimal and max are refused.",
		},
		{Key: KeyTemperature, Tool: "hermes", Settings: []string{"hermes/model.temperature"}, Transform: TransformIdentity, Source: hermesRef},
		{
			Key: KeyApprovalMode, Tool: "hermes", Settings: []string{"hermes/approvals.mode"}, Transform: TransformEnumMap,
			Params: TransformParams{Enum: []EnumPair{{"auto_edits", []any{"manual"}}, {"full_auto", []any{"off"}}}}, Source: hermesRef,
			Note: "Hermes approvals gate dangerous commands only, so manual is auto_edits; smart (model-judged) has no canonical form; plan, ask and ask_edits are refused.",
		},
		{
			Key: KeyMCPServers, Tool: "hermes", Settings: []string{"hermes/mcp_servers"}, Transform: TransformMCPShape,
			Params: TransformParams{MCP: hermesMCPShape}, Lossy: true, Source: hermesExample, Note: mcpLossyNote,
		},
		{
			Key: KeyTelemetryOff, Tool: "hermes", Settings: []string{"hermes/telemetry.shared_metrics.enabled"}, Transform: TransformBoolInvert,
			Source: hermesRef, Note: "Shared metrics are opt-in (default off).",
		},
		{
			Key: KeyAutoUpdate, Tool: "hermes", Settings: []string{"hermes/updates.check"}, Transform: TransformIdentity, Source: hermesRef,
			Note: "Checks only; hermes update installs.",
		},
		{
			Key: KeyBaseURL, Tool: "hermes", Settings: []string{"hermes/model.base_url"}, Transform: TransformIdentity, Source: hermesRef,
			Note: "Custom provider only; Observer's route uses the providers.observer entry instead.",
		},
	},
}
