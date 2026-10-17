package agentconfig

// Mistral Vibe (`vibe`) catalogue, doc-authored (phase P2). Sources: the Vibe
// configuration + configuration-reference pages (re-fetched 2026-10-04),
// audit docs/audits/adapter-config-surface-2026-10-03.md section 5
// "mistral-code" and section 6 (node 1 live record).
const (
	mistralCodeRef     = "https://docs.mistral.ai/vibe/code/cli/configuration-reference"
	mistralCodeConfig  = "https://docs.mistral.ai/vibe/code/cli/configuration"
	mistralCodeHooks   = "https://docs.mistral.ai/vibe/code/cli/hooks"
	mistralCodeReadme  = "https://raw.githubusercontent.com/mistralai/mistral-vibe/main/README.md"
	mistralCodeLiveSrc = "node 1 live probe 2026-10-03 (audit section 6, mistral-code)"
)

func mistralCodeRow(path KeyPath, typ ValueType, def, desc string) SettingRow {
	return SettingRow{
		ID: "mistral-code/" + path.String(), Tool: "mistral-code", FileID: "mistral-code/config", Path: path,
		Type: typ, Default: def, Desc: desc, Source: mistralCodeRef, Verified: VerifiedDoc, Owner: OwnerUser,
	}
}

var mistralCodeCatalog = toolCatalog{
	Tool: "mistral-code",
	VerifiedAgainst: "docs.mistral.ai Vibe configuration + configuration-reference (audit section 5, fetched 2026-10-03; re-fetched 2026-10-04); " +
		"node 1 live key names 2026-10-03 for active_model, theme and trusted_folders.toml trusted / untrusted",
	Files: []FileRow{
		{
			ID: "mistral-code/user-config", Tool: "mistral-code", Shape: "mistral-code/config", Kind: KindSettings, Scope: ScopeUser,
			Paths: samePath("~/.vibe/config.toml"), Format: FormatTOML, Precedence: 20, WriteNeeds: PrivUser,
			Grounding: live("~/.vibe/config.toml carries active_model, theme"),
			Note:      "VIBE_HOME relocates ~/.vibe (the docs open ~\\.vibe\\config.toml on Windows). Mode 0600. Higher layers: env vars, CLI flags, then the server-delivered admin config.",
		},
		{
			ID: "mistral-code/project-config", Tool: "mistral-code", Shape: "mistral-code/config", Kind: KindSettings, Scope: ScopeProjectShared,
			Paths: samePath("<project>/.vibe/config.toml"), Format: FormatTOML, Precedence: 30, WriteNeeds: PrivUser,
			Grounding: doc(mistralCodeConfig),
			Note:      "Loaded only when the working directory is trusted (trusted_folders.toml); merges over the user file. Admin config (fetched over HTTP each session, kept in memory, NEVER written to disk) overrides both and shows locked keys with a lock glyph.",
		},
		{
			ID: "mistral-code/trusted-folders", Tool: "mistral-code", Shape: "mistral-code/trust", Kind: KindPermissions, Scope: ScopeUser,
			Paths: samePath("~/.vibe/trusted_folders.toml"), Format: FormatTOML, Precedence: 20, WriteNeeds: PrivUser,
			Grounding: live("~/.vibe/trusted_folders.toml carries trusted, untrusted"),
			Note:      "Folder trust, kept apart from config.toml; a trusted folder's .vibe/config.toml is honoured.",
		},
		{
			ID: "mistral-code/user-hooks", Tool: "mistral-code", Shape: "mistral-code/hooks", Kind: KindHooks, Scope: ScopeUser,
			Paths: samePath("~/.vibe/hooks.toml"), Format: FormatTOML, Precedence: 20, WriteNeeds: PrivUser,
			Grounding: doc(mistralCodeHooks),
			Note:      "Three hook events; exit 2 is NOT a block (hook-parity audit). No rows.",
		},
		{
			ID: "mistral-code/dotenv", Tool: "mistral-code", Shape: "mistral-code/dotenv", Kind: KindToolState, Scope: ScopeUser,
			Paths: samePath("~/.vibe/.env"), Format: FormatScript, Precedence: 0, WriteNeeds: PrivNever,
			Grounding: live("~/.vibe/.env present (not opened: credentials)"),
			Note:      "A dotenv file holding MISTRAL_API_KEY (mode 0600): never read, never written (raw dotenv text, never parsed).",
		},
		{
			ID: "mistral-code/connector-cache", Tool: "mistral-code", Shape: "mistral-code/connector-cache", Kind: KindToolState, Scope: ScopeUser,
			Paths: samePath("~/.vibe/connector_bootstrap_cache.json"), Format: FormatJSON, Precedence: 0, WriteNeeds: PrivNever,
			Grounding: Grounding{Kind: GroundAdapterDoc, Source: "docs/mistral-code-adapter.md", Date: auditDate},
			Note:      "Connector bootstrap cache: tool state, never written.",
		},
	},
	Extra: []SettingRow{
		func() SettingRow {
			r := mistralCodeRow(KeyPath{"active_model"}, TypeString, "", "Alias of the [[models]] entry in use.")
			r.Verified, r.Note = VerifiedLive, "Seen on node 1 (audit section 6). Must name an alias defined in [[models]]."
			return r
		}(),
		func() SettingRow {
			r := mistralCodeRow(KeyPath{"theme"}, TypeString, "", "Terminal theme.")
			r.Verified, r.Source = VerifiedLive, mistralCodeReadme
			r.Note = "Seen on node 1 (audit section 6); README-listed only, values UNVERIFIED."
			return r
		}(),
		func() SettingRow {
			r := mistralCodeRow(KeyPath{"default_agent"}, TypeString, "", "Default interactive agent profile: ask, plan, accept-edits, auto-approve or a custom agent name.")
			r.Enum, r.Danger = []string{"ask", "plan", "accept-edits", "auto-approve"}, DangerConfirm
			r.Note = "\"auto-approve\" runs every tool without asking (programmatic mode falls back to it when --agent is absent). Ignored in programmatic mode."
			return r
		}(),
		mistralCodeRow(KeyPath{"allowed_models"}, TypeStringList, "", "Allow-list of model name patterns (exact, glob or re:)."),
		mistralCodeRow(KeyPath{"compaction_model"}, TypeObject, "", "Model used to summarise the conversation on /compact."),
		mistralCodeRow(KeyPath{"vision_model"}, TypeObject, "", "Vision-capable model for image descriptions."),
		func() SettingRow {
			r := mistralCodeRow(KeyPath{"log_interactions"}, TypeBool, "true", "Session logging.")
			r.Danger = DangerRefuseWhileIntegrated
			r.Note = "false stops the session logs under ~/.vibe/logs/session/ that Observer captures from."
			return r
		}(),
		mistralCodeRow(KeyPath{"skill_paths"}, TypeStringList, "", "Extra directories for skill discovery."),
		mistralCodeRow(KeyPath{"enabled_skills"}, TypeStringList, "", "Skill allow-list (exact, glob or re: patterns)."),
		mistralCodeRow(KeyPath{"disabled_skills"}, TypeStringList, "", "Skill deny-list (exact, glob or re: patterns)."),
		func() SettingRow {
			r := mistralCodeRow(KeyPath{"enabled_tools"}, TypeStringList, "", "Tool allow-list (exact, glob or re: patterns; MCP tools are <server>_<tool>).")
			r.Danger, r.Note = DangerConfirm, "A broad pattern widens what the agent may call."
			return r
		}(),
		func() SettingRow {
			r := mistralCodeRow(KeyPath{"disabled_tools"}, TypeStringList, "", "Tool deny-list (exact, glob or re: patterns).")
			r.Danger, r.Note = DangerConfirm, "The audit lists it among the dangerous values (it can switch tools off)."
			return r
		}(),
		mistralCodeRow(KeyPath{"enable_auto_update"}, TypeBool, "true", "Check for and install new releases."),
		mistralCodeRow(KeyPath{"enable_notifications"}, TypeBool, "true", "OS-level notifications."),
		mistralCodeRow(KeyPath{"enable_telemetry"}, TypeBool, "true", "Anonymous usage telemetry."),
		func() SettingRow {
			r := mistralCodeRow(KeyPath{"providers"}, TypeObject, "", "Provider presets: an ARRAY of [[providers]] tables (name, api_base, api_key_env_var, api_style, backend, region, extra_headers).")
			r.Danger = DangerConfirm
			r.Note = "api_base redirects model traffic (Observer's route would live here; never driven live). Keys are referenced by env var NAME (api_key_env_var), never inline. region is enterprise-only."
			return r
		}(),
		mistralCodeRow(KeyPath{"models"}, TypeObject, "", "Model presets: an ARRAY of [[models]] tables (name, provider, alias, temperature 0.2, thinking \"off\", thinking_levels, max_context_length, auto_compact_threshold, prices, supports_images)."),
		func() SettingRow {
			r := mistralCodeRow(KeyPath{"mcp_servers"}, TypeObject, "", "MCP servers: an ARRAY of [[mcp_servers]] tables (name, transport http / streamable-http / stdio, url, command, args, env, headers, api_key_env, api_key_header, api_key_format, timeouts).")
			r.Note = "An array, not a name-keyed map, so it has no common mapping. Also edited by `vibe mcp add / remove`."
			return r
		}(),
		func() SettingRow {
			r := mistralCodeRow(KeyPath{"tools"}, TypeObject, "", "Per-tool permission tables [tools.<tool>] with permission (always / ask) and, for bash, allow / deny lists.")
			r.Danger, r.Note = DangerConfirm, "permission = \"always\" auto-allows that tool."
			return r
		}(),
		func() SettingRow {
			r := mistralCodeRow(KeyPath{"tools", "bash", "permission"}, TypeEnum, "\"always\"", "Whether bash runs without a prompt.")
			r.Enum, r.Danger = []string{"always", "ask"}, DangerConfirm
			r.Note = "\"always\" auto-allows every command not denied."
			return r
		}(),
		func() SettingRow {
			r := mistralCodeRow(KeyPath{"tools", "bash", "allow"}, TypeStringList, "", "Bash command allow-list.")
			r.Danger, r.Note = DangerConfirm, "A broad pattern auto-approves arbitrary commands."
			return r
		}(),
		func() SettingRow {
			r := mistralCodeRow(KeyPath{"tools", "bash", "deny"}, TypeStringList, "", "Bash command deny-list.")
			r.Danger, r.Note = DangerConfirm, "Emptying it removes the guard (audit dangerous values)."
			return r
		}(),
		{
			ID: "mistral-code/trusted", Tool: "mistral-code", FileID: "mistral-code/trust", Path: KeyPath{"trusted"}, Type: TypeStringList,
			Desc: "Folders whose project config is honoured.", Source: mistralCodeLiveSrc, Verified: VerifiedLive, Owner: OwnerUser, Danger: DangerConfirm,
			Note: "Seen on node 1 (audit section 6); trusting a folder lets its .vibe/config.toml (agents, tools, MCP) apply.",
		},
		{
			ID: "mistral-code/untrusted", Tool: "mistral-code", FileID: "mistral-code/trust", Path: KeyPath{"untrusted"}, Type: TypeStringList,
			Desc: "Folders whose project config is ignored.", Source: mistralCodeLiveSrc, Verified: VerifiedLive, Owner: OwnerUser,
			Note: "Seen on node 1 (audit section 6).",
		},
	},
	Map: []MapRow{
		{
			Key: KeyApprovalMode, Tool: "mistral-code", Settings: []string{"mistral-code/default_agent"}, Transform: TransformEnumMap,
			Params: TransformParams{Enum: []EnumPair{
				{"plan", []any{"plan"}}, {"auto_edits", []any{"accept-edits"}}, {"full_auto", []any{"auto-approve"}},
			}},
			Source: mistralCodeRef,
			Note:   "ask and ask_edits are refused: the docs do not say whether the \"ask\" profile prompts for reads. A custom agent reads back as tool-specific.",
		},
		{Key: KeyTelemetryOff, Tool: "mistral-code", Settings: []string{"mistral-code/enable_telemetry"}, Transform: TransformBoolInvert, Source: mistralCodeRef},
		{Key: KeyAutoUpdate, Tool: "mistral-code", Settings: []string{"mistral-code/enable_auto_update"}, Transform: TransformIdentity, Source: mistralCodeRef},
	},
}
