package agentconfig

// Gemini CLI overlay. Sources: github.com/google-gemini/gemini-cli
// docs/reference/configuration.md and docs/cli/enterprise.md (fetched
// 2026-10-03; audit section 5 "gemini-cli").
const (
	geminiRef        = "https://raw.githubusercontent.com/google-gemini/gemini-cli/main/docs/reference/configuration.md"
	geminiEnterprise = "https://raw.githubusercontent.com/google-gemini/gemini-cli/main/docs/cli/enterprise.md"
)

var geminiCLICatalog = toolCatalog{
	Tool:            "gemini-cli",
	Map:             geminiCLIMap,
	VerifiedAgainst: "schemas/settings.schema.json on main, fetched 2026-10-03 (the schema carries no release version)",
	Overlays: []Overlay{
		{
			ID: "gemini-cli/general.defaultApprovalMode", Danger: DangerConfirm, Source: geminiRef,
			Note: "\"auto_edit\" applies edits without asking (yolo is flag-only).",
		},
		{
			ID: "gemini-cli/tools.allowed", Type: TypeRuleList, Danger: DangerConfirm, Source: geminiRef,
			Note: "Entries skip confirmation; a broad entry such as run_shell_command needs a confirmation.",
		},
		{ID: "gemini-cli/tools.core", Type: TypeRuleList, Source: geminiRef},
		{ID: "gemini-cli/tools.exclude", Type: TypeRuleList, Source: geminiRef},
		{ID: "gemini-cli/tools.sandbox", Danger: DangerConfirm, Source: geminiRef, Note: "false runs tools unsandboxed."},
		{ID: "gemini-cli/tools.sandbox.enabled", Danger: DangerConfirm, Source: geminiRef},
		{ID: "gemini-cli/tools.sandboxNetworkAccess", Danger: DangerConfirm, Source: geminiRef},
		{ID: "gemini-cli/tools.sandbox.networkAccess", Danger: DangerConfirm, Source: geminiRef},
		{ID: "gemini-cli/security.folderTrust.enabled", Danger: DangerConfirm, Source: geminiRef, Note: "false trusts every folder."},
		{ID: "gemini-cli/hooksConfig.enabled", Danger: DangerRefuseWhileIntegrated, Source: geminiRef, Note: "false switches off hooks, Observer's included."},
		{ID: "gemini-cli/hooksConfig.disabled", Danger: DangerRefuseWhileIntegrated, Source: geminiRef, Note: "Can disable Observer's hooks by name."},
		{
			ID: "gemini-cli/hooks", Prefix: true, Source: geminiRef,
			Note: "Observer's hook registration owns its own entries (stamped as an Observer-owned span in phase P3).",
		},
		{ID: "gemini-cli/mcpServers", Type: TypeMCPMap, Source: geminiRef},
		{ID: "gemini-cli/mcp.allowed", Danger: DangerRefuseWhileIntegrated, Source: geminiRef, Note: "A list without Observer drops its MCP server."},
		{ID: "gemini-cli/mcp.excluded", Danger: DangerRefuseWhileIntegrated, Source: geminiRef, Note: "Can drop Observer's MCP server."},
		{
			ID: "gemini-cli/admin", Prefix: true, Scopes: managedScope, Source: geminiEnterprise,
			Note: "Admin controls: honoured from the system settings file.",
		},
	},
}
