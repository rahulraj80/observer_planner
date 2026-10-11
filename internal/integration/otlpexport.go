package integration

// OTLPOrigin says where a tool's own OpenTelemetry export is sent FROM,
// which decides where it can land: a local process can reach the node's
// loopback receiver, a vendor's cloud cannot.
type OTLPOrigin string

// Export origins. The zero value is the honest floor: no grounded export.
const (
	// OTLPNone: no OpenTelemetry export grounded for the tool (the zero
	// value; never a guess).
	OTLPNone OTLPOrigin = ""
	// OTLPLocal: the tool's own process on the developer machine exports.
	OTLPLocal OTLPOrigin = "local"
	// OTLPVendorCloud: the vendor exports server-side from its cloud (or
	// from a vendor-managed VM), configured once for a team; it cannot
	// reach a node's loopback receiver and belongs on the org server.
	OTLPVendorCloud OTLPOrigin = "vendor_cloud"
)

// OTLPExport is the registry column for a tool's native OpenTelemetry export
// (T-410 design, docs/plans/otlp-any-adapter-design-2026-10-06.md). It is
// the CAPABILITY declaration; the attribute mapping itself lives in one
// place, internal/otlpvendor, and a test pins that every Mapped row has a
// mapping row and every mapping row is declared here.
type OTLPExport struct {
	Origin OTLPOrigin
	// Signals names the OTLP signals the tool exports ("logs", "traces",
	// "logs, metrics").
	Signals string
	// Enable is how an operator turns the export on and points it at a
	// collector: the exact env vars or config keys and file.
	Enable string
	// Protocols lists the OTLP transports the exporter speaks.
	Protocols string
	// Mapped reports that internal/otlpvendor maps this export and a node
	// lands it (a vendor-cloud export can be mapped yet still refused on a
	// node; see internal/otlpvendor Grain).
	Mapped bool
	// Grounding names the evidence: "live" (captured from an installed
	// tool), "doc" or "source" (vendor documentation or source only).
	Grounding string
	// Note records a caveat: why an exporting tool is not mapped, a gap in
	// the export (no tokens, no session id), sampling.
	Note string
	// Wire is how Observer can point the export at this node's receiver
	// (T-537, opt-in per tool). The zero value means no documented config
	// file Observer writes: the export is turned on by environment
	// variables only (a launcher or terminal profile), or not at all.
	Wire OTLPWire
}

// OTLPWireKind names the SHAPE of the exporter-config edit Observer writes
// (internal/telemetrywire owns each shape's edit; internal/clientwire is the
// one writer).
type OTLPWireKind string

// Exporter-config edit shapes.
const (
	// OTLPWireNone: no file writer (the zero value).
	OTLPWireNone OTLPWireKind = ""
	// OTLPWireEnvSettingsJSON: OTEL keys in a settings.json "env" block
	// (Claude Code).
	OTLPWireEnvSettingsJSON OTLPWireKind = "env_settings_json" //nolint:gosec // G101: an edit-shape name, not a credential
	// OTLPWireTelemetryJSON: a "telemetry" object in settings.json (Gemini
	// CLI and its fork Qwen Code).
	OTLPWireTelemetryJSON OTLPWireKind = "telemetry_settings_json" //nolint:gosec // G101: an edit-shape name, not a credential
	// OTLPWireCodexTOML: a marked [otel] table in Codex config.toml.
	OTLPWireCodexTOML OTLPWireKind = "codex_otel_toml" //nolint:gosec // G101: an edit-shape name, not a credential
)

// OTLPWire describes one tool's exporter-config writer.
type OTLPWire struct {
	Kind OTLPWireKind
	// File is the config file, relative to the user's home.
	File string
	// DirEnv, when set, names an environment variable whose value replaces
	// the file's directory (Codex's CODEX_HOME).
	DirEnv string
	// SharesCredentials marks a file that can also hold the vendor's
	// credentials (D-268 / D-270): only Observer's own subtree is edited,
	// the file mode is kept, and no value from it is ever logged or
	// returned.
	SharesCredentials bool
}

