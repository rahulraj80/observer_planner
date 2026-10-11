package agentconfig

// ZCode (Z.AI, an OpenCode fork with its own config shape) doc-authored
// catalogue. Sources: zcode.z.ai docs (hooks, mcp-services, configuration)
// fetched 2026-10-04, audit section 5 "zcode" and repo docs/zcode-adapter.md.
// ZCode is not installed on node 1: doc-only. Most of ZCode's settings
// (model, permission mode, theme) are UI-only with no documented file key.
const (
	zcodeHooksRef = "https://zcode.z.ai/en/docs/hooks"
	zcodeMCPRef   = "https://zcode.z.ai/en/docs/mcp-services"
	zcodeConfRef  = "https://zcode.z.ai/en/docs/configuration"
	zcodeShape    = "zcode/config"
)

// zcodeRow fills the tool, shape, owner, verification and id of one row.
func zcodeRow(r SettingRow) SettingRow {
	r.Tool = "zcode"
	if r.FileID == "" {
		r.FileID = zcodeShape
	}
	if r.Owner == "" {
		r.Owner = OwnerUser
	}
	if r.Verified == "" {
		r.Verified = VerifiedDoc
	}
	if r.Source == "" {
		r.Source = zcodeHooksRef
	}
	r.ID = r.Tool + "/" + r.Path.String()
	return r
}

// zcodeMCPShape is mcp.servers' documented entry: command / args / env. No
// remote transport is documented, so http and sse are refused.
var zcodeMCPShape = MCPShape{EnvKey: "env", TypeNames: map[string]string{"stdio": ""}}

var zcodeCatalog = toolCatalog{
	Tool: "zcode",
	VerifiedAgainst: "zcode.z.ai docs fetched 2026-10-04 (hooks, mcp-services, configuration; audit section 5 zcode); repo docs/zcode-adapter.md for the plaintext-key finding. " +
		"Not installed on node 1: no live evidence. Precedence between the user and workspace files is not documented.",
	Files: []FileRow{
		{
			ID: "zcode/user-cli-config", Tool: "zcode", Shape: zcodeShape, Kind: KindSettings, Scope: ScopeUser,
			Paths: samePath("~/.zcode/cli/config.json"), Format: FormatJSON, Precedence: 20, WriteNeeds: PrivNever,
			Grounding: doc(zcodeHooksRef),
			Note: "The CLI agent runtime's config (hooks, mcp.servers). Repo grounding (docs/zcode-adapter.md) saw plaintext provider apiKey values in it, " +
				"so the settings editor never reads or writes it; its rows come from the vendor docs only. " +
				"The one exception is Observer's hook registration, which edits only the hooks key of this file under the declared " +
				"credential-file exception (D-268 / D-270, ledger HOOKCRED-1).",
		},
		{
			ID: "zcode/project-config", Tool: "zcode", Shape: zcodeShape, Kind: KindSettings, Scope: ScopeProjectShared,
			Paths: samePath("<project>/.zcode/config.json"), Format: FormatJSON, Precedence: 30, WriteNeeds: PrivUser,
			Grounding: doc(zcodeMCPRef), Note: "Only mcp.servers is honoured here; project hooks (here or in zcode.json) are ignored by design.",
		},
		{
			ID: "zcode/user-model-providers", Tool: "zcode", Shape: "zcode/model-providers", Kind: KindToolState, Scope: ScopeUser,
			Paths: samePath("~/.zcode/v2/config.json"), Format: FormatJSON, Precedence: 20, WriteNeeds: PrivNever,
			Grounding: doc(zcodeConfRef),
			Note:      "Model providers, managed by Settings -> Model Providers; provider options.apiKey is a literal string, so the file is never read or written.",
		},
		{
			ID: "zcode/user-agents-mcp", Tool: "zcode", Shape: "zcode/agents-mcp", Kind: KindMCP, Scope: ScopeUser,
			Paths: samePath("~/.agents/mcp.json"), Format: FormatJSON, Precedence: 20, WriteNeeds: PrivUser,
			Grounding: doc(zcodeMCPRef), Note: "Shared .agents convention, read only when the .zcode file of the same scope defines no server (no merging).",
		},
		{
			ID: "zcode/project-agents-mcp", Tool: "zcode", Shape: "zcode/agents-mcp", Kind: KindMCP, Scope: ScopeProjectShared,
			Paths: samePath("<project>/.agents/mcp.json"), Format: FormatJSON, Precedence: 30, WriteNeeds: PrivUser,
			Grounding: doc(zcodeMCPRef), Note: "Fallback only, as for the user file.",
		},
	},
	Extra: []SettingRow{
		zcodeRow(SettingRow{
			Path: KeyPath{"mcp", "servers"}, Type: TypeMCPMap, Source: zcodeMCPRef,
			Desc: "MCP servers by name: {command, args, env, enable}.",
			Note: "The flag is enable (not enabled); \"enable\": false disables a server. The settings panel always writes here, never to .agents.",
		}),
		zcodeRow(SettingRow{
			Path: KeyPath{"hooks", "enabled"}, Type: TypeBool, Scopes: []Scope{ScopeUser}, Desc: "Master switch for hooks; must be true for any hook to run.",
			Danger: DangerRefuseWhileIntegrated, Note: "false switches every hook off. User file only.",
		}),
		zcodeRow(SettingRow{Path: KeyPath{"hooks", "timeoutMs"}, Type: TypeInt, Default: "60000", Scopes: []Scope{ScopeUser}, Desc: "Default hook timeout in milliseconds."}),
		zcodeRow(SettingRow{Path: KeyPath{"hooks", "maxOutputBytes"}, Type: TypeInt, Default: "32768", Scopes: []Scope{ScopeUser}, Desc: "Hook stdout cap in bytes."}),
		zcodeRow(SettingRow{
			Path: KeyPath{"hooks", "events"}, Type: TypeObject, Scopes: []Scope{ScopeUser},
			Desc: "Hooks by event (UserPromptSubmit, PreToolUse, PermissionRequest, PostToolUse, PostToolUseFailure, Stop, ...): [{matcher, hooks: [{type: process | command, command, args, enabled, timeoutMs}]}].",
			Note: "type command runs a shell string; a single hook can be skipped with enabled: false. Hooks skip sub-agents.",
		}),
		zcodeRow(SettingRow{
			Path: KeyPath{"mcpServers"}, FileID: "zcode/agents-mcp", Type: TypeMCPMap, Source: zcodeMCPRef,
			Desc: "MCP servers in the shared .agents format.",
		}),
	},
	Map: []MapRow{
		{
			Key: KeyMCPServers, Tool: "zcode", Settings: []string{"zcode/mcp.servers"}, Transform: TransformMCPShape,
			Params: TransformParams{MCP: zcodeMCPShape}, Lossy: true, Source: zcodeMCPRef,
			Note: "Local servers only (no remote transport is documented). The user file is never written (plaintext keys), so writes land in the project file. " + mcpLossyNote,
		},
	},
}
