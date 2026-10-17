package agentconfig

// Antigravity CLI (`agy`) doc-authored catalogue. Sources: antigravity.google
// docs (settings, cli/reference, permissions, sandbox, mcp, hooks,
// cli/statusline, cli/title, cli/install) fetched 2026-10-04, audit section 5
// "antigravity-cli" and the node-1 live key names of 2026-10-03. The desktop
// app and IDE are the separate "antigravity" tool; the MCP and hooks files
// under ~/.gemini/config are shared by every Antigravity surface.
const (
	antigravityCLISettingsRef = "https://antigravity.google/docs/settings"
	antigravityCLIReference   = "https://antigravity.google/docs/cli/reference"
	antigravityCLIPermsRef    = "https://antigravity.google/docs/permissions"
	antigravityCLISandboxRef  = "https://antigravity.google/docs/sandbox"
	antigravityCLIMCPRef      = "https://antigravity.google/docs/mcp"
	antigravityCLIHooksRef    = "https://antigravity.google/docs/hooks"
	antigravityCLIInstallRef  = "https://antigravity.google/docs/cli/install"
	antigravityCLIShape       = "antigravity-cli/settings"
	antigravityCLILive        = "Seen on node 1 (2026-10-03, audit section 5 antigravity-cli)."
)

// antigravityCLIRow fills the tool, shape, owner, verification and id of one
// row.
func antigravityCLIRow(r SettingRow) SettingRow {
	r.Tool = "antigravity-cli"
	if r.FileID == "" {
		r.FileID = antigravityCLIShape
	}
	if r.Owner == "" {
		r.Owner = OwnerUser
	}
	if r.Verified == "" {
		r.Verified = VerifiedDoc
	}
	if r.Source == "" {
		r.Source = antigravityCLIReference
	}
	r.ID = r.Tool + "/" + r.Path.String()
	return r
}

// antigravityCLIMCPShape is mcp_config.json's entry shape: command / args /
// env for stdio, serverUrl for a remote server (no transport key; legacy url
// and httpUrl are not supported).
var antigravityCLIMCPShape = MCPShape{
	EnvKey: "env", TypeNames: map[string]string{"stdio": "", "http": ""},
	URLKeys: map[string]string{"http": "serverUrl"},
}