// otlpExports is the ONE owner of the OTLP column, keyed by registry tool.
// A tool absent here exports nothing Observer could find (vendor research
// 2026-10-06, the design doc's per-adapter table).
var otlpExports = map[string]OTLPExport{
	"claude-code": {
		Origin: OTLPLocal, Signals: "logs, metrics, traces (beta)", Mapped: true, Grounding: "doc",
		Enable:    "CLAUDE_CODE_ENABLE_TELEMETRY=1, OTEL_LOGS_EXPORTER=otlp, OTEL_EXPORTER_OTLP_PROTOCOL, OTEL_EXPORTER_OTLP_ENDPOINT (env or settings.json env block)",
		Protocols: "grpc, http/protobuf, http/json",
		// settings.json env can carry ANTHROPIC_API_KEY.
		Wire: OTLPWire{Kind: OTLPWireEnvSettingsJSON, File: ".claude/settings.json", SharesCredentials: true},
	},
	"codex": {
		Origin: OTLPLocal, Signals: "logs, traces, metrics", Mapped: true, Grounding: "live (codex-cli 0.154.0)",
		Enable:    "[otel] in $CODEX_HOME/config.toml: exporter = { otlp-http = { endpoint = \"http://127.0.0.1:4318/v1/logs\", protocol = \"binary\" } }",
		Protocols: "http/protobuf, http/json, grpc",
		Note:      "metrics_exporter defaults to Statsig (OpenAI); only logs carry per-call tokens",
		// config.toml can carry provider headers (D-270 audit list).
		Wire: OTLPWire{Kind: OTLPWireCodexTOML, File: ".codex/config.toml", DirEnv: "CODEX_HOME", SharesCredentials: true},
	},
	"gemini-cli": {
		Origin: OTLPLocal, Signals: "logs, metrics, traces", Mapped: true, Grounding: "live (gemini-cli 0.53.1)",
		Enable:    "telemetry block in ~/.gemini/settings.json (enabled, target local, otlpEndpoint, otlpProtocol) or GEMINI_TELEMETRY_* env",
		Protocols: "grpc (default), http (= OTLP/HTTP JSON)",
		Note:      "the gemini transcript adapter keys sessions by chat file name, not the export's session.id uuid, so the export lands tokens only while the gemini-cli adapter is disabled",
		Wire:      OTLPWire{Kind: OTLPWireTelemetryJSON, File: ".gemini/settings.json"},
	},
	"qwen-code": {
		Origin: OTLPLocal, Signals: "logs, metrics, traces", Mapped: true, Grounding: "live (qwen-code 0.21.0)",
		Enable:    "telemetry block in ~/.qwen/settings.json or QWEN_TELEMETRY_* env",
		Protocols: "grpc (default), http (= OTLP/HTTP JSON)",
		// ~/.qwen/settings.json embeds provider API keys (D-270).
		Wire: OTLPWire{Kind: OTLPWireTelemetryJSON, File: ".qwen/settings.json", SharesCredentials: true},
	},
	"copilot-cli": {
		Origin: OTLPLocal, Signals: "traces, metrics", Mapped: true, Grounding: "live (@github/copilot 1.0.77, BYOK offline)",
		Enable:    "COPILOT_OTEL_ENABLED=true and OTEL_EXPORTER_OTLP_ENDPOINT, or the managed-settings telemetry property",
		Protocols: "http/json (default), http/protobuf",
	},
	"copilot": {
		Origin: OTLPLocal, Signals: "traces, metrics, logs", Mapped: true, Grounding: "doc",
		Enable:    "VS Code settings github.copilot.chat.otel.enabled / .otlpEndpoint / .exporterType, or COPILOT_OTEL_ENABLED + OTEL_EXPORTER_OTLP_ENDPOINT",
		Protocols: "http/protobuf (default), grpc",
	},
	"goose": {
		Origin: OTLPLocal, Signals: "traces, metrics, logs", Mapped: true, Grounding: "source",
		Enable:    "OTEL_EXPORTER_OTLP_ENDPOINT (or otel_exporter_otlp_endpoint in ~/.config/goose/config.yaml)",
		Protocols: "http/protobuf",
	},
	"opencode": {
		Origin: OTLPLocal, Signals: "traces, logs", Mapped: true, Grounding: "source",
		Enable:    "OTEL_EXPORTER_OTLP_ENDPOINT plus \"experimental\": {\"openTelemetry\": true} in opencode.json",
		Protocols: "http/json",
		Note:      "opencode 1.15.6 exported logs only in a live run; the ai.* LLM spans the row maps are from current source",
	},
	"grok": {
		Origin: OTLPLocal, Signals: "logs, metrics", Mapped: true, Grounding: "doc",
		Enable:    "GROK_EXTERNAL_OTEL=1 with OTEL_LOGS_EXPORTER=otlp, or [telemetry] otel_* keys in ~/.grok/config.toml",
		Protocols: "http/protobuf (default), grpc",
	},
	"zcode": {
		Origin: OTLPLocal, Signals: "traces, metrics", Mapped: true, Grounding: "source",
		Enable:    "OTEL_EXPORTER_OTLP_ENDPOINT",
		Protocols: "http/protobuf",
		Note:      "the vendor samples traces at a hard-coded 10%: the export alone undercounts; the zcode adapter stays the token owner",
	},
	"cursor": {
		Origin: OTLPVendorCloud, Signals: "logs, metrics", Mapped: true, Grounding: "doc",
		Enable:    "Enterprise Team Settings > OpenTelemetry Export (server-side, one destination per team)",
		Protocols: "http/protobuf",
		Note:      "sent from Cursor's cloud to a public HTTPS endpoint: team grain, refused on a node; the destination is the org server (T-411)",
	},
	"grokbot": {
		Origin: OTLPVendorCloud, Signals: "logs", Grounding: "doc",
		Enable: "Cursor Enterprise OpenTelemetry Export (grok_bot_agent_actions family, needs Action Recording)",
		Note:   "rides Cursor's team export (cursor.surface=grok_bot); org-side only",
	},
	"cowork": {
		Origin: OTLPVendorCloud, Signals: "logs", Mapped: true, Grounding: "doc",
		Enable:    "Claude admin settings > Cowork (central, not on the device)",
		Protocols: "http/json, http/protobuf",
		Note:      "exported from inside the Cowork VM to an admin-set endpoint: refused on a node; org-side destination",
	},
	"mistral-code": {
		Origin: OTLPLocal, Signals: "traces", Grounding: "source",
		Enable:    "enable_otel = true and otel_endpoint in ~/.vibe/config.toml",
		Protocols: "http/protobuf",
		Note:      "not mapped: the session id is only on the invoke_agent root span, which ends after its chat spans; needs a trace join",
	},
	"droid": {
		Origin: OTLPLocal, Signals: "metrics, traces", Grounding: "live (droid 0.233.0, metrics only)",
		Enable:    "OTEL_TELEMETRY_ENDPOINT (falls back to OTEL_EXPORTER_OTLP_ENDPOINT), OTEL_TELEMETRY_FORMAT=genai",
		Protocols: "http/json",
		Note:      "not mapped: no token or cost data in the customer export, and service.name is the generic \"cli\"; live on node 1 (2026-10-07, T-541) with only OTEL_EXPORTER_OTLP_ENDPOINT set, a run that stopped at the vendor's 402 still sent one OTLP/HTTP JSON metrics body (droid.session.count, carrying session.id, model.id and the account's user.id, user.email and organization.id); no traces",
	},
	"cline": {
		Origin: OTLPLocal, Signals: "logs, metrics", Grounding: "source",
		Enable: "CLINE_OTEL_TELEMETRY_ENABLED=true, CLINE_OTEL_LOGS_EXPORTER=otlp, CLINE_OTEL_EXPORTER_OTLP_ENDPOINT",
		Note:   "not mapped: unverified that task.* events reach a user endpoint on current SDK builds; needs a live capture",
	},
	"kilo-code-cli": {
		Origin: OTLPLocal, Signals: "traces, logs", Grounding: "source",
		Enable: "OTEL_EXPORTER_OTLP_ENDPOINT",
		Note:   "not mapped: AI SDK spans are hard-disabled, so the export carries no tokens or tool calls",
	},
	"openclaw": {
		Origin: OTLPLocal, Signals: "traces, metrics, logs", Grounding: "doc",
		Enable: "diagnostics-otel plugin + diagnostics.otel.* in ~/.openclaw/openclaw.json",
		Note:   "not mapped: session, run and tool-call ids are deliberately dropped from the export",
	},
	"devin": {
		Origin: OTLPLocal, Grounding: "doc",
		Enable: "otel block in ~/.config/devin/config.json or OTEL_EXPORTER_OTLP_* (Devin CLI 3000.11.1+)",
		Note:   "not mapped: event and attribute names are unpublished; needs a live capture with an account",
	},
	"open-interpreter": {
		Origin: OTLPLocal, Signals: "logs, traces, metrics", Grounding: "source",
		Enable: "[otel] in ~/.openinterpreter/config.toml (the Codex crate)",
		Note:   "not mapped separately: same events as codex and service.name codex_cli_rs, so its export lands as codex",
	},
	"hermes": {
		Origin: OTLPLocal, Signals: "traces", Grounding: "source",
		Note: "core exports gateway-health spans only; per-session telemetry needs the third-party hermes-otel plugin (not mapped)",
	},
	"kiro-cli": {
		Origin: OTLPVendorCloud, Signals: "metrics", Grounding: "doc",
		Note: "daily per-user aggregates from Kiro's cloud (kiro.daily.*); no sessions or tokens",
	},
	"deepseek": {
		Origin: OTLPLocal, Signals: "logs", Grounding: "source",
		Note: "feedback-gated: records leave only after the user submits /feedback; not usable for continuous capture",
	},
}

// init folds the OTLP column into the registry rows. A key with no registry
// row is a table error caught by TestOTLPExportsAreRegistryRows.
func init() {
	for tool, x := range otlpExports {
		if c, ok := registry[tool]; ok {
			c.OTLP = x
			registry[tool] = c
		}
	}
}

// OTLPExportTools returns every tool with a declared OTLP export, unsorted.
func OTLPExportTools() []string {
	out := make([]string, 0, len(otlpExports))
	for t := range otlpExports {
		out = append(out, t)
	}
	return out
}
