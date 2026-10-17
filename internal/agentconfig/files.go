package agentconfig

import (
	"sort"
	"strings"
	"sync"

	"github.com/marmutapp/superbased-observer/internal/guidance"
)

// Grounding shorthands. Vendor facts were fetched 2026-10-03 (audit
// docs/audits/adapter-config-surface-2026-10-03.md section 5, which cites
// every URL); "node 1" is the read-only live probe of audit section 6.
const auditDate = "2026-10-03"

func doc(url string) Grounding { return Grounding{Kind: GroundDoc, Source: url, Date: auditDate} }
func live(what string) Grounding {
	return Grounding{Kind: GroundLive, Source: "node 1: " + what, Date: auditDate}
}
func source(url string) Grounding { return Grounding{Kind: GroundSource, Source: url, Date: auditDate} }

// settingsFiles is the settings-kind part of the Files table: the tools
// with a schema-generated catalogue (phase P1). The doc-authored tools of
// phase P2 append their own rows here.
var settingsFiles = []FileRow{
	// --- Claude Code (code.claude.com/docs/en/settings, settings-reference,
	// managed-settings). Settings files are strict JSON (the settings page
	// and settings-example page both say comments are not accepted).
	{
		ID: "claude-code/user-settings", Tool: "claude-code", Shape: "claude-code/settings", Kind: KindSettings, Scope: ScopeUser,
		Paths: samePath("~/.claude/settings.json"), Format: FormatJSON, SchemaURL: "https://json.schemastore.org/claude-code-settings.json",
		Precedence: 20, WriteNeeds: PrivUser, Grounding: live("~/.claude/settings.json carries hooks + theme"),
		Note: "CLAUDE_CONFIG_DIR relocates ~/.claude.",
	},
	{
		ID: "claude-code/project-settings", Tool: "claude-code", Shape: "claude-code/settings", Kind: KindSettings, Scope: ScopeProjectShared,
		Paths: samePath("<project>/.claude/settings.json"), Format: FormatJSON, SchemaURL: "https://json.schemastore.org/claude-code-settings.json",
		Precedence: 30, WriteNeeds: PrivUser, Grounding: doc("https://code.claude.com/docs/en/settings"),
	},
	{
		ID: "claude-code/project-local-settings", Tool: "claude-code", Shape: "claude-code/settings", Kind: KindSettings, Scope: ScopeProjectLocal,
		Paths: samePath("<project>/.claude/settings.local.json"), Format: FormatJSON, SchemaURL: "https://json.schemastore.org/claude-code-settings.json",
		Precedence: 40, WriteNeeds: PrivUser, Gitignored: true, Grounding: doc("https://code.claude.com/docs/en/settings"),
	},
	{
		ID: "claude-code/managed-settings", Tool: "claude-code", Shape: "claude-code/settings", Kind: KindSettings, Scope: ScopeManaged,
		Paths: OSPaths{
			Linux:   "/etc/claude-code/managed-settings.json",
			Darwin:  "/Library/Application Support/ClaudeCode/managed-settings.json",
			Windows: `C:\Program Files\ClaudeCode\managed-settings.json`,
		},
		Format: FormatJSON, SchemaURL: "https://json.schemastore.org/claude-code-settings.json",
		Precedence: 100, WriteNeeds: PrivAdminRoot, Grounding: doc("https://code.claude.com/docs/en/managed-settings"),
		Note: "Plus managed-settings.d/*.json (merged) and the MDM / registry sources; /etc/claude-code is reported user-writable on WSL, so it is not a lock there.",
	},
	{
		ID: "claude-code/global-config", Tool: "claude-code", Shape: "claude-code/global-config", Kind: KindToolState, Scope: ScopeUser,
		Paths: samePath("~/.claude.json"), Format: FormatJSON, Precedence: 20, WriteNeeds: PrivNever,
		Grounding: live("~/.claude.json carries autoUpdates beside large server caches"),
		Note:      "Mostly server caches (cachedGrowthBookFeatures, cachedExperimentData) plus user / local MCP servers: shown, never form-edited.",
	},
	{
		ID: "claude-code/project-mcp", Tool: "claude-code", Shape: "claude-code/mcp", Kind: KindMCP, Scope: ScopeProjectShared,
		Paths: samePath("<project>/.mcp.json"), Format: FormatJSON, Precedence: 30, WriteNeeds: PrivUser,
		Grounding: doc("https://code.claude.com/docs/en/settings"),
	},

	// --- Codex (learn.chatgpt.com/docs/config-file/config-reference,
	// managed-configuration). Precedence: project > user > system; the
	// managed defaults file overrides user config at startup.
	{
		ID: "codex/system-config", Tool: "codex", Shape: "codex/config", Kind: KindSettings, Scope: ScopeSystem,
		Paths: OSPaths{Linux: "/etc/codex/config.toml", Darwin: "/etc/codex/config.toml"}, Format: FormatTOML,
		SchemaURL:  "https://raw.githubusercontent.com/openai/codex/main/codex-rs/core/config.schema.json",
		Precedence: 10, WriteNeeds: PrivAdminRoot, Grounding: doc("https://learn.chatgpt.com/docs/config-file/config-reference"),
	},
	{
		ID: "codex/user-config", Tool: "codex", Shape: "codex/config", Kind: KindSettings, Scope: ScopeUser,
		Paths: samePath("~/.codex/config.toml"), Format: FormatTOML,
		SchemaURL:  "https://raw.githubusercontent.com/openai/codex/main/codex-rs/core/config.schema.json",
		Precedence: 20, WriteNeeds: PrivUser, Grounding: live("~/.codex/config.toml carries model, model_reasoning_effort, projects, tui"),
		Note: "CODEX_HOME relocates ~/.codex.",
	},
	{
		ID: "codex/project-config", Tool: "codex", Shape: "codex/config", Kind: KindSettings, Scope: ScopeProjectShared,
		Paths: samePath("<project>/.codex/config.toml"), Format: FormatTOML,
		SchemaURL:  "https://raw.githubusercontent.com/openai/codex/main/codex-rs/core/config.schema.json",
		Precedence: 30, WriteNeeds: PrivUser, Grounding: doc("https://learn.chatgpt.com/docs/config-file/config-reference"),
		Note: "Read only when the project is trusted (projects.<path>.trust_level = \"trusted\").",
	},
	{
		ID: "codex/managed-config", Tool: "codex", Shape: "codex/config", Kind: KindSettings, Scope: ScopeManaged,
		Paths:  OSPaths{Linux: "/etc/codex/managed_config.toml", Darwin: "/etc/codex/managed_config.toml", Windows: "~/.codex/managed_config.toml"},
		Format: FormatTOML, SchemaURL: "https://raw.githubusercontent.com/openai/codex/main/codex-rs/core/config.schema.json",
		Precedence: 50, WriteNeeds: PrivAdminRoot,
		Grounding: doc("https://learn.chatgpt.com/docs/enterprise/managed-configuration"),
		Note:      "Managed DEFAULTS (reset each launch). The Windows path is user-writable: lever B there, not a lock.",
	},
	{
		ID: "codex/requirements", Tool: "codex", Shape: "codex/requirements", Kind: KindPermissions, Scope: ScopeManaged,
		Paths:  OSPaths{Linux: "/etc/codex/requirements.toml", Darwin: "/etc/codex/requirements.toml", Windows: `%ProgramData%\OpenAI\Codex\requirements.toml`},
		Format: FormatTOML, Precedence: 100, WriteNeeds: PrivAdminRoot,
		Grounding: doc("https://learn.chatgpt.com/docs/enterprise/managed-configuration"),
		Note:      "Enforced constraints (allowed_approval_policies, allowed_sandbox_modes, ...); its rows land with the lever A phase (P8).",
	},
	{
		ID: "codex/user-hooks", Tool: "codex", Shape: "codex/hooks", Kind: KindHooks, Scope: ScopeUser,
		Paths: samePath("~/.codex/hooks.json"), Format: FormatJSON, Precedence: 20, WriteNeeds: PrivUser,
		Grounding: doc("https://learn.chatgpt.com/docs/config-file/config-reference"),
		Note:      "Observer hook registration writes this file (hook-parity Lane 0 owns it).",
	},

	// --- Open Interpreter (openinterpreter.com/docs/terminal/config-reference;
	// repo docs/openinterpreter-adapter.md). Codex-shaped config.
	{
		ID: "open-interpreter/user-config", Tool: "open-interpreter", Shape: "open-interpreter/config", Kind: KindSettings, Scope: ScopeUser,
		Paths: samePath("~/.openinterpreter/config.toml"), Format: FormatTOML,
		SchemaURL:  "https://raw.githubusercontent.com/openai/codex/main/codex-rs/core/config.schema.json",
		Precedence: 20, WriteNeeds: PrivUser, Grounding: doc("https://www.openinterpreter.com/docs/terminal/config-reference"),
		Note: "INTERPRETER_HOME relocates ~/.openinterpreter.",
	},
	{
		ID: "open-interpreter/project-config", Tool: "open-interpreter", Shape: "open-interpreter/config", Kind: KindSettings, Scope: ScopeProjectShared,
		Paths: samePath("<project>/.openinterpreter/config.toml"), Format: FormatTOML,
		SchemaURL:  "https://raw.githubusercontent.com/openai/codex/main/codex-rs/core/config.schema.json",
		Precedence: 30, WriteNeeds: PrivUser, Grounding: doc("https://www.openinterpreter.com/docs/terminal/config-reference"),
		Note: "Read only when the project is trusted.",
	},
	{
		ID: "open-interpreter/desktop-config", Tool: "open-interpreter", Shape: "open-interpreter/desktop-config", Kind: KindToolState, Scope: ScopeUser,
		Paths: OSPaths{Windows: `%APPDATA%\interpreter\codex-home\config.toml`}, Format: FormatTOML,
		Precedence: 20, WriteNeeds: PrivNever, Grounding: Grounding{Kind: GroundAdapterDoc, Source: "docs/openinterpreter-adapter.md", Date: "2026-09-03"},
		Note: "The desktop app's config holds PLAINTEXT provider API keys: never read, never written. macOS / Linux desktop roots unverified.",
	},

	// --- Gemini CLI (github.com/google-gemini/gemini-cli docs/reference/
	// configuration.md). The settings loader strips comments
	// (packages/cli/src/config/settings.ts: stripJsonComments), so JSONC.
	{
		ID: "gemini-cli/system-defaults", Tool: "gemini-cli", Shape: "gemini-cli/settings", Kind: KindSettings, Scope: ScopeSystem,
		Paths: OSPaths{
			Linux:   "/etc/gemini-cli/system-defaults.json",
			Darwin:  "/Library/Application Support/GeminiCli/system-defaults.json",
			Windows: `C:\ProgramData\gemini-cli\system-defaults.json`,
		},
		Format: FormatJSONC, SchemaURL: "https://raw.githubusercontent.com/google-gemini/gemini-cli/main/schemas/settings.schema.json",
		Precedence: 10, WriteNeeds: PrivAdminRoot, Grounding: doc("https://raw.githubusercontent.com/google-gemini/gemini-cli/main/docs/reference/configuration.md"),
	},
	{
		ID: "gemini-cli/user-settings", Tool: "gemini-cli", Shape: "gemini-cli/settings", Kind: KindSettings, Scope: ScopeUser,
		Paths: samePath("~/.gemini/settings.json"), Format: FormatJSONC,
		SchemaURL:  "https://raw.githubusercontent.com/google-gemini/gemini-cli/main/schemas/settings.schema.json",
		Precedence: 20, WriteNeeds: PrivUser, Grounding: source("https://raw.githubusercontent.com/google-gemini/gemini-cli/main/packages/cli/src/config/settings.ts"),
		Note: "GEMINI_CLI_HOME relocates ~/.gemini.",
	},
	{
		ID: "gemini-cli/project-settings", Tool: "gemini-cli", Shape: "gemini-cli/settings", Kind: KindSettings, Scope: ScopeProjectShared,
		Paths: samePath("<project>/.gemini/settings.json"), Format: FormatJSONC,
		SchemaURL:  "https://raw.githubusercontent.com/google-gemini/gemini-cli/main/schemas/settings.schema.json",
		Precedence: 30, WriteNeeds: PrivUser, Grounding: doc("https://raw.githubusercontent.com/google-gemini/gemini-cli/main/docs/reference/configuration.md"),
		Note: "Gated by folder trust.",
	},
	{
		ID: "gemini-cli/system-settings", Tool: "gemini-cli", Shape: "gemini-cli/settings", Kind: KindSettings, Scope: ScopeManaged,
		Paths: OSPaths{
			Linux:   "/etc/gemini-cli/settings.json",
			Darwin:  "/Library/Application Support/GeminiCli/settings.json",
			Windows: `C:\ProgramData\gemini-cli\settings.json`,
		},
		Format: FormatJSONC, SchemaURL: "https://raw.githubusercontent.com/google-gemini/gemini-cli/main/schemas/settings.schema.json",
		Precedence: 100, WriteNeeds: PrivAdminRoot, Grounding: doc("https://raw.githubusercontent.com/google-gemini/gemini-cli/main/docs/cli/enterprise.md"),
		Note: "GEMINI_CLI_SYSTEM_SETTINGS_PATH is user-settable, so this is a lock only with a wrapper.",
	},

	// --- OpenCode (opencode.ai/docs/config). JSON or JSONC; merged.
	{
		ID: "opencode/user-config", Tool: "opencode", Shape: "opencode/config", Kind: KindSettings, Scope: ScopeUser,
		Paths: samePath("~/.config/opencode/opencode.json"), Format: FormatJSONC, SchemaURL: "https://opencode.ai/config.json",
		Precedence: 20, WriteNeeds: PrivUser, Grounding: live("~/.config/opencode/opencode.json carries $schema, mcp, provider, small_model"),
		Note: "opencode.jsonc is also read; the Windows path is the same XDG layout (inferred, not live-checked).",
	},
	{
		ID: "opencode/project-config", Tool: "opencode", Shape: "opencode/config", Kind: KindSettings, Scope: ScopeProjectShared,
		Paths: samePath("<project>/opencode.json"), Format: FormatJSONC, SchemaURL: "https://opencode.ai/config.json",
		Precedence: 30, WriteNeeds: PrivUser, Grounding: doc("https://opencode.ai/docs/config/"),
	},
	{
		ID: "opencode/managed-config", Tool: "opencode", Shape: "opencode/config", Kind: KindSettings, Scope: ScopeManaged,
		Paths: OSPaths{
			Linux:   "/etc/opencode/opencode.json",
			Darwin:  "/Library/Application Support/opencode/opencode.json",
			Windows: `%ProgramData%\opencode\opencode.json`,
		},
		Format: FormatJSONC, SchemaURL: "https://opencode.ai/config.json",
		Precedence: 100, WriteNeeds: PrivAdminRoot, Grounding: doc("https://opencode.ai/docs/enterprise/"),
	},
	{
		ID: "opencode/user-tui", Tool: "opencode", Shape: "opencode/tui", Kind: KindSettings, Scope: ScopeUser,
		Paths: samePath("~/.config/opencode/tui.json"), Format: FormatJSONC, SchemaURL: "https://opencode.ai/tui.json",
		Precedence: 20, WriteNeeds: PrivUser, Grounding: doc("https://opencode.ai/docs/config/"),
		Note: "TUI settings (theme); its schema is not pinned yet, so it has no catalogue rows.",
	},

	// --- Kilo Code 7.x and the Kilo CLI share one OpenCode-lineage config
	// (kilo.ai/docs; source packages/opencode/src/config/{paths,managed}.ts).
	{
		ID: "kilo-code-cli/user-config", Tool: "kilo-code-cli", Shape: "kilo-code-cli/config", Kind: KindSettings, Scope: ScopeUser,
		Paths: samePath("~/.config/kilo/kilo.jsonc"), Format: FormatJSONC, SchemaURL: "https://app.kilo.ai/config.json",
		Precedence: 20, WriteNeeds: PrivUser, Grounding: live("~/.config/kilo/kilo.jsonc holds only $schema"),
		Note: "config.json, kilo.json, opencode.json(c) in the same dir are also read; the XDG path is used on Windows too.",
	},
	{
		ID: "kilo-code-cli/project-config", Tool: "kilo-code-cli", Shape: "kilo-code-cli/config", Kind: KindSettings, Scope: ScopeProjectShared,
		Paths: samePath("<project>/kilo.jsonc"), Format: FormatJSONC, SchemaURL: "https://app.kilo.ai/config.json",
		Precedence: 30, WriteNeeds: PrivUser, Grounding: doc("https://kilo.ai/docs/contributing/architecture/config-schema"),
	},
	{
		ID: "kilo-code-cli/managed-dir", Tool: "kilo-code-cli", Shape: "kilo-code-cli/config", Kind: KindSettings, Scope: ScopeManaged,
		Paths:  OSPaths{Linux: "/etc/kilo/", Darwin: "/Library/Application Support/kilo/", Windows: `%ProgramData%\kilo\`},
		Format: FormatJSONC, SchemaURL: "https://app.kilo.ai/config.json",
		Precedence: 100, WriteNeeds: PrivAdminRoot, Grounding: source("https://raw.githubusercontent.com/Kilo-Org/kilocode/main/packages/opencode/src/config/managed.ts"),
		Note: "Managed config DIRECTORY; the file name inside it is unverified.",
	},
	{
		ID: "kilo-code/user-config", Tool: "kilo-code", Shape: "kilo-code/config", Kind: KindSettings, Scope: ScopeUser,
		Paths: samePath("~/.config/kilo/kilo.jsonc"), Format: FormatJSONC, SchemaURL: "https://app.kilo.ai/config.json",
		Precedence: 20, WriteNeeds: PrivUser, Grounding: doc("https://kilo.ai/docs/llms.txt"),
		Note: "Kilo Code 7.x shares the Kilo CLI config file; the pre-7 extension keeps its settings in VS Code state (read-only, D-110).",
	},
	{
		ID: "kilo-code/project-config", Tool: "kilo-code", Shape: "kilo-code/config", Kind: KindSettings, Scope: ScopeProjectShared,
		Paths: samePath("<project>/kilo.jsonc"), Format: FormatJSONC, SchemaURL: "https://app.kilo.ai/config.json",
		Precedence: 30, WriteNeeds: PrivUser, Grounding: doc("https://kilo.ai/docs/llms.txt"),
	},

	// --- Crush (github.com/charmbracelet/crush README + docs/config).
	// crush.json is the legacy format (the schema covers it); crushrc is a
	// shell script, raw text only.
	{
		ID: "crush/user-json", Tool: "crush", Shape: "crush/config-json", Kind: KindSettings, Scope: ScopeUser,
		Paths: samePath("~/.config/crush/crush.json"), Format: FormatJSON, SchemaURL: "https://charm.land/crush.json",
		Precedence: 20, WriteNeeds: PrivUser, Grounding: doc("https://raw.githubusercontent.com/charmbracelet/crush/main/docs/config/README.md"),
		Note: "Legacy format (\"deprecated, still supported\"); $XDG_CONFIG_HOME / CRUSH_GLOBAL_CONFIG relocate it.",
	},
	{
		ID: "crush/project-json", Tool: "crush", Shape: "crush/config-json", Kind: KindSettings, Scope: ScopeProjectShared,
		Paths: samePath("<project>/crush.json"), Format: FormatJSON, SchemaURL: "https://charm.land/crush.json",
		Precedence: 30, WriteNeeds: PrivUser, Grounding: doc("https://raw.githubusercontent.com/charmbracelet/crush/main/README.md"),
		Note: "<project>/.crush.json takes priority over crush.json.",
	},
	{
		ID: "crush/user-crushrc", Tool: "crush", Shape: "crush/crushrc", Kind: KindSettings, Scope: ScopeUser,
		Paths: samePath("~/.config/crush/crushrc"), Format: FormatCrushrc,
		Precedence: 25, WriteNeeds: PrivUser, Grounding: doc("https://raw.githubusercontent.com/charmbracelet/crush/main/docs/config/README.md"),
		Note: "A shell script of crush commands: raw editor only, no form. Overrides crush.json in the same directory.",
	},
	{
		ID: "crush/project-crushrc", Tool: "crush", Shape: "crush/crushrc", Kind: KindSettings, Scope: ScopeProjectShared,
		Paths: samePath("<project>/.crushrc"), Format: FormatCrushrc,
		Precedence: 45, WriteNeeds: PrivUser, Grounding: doc("https://raw.githubusercontent.com/charmbracelet/crush/main/docs/config/README.md"),
		Note: "Highest-priority project file (then ./crushrc). $(...) runs at load: trusted code.",
	},
	{
		ID: "crush/state", Tool: "crush", Shape: "crush/state", Kind: KindToolState, Scope: ScopeUser,
		Paths:  OSPaths{Linux: "~/.local/share/crush/crush.json", Darwin: "~/.local/share/crush/crush.json", Windows: `%LOCALAPPDATA%\crush\crush.json`},
		Format: FormatJSON, Precedence: 0, WriteNeeds: PrivNever, Grounding: doc("https://raw.githubusercontent.com/charmbracelet/crush/main/README.md"),
		Note: "Machine-owned state, not config.",
	},
}

// Files returns the whole Files table: the declared settings rows plus the
// instruction / rule / skill / agent / command rows derived from
// internal/guidance (the one owner of instruction globs). Sorted by id.
//
// The table is compiled once per process (T-399): every input is a static
// table, so rebuilding and re-sorting it on every UserFileFor /
// ProjectFileFor call (the doc generator and the org lint call those per
// catalogue row) only burned time. Files returns a copy so a caller can never
// change the compiled table; FileRow holds no slice or map, so the copy is
// complete. Package-internal readers walk filesTable() without copying.
func Files() []FileRow {
	return append([]FileRow(nil), filesTable()...)
}

var (
	filesOnce     sync.Once
	filesCompiled []FileRow
)

// filesTable is the compiled Files table, shared and read-only: callers in
// this package must not modify it.
func filesTable() []FileRow {
	filesOnce.Do(func() { filesCompiled = compileFiles() })
	return filesCompiled
}

// compileFiles assembles and sorts the Files table.
func compileFiles() []FileRow {
	out := append([]FileRow(nil), settingsFiles...)
	for _, tc := range toolCatalogs {
		out = append(out, tc.Files...)
	}
	out = append(out, guidanceFiles()...)
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// guidanceKind maps guidance's vocabulary onto FileKind. guidance
// "config" rows (settings.json, opencode.json) are NOT derived: settings
// files are declared above with their full metadata.
var guidanceKind = map[guidance.Kind]FileKind{
	guidance.KindInstructions: KindInstructions,
	guidance.KindSkill:        KindSkill,
	guidance.KindAgent:        KindAgent,
	guidance.KindCommand:      KindCommand,
	guidance.KindRule:         KindRule,
}

var guidanceFormat = map[string]Format{
	guidance.FrontmatterNone: FormatMarkdown,
	guidance.FrontmatterYAML: FormatMarkdown,
	guidance.FrontmatterMDC:  FormatMDC,
	guidance.FrontmatterJSON: FormatJSON,
}

func guidanceFiles() []FileRow {
	var out []FileRow
	seen := map[string]bool{}
	for _, r := range guidance.Rules() {
		kind, ok := guidanceKind[r.Kind]
		if !ok {
			continue
		}
		scope, path, prec := ScopeProjectShared, "<project>/"+r.Glob, 30
		switch {
		case r.Scope == guidance.ScopeUser:
			scope, path, prec = ScopeUser, r.Glob, 20
		case r.Personal:
			// A per-developer overlay the vendor keeps out of git
			// (CLAUDE.local.md): in the project tree, never shared.
			scope, prec = ScopeProjectLocal, 40
		}
		id := r.Tool + "/" + string(kind) + ":" + r.Glob
		if seen[id] {
			continue
		}
		seen[id] = true
		format := guidanceFormat[r.Frontmatter]
		if format == "" {
			format = FormatMarkdown
		}
		out = append(out, FileRow{
			ID: id, Tool: r.Tool, Shape: id, Kind: kind, Scope: scope,
			Paths: samePath(path), Format: format, Precedence: prec, WriteNeeds: PrivUser,
			Gitignored: r.Personal,
			Grounding:  Grounding{Kind: GroundAdapterDoc, Source: "internal/guidance/rules.go"},
			Note:       strings.TrimSpace(r.Note),
		})
	}
	return out
}
