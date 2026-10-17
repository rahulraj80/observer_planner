package agentconfig

// Grok CLI (xAI) catalogue, doc-authored (phase P2). Sources: the Grok
// config.toml reference and enterprise pages (fetched 2026-10-04 to confirm
// the audit's key list), audit docs/audits/adapter-config-surface-2026-10-03.md
// section 5 "grok" and section 6 (node 1 live record).
const (
	grokRef        = "https://docs.x.ai/build/settings/reference"
	grokEnterprise = "https://docs.x.ai/build/enterprise"
	grokSettings   = "https://docs.x.ai/build/settings"
	grokHooks      = "https://docs.x.ai/build/features/hooks"
	grokLiveSrc    = "node 1 live probe 2026-10-03 (audit section 6, grok)"
)

// grokUserScopes are the scopes of every config.toml key outside the three
// project-contributable tables ([mcp_servers], [plugins], [permission]): a
// project .grok/config.toml contributes nothing else.
var grokUserScopes = []Scope{ScopeSystem, ScopeUser}

func grokRow(path KeyPath, typ ValueType, desc string) SettingRow {
	return SettingRow{
		ID: "grok/" + path.String(), Tool: "grok", FileID: "grok/config", Path: path, Scopes: grokUserScopes,
		Type: typ, Desc: desc, Source: grokRef, Verified: VerifiedDoc, Owner: OwnerUser,
	}
}

func grokWith(r SettingRow, edit func(*SettingRow)) SettingRow {
	edit(&r)
	return r
}