var antigravityCLICatalog = toolCatalog{
	Tool: "antigravity-cli",
	VerifiedAgainst: "antigravity.google docs fetched 2026-10-04 (settings, cli/reference, permissions, sandbox, mcp, hooks; audit section 5 antigravity-cli); " +
		"node 1 live key names 2026-10-03 for permissions.allow and trustedWorkspaces. permissions.allow / deny are not mapped to the common rule lists: " +
		"agy requires action(target) with an explicit target (command(*)), so a bare canonical rule has no valid spelling.",
	Files: []FileRow{
		{
			ID: "antigravity-cli/user-settings", Tool: "antigravity-cli", Shape: antigravityCLIShape, Kind: KindSettings, Scope: ScopeUser,
			Paths: samePath("~/.gemini/antigravity-cli/settings.json"), Format: FormatJSON, Precedence: 20, WriteNeeds: PrivUser,
			Grounding: live("~/.gemini/antigravity-cli/settings.json carries permissions.allow and trustedWorkspaces"),
			Note:      "Sparse: only non-default values are persisted. CLI flags (--sandbox, --model) override it for the session. No project settings.json is documented.",
		},
		{
			ID: "antigravity-cli/user-keybindings", Tool: "antigravity-cli", Shape: "antigravity-cli/keybindings", Kind: KindSettings, Scope: ScopeUser,
			Paths: samePath("~/.gemini/antigravity-cli/keybindings.json"), Format: FormatJSON, Precedence: 20, WriteNeeds: PrivUser,
			Grounding: doc(antigravityCLISettingsRef), Note: "Maps a TUI action (cli.clear_screen, prompt.insert_newline, ...) to an array of key sequences; [] disables a default.",
		},
		{
			ID: "antigravity-cli/user-mcp", Tool: "antigravity-cli", Shape: "antigravity-cli/mcp", Kind: KindMCP, Scope: ScopeUser,
			Paths: samePath("~/.gemini/config/mcp_config.json"), Format: FormatJSON, Precedence: 20, WriteNeeds: PrivUser,
			Grounding: doc(antigravityCLIMCPRef),
			Note:      "Shared by every Antigravity surface (also catalogued as antigravity/user-mcp). Present but empty on node 1 (2026-10-03).",
		},
		{
			ID: "antigravity-cli/project-mcp", Tool: "antigravity-cli", Shape: "antigravity-cli/mcp", Kind: KindMCP, Scope: ScopeProjectShared,
			Paths: samePath("<project>/.agents/mcp_config.json"), Format: FormatJSON, Precedence: 30, WriteNeeds: PrivUser,
			Grounding: doc(antigravityCLIMCPRef), Note: "Shared by every Antigravity surface.",
		},
		{
			ID: "antigravity-cli/user-hooks", Tool: "antigravity-cli", Shape: "antigravity-cli/hooks", Kind: KindHooks, Scope: ScopeUser,
			Paths: samePath("~/.gemini/config/hooks.json"), Format: FormatJSON, Precedence: 20, WriteNeeds: PrivUser,
			Grounding: doc(antigravityCLIHooksRef),
			Note: "Top-level keys are hook names, each {enabled, PreToolUse, PostToolUse, PreInvocation, PostInvocation, Stop}. " +
				"Observer owns one hook name; every other key is the user's. Shared with the desktop app and IDE, so it is catalogued here once.",
		},
		{
			ID: "antigravity-cli/project-hooks", Tool: "antigravity-cli", Shape: "antigravity-cli/hooks", Kind: KindHooks, Scope: ScopeProjectShared,
			Paths: samePath("<project>/.agents/hooks.json"), Format: FormatJSON, Precedence: 30, WriteNeeds: PrivUser,
			Grounding: doc(antigravityCLIHooksRef), Note: "Same named-hook-set layout as the user file.",
		},
	},
	Extra: []SettingRow{
		antigravityCLIRow(SettingRow{
			Path: KeyPath{"toolPermission"}, Type: TypeEnum, Enum: []string{"request-review", "proceed-in-sandbox", "strict", "always-proceed"},
			Default: "\"request-review\"", Desc: "Approval preset for tools (commands, writes, web).", Danger: DangerConfirm,
			Note: "always-proceed runs every tool without prompting.",
		}),
		antigravityCLIRow(SettingRow{
			Path: KeyPath{"artifactReviewPolicy"}, Type: TypeEnum, Enum: []string{"asks-for-review", "agent-decides", "always-proceed"},
			Default: "\"asks-for-review\"", Desc: "When the agent asks to review generated artifacts before writing them.", Danger: DangerConfirm,
			Note: "always-proceed writes changes without review.",
		}),
		antigravityCLIRow(SettingRow{
			Path: KeyPath{"enableTerminalSandbox"}, Type: TypeBool, Default: "false", Desc: "Run agent terminal commands inside the OS sandbox.",
			Source: antigravityCLISandboxRef, Danger: DangerConfirm, Note: "false (the CLI default) runs commands unsandboxed. --sandbox forces it on for a session.",
		}),
		antigravityCLIRow(SettingRow{
			Path: KeyPath{"allowNonWorkspaceAccess"}, Type: TypeBool, Default: "false", Desc: "Let file tools read and write outside the workspace roots.",
			Danger: DangerConfirm, Note: "true opens the whole filesystem to the file tools.",
		}),
		antigravityCLIRow(SettingRow{
			Path: KeyPath{"altScreenMode"}, Type: TypeEnum, Enum: []string{"default", "always", "never"}, Default: "\"default\"",
			Desc: "TUI screen buffer: adaptive, alternate screen, or inline.",
		}),
		antigravityCLIRow(SettingRow{
			Path: KeyPath{"colorScheme"}, Type: TypeEnum,
			Enum: []string{
				"terminal", "light", "dark", "solarized light", "solarized dark", "colorblind-friendly light", "colorblind-friendly dark", "tokyo night",
			},
			Default: "\"terminal\"", Desc: "Color theme.",
		}),
		antigravityCLIRow(SettingRow{
			Path: KeyPath{"runningLightSpeed"}, Type: TypeEnum, Enum: []string{"fast", "medium", "slow", "off"}, Default: "\"medium\"",
			Desc: "Progress animation speed.",
		}),
		antigravityCLIRow(SettingRow{
			Path: KeyPath{"verbosity"}, Type: TypeEnum, Enum: []string{"high", "low"}, Default: "\"high\"", Desc: "Show full thoughts and tool steps, or minimal progress.",
		}),
		antigravityCLIRow(SettingRow{
			Path: KeyPath{"editor"}, Type: TypeString, Enum: []string{"auto", "vim", "emacs"}, Default: "\"auto\"",
			Desc: "External editor for artifacts and Ctrl+G prompts (auto uses $EDITOR).",
		}),
		antigravityCLIRow(SettingRow{
			Path: KeyPath{"editorMode"}, Type: TypeEnum, Enum: []string{"default", "vim"}, Default: "\"default\"", Desc: "Prompt editing model.",
		}),
		antigravityCLIRow(SettingRow{
			Path: KeyPath{"vimInsertFirst"}, Type: TypeBool, Default: "false", Desc: "Start Vim mode in Insert and let a bare Enter submit.",
		}),
		antigravityCLIRow(SettingRow{
			Path: KeyPath{"notifications"}, Type: TypeBool, Default: "false", Desc: "Desktop notification and bell when a task completes or needs attention.",
		}),
		antigravityCLIRow(SettingRow{
			Path: KeyPath{"useG1Credits"}, Type: TypeBool, Default: "false", Desc: "Spend personal AI credits once the plan quota is exhausted (external builds).",
		}),
		antigravityCLIRow(SettingRow{
			Path: KeyPath{"enableTelemetry"}, Type: TypeBool, Default: "true", Desc: "Send usage statistics and crash reports to Google.",
		}),
		antigravityCLIRow(SettingRow{Path: KeyPath{"showTips"}, Type: TypeBool, Default: "true", Desc: "Show tips while the agent works."}),
		antigravityCLIRow(SettingRow{Path: KeyPath{"showFeedbackSurvey"}, Type: TypeBool, Default: "true", Desc: "Show periodic feedback surveys."}),
		antigravityCLIRow(SettingRow{
			Path: KeyPath{"permissions", "allow"}, Type: TypeRuleList, Source: antigravityCLIPermsRef, Verified: VerifiedLive,
			Desc:   "Rules that run without a prompt: action(target) with action read_file, write_file, read_url, execute_url, command, unsandboxed or mcp.",
			Danger: DangerConfirm,
			Note:   antigravityCLILive + " command(*), unsandboxed(*) or mcp(*) allow everything in that action.",
		}),
		antigravityCLIRow(SettingRow{
			Path: KeyPath{"permissions", "ask"}, Type: TypeRuleList, Source: antigravityCLIPermsRef, Desc: "Rules that always prompt (beats allow).",
		}),
		antigravityCLIRow(SettingRow{
			Path: KeyPath{"permissions", "deny"}, Type: TypeRuleList, Source: antigravityCLIPermsRef, Desc: "Rules that are always blocked (Deny > Ask > Allow).",
		}),
		antigravityCLIRow(SettingRow{
			Path: KeyPath{"trustedWorkspaces"}, Type: TypeStringList, Source: antigravityCLISettingsRef, Verified: VerifiedLive,
			Owner: OwnerToolState, Danger: DangerConfirm, Desc: "Workspaces the user has trusted.",
			Note: antigravityCLILive + " Not on the settings reference; written by the trust prompt. The value type was not recorded by the probe: a list is assumed.",
		}),
		antigravityCLIRow(SettingRow{
			Path: KeyPath{"modelProvider"}, Type: TypeEnum, Enum: []string{"gemini"}, Source: antigravityCLIInstallRef,
			Desc: "API-key mode: use a Gemini API key from GEMINI_API_KEY instead of Google sign-in.",
			Note: "Only then does GOOGLE_GEMINI_BASE_URL (an env var, the proxy seam) take effect; removing it returns to OAuth and off any proxy route. GOOGLE_API_KEY and .env files are not read.",
		}),
		antigravityCLIRow(SettingRow{
			Path: KeyPath{"statusLine"}, Type: TypeObject, Source: "https://antigravity.google/docs/cli/statusline",
			Desc: "Custom status line {type: command, command, padding, enabled, stack_with_default}.", Danger: DangerConfirm,
			Note: "command runs an arbitrary script on every agent state change.",
		}),
		antigravityCLIRow(SettingRow{
			Path: KeyPath{"title"}, Type: TypeObject, Source: "https://antigravity.google/docs/cli/title",
			Desc: "Custom terminal title {type: command, command}.", Danger: DangerConfirm, Note: "command runs an arbitrary script on every agent state change.",
		}),
		antigravityCLIRow(SettingRow{
			Path: KeyPath{"mcpServers"}, Type: TypeMCPMap, FileID: "antigravity-cli/mcp", Source: antigravityCLIMCPRef,
			Desc: "MCP servers by name: command, args, env, cwd, serverUrl, headers, authProviderType, oauth, disabled, disabledTools.",
			Note: "Remote servers use serverUrl (url / httpUrl are not supported). headers and oauth.clientSecret may hold credentials.",
		}),
	},
	Map: []MapRow{
		{
			Key: KeyApprovalMode, Tool: "antigravity-cli", Settings: []string{"antigravity-cli/toolPermission"}, Transform: TransformEnumMap,
			Params: TransformParams{Enum: []EnumPair{
				{"ask_edits", []any{"request-review"}}, {"full_auto", []any{"always-proceed"}},
			}}, Source: antigravityCLIReference,
			Note: "plan, ask and auto_edits are refused; strict and proceed-in-sandbox have no canonical form.",
		},
		{
			Key: KeySandboxMode, Tool: "antigravity-cli", Settings: []string{"antigravity-cli/enableTerminalSandbox"}, Transform: TransformEnumMap,
			Params: TransformParams{Enum: []EnumPair{{"workspace_write", []any{true}}, {"off", []any{false}}}}, Source: antigravityCLISandboxRef,
			Note: "read_only is refused: the sandbox always permits workspace writes.",
		},
		{Key: KeyTelemetryOff, Tool: "antigravity-cli", Settings: []string{"antigravity-cli/enableTelemetry"}, Transform: TransformBoolInvert, Source: antigravityCLIReference},
		{
			Key: KeyTheme, Tool: "antigravity-cli", Settings: []string{"antigravity-cli/colorScheme"}, Transform: TransformEnumMap,
			Params: TransformParams{Enum: []EnumPair{{"light", []any{"light"}}, {"dark", []any{"dark"}}}}, Source: antigravityCLIReference,
			Note: "terminal, solarized, tokyo night and the colorblind variants stay tool-specific.",
		},
		{
			Key: KeyMCPServers, Tool: "antigravity-cli", Settings: []string{"antigravity-cli/mcpServers"}, Transform: TransformMCPShape,
			Params: TransformParams{MCP: antigravityCLIMCPShape}, Lossy: true, Source: antigravityCLIMCPRef,
			Note: "SSE is refused (serverUrl does not distinguish it on write). " + mcpLossyNote,
		},
	},
}
