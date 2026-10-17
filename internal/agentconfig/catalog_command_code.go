package agentconfig

// Command Code catalogue, doc-authored (phase P2). Sources: commandcode.ai
// settings, permissions, MCP and hooks pages (settings / permissions / MCP
// re-fetched 2026-10-04) and audit docs/audits/adapter-config-surface-2026-10-03.md
// section 5 "command-code". Command Code was not installed on node 1, so no
// row is live.
const (
	commandCodeRef   = "https://commandcode.ai/docs/settings"
	commandCodePerms = "https://commandcode.ai/docs/permissions"
	commandCodeMCP   = "https://commandcode.ai/docs/mcp"
	commandCodeHooks = "https://commandcode.ai/docs/hooks"
	commandCodeMods  = "https://commandcode.ai/docs/mods"
)

func commandCodeRow(path KeyPath, typ ValueType, def, desc string) SettingRow {
	return SettingRow{
		ID: "command-code/" + path.String(), Tool: "command-code", FileID: "command-code/settings", Path: path,
		Type: typ, Default: def, Desc: desc, Source: commandCodeRef, Verified: VerifiedDoc, Owner: OwnerUser,
	}
}

func commandCodeWith(r SettingRow, edit func(*SettingRow)) SettingRow {
	edit(&r)
	return r
}