var grokCatalog = toolCatalog{
	Tool: "grok",
	VerifiedAgainst: "docs.x.ai config.toml reference + enterprise page (audit section 5, fetched 2026-10-03; reference re-fetched 2026-10-04); " +
		"node 1 live key names 2026-10-03 for cli.installer, marketplace and the signed settings_cache.json",
	Files: []FileRow{
		{
			ID: "grok/system-managed-config", Tool: "grok", Shape: "grok/config", Kind: KindSettings, Scope: ScopeSystem,
			Paths: OSPaths{Linux: "/etc/grok/managed_config.toml", Darwin: "/etc/grok/managed_config.toml"}, Format: FormatTOML,
			Precedence: 10, WriteNeeds: PrivAdminRoot, Grounding: doc(grokEnterprise),
			Note: "Managed DEFAULTS (any lower layer may override them). Windows path not documented.",
		},
		{
			ID: "grok/user-managed-config", Tool: "grok", Shape: "grok/config", Kind: KindSettings, Scope: ScopeSystem,
			Paths: samePath("~/.grok/managed_config.toml"), Format: FormatTOML,
			Precedence: 15, WriteNeeds: PrivAdminRoot, Grounding: doc(grokEnterprise),
			Note: "Per-user managed defaults below config.toml. A user-writable home path: an admin-provisioned layer (lever B), never a lock. node 1 had managed_config.lock beside it (audit section 6).",
		},
		{
			ID: "grok/user-config", Tool: "grok", Shape: "grok/config", Kind: KindSettings, Scope: ScopeUser,
			Paths: samePath("~/.grok/config.toml"), Format: FormatTOML,
			Precedence: 20, WriteNeeds: PrivUser, Grounding: live("~/.grok/config.toml carries cli.installer and marketplace.* only"),
			Note: "GROK_HOME relocates ~/.grok. Windows: the reference says %APPDATA%\\grok\\ while the settings overview says %USERPROFILE%\\.grok\\config.toml (conflict, unverified live; Observer's adapter grounds ~/.grok). GROK_* env vars override this file.",
		},
		{
			ID: "grok/project-config", Tool: "grok", Shape: "grok/config", Kind: KindSettings, Scope: ScopeProjectShared,
			Paths: samePath("<project>/.grok/config.toml"), Format: FormatTOML,
			Precedence: 30, WriteNeeds: PrivUser, Grounding: doc(grokRef),
			Note: "Walked from cwd up to the git root; contributes ONLY [mcp_servers], [plugins] and [permission].",
		},
		{
			ID: "grok/user-requirements", Tool: "grok", Shape: "grok/requirements", Kind: KindPermissions, Scope: ScopeManaged,
			Paths: samePath("~/.grok/requirements.toml"), Format: FormatTOML,
			Precedence: 90, WriteNeeds: PrivAdminRoot, Grounding: doc(grokEnterprise),
			Note: "Pins no lower layer (nor remote settings) may override. A user-writable home path: lever B, not a lock. Absent on node 1.",
		},
		{
			ID: "grok/system-requirements", Tool: "grok", Shape: "grok/requirements", Kind: KindPermissions, Scope: ScopeManaged,
			Paths: OSPaths{Linux: "/etc/grok/requirements.toml", Darwin: "/etc/grok/requirements.toml"}, Format: FormatTOML,
			Precedence: 100, WriteNeeds: PrivAdminRoot, Grounding: doc(grokEnterprise),
			Note: "Highest layer (root-owned). Windows path not documented. Grok also honours Claude Code managed-settings.json; its own requirements win.",
		},
		{
			ID: "grok/user-sandbox", Tool: "grok", Shape: "grok/sandbox", Kind: KindSettings, Scope: ScopeUser,
			Paths: samePath("~/.grok/sandbox.toml"), Format: FormatTOML, Precedence: 20, WriteNeeds: PrivUser, Grounding: doc(grokRef),
			Note: "Custom sandbox profiles (extends, restrict_network, read_only, read_write, deny). The reference does not show the table nesting, so the shape carries no rows yet.",
		},
		{
			ID: "grok/project-sandbox", Tool: "grok", Shape: "grok/sandbox", Kind: KindSettings, Scope: ScopeProjectShared,
			Paths: samePath("<project>/.grok/sandbox.toml"), Format: FormatTOML, Precedence: 30, WriteNeeds: PrivUser, Grounding: doc(grokRef),
		},
		{
			ID: "grok/user-hooks", Tool: "grok", Shape: "grok/hooks", Kind: KindHooks, Scope: ScopeUser,
			Paths: samePath("~/.grok/hooks/"), Format: FormatJSON, Precedence: 20, WriteNeeds: PrivUser, Grounding: doc(grokHooks),
			Note: "A directory of *.json hook files (PreToolUse is the only blocking event). Grok also replays .claude/settings.json and .cursor/hooks.json hooks.",
		},
		{
			ID: "grok/settings-cache", Tool: "grok", Shape: "grok/settings-cache", Kind: KindToolState, Scope: ScopeUser,
			Paths: samePath("~/.grok/settings_cache.json"), Format: FormatJSON, Precedence: 0, WriteNeeds: PrivNever,
			Grounding: live("~/.grok/settings_cache.json is a signed payload: payload string + signature byte list"),
			Note:      "Grok's signed cache of server-managed settings: read-only evidence of the vendor's org policy, never an edit target.",
		},
		{
			ID: "grok/auth", Tool: "grok", Shape: "grok/auth", Kind: KindToolState, Scope: ScopeUser,
			Paths: samePath("~/.grok/auth.json"), Format: FormatJSON, Precedence: 0, WriteNeeds: PrivNever, Grounding: doc(grokSettings),
			Note: "Credential store: never read, never written.",
		},
	},
	Extra: []SettingRow{
		// [models]
		grokWith(grokRow(KeyPath{"models", "default"}, TypeString, "Model used for new sessions (e.g. grok-build)."), func(r *SettingRow) {
			r.Note = "Env GROK_DEFAULT_MODEL overrides it; requirements.toml may pin it."
		}),
		grokRow(KeyPath{"models", "web_search"}, TypeString, "Model for the client web_search tool."),
		grokWith(grokRow(KeyPath{"models", "default_reasoning_effort"}, TypeString, "Default reasoning effort for the default model."), func(r *SettingRow) {
			r.Verified = VerifiedUnverified
			r.Note = "Allowed levels are not listed (audit: values UNVERIFIED)."
		}),
		grokRow(KeyPath{"models", "session_summary"}, TypeString, "Model for session summaries."),
		grokRow(KeyPath{"models", "image_description"}, TypeString, "Model for image descriptions."),
		grokWith(grokRow(KeyPath{"models", "extra_headers"}, TypeStringMap, "Headers sent with every model request."), func(r *SettingRow) {
			r.Note = "A header value can carry a credential: the secret posture applies to every value."
		}),
		grokRow(KeyPath{"models", "temperature"}, TypeFloat, "Global sampling temperature."),
		grokRow(KeyPath{"models", "top_p"}, TypeFloat, "Global nucleus sampling value."),
		grokRow(KeyPath{"models", "max_completion_tokens"}, TypeInt, "Global maximum output tokens per response."),
		grokRow(KeyPath{"models", "max_retries"}, TypeInt, "Global inference retry count."),
		grokRow(KeyPath{"models", "stream_tool_calls"}, TypeBool, "Whether tool calls are requested as a stream."),
		grokRow(KeyPath{"models", "allowed_models"}, TypeStringList, "Glob list restricting the model picker, default and -m selection."),
		grokRow(KeyPath{"models", "hidden_models"}, TypeStringList, "Model ids hidden from the picker (still usable via -m)."),
		grokRow(KeyPath{"models", "disabled_models"}, TypeStringList, "Model ids removed from the catalogue (wins over hidden)."),
		grokWith(grokRow(KeyPath{"model"}, TypeObject, "Custom / BYOK models by id: model, base_url, name, api_backend, env_key, api_key, context_window, sampling and retry fields."), func(r *SettingRow) {
			r.Secret = true
			r.Danger = DangerConfirm
			r.Note = "Entries may carry an inline api_key (prefer env_key), so the whole map is treated as secret. A base_url override sends model traffic elsewhere. api_backend is chat_completions, responses or messages; context_window drives auto-compact timing."
		}),
		// [mcp_servers] (project-contributable)
		{
			ID: "grok/mcp_servers", Tool: "grok", FileID: "grok/config", Path: KeyPath{"mcp_servers"}, Type: TypeMCPMap,
			Desc:   "MCP servers by name: command / args / env / cwd for stdio, url / headers / bearer_token_env_var for HTTP; enabled, startup_timeout_sec, tool_timeout_sec, tool_timeouts.",
			Source: grokRef, Verified: VerifiedDoc, Owner: OwnerUser,
			Note: "enabled = false switches a server off. Grok also merges ~/.claude.json, .cursor/mcp.json and project .mcp.json servers below this table.",
		},
		// [tools] / [toolset.*]
		grokWith(grokRow(KeyPath{"tools", "respect_gitignore"}, TypeBool, "Skip gitignored files in the search and read tools."), func(r *SettingRow) { r.Default = "false" }),
		grokWith(grokRow(KeyPath{"tools", "disable_zdr_incompatible_tools"}, TypeBool, "Restrict tools that need xAI-hosted output under zero data retention."), func(r *SettingRow) { r.Default = "false" }),
		grokWith(grokRow(KeyPath{"toolset", "bash", "file_toolset"}, TypeEnum, "File edit tool scheme."), func(r *SettingRow) { r.Enum = []string{"standard", "hashline"} }),
		grokWith(grokRow(KeyPath{"toolset", "bash", "timeout_secs"}, TypeInt, "Foreground bash command timeout in seconds."), func(r *SettingRow) { r.Default = "120" }),
		grokWith(grokRow(KeyPath{"toolset", "bash", "output_byte_limit"}, TypeInt, "Maximum captured bash output in bytes."), func(r *SettingRow) { r.Default = "20000" }),
		grokWith(grokRow(KeyPath{"toolset", "bash", "max_timeout_secs"}, TypeInt, "Cap on model-requested foreground timeouts in seconds."), func(r *SettingRow) { r.Default = "36000" }),
		grokWith(grokRow(KeyPath{"toolset", "bash", "auto_background_on_timeout"}, TypeBool, "Move a command to the background when it times out."), func(r *SettingRow) { r.Default = "true" }),
		grokWith(grokRow(KeyPath{"toolset", "web_fetch", "proxy_endpoint"}, TypeString, "Egress proxy URL for web_fetch."), func(r *SettingRow) {
			r.Note = "A URL that routes fetched traffic elsewhere."
		}),
		grokRow(KeyPath{"toolset", "web_fetch", "allowed_domains"}, TypeStringList, "Domain allow-list override for web_fetch."),
		// [sandbox]
		grokWith(grokRow(KeyPath{"sandbox", "profile"}, TypeString, "Filesystem sandbox profile: off, workspace, read-only, strict or a custom profile name."), func(r *SettingRow) {
			r.Enum = []string{"off", "workspace", "read-only", "strict"}
			r.Danger = DangerConfirm
			r.Note = "\"off\" runs commands unsandboxed (also env GROK_SANDBOX / --sandbox). requirements.toml may pin it."
		}),
		grokWith(grokRow(KeyPath{"sandbox", "auto_allow_bash"}, TypeBool, "Skip bash permission prompts while a sandbox is active."), func(r *SettingRow) {
			r.Default = "false"
			r.Danger = DangerConfirm
			r.Note = "true auto-approves every bash command inside the sandbox."
		}),
		// [session]
		grokWith(grokRow(KeyPath{"session", "auto_compact_threshold_percent"}, TypeInt, "Context usage percent at which the session auto-compacts."), func(r *SettingRow) {
			r.Min, r.Max, r.Default = f64(0), f64(100), "85"
		}),
		grokWith(grokRow(KeyPath{"session", "load_envrc"}, TypeBool, "Inject .envrc variables into bash."), func(r *SettingRow) { r.Default = "true" }),
		// [cli]
		grokWith(grokRow(KeyPath{"cli", "auto_update"}, TypeBool, "Check for CLI updates on launch."), func(r *SettingRow) { r.Default = "true" }),
		grokWith(grokRow(KeyPath{"cli", "channel"}, TypeEnum, "Release channel."), func(r *SettingRow) { r.Enum = []string{"stable", "alpha"} }),
		grokRow(KeyPath{"cli", "show_tips"}, TypeBool, "Show tips at startup."),
		grokWith(grokRow(KeyPath{"cli", "installer"}, TypeString, "How the CLI was installed (recorded by the installer)."), func(r *SettingRow) {
			r.Owner, r.Verified, r.Source = OwnerToolState, VerifiedLive, grokLiveSrc
			r.Note = "Seen on node 1; not in the vendor reference. Written by the installer, not a user preference."
		}),
		grokWith(grokRow(KeyPath{"marketplace"}, TypeObject, "Plugin marketplace state."), func(r *SettingRow) {
			r.Owner, r.Verified, r.Source = OwnerToolState, VerifiedLive, grokLiveSrc
			r.Note = "marketplace.* keys seen on node 1; not in the vendor reference, so shown read-only."
		}),
		// [hints]
		grokWith(grokRow(KeyPath{"hints", "new_session_worktree_mode"}, TypeEnum, "Whether /new offers a worktree."), func(r *SettingRow) {
			r.Enum, r.Default = []string{"ask", "always", "never"}, "\"never\""
		}),
		grokWith(grokRow(KeyPath{"hints", "fork_worktree_mode"}, TypeEnum, "Whether /fork offers a worktree."), func(r *SettingRow) {
			r.Enum, r.Default = []string{"ask", "always", "never"}, "\"ask\""
		}),
		// [ui]
		grokWith(grokRow(KeyPath{"ui", "permission_mode"}, TypeEnum, "Default tool-permission behaviour."), func(r *SettingRow) {
			r.Enum = []string{"default", "ask", "auto", "always-approve"}
			r.Danger = DangerConfirm
			r.Note = "\"always-approve\" (and \"auto\") skip approvals; requirements.toml can forbid bypass via ui.disable_bypass_permissions_mode."
		}),
		grokWith(grokRow(KeyPath{"ui", "default_selected_permission"}, TypeEnum, "Approval row preselected on the first prompt."), func(r *SettingRow) {
			r.Enum = []string{"always_allow_all_sessions", "allow_command_always", "allow_once", "reject"}
			r.Danger = DangerConfirm
			r.Note = "\"always_allow_all_sessions\" makes one keypress a standing approval."
		}),
		grokWith(grokRow(KeyPath{"ui", "remember_tool_approvals"}, TypeBool, "Offer per-tool Always allow options."), func(r *SettingRow) { r.Default = "true" }),
		grokWith(grokRow(KeyPath{"ui", "theme"}, TypeString, "Colour theme: a theme name, auto (follows the OS) or system."), func(r *SettingRow) {
			r.Note = "Default is the Grok Night theme."
		}),
		grokWith(grokRow(KeyPath{"ui", "auto_dark_theme"}, TypeString, "Theme used when theme = auto and the OS is dark."), func(r *SettingRow) { r.Default = "\"groknight\"" }),
		grokWith(grokRow(KeyPath{"ui", "auto_light_theme"}, TypeString, "Theme used when theme = auto and the OS is light."), func(r *SettingRow) { r.Default = "\"grokday\"" }),
		grokWith(grokRow(KeyPath{"ui", "compact_mode"}, TypeBool, "Denser message padding."), func(r *SettingRow) { r.Default = "false" }),
		grokWith(grokRow(KeyPath{"ui", "screen_mode"}, TypeEnum, "Default render mode."), func(r *SettingRow) {
			r.Enum, r.Default = []string{"fullscreen", "minimal"}, "\"fullscreen\""
		}),
		grokWith(grokRow(KeyPath{"ui", "show_timestamps"}, TypeBool, "Show clock time next to messages."), func(r *SettingRow) { r.Default = "true" }),
		grokWith(grokRow(KeyPath{"ui", "show_thinking_blocks"}, TypeBool, "Show reasoning blocks while streaming."), func(r *SettingRow) { r.Default = "true" }),
		grokWith(grokRow(KeyPath{"ui", "vim_mode"}, TypeBool, "Vim keys in scrollback."), func(r *SettingRow) { r.Default = "false" }),
		grokWith(grokRow(KeyPath{"ui", "prompt_suggestions"}, TypeBool, "Next-prompt ghost text after each turn."), func(r *SettingRow) { r.Default = "true" }),
		grokWith(grokRow(KeyPath{"ui", "cancel_subagents_on_turn_cancel"}, TypeEnum, "What happens to running subagents when a turn is cancelled."), func(r *SettingRow) {
			r.Enum, r.Default = []string{"ask", "always_stop", "always_continue"}, "\"ask\""
		}),
		grokWith(grokRow(KeyPath{"ui", "hunk_tracker_mode"}, TypeEnum, "File-change hunk tracking."), func(r *SettingRow) {
			r.Enum, r.Default = []string{"agent_only", "all_dirty", "off"}, "\"agent_only\""
		}),
		grokRow(KeyPath{"ui", "fork_secondary_model"}, TypeString, "Model for the secondary agent when forking (default: the main model)."),
		grokWith(grokRow(KeyPath{"ui", "status_line", "type"}, TypeEnum, "Status line source."), func(r *SettingRow) {
			r.Enum, r.Default = []string{"builtin", "command", "disabled"}, "\"disabled\""
		}),
		grokWith(grokRow(KeyPath{"ui", "status_line", "command"}, TypeString, "Command whose output fills a custom status line."), func(r *SettingRow) {
			r.Danger = DangerConfirm
			r.Note = "Runs an arbitrary command."
		}),
		// [permission] (project-contributable)
		{
			ID: "grok/permission.allow", Tool: "grok", FileID: "grok/config", Path: KeyPath{"permission", "allow"}, Type: TypeRuleList,
			Desc: "Compact allow rules, e.g. Bash(git *) or Read(src/**).", Source: grokRef, Verified: VerifiedDoc, Owner: OwnerUser,
			Danger: DangerConfirm, Note: "A broad rule (tool any, pattern *) needs a confirmation. deny > ask > allow.",
		},
		{
			ID: "grok/permission.ask", Tool: "grok", FileID: "grok/config", Path: KeyPath{"permission", "ask"}, Type: TypeRuleList,
			Desc: "Compact rules that always prompt.", Source: grokRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "grok/permission.deny", Tool: "grok", FileID: "grok/config", Path: KeyPath{"permission", "deny"}, Type: TypeRuleList,
			Desc: "Compact rules that never run.", Source: grokRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "grok/permission.rules", Tool: "grok", FileID: "grok/config", Path: KeyPath{"permission", "rules"}, Type: TypeObject,
			Desc:   "Verbose rule list: [{action, tool, pattern}], action allow / ask / deny, tool any / bash / edit / read / grep / mcp / webfetch.",
			Source: grokRef, Verified: VerifiedDoc, Owner: OwnerUser, Danger: DangerConfirm,
			Note: "An array of tables; action = \"allow\" with tool = \"any\" and pattern = \"*\" approves everything.",
		},
		// [features] / [subagents] / [memory]
		grokRow(KeyPath{"features", "web_fetch"}, TypeBool, "Enable the web_fetch tool."),
		grokWith(grokRow(KeyPath{"features", "lsp_tools"}, TypeBool, "Expose the LSP tool."), func(r *SettingRow) { r.Default = "false" }),
		grokWith(grokRow(KeyPath{"features", "write_file"}, TypeBool, "Enable the write tool."), func(r *SettingRow) { r.Default = "true" }),
		grokWith(grokRow(KeyPath{"features", "tool_search"}, TypeBool, "MCP tool search and discovery."), func(r *SettingRow) { r.Default = "true" }),
		grokWith(grokRow(KeyPath{"subagents", "enabled"}, TypeBool, "Subagent / task tool master switch."), func(r *SettingRow) { r.Default = "false" }),
		grokRow(KeyPath{"subagents", "toggle"}, TypeObject, "Per-subagent-type on / off switches."),
		grokRow(KeyPath{"subagents", "models"}, TypeStringMap, "Per-subagent model routing."),
		grokWith(grokRow(KeyPath{"memory", "enabled"}, TypeBool, "Cross-session memory master switch."), func(r *SettingRow) { r.Default = "false" }),
		// [skills] / [plugins] (plugins project-contributable)
		grokRow(KeyPath{"skills", "paths"}, TypeStringList, "Extra skill directories."),
		grokRow(KeyPath{"skills", "disabled"}, TypeStringList, "Skills discovered but not activated."),
		{
			ID: "grok/plugins.paths", Tool: "grok", FileID: "grok/config", Path: KeyPath{"plugins", "paths"}, Type: TypeStringList,
			Desc: "Extra plugin directories.", Source: grokRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "grok/plugins.disabled", Tool: "grok", FileID: "grok/config", Path: KeyPath{"plugins", "disabled"}, Type: TypeStringList,
			Desc: "Plugins discovered but not activated.", Source: grokRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "grok/plugins.enabled", Tool: "grok", FileID: "grok/config", Path: KeyPath{"plugins", "enabled"}, Type: TypeStringList,
			Desc: "Plugins explicitly enabled.", Source: grokRef, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		// [compat.claude] / [compat.cursor]: imports of other tools' config.
		grokWith(grokRow(KeyPath{"compat", "claude", "hooks"}, TypeBool, "Replay Claude Code hooks (.claude/settings.json)."), func(r *SettingRow) {
			r.Default = "true"
			r.Danger = DangerRefuseWhileIntegrated
			r.Note = "false (like env GROK_CLAUDE_HOOKS_ENABLED=0) stops replaying Observer's Claude Code hooks under Grok."
		}),
		grokWith(grokRow(KeyPath{"compat", "claude", "mcps"}, TypeBool, "Import Claude Code MCP servers."), func(r *SettingRow) {
			r.Default = "true"
			r.Danger = DangerRefuseWhileIntegrated
			r.Note = "false drops the Claude-registered servers, Observer's MCP entry among them."
		}),
		grokWith(grokRow(KeyPath{"compat", "claude", "skills"}, TypeBool, "Import Claude Code skills."), func(r *SettingRow) { r.Default = "true" }),
		grokWith(grokRow(KeyPath{"compat", "claude", "rules"}, TypeBool, "Import Claude Code rules."), func(r *SettingRow) { r.Default = "true" }),
		grokWith(grokRow(KeyPath{"compat", "claude", "agents"}, TypeBool, "Import CLAUDE.md / CLAUDE.local.md."), func(r *SettingRow) { r.Default = "true" }),
		grokWith(grokRow(KeyPath{"compat", "cursor", "hooks"}, TypeBool, "Replay Cursor hooks (.cursor/hooks.json)."), func(r *SettingRow) {
			r.Default = "true"
			r.Danger = DangerRefuseWhileIntegrated
			r.Note = "false (like env GROK_CURSOR_HOOKS_ENABLED=0) stops replaying Observer's Cursor hooks under Grok."
		}),
		grokWith(grokRow(KeyPath{"compat", "cursor", "mcps"}, TypeBool, "Import Cursor mcp.json servers."), func(r *SettingRow) {
			r.Default = "true"
			r.Danger = DangerRefuseWhileIntegrated
			r.Note = "false drops the Cursor-registered servers, Observer's MCP entry among them."
		}),
		grokWith(grokRow(KeyPath{"compat", "cursor", "skills"}, TypeBool, "Import Cursor skills."), func(r *SettingRow) { r.Default = "true" }),
		grokWith(grokRow(KeyPath{"compat", "cursor", "rules"}, TypeBool, "Import .cursor/rules/."), func(r *SettingRow) { r.Default = "true" }),
		grokWith(grokRow(KeyPath{"compat", "cursor", "agents"}, TypeBool, "Import Cursor agent definitions."), func(r *SettingRow) { r.Default = "true" }),
		// requirements.toml pins (managed only)
		{
			ID: "grok/ui.disable_bypass_permissions_mode", Tool: "grok", FileID: "grok/requirements", Path: KeyPath{"ui", "disable_bypass_permissions_mode"},
			Type: TypeBool, Desc: "Forbid the approval-bypassing permission modes.", Source: grokEnterprise, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "grok/grok_com_config.disable_api_key_auth", Tool: "grok", FileID: "grok/requirements", Path: KeyPath{"grok_com_config", "disable_api_key_auth"},
			Type: TypeBool, Desc: "Refuse API-key sign-in (team login only).", Source: grokEnterprise, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "grok/grok_com_config.force_login_team_uuid", Tool: "grok", FileID: "grok/requirements", Path: KeyPath{"grok_com_config", "force_login_team_uuid"},
			Type: TypeString, Desc: "Team id (or list of ids) sign-in is restricted to.", Source: grokEnterprise, Verified: VerifiedDoc, Owner: OwnerUser,
			Note: "The enterprise page allows a string or a list.",
		},
		{
			ID: "grok/auth.oidc.issuer", Tool: "grok", FileID: "grok/requirements", Path: KeyPath{"auth", "oidc", "issuer"},
			Type: TypeString, Desc: "OIDC issuer for enterprise sign-in.", Source: grokEnterprise, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "grok/auth.oidc.client_id", Tool: "grok", FileID: "grok/requirements", Path: KeyPath{"auth", "oidc", "client_id"},
			Type: TypeString, Desc: "OIDC client id for enterprise sign-in.", Source: grokEnterprise, Verified: VerifiedDoc, Owner: OwnerUser,
		},
		{
			ID: "grok/auth.auth_provider_command", Tool: "grok", FileID: "grok/requirements", Path: KeyPath{"auth", "auth_provider_command"},
			Type: TypeString, Desc: "Command that supplies sign-in credentials.", Source: grokEnterprise, Verified: VerifiedDoc, Owner: OwnerUser,
			Danger: DangerConfirm, Note: "Runs an arbitrary command.",
		},
		// settings_cache.json (signed server payload)
		{
			ID: "grok/payload", Tool: "grok", FileID: "grok/settings-cache", Path: KeyPath{"payload"}, Type: TypeString,
			Desc: "Server-managed settings as delivered by xAI.", Source: grokLiveSrc, Verified: VerifiedLive, Owner: OwnerServerCache,
			Note: "Signed; any edit breaks the signature.",
		},
		{
			ID: "grok/signature", Tool: "grok", FileID: "grok/settings-cache", Path: KeyPath{"signature"}, Type: TypeObject,
			Desc: "Signature over payload (a list of byte values).", Source: grokLiveSrc, Verified: VerifiedLive, Owner: OwnerServerCache,
		},
	},
	Map: grokMap,
}

// grokRuleGrammar is the compact [permission] spelling. Only Bash(...) and
// Read(...) are shown in the reference; other kinds are refused.
var grokRuleGrammar = RuleGrammar{
	"shell": {Name: "Bash", Style: RuleCall},
	"read":  {Name: "Read", Style: RuleCall},
}

var grokMap = []MapRow{
	{Key: KeyModel, Tool: "grok", Settings: []string{"grok/models.default"}, Transform: TransformModelAlias, Source: grokRef},
	{
		Key: KeyMaxOutputTokens, Tool: "grok", Settings: []string{"grok/models.max_completion_tokens"}, Transform: TransformIdentity, Source: grokRef,
		Note: "The global default; a [model.<id>] entry may override it.",
	},
	{Key: KeyTemperature, Tool: "grok", Settings: []string{"grok/models.temperature"}, Transform: TransformIdentity, Source: grokRef},
	{Key: KeyAutoCompact, Tool: "grok", Settings: []string{"grok/session.auto_compact_threshold_percent"}, Transform: TransformIdentity, Source: grokRef},
	{
		Key: KeySandboxMode, Tool: "grok", Settings: []string{"grok/sandbox.profile"}, Transform: TransformEnumMap,
		Params: TransformParams{Enum: []EnumPair{
			{"read_only", []any{"read-only"}}, {"workspace_write", []any{"workspace"}}, {"off", []any{"off"}},
		}}, Source: grokRef, Note: "\"strict\" and custom profiles read back as tool-specific.",
	},
	{
		Key: KeyAllowedTools, Tool: "grok", Settings: []string{"grok/permission.allow"}, Transform: TransformRuleTranslate,
		Params: TransformParams{Grammar: grokRuleGrammar}, Source: grokRef,
		Note: "shell and read only: the reference shows no compact spelling for edit, write, fetch, search or MCP rules.",
	},
	{
		Key: KeyDeniedTools, Tool: "grok", Settings: []string{"grok/permission.deny"}, Transform: TransformRuleTranslate,
		Params: TransformParams{Grammar: grokRuleGrammar}, Source: grokRef,
		Note: "shell and read only, as for allowed_tools.",
	},
	{
		Key: KeyMCPServers, Tool: "grok", Settings: []string{"grok/mcp_servers"}, Transform: TransformMCPShape,
		Params: TransformParams{MCP: mcpCodex}, Lossy: true, Source: grokRef, Note: mcpLossyNote,
	},
	{Key: KeyAutoUpdate, Tool: "grok", Settings: []string{"grok/cli.auto_update"}, Transform: TransformIdentity, Source: grokRef},
}
