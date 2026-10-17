package integration

import (
	"sort"
	"strings"
)

// The D-268 credential-file exception (HookSpec.HooksShareCredentialFile):
// the grounding for every row whose hook writer read-modify-writes a vendor
// file that also holds plaintext credentials. The audit behind this list
// (Integration 6, T-375) walked every hook writer in internal/hook and
// checked the file it rewrites against the agentconfig catalogue: a file is
// credential-bearing when the catalogue carries a Secret row for it or says
// it holds literal keys. Writers that own a dedicated hooks file (Cursor,
// Codex hooks.json, Grok, Copilot CLI, Goose, Open Interpreter, Droid,
// Antigravity CLI, Cascade, Mistral Vibe hooks.toml, Command Code's mod
// file, the plugin shims) are not flagged. Gemini CLI and Qoder settings.json
// carry no catalogued credential slot and are not flagged either.
//
//nolint:gosec // G101: grounding prose that names a credential slot, not a credential.
const (
	credFileClaudeCode = "~/.claude/settings.json: its env block can carry ANTHROPIC_API_KEY, ANTHROPIC_AUTH_TOKEN and the other credential variables the agentconfig catalogue marks Secret (claude-code/settings env.*)."
	credFileCodex      = "~/.codex/config.toml: [features].hooks lives in the file that can hold a literal bearer token (features.multi_agent_v2.message_board_remote.bearer_token, a catalogue Secret row) beside the provider settings."
	credFileQwenCode   = "~/.qwen/settings.json: security.auth.apiKey (a catalogue Secret row) and the env block can hold a literal provider key."
	credFileCrush      = "~/.config/crush/crush.json: providers.<id>.api_key may be a literal key (the vendor schema's providers block, catalogue row crush/providers)."
	credFileJunie      = "~/.junie/config.json: byok holds literal BYOK API keys (catalogue Secret row junie/byok)."
	credFilePoolside   = "~/.config/poolside/settings.yaml: web_search.api_key and agent_servers can hold credentials (catalogue Secret rows)."
	credFileHermes     = "~/.hermes/config.yaml: the plugins.enabled entry the hook plugin needs shares the file with provider api_key values and request headers (catalogue Secret rows)."
	credFileZcode      = "~/.zcode/cli/config.json: the vendor stores hooks alongside plaintext provider keys (docs/zcode-adapter.md); Observer edits only the hooks key."
	credFileKimiCode   = "~/.kimi-code/config.toml: the vendor's only hooks file ([[hooks]] tables) also holds [providers.<name>].api_key and services.*.api_key as plaintext (catalogue Secret rows); Observer edits only its own [[hooks]] tables (T-378, D-270)."
)

// The MCP half of the exception (MCPTarget.SharesCredentialFile, review
// T-384): every MCP config file Observer writes keeps the user's OTHER
// servers beside observer's entry, and those carry env blocks and request
// headers that hold literal tokens (each vendor's MCP config documents an
// env map), so every implemented MCP target is declared.
//
//nolint:gosec // G101: grounding prose that names a credential slot, not a credential.
const (
	mcpCredClaudeCode  = "~/.claude.json: mcpServers.<name>.env and headers hold literal tokens for the user's other servers, beside the account state the file keeps."
	mcpCredCursor      = "~/.cursor/mcp.json: mcpServers.<name>.env and headers hold literal tokens for the user's other servers."
	mcpCredCodex       = "~/.codex/config.toml: [mcp_servers.<name>] env and http_headers, and the provider settings, can hold literal keys (the hook-side reason applies too)."
	mcpCredOpenCode    = "~/.config/opencode/opencode.json: provider.<id>.options.apiKey and mcp.<name>.environment / headers can hold literal keys."
	mcpCredCline       = "cline_mcp_settings.json: mcpServers.<name>.env holds literal tokens for the user's other servers."
	mcpCredHermes      = "~/.hermes/config.yaml: mcp_servers.<name>.env shares the file with provider api_key values (the hook-side reason applies too)."
	mcpCredDroid       = "~/.factory/mcp.json: mcpServers.<name>.env and headers hold literal tokens for the user's other servers."
	mcpCredCommandCode = "~/.commandcode/mcp.json: mcpServers.<name>.env and headers hold literal tokens for the user's other servers."
)

// MCPSharesCredentialFile reports whether tool's MCP config file is declared
// under the credential-file exception. A "-windows" cross-OS target answers
// for its base row. An unknown tool, or a row with no MCP target, is false.
func MCPSharesCredentialFile(tool string) bool {
	c, ok := For(strings.TrimSuffix(tool, "-windows"))
	return ok && c.MCP != nil && c.MCP.SharesCredentialFile
}

// HooksShareCredentialFile reports whether tool's hook writer works under
// the D-268 credential-file exception. A "-windows" cross-OS target answers
// for its base row (the bridge writes the same file shape on the Windows
// side). An unknown tool is false.
func HooksShareCredentialFile(tool string) bool {
	c, ok := For(strings.TrimSuffix(tool, "-windows"))
	return ok && c.Hook.HooksShareCredentialFile
}

// CredentialFileHookTools lists, sorted, every registry row that carries
// the credential-file exception.
func CredentialFileHookTools() []string {
	var out []string
	for _, c := range Capabilities() {
		if c.Hook.HooksShareCredentialFile {
			out = append(out, c.Tool)
		}
	}
	sort.Strings(out)
	return out
}