var commandCodeCatalog = toolCatalog{
	Tool: "command-code",
	VerifiedAgainst: "commandcode.ai settings / permissions / mcp / hooks / mods pages (audit section 5, fetched 2026-10-03; settings, permissions and mcp re-fetched 2026-10-04); " +
		"not installed on node 1, so no live rows",
	Files: []FileRow{
		{
			ID: "command-code/user-settings", Tool: "command-code", Shape: "command-code/settings", Kind: KindSettings, Scope: ScopeUser,
			Paths: samePath("~/.commandcode/settings.json"), Format: FormatJSON, Precedence: 20, WriteNeeds: PrivUser,
			Grounding: doc(commandCodeRef),
			Note:      "The same ~/.commandcode/ on Linux, macOS and Windows (HOME / USERPROFILE). Permission lists (allow, ask, deny, additionalDirectories) UNION across layers.",
		},
		{
			ID: "command-code/project-settings", Tool: "command-code", Shape: "command-code/settings", Kind: KindSettings, Scope: ScopeProjectShared,
			Paths: samePath("<project>/.commandcode/settings.json"), Format: FormatJSON, Precedence: 30, WriteNeeds: PrivUser,
			Grounding: doc(commandCodeRef), Note: "Team-shared (committed).",
		},
		{
			ID: "command-code/project-local-settings", Tool: "command-code", Shape: "command-code/settings", Kind: KindSettings, Scope: ScopeProjectLocal,
			Paths: samePath("<project>/.commandcode/settings.local.json"), Format: FormatJSON, Precedence: 40, WriteNeeds: PrivUser,
			Gitignored: true, Grounding: doc(commandCodeRef), Note: "Personal overrides (gitignored).",
		},
		{
			ID: "command-code/legacy-config", Tool: "command-code", Shape: "command-code/legacy-config", Kind: KindToolState, Scope: ScopeUser,
			Paths: samePath("~/.commandcode/config.json"), Format: FormatJSON, Precedence: 10, WriteNeeds: PrivNever,
			Grounding: doc(commandCodeRef),
			Note:      "Legacy fields (provider, model, theme, compactMode, ...) managed by slash commands; the docs say not to hand-edit it. Lowest layer.",
		},
		{
			ID: "command-code/user-mcp", Tool: "command-code", Shape: "command-code/mcp", Kind: KindMCP, Scope: ScopeUser,
			Paths: samePath("~/.commandcode/mcp.json"), Format: FormatJSON, Precedence: 20, WriteNeeds: PrivUser,
			Grounding: doc(commandCodeMCP),
			Note:      "User-scope MCP servers. Observer's MCP registrar writes mcpServers.observer here (owned span); the Agent Access relay may wrap entries. A per-project private file ~/.commandcode/projects/<slug>/mcp.json also exists (not declared: the slug is vendor-derived).",
		},
		{
			ID: "command-code/project-mcp", Tool: "command-code", Shape: "command-code/mcp", Kind: KindMCP, Scope: ScopeProjectShared,
			Paths: samePath("<project>/.mcp.json"), Format: FormatJSON, Precedence: 30, WriteNeeds: PrivUser,
			Grounding: doc(commandCodeMCP), Note: "Project-scope MCP servers (committed); OAuth client secrets are stripped from it.",
		},
		{
			ID: "command-code/observer-guard-mod", Tool: "command-code", Shape: "command-code/observer-guard-mod", Kind: KindHooks, Scope: ScopeUser,
			Paths: samePath("~/.commandcode/mods/observer-guard.ts"), Format: FormatScript, Precedence: 20, WriteNeeds: PrivUser,
			Grounding: doc(commandCodeMods),
			Note:      "Observer's hook mod (TypeScript): Observer owns the whole file (internal/hook/register_commandcode.go). A foreign file at this path is backed up to .bak under --force.",
		},
		{
			ID: "command-code/providers", Tool: "command-code", Shape: "command-code/providers", Kind: KindSettings, Scope: ScopeUser,
			Paths: samePath("~/.commandcode/providers.json"), Format: FormatJSON, Precedence: 20, WriteNeeds: PrivUser,
			Grounding: doc(commandCodeRef),
			Note:      "BYOK provider endpoints (hand-editable). Shape UNVERIFIED, so no rows; a base URL here redirects model traffic.",
		},
		{
			ID: "command-code/keybindings", Tool: "command-code", Shape: "command-code/keybindings", Kind: KindSettings, Scope: ScopeUser,
			Paths: samePath("~/.commandcode/keybindings.json"), Format: FormatJSON, Precedence: 20, WriteNeeds: PrivUser,
			Grounding: doc(commandCodeRef), Note: "Keybinding overrides; no rows.",
		},
		{
			ID: "command-code/cron-jobs", Tool: "command-code", Shape: "command-code/cron-jobs", Kind: KindToolState, Scope: ScopeUser,
			Paths: samePath("~/.commandcode/cron/jobs.json"), Format: FormatJSON, Precedence: 0, WriteNeeds: PrivNever,
			Grounding: doc(commandCodeRef), Note: "Scheduled jobs (not hand-editable per the docs).",
		},
		{
			ID: "command-code/auth", Tool: "command-code", Shape: "command-code/auth", Kind: KindToolState, Scope: ScopeUser,
			Paths: samePath("~/.commandcode/auth.json"), Format: FormatJSON, Precedence: 0, WriteNeeds: PrivNever,
			Grounding: doc(commandCodeRef), Note: "Login tokens and API keys (mode 0600): never read, never written.",
		},
	},
	Extra: []SettingRow{
		commandCodeRow(KeyPath{"model"}, TypeString, "", "Main model for the session (default: a curated model)."),
		commandCodeWith(commandCodeRow(KeyPath{"effort"}, TypeString, "", "Global reasoning effort."), func(r *SettingRow) {
			r.Note = "Levels are not listed for this key (audit: UNVERIFIED); reasoningEffort takes low, medium, high, xhigh, max."
		}),
		commandCodeRow(KeyPath{"reasoningEffort"}, TypeObject, "", "Per-model reasoning effort overrides (low, medium, high, xhigh, max)."),
		commandCodeWith(commandCodeRow(KeyPath{"permissions", "defaultMode"}, TypeEnum, "\"default\"", "Default permission mode."), func(r *SettingRow) {
			r.Enum, r.Source, r.Danger = []string{"default", "accept-edits", "plan", "yolo", "dont-ask", "auto-accept", "bypass"}, commandCodePerms, DangerConfirm
			r.Note = "\"yolo\" (alias \"bypass\"; flags --yolo / --dangerously-skip-permissions) bypasses approvals; \"dont-ask\" never prompts and denies what is not pre-approved. \"auto-accept\" is an alias of accept-edits."
		}),
		commandCodeWith(commandCodeRow(KeyPath{"permissionMode"}, TypeString, "\"default\"", "Legacy permission mode."), func(r *SettingRow) {
			r.Deprecated, r.Danger = true, DangerConfirm
			r.Note = "Superseded by permissions.defaultMode; same values."
		}),
		commandCodeWith(commandCodeRow(KeyPath{"permissions", "allow"}, TypeRuleList, "", "Rules that run without a prompt: Tool or Tool(specifier), e.g. Shell(npm run *), Read(src/**), mcp__srv__tool."), func(r *SettingRow) {
			r.Source, r.Danger = commandCodePerms, DangerConfirm
			r.Note = "A broad rule such as Shell(*) needs a confirmation. The shell tool is Shell, not Claude's Bash. Unions across layers."
		}),
		commandCodeWith(commandCodeRow(KeyPath{"permissions", "ask"}, TypeRuleList, "", "Rules that always prompt (except in dont-ask)."), func(r *SettingRow) { r.Source = commandCodePerms }),
		commandCodeWith(commandCodeRow(KeyPath{"permissions", "deny"}, TypeRuleList, "", "Rules that never run; deny always wins."), func(r *SettingRow) { r.Source = commandCodePerms }),
		commandCodeWith(commandCodeRow(KeyPath{"permissions", "additionalDirectories"}, TypeStringList, "", "Paths that count as inside the workspace."), func(r *SettingRow) {
			r.Source, r.Danger = commandCodePerms, DangerConfirm
			r.Note = "Widens where the agent may read and write; accumulates across settings files."
		}),
		commandCodeWith(commandCodeRow(KeyPath{"permissions", "disableBypass"}, TypeBool, "", "Prevent entering yolo mode at any layer."), func(r *SettingRow) {
			r.Source, r.Danger = commandCodePerms, DangerConfirm
			r.Note = "Also accepts the string \"disable\". false re-allows the bypass mode (the only self-lock Command Code has)."
		}),
		commandCodeWith(commandCodeRow(KeyPath{"hooks"}, TypeObject, "", "Lifecycle hooks: PreToolUse, PostToolUse, Stop, SessionStart lists of {matcher, hooks: [{type: command, command, timeout 0-600, async, failClosed}]}."), func(r *SettingRow) {
			r.Source, r.Danger = commandCodeHooks, DangerConfirm
			r.Note = "Hook commands run arbitrary code; matchers are shell, read, write, edit. Plan mode skips hooks entirely. Observer hooks Command Code through its mod file, not this block."
		}),
		commandCodeRow(KeyPath{"theme"}, TypeString, "", "Terminal theme override."),
		commandCodeRow(KeyPath{"tasteLearning"}, TypeBool, "true", "Per-project taste learning."),
		commandCodeWith(commandCodeRow(KeyPath{"compactMode"}, TypeEnum, "\"default\"", "Auto-compact aggressiveness."), func(r *SettingRow) {
			r.Enum = []string{"default", "fast"}
		}),
		commandCodeRow(KeyPath{"featureModels"}, TypeObject, "", "Per-feature model overrides (planning, implementation, titleGeneration, compaction, toolDescription, tasteOnboarding, tasteLearning, branchSummarization)."),
		commandCodeRow(KeyPath{"featureModels", "planning"}, TypeString, "", "Model for plan-mode exploration."),
		commandCodeRow(KeyPath{"featureModels", "implementation"}, TypeString, "", "Model for the execution phase."),
		commandCodeRow(KeyPath{"featureModels", "compaction"}, TypeString, "", "Model for conversation compaction."),
		commandCodeWith(commandCodeRow(KeyPath{"providers"}, TypeObject, "", "BYOK provider configuration."), func(r *SettingRow) {
			r.Danger, r.Verified = DangerConfirm, VerifiedUnverified
			r.Note = "Shape UNVERIFIED (audit). A provider base URL redirects model traffic."
		}),
		commandCodeRow(KeyPath{"plugins"}, TypeStringList, "", "Plugin list."),
		commandCodeRow(KeyPath{"skills"}, TypeStringList, "", "Extra skill directories."),
		commandCodeRow(KeyPath{"disabledSkills"}, TypeStringList, "", "Skills to disable (unioned across layers)."),
		commandCodeRow(KeyPath{"disableSkillShellExecution"}, TypeBool, "false", "Disable dynamic shell placeholders in skills."),
		commandCodeRow(KeyPath{"disableScratchpad"}, TypeBool, "false", "Disable the per-session scratchpad."),
		commandCodeWith(commandCodeRow(KeyPath{"mcp"}, TypeObject, "", "Inline MCP servers: {servers: [...]}."), func(r *SettingRow) {
			r.Note = "An array under servers; mcp.json's mcpServers map is the mapped surface."
		}),
		commandCodeWith(commandCodeRow(KeyPath{"mods"}, TypeObject, "", "Declarative mod configuration."), func(r *SettingRow) {
			r.Source = commandCodeMods
			r.Note = "Mods are TypeScript in ~/.commandcode/mods/; Observer's observer-guard.ts is one of them."
		}),
		commandCodeRow(KeyPath{"attribution"}, TypeObject, "", "Commit co-author trailer (default: the CommandCodeBot trailer)."),
		{
			ID: "command-code/mcpServers", Tool: "command-code", FileID: "command-code/mcp", Path: KeyPath{"mcpServers"}, Type: TypeMCPMap,
			Desc:   "MCP servers by name: transport (alias type) http with url / headers / env, or a stdio command; enabled.",
			Source: commandCodeMCP, Verified: VerifiedDoc, Owner: OwnerUser,
			Note: "The docs show only HTTP entries; the stdio entry shape follows Observer's own registrar. Observer's entry (observer) is an owned span.",
		},
	},
	Map: []MapRow{
		{Key: KeyModel, Tool: "command-code", Settings: []string{"command-code/model"}, Transform: TransformModelAlias, Source: commandCodeRef},
		{
			Key: KeyApprovalMode, Tool: "command-code", Settings: []string{"command-code/permissions.defaultMode"}, Transform: TransformEnumMap,
			Params: TransformParams{Enum: []EnumPair{
				{"plan", []any{"plan"}},
				{"ask_edits", []any{"default"}},
				{"auto_edits", []any{"accept-edits"}},
				{"full_auto", []any{"yolo"}},
				{"auto_edits", []any{"auto-accept"}},
				{"full_auto", []any{"bypass"}},
			}},
			Lossy: true, Source: commandCodePerms,
			Note: "ask (prompt for reads too) is refused: reads are always free. The aliases auto-accept and bypass read back as auto_edits / full_auto; dont-ask has no canonical form.",
		},
		{
			Key: KeyAllowedTools, Tool: "command-code", Settings: []string{"command-code/permissions.allow"}, Transform: TransformRuleTranslate,
			Params: TransformParams{Grammar: commandCodeRuleGrammar}, Source: commandCodePerms,
		},
		{
			Key: KeyDeniedTools, Tool: "command-code", Settings: []string{"command-code/permissions.deny"}, Transform: TransformRuleTranslate,
			Params: TransformParams{Grammar: commandCodeRuleGrammar}, Source: commandCodePerms,
		},
		{
			Key: KeyAdditionalDirs, Tool: "command-code", Settings: []string{"command-code/permissions.additionalDirectories"},
			Transform: TransformIdentity, Source: commandCodePerms,
		},
		{
			Key: KeyMCPServers, Tool: "command-code", Settings: []string{"command-code/mcpServers"}, Transform: TransformMCPShape,
			Params: TransformParams{MCP: commandCodeMCPShape}, Lossy: true, Source: commandCodeMCP, Note: mcpLossyNote,
		},
	},
}

// commandCodeRuleGrammar is Command Code's permission rule spelling (the
// permissions page): Claude-like, but the shell tool is Shell.
var commandCodeRuleGrammar = RuleGrammar{
	"shell":  {Name: "Shell", Style: RuleCall},
	"read":   {Name: "Read", Style: RuleCall},
	"edit":   {Name: "Edit", Style: RuleCall},
	"write":  {Name: "Write", Style: RuleCall},
	"fetch":  {Name: "WebFetch", Style: RuleCall, PatternPrefix: "domain:"},
	"search": {Name: "WebSearch", Style: RuleBare},
	"mcp":    {Name: "mcp__", Style: RulePrefix, Sep: "__"},
}

// commandCodeMCPShape: mcpServers.<name> with transport "http" + url for a
// remote server; a local entry carries command / args / env.
var commandCodeMCPShape = MCPShape{
	EnvKey: "env", TypeKey: "transport",
	TypeNames: map[string]string{"stdio": "stdio", "http": "http"},
	URLKeys:   map[string]string{"http": "url"},
}
