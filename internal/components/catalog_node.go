package components

// catalog_node.go holds the node binary's core, feature and integration
// rows (plan section 3.3). Adapter rows live in catalog_adapters.go, org
// server rows in catalog_server.go, external artifacts in
// catalog_artifacts.go.
//
// Reading a row: Levels.Implemented is what this build's code delivers
// TODAY (P0 gates nothing, so most features are L3 config toggles and
// nothing is compiled out); Levels.Target is the plan's destination and
// Levels.Phase the phase that delivers it. Distros is the plan's distro
// column: on / off / cond / out ("-" in the plan table).

// mod is the module-root import path prefix, kept out of every Packages
// entry so rows stay readable.
const modulePath = "github.com/marmutapp/superbased-observer"

// dist builds a distro column set in plan-table order.
func dist(minimal, developer, managed, full Default) map[Distro]Default {
	return map[Distro]Default{
		DistroMinimal:   minimal,
		DistroDeveloper: developer,
		DistroManaged:   managed,
		DistroFull:      full,
	}
}

// lv builds a Levels value.
func lv(implemented []Level, target []Level, phase string) Levels {
	return Levels{Implemented: implemented, Target: target, Phase: phase}
}

// Short level sets reused across rows.
var (
	onlyL0 = []Level{L0}
	onlyL3 = []Level{L3}
	l2l3   = []Level{L2, L3}
	l1l2l3 = []Level{L1, L2, L3}
)

// loopback is the common Listener shape for a 127.0.0.1 TCP bind.
func loopback(addr, auth, key string, trig Trigger) Listener {
	return Listener{Proto: "tcp", Addr: addr, Loopback: true, Auth: auth, ConfigKey: key, Trigger: trig}
}

const (
	on   = DefaultOn
	off  = DefaultOff
	cond = DefaultCond
	out  = DefaultOut
)

func nodeRows() []Component {
	return []Component{
		{
			ID: "core", Class: ClassCore, Binary: BinaryNode,
			Title:  "Core",
			About:  "Capture framework (watcher, adapter interface, hook receiver runtime), store and migrations, scrubbing, config and its migrator, the cost engine, the session message derivation, the integration registry data, the CLI root and doctor. Never removable.",
			Levels: lv(onlyL0, onlyL0, ""),
			// The generic OTLP receiver (internal/ingest/otlp: grpc, the OTLP
			// protos) and the neutral self-observability model
			// (internal/selfobs/shape: OTel attribute values) are core (D-436).
			DirectModules: []string{
				"github.com/BurntSushi/toml", "github.com/fsnotify/fsnotify", "github.com/spf13/cobra", "github.com/spf13/pflag", "github.com/google/uuid", "github.com/tailscale/hujson", "golang.org/x/sync", "golang.org/x/sys", "golang.org/x/term", "gopkg.in/yaml.v3", "modernc.org/sqlite", "go.opentelemetry.io/otel", "go.opentelemetry.io/proto/otlp", "google.golang.org/grpc", "google.golang.org/protobuf",
				// The owner-only local IPC transport (internal/localipc: the
				// Windows named pipe) is core since lane MP-L1K (T-715): the
				// MCP relay's IPC and the terminals attach channel share it.
				"github.com/Microsoft/go-winio",
				// internal/cloudcontract (its SafeText NFC normalisation) is
				// core since the D-512 ruling (org wire vocabulary).
				"golang.org/x/text",
				// internal/keystore (the keychain-first secret store the Plans & limits
				// module and cloudcred share) is core.
				"github.com/zalando/go-keyring",
			},
			Surface: Surface{
				FilesWritten: []string{"~/.observer/observer.db", "~/.observer/config.toml", "~/.observer/plans-statusline-last.json"},
				// Plans & limits (docs/plans-and-limits.md) runs a vendor tool or
				// reads the keychain only for an account the user adds; its
				// vendor egress is described there (core declares no egress).
				Processes:   []string{"codex app-server, kiro-cli, amp, auggie (only for a Plans & limits account using that tool)"},
				Credentials: []string{"OS keychain service sbo-plans-v1 (Plans & limits keys)"},
			},
			Tables:     []string{"sessions", "actions", "token_usage", "api_turns", "projects", "parse_cursors", "schema_meta", "session_pid_bridge"},
			Verbs:      []string{"scan", "watch", "init", "uninstall", "doctor", "adapters", "status", "hook", "config", "db", "start"},
			ConfigKeys: []string{"observer"},
			Stability:  StabilityStable,
			Distros:    dist(on, on, on, on),
		},
		{
			ID: "ui.dashboard", Class: ClassFeature, Binary: BinaryNode,
			Title:         "Local dashboard",
			About:         "The node dashboard: the embedded web app and its JSON API on a loopback port. The version check is a browser fetch the user triggers.",
			Levels:        lv([]Level{L1, L3}, l1l2l3, "P2"),
			DirectModules: []string{"github.com/BurntSushi/toml"},
			Packages:      []string{"internal/intelligence/dashboard", "internal/intelligence/dashboard/webapp"},
			Surface: Surface{
				Listeners: []Listener{loopback("127.0.0.1:8081", "dashboard capability", "dashboard.addr", TriggerAutomatic)},
				Egress:    []Egress{{Host: "registry.npmjs.org", Purpose: "Version check, fetched by the browser when the user asks", Gate: "user action", Trigger: TriggerUserTriggered}},
			},
			Routes:       []string{"/api/status", "/api/health", "/api/sessions"},
			Verbs:        []string{"dashboard"},
			ConfigKeys:   []string{"dashboard"},
			Stability:    StabilityStable,
			Distros:      dist(on, on, on, on),
			RestartClass: "restart",
			Note:         "Owns no third-party module itself: websocket and excelize are hosted here for terminals and ui.export-xlsx until phase PB splits them. L1 since lane MP-L1M (T-715): the types, seam interfaces and CLI compositions the rest of the node shares with the dashboard are core internal/dashapi (re-exported here), cmd/observer reaches the server only through the ui_dashboard.go constructor seam, and a build without it starts headless with one reason.",
		},
		{
			ID: "ui.export-xlsx", Class: ClassFeature, Binary: BinaryNode,
			Title:         "Spreadsheet export",
			About:         "Exports dashboard tables as .xlsx files, which brings the excelize module and five of its dependencies.",
			Levels:        lv([]Level{L1, L3}, l1l2l3, "P2"),
			HostedIn:      []string{"internal/intelligence/dashboard"},
			DirectModules: []string{"github.com/xuri/excelize/v2"},
			Routes:        []string{"/api/export.xlsx"},
			Stability:     StabilityStable,
			Distros:       dist(out, on, off, on),
		},
		{
			ID: "capture.proxy", Class: ClassFeature, Binary: BinaryNode,
			Title:         "API proxy",
			About:         "The local reverse proxy that AI tools can route through for accurate token and cost capture. Forwards requests to the model providers.",
			Levels:        lv([]Level{L1, L3}, l1l2l3, "P5"),
			Packages:      []string{"internal/proxy"},
			DirectModules: []string{"github.com/klauspost/compress"},
			Surface: Surface{
				Listeners: []Listener{loopback("127.0.0.1:8820", "none (loopback)", "proxy.port", TriggerAutomatic)},
				Egress:    []Egress{{Host: "<model provider APIs>", Purpose: "Forwards the routed AI tool's own requests upstream", Gate: "the AI tool routes through the proxy", Trigger: TriggerAutomatic}},
			},
			Tables:       []string{"api_turns", "limit_snapshots"},
			Verbs:        []string{"proxy"},
			ConfigKeys:   []string{"proxy"},
			Stability:    StabilityStable,
			Distros:      dist(out, on, on, on),
			RestartClass: "restart",
			Note:         "L1 since lane MP-L1L (T-715): every proxy seam (routing, compression, guard scan, MCP relay tools filter, obs admitter and gateway rail, limit sink, cache feed) has a proxy-tagged half, and a build without it starts with the proxy off. The cache engine, the MCP relay and the admission service stay in core.",
		},
		{
			ID: "proxy.compression", Class: ClassFeature, Binary: BinaryNode,
			Title:    "Conversation compression",
			About:    "Rewrites upstream request bodies to compress long conversations and keeps a local stash of removed content.",
			Levels:   lv(onlyL3, l2l3, "P1"),
			Packages: []string{"internal/compression/conversation", "internal/compression/conversation/types", "internal/stash", "internal/stashalign"},
			Requires: []string{"capture.proxy"},
			Surface:  Surface{FilesWritten: []string{"~/.observer/stash/"}},
			Tables:   []string{"compression_events"},
			Routes:   []string{"/api/compression", "/api/experiments", "/api/config/profiles", "/api/config/profile-assignments"},
			// Compression profiles and their A/B experiments (T-674).
			Verbs:        []string{"profile", "experiment"},
			ConfigKeys:   []string{"compression"},
			Stability:    StabilityStable,
			Distros:      dist(out, off, off, off),
			RestartClass: "soft",
		},
		{
			ID: "proxy.routing", Class: ClassFeature, Binary: BinaryNode,
			Title:         "Model routing",
			About:         "Advises on or rewrites the model a routed request uses. `routing apply` writes per-tool vendor config.",
			Levels:        lv([]Level{L1, L3}, l1l2l3, "P5"),
			DirectModules: []string{"github.com/BurntSushi/toml"},
			// The tier table, turn-kind classifier, command classes and the
			// shared decision types are core internal/routingcore since lane
			// MP-L1I (T-715, ruling D-512): the Model Value Report, the
			// store's model-value loader and the MCP recommendation tool use
			// them. The component owns the router engine, the policy
			// compiler, the bases pipeline, Simulate and routing apply.
			Packages:   []string{"internal/routing", "internal/routingconfig", "internal/routingapply"},
			Requires:   []string{"capture.proxy"},
			Surface:    Surface{VendorConfigs: []string{"~/.claude/settings.json model (routing apply)"}},
			Tables:     []string{"router_decisions", "model_calibration"},
			Routes:     []string{"/api/routing"},
			Verbs:      []string{"routing", "model-value"},
			ConfigKeys: []string{"routing"},
			Stability:  StabilityPreview,
			Distros:    dist(out, off, off, off),
		},
		{
			ID: "analytics", Class: ClassFeature, Binary: BinaryNode,
			Title:         "Analytics",
			About:         "Cache tracking and expiry warnings, the next-message predictor, advisor, scoring, patterns, suggestions, learning, tasks, handoff and guidance. Pure analysis over captured data, plus two verbs the user runs by hand: `observer metrics` (a Prometheus /metrics listener while it runs) and `observer summarize` (calls the Anthropic Messages API with the user's own key). Nothing starts on its own: no listener and no egress unless one of those verbs is running.",
			Levels:        lv(onlyL3, onlyL3, ""),
			DirectModules: []string{"gopkg.in/yaml.v3"},
			Packages: []string{
				"internal/cachetrack", "internal/cachewarm", "internal/cachewarmsvc", "internal/predict",
				"internal/intelligence/advisor", "internal/intelligence/scoring", "internal/intelligence/patterns",
				"internal/intelligence/suggest", "internal/intelligence/learn", "internal/intelligence/discover",
				"internal/intelligence/modelvalue", "internal/intelligence/compaction", "internal/intelligence/summary",
				"internal/handoff", "internal/handoffsvc", "internal/guidance", "internal/taskflow", "internal/taskreport",
				"internal/metrics",
			},
			Surface: Surface{
				Listeners: []Listener{loopback("127.0.0.1:9090", "none (loopback); --addr can widen it", "", TriggerUserTriggered)},
				Egress:    []Egress{{Host: "api.anthropic.com", Purpose: "observer summarize: session summaries through the Anthropic Messages API with the user's own key", Gate: "user runs the verb", Trigger: TriggerUserTriggered}},
			},
			Tables:     []string{"cache_segments", "cache_entries", "cache_events", "limit_snapshots", "handoffs", "task_items", "task_transitions"},
			Routes:     []string{"/api/cache", "/api/patterns", "/api/suggestions", "/api/tasks", "/api/projects/guidance", "/api/suggest", "/api/discover"},
			Verbs:      []string{"cache-health", "cache-status", "predict", "advise", "score", "patterns", "learn", "suggest", "tasks", "handoff", "guidance", "discover", "metrics", "summarize"},
			ConfigKeys: []string{"cachetrack", "cachewarm", "predict", "advisor", "tasks", "handoff", "guidance"},
			Stability:  StabilityStable,
			Distros:    dist(on, on, on, on),
			Note:       "Stays L3: compiling pure analysis out buys nothing a reviewer can measure, and it feeds core surfaces.",
		},
		{
			ID: "compression.shell", Class: ClassFeature, Binary: BinaryNode,
			Title:         "Shell output compression",
			About:         "Filters noisy shell command output on the hook path before it reaches the model.",
			Levels:        lv(onlyL3, onlyL3, ""),
			DirectModules: []string{"github.com/BurntSushi/toml"},
			Packages:      []string{"internal/compression/shell"},
			ConfigKeys:    []string{"compression"},
			Stability:     StabilityStable,
			Distros:       dist(on, on, on, on),
		},
		{
			ID: "compression.indexing", Class: ClassFeature, Binary: BinaryNode,
			Title:      "Tool output indexing",
			About:      "Indexes tool output excerpts into SQLite FTS5 tables for search and retrieval.",
			Levels:     lv(onlyL3, onlyL3, ""),
			Packages:   []string{"internal/compression/indexing"},
			Routes:     []string{"/api/search"},
			ConfigKeys: []string{"compression"},
			Stability:  StabilityStable,
			Distros:    dist(on, on, on, on),
		},
		{
			ID: "guard", Class: ClassFeature, Binary: BinaryNode,
			Title:         "Guard",
			About:         "Policy decisions on the developer's own coding-agent tool calls: dangerous commands, MCP pins, prompt guard, budgets. Observe mode by default in minimal and developer. Making it inactive stops its background loops, its dashboard pages and `observer guard`, not its decisions: hook-time and proxy-time guard decisions keep running from [guard] (turn those off with [guard].enabled).",
			Levels:        lv(onlyL3, l2l3, "P1"),
			DirectModules: []string{"github.com/BurntSushi/toml"},
			Packages: []string{
				"internal/guard", "internal/guard/mcpsec", "internal/guard/compile", "internal/guard/export",
				"internal/guard/notify", "internal/guardfallback", "internal/guardposture", "internal/policy",
			},
			Surface:    Surface{FilesWritten: []string{"guard state files beside the DB"}},
			Tables:     []string{"guard_events", "guard_pins", "guard_approvals", "guard_policy_state", "guard_prompt_presend", "guard_prompt_reconsider"},
			Routes:     []string{"/api/guard"},
			Verbs:      []string{"guard"},
			ConfigKeys: []string{"guard"},
			Stability:  StabilityStable,
			Distros:    dist(on, on, on, on),
			Note:       "Not compile-out-able in this plan: the store, the hook runtime and managed governance depend on it, and it is the mechanism that restricts the other components.",
		},
		{
			ID: "guard.cloud", Class: ClassFeature, Binary: BinaryNode,
			Title:    "Guard webhooks and judge",
			About:    "Sends guard alerts to configured webhooks and asks a configured LLM judge.",
			Levels:   lv(onlyL3, l1l2l3, "P2"),
			HostedIn: []string{"internal/guard/notify"},
			Requires: []string{"guard"},
			Surface: Surface{Egress: []Egress{
				{Host: "<configured webhook>", Purpose: "Guard alert delivery", Gate: "config", Trigger: TriggerOptIn},
				{Host: "<configured judge endpoint>", Purpose: "LLM judge decision", Gate: "config", Trigger: TriggerOptIn},
			}},
			ConfigKeys: []string{"guard"},
			Stability:  StabilityPreview,
			Distros:    dist(out, off, off, off),
		},
		{
			ID: "integration.hooks", Class: ClassIntegration, Binary: BinaryNode,
			Title:    "Hook registration",
			About:    "Registers Observer's hook command in each selected AI tool's own hook config, and re-registers on every start.",
			Levels:   lv(onlyL3, l2l3, "P1"),
			Packages: []string{"internal/hook/pluginshim", "internal/hook/hermesplugin", "internal/hook/commandcodemod", "internal/hookdisable"},
			HostedIn: []string{"internal/hook"},
			Surface: Surface{
				VendorConfigs: []string{"~/.claude/settings.json hooks", "~/.codex/hooks.json", "~/.cursor/hooks.json", "other selected tools' hook configs"},
			},
			Routes:     []string{"/api/setup/hooks"},
			Teardown:   []TeardownStep{{Owner: "internal/clientwire Remover", What: "Removes Observer's hook entries, leaving a user-edited file in place and reported."}},
			Stability:  StabilityStable,
			Distros:    dist(on, on, on, on),
			ConfigKeys: []string{"observer"},
			Note:       "The hook RECEIVER runtime (`observer hook`) is core; only the registration writer is this component.",
		},
		{
			ID: "integration.mcp-server", Class: ClassIntegration, Binary: BinaryNode,
			Title:  "MCP server",
			About:  "The stdio MCP server AI tools spawn for on-demand project queries, and its registration in each tool's MCP config.",
			Levels: lv([]Level{L1, L3}, l1l2l3, "P2"),
			// The toml-writing registrar is core (internal/mcpregister) since
			// lane MP-L1H; the server itself imports no third-party module.
			Packages: []string{"internal/mcp"},
			Surface:  Surface{VendorConfigs: []string{"each selected AI tool's MCP server config"}},
			Teardown: []TeardownStep{{Owner: "internal/clientwire Remover", What: "Removes Observer's MCP server entries."}},
			Routes:   []string{"/api/setup/mcp", "/api/mcp/value"},
			// `observer serve` is the stdio MCP server a registered AI tool
			// spawns; `observer mcp` is the Agent Access node surface
			// (agentaccess.relay), not this component (T-661).
			// mcp-audit reads and prunes the call log the MCP server's retrieval
			// tools write (T-674).
			Verbs:      []string{"serve", "mcp-audit"},
			Tables:     []string{"mcp_audit"},
			Stability:  StabilityStable,
			Distros:    dist(out, off, off, off),
			ConfigKeys: []string{"observer"},
		},
		{
			ID: "integration.proxy-route", Class: ClassIntegration, Binary: BinaryNode,
			Title:         "Proxy routing config",
			About:         "Points AI tools at the local proxy: ANTHROPIC_BASE_URL in Claude Code settings, codex base_url and other per-tool routes.",
			Levels:        lv(onlyL3, l2l3, "P1"),
			DirectModules: []string{"github.com/BurntSushi/toml"},
			Packages:      []string{"internal/proxyroute", "internal/routeprov"},
			Requires:      []string{"capture.proxy"},
			Surface:       Surface{VendorConfigs: []string{"~/.claude/settings.json env.ANTHROPIC_BASE_URL", "~/.codex/config.toml base_url", "other proxied tools' route settings"}},
			Teardown: []TeardownStep{
				{Owner: "internal/clientwire Remover", What: "Removes the route first; the proxy listener stops only after the route is off."},
			},
			// The dashboard's Claude Code and Codex route writers (T-671):
			// an inactive component writes no vendor config (L2).
			Routes:    []string{"/api/setup/claude", "/api/setup/codex"},
			Stability: StabilityStable,
			Distros:   dist(out, on, on, on),
		},
		{
			ID: "integration.statusline", Class: ClassIntegration, Binary: BinaryNode,
			Title:     "Claude Code statusline",
			About:     "Installs Observer's statusLine command in Claude Code settings. Opt-in through `observer init --statusline`.",
			Levels:    lv(onlyL3, l2l3, "P1"),
			Packages:  []string{"internal/statusline"},
			Surface:   Surface{VendorConfigs: []string{"~/.claude/settings.json statusLine"}},
			Teardown:  []TeardownStep{{Owner: "observer statusline uninstall", What: "Removes the statusLine entry."}},
			Routes:    []string{"/api/statusline"},
			Verbs:     []string{"statusline"},
			Stability: StabilityStable,
			Distros:   dist(off, off, off, off),
			Note:      "The plan table shows this out of minimal, but the plan gives it no L1 level, so minimal compiles it in and inactive (L2) until a phase adds L1.",
		},
		{
			ID: "integration.shell-wrap", Class: ClassIntegration, Binary: BinaryNode,
			Title:         "Command wrapping",
			About:         "PATH shims so typing a tool's name runs it through Observer, placed first on PATH by marked blocks in shell rc files or the PowerShell profile.",
			Levels:        lv([]Level{L1, L3}, l1l2l3, "P2"),
			DirectModules: []string{"golang.org/x/sys"},
			Packages:      []string{"internal/shellwrap", "internal/shellwrapsvc"},
			Surface: Surface{
				FilesWritten:  []string{"~/.observer/shims/"},
				OSIntegration: []string{"bash and zsh rc blocks", "fish conf.d drop-in", "PowerShell profile block"},
			},
			Teardown:   []TeardownStep{{Owner: "observer shell-wrap disable", What: "Removes the shims and the marked blocks byte-exactly."}},
			Routes:     []string{"/api/shell-wrap"},
			Verbs:      []string{"shell-wrap"},
			ConfigKeys: []string{"shell_wrap"},
			Ledger:     []string{"SHELLWRAP-1"},
			Stability:  StabilityStable,
			Distros:    dist(out, off, out, off),
		},
		{
			ID: "integration.agent-config", Class: ClassIntegration, Binary: BinaryNode,
			Title:         "AI tool settings editor",
			About:         "The dashboard editor for AI tools' own settings files, validated against vendor schemas.",
			Levels:        lv(onlyL3, l2l3, "P1"),
			Packages:      []string{"internal/agentconfig", "internal/agentconfig/vendorschema", "internal/agentconfigsvc", "internal/agentconfigorg"},
			DirectModules: []string{"github.com/santhosh-tekuri/jsonschema/v6", "golang.org/x/text"},
			Surface:       Surface{VendorConfigs: []string{"AI tool settings files the user edits in the dashboard"}},
			Tables:        []string{"agent_config_layers", "agent_config_writes", "agent_config_org_state"},
			Routes:        []string{"/api/agent-config"},
			Verbs:         []string{"agent-config"},
			Stability:     StabilityStable,
			Distros:       dist(off, on, off, on),
			Note:          "The plan table shows this out of minimal, but the plan gives it no L1 level, so minimal compiles it in and inactive (L2) until a phase adds L1.",
		},
		{
			ID: "codeintel", Class: ClassFeature, Binary: BinaryNode,
			Title:  "Code intelligence",
			About:  "In-process symbol, import and call-graph extraction with search and analysis, using tree-sitter grammars run under the wazero WebAssembly runtime (about 5.1 MB of grammars).",
			Levels: lv([]Level{L1, L3}, l1l2l3, "P2"),
			// The facade (internal/codeintelapi) and the MCP regex fallback
			// (internal/codeintel/parse/livesym, stdlib only) are core since
			// lane MP-L1H (T-715): the MCP symbol tools answer from them with
			// no index.
			Packages: []string{
				"internal/codeintel", "internal/codeintel/resolve", "internal/codeintel/semantic", "internal/codeintel/parse",
				"internal/codeintel/parse/goast", "internal/codeintel/parse/heuristic", "internal/codeintel/parse/treesitter",
				"internal/codeintel/index", "internal/codeintel/analyze",
				"internal/codeintel/query", "internal/codeintel/surface",
			},
			DirectModules: []string{"github.com/tetratelabs/wazero", "github.com/klauspost/compress"},
			Tables:        []string{"codeintel_files", "codeintel_nodes", "codeintel_edges", "codeintel_sites", "codeintel_embeddings", "codeintel_minhash"},
			Verbs:         []string{"codeintel", "index"},
			ConfigKeys:    []string{"codeintel"},
			Stability:     StabilityPreview,
			Distros:       dist(out, off, off, off),
		},
		{
			ID: "terminals", Class: ClassFeature, Binary: BinaryNode,
			Title:  "Terminals",
			About:  "The terminal cockpit: dashboard terminals over a websocket, session attach, launching and installing AI tools, and workspaces. Runs vendor CLIs and installers.",
			Levels: lv([]Level{L1, L3}, l1l2l3, "P2"),
			// Since lane MP-L1K (T-715) the neutral vocabulary core packages
			// speak is core: internal/termcore (run kinds, standing denial
			// classes, the shell pseudo-tool, sandbox sentinels, project-root
			// validators), internal/localipc (the owner-only socket / pipe
			// transport the MCP relay shares) and internal/toolinstall (pure
			// install / runtime planning the launch preflight and doctor read).
			Packages: []string{
				"internal/termsession", "internal/termsvc", "internal/termrun", "internal/termfeed", "internal/termlease",
				"internal/termoob", "internal/termscan", "internal/termstatus", "internal/attachsock",
				"internal/workspace", "internal/guilaunch", "internal/launch",
			},
			HostedIn:      []string{"internal/intelligence/dashboard"},
			DirectModules: []string{"github.com/creack/pty", "github.com/coder/websocket", "golang.org/x/sys"},
			Surface: Surface{
				Listeners: []Listener{
					{Proto: "unix", Addr: "~/.observer/attach/<socket>", Loopback: true, Auth: "owner-only (0700 dir, 0600 socket)", Trigger: TriggerAutomatic},
					{Proto: "pipe", Addr: `\\.\pipe\observer-attach-<hash>`, Loopback: true, Auth: "owner-only ACL, deny network", Trigger: TriggerAutomatic},
				},
				Processes: []string{"vendor AI tool CLIs", "vendor installers"},
			},
			Tables:     []string{"terminal_run", "terminal_run_session", "terminal_commands", "tool_installs", "workspace_layouts", "launch_seeds"},
			Routes:     []string{"/api/terminal", "/api/launch", "/api/attach", "/ws/terminal", "/ws/launch", "/api/tools/launch"},
			Verbs:      []string{"workspaces", "ide"},
			ConfigKeys: []string{"terminal", "launch"},
			Ledger:     []string{"ATT-1"},
			Stability:  StabilityStable,
			Distros:    dist(out, on, off, on),
		},
		{
			ID: "terminals.ssh", Class: ClassFeature, Binary: BinaryNode,
			Title:      "SSH terminals",
			About:      "Remote terminals through SSH profiles and local port forwards. Runs the system ssh client.",
			Levels:     lv([]Level{L1, L3}, l1l2l3, "P2"),
			Packages:   []string{"internal/sshforward", "internal/sshprofile"},
			Requires:   []string{"terminals"},
			Surface:    Surface{Processes: []string{"ssh"}},
			Routes:     []string{"/api/terminal/ssh"},
			Verbs:      []string{"ssh"},
			ConfigKeys: []string{"terminal"},
			Stability:  StabilityStable,
			Distros:    dist(out, on, off, on),
		},
		{
			ID: "terminals.sandbox", Class: ClassFeature, Binary: BinaryNode,
			Title:      "Sandboxed terminals",
			About:      "Runs dashboard terminal launches inside a bubblewrap filesystem sandbox (Linux and WSL2 only).",
			Levels:     lv([]Level{L1, L3}, l1l2l3, "P2"),
			Packages:   []string{"internal/sandbox", "internal/sandboxnet"},
			Requires:   []string{"terminals"},
			Surface:    Surface{Processes: []string{"bwrap"}, Listeners: []Listener{{Proto: "unix", Addr: "<sandbox forwarder socket>", Loopback: true, Auth: "owner-only", Trigger: TriggerOptIn}}},
			Routes:     []string{"/api/terminal/sandbox"},
			ConfigKeys: []string{"terminal"},
			Stability:  StabilityPreview,
			Distros:    dist(out, off, off, off),
		},
		{
			ID: "remote", Class: ClassFeature, Binary: BinaryNode,
			Title:         "Remote dashboard",
			About:         "Remote access to the dashboard over a tailnet, device pairing and outbound push notifications.",
			Levels:        lv([]Level{L1, L3}, l1l2l3, "P2"),
			Packages:      []string{"internal/remoteauth", "internal/remotecfg", "internal/remotenotify", "internal/tailnet"},
			DirectModules: []string{"golang.org/x/crypto"},
			Surface: Surface{
				Listeners: []Listener{{Proto: "tcp", Addr: "<tailnet address via tailscale serve>", Loopback: false, Auth: "paired device session", Trigger: TriggerOptIn}},
				Egress:    []Egress{{Host: "<configured push endpoint>", Purpose: "Remote push notifications", Gate: "config", Trigger: TriggerOptIn}},
				Processes: []string{"tailscale"},
			},
			Tables:     []string{"remote_sessions", "remote_session_state", "remote_audit"},
			Migrations: []MigrationRef{{File: "internal/db/migrations/066_remote_sessions.sql", Class: MigrationSingletonSeed, Note: "Seeds a singleton state row even when remote is never used."}},
			Routes:     []string{"/api/remote"},
			Verbs:      []string{"remote"},
			ConfigKeys: []string{"remote"},
			Stability:  StabilityPreview,
			Distros:    dist(out, off, out, off),
		},
		{
			ID: "capture.browser", Class: ClassFeature, Binary: BinaryNode,
			Title:    "Browser chat capture",
			About:    "The native-messaging host and loopback ingest listener that receive captures from the browser extension. Writes native-messaging host manifests and, on Windows, HKCU registry keys.",
			Levels:   lv(onlyL3, l1l2l3, "P2"),
			Packages: []string{"internal/browserhost", "internal/browserhost/hostfiles", "internal/ingest/browser"},
			Bundles:  []string{"adapter.chatgpt-web", "adapter.claude-web", "adapter.perplexity-web", "adapter.gemini-web", "adapter.copilot-web"},
			Surface: Surface{
				Listeners:     []Listener{loopback("127.0.0.1:8821", "local token", "browser.listener.listen_addr", TriggerOptIn)},
				OSIntegration: []string{"native-messaging host manifests per browser", "Windows HKCU NativeMessagingHosts keys"},
			},
			Teardown:   []TeardownStep{{Owner: "browser native-messaging host remover (`observer components teardown capture.browser`)", What: "Removes the host manifests, the registry keys and the host files; the extension itself keeps running in the browser (artifact.browser-extension reports it)."}},
			Verbs:      []string{"browser"},
			ConfigKeys: []string{"browser"},
			Ledger:     []string{"MODPKG-5"},
			Stability:  StabilityPreview,
			Distros:    dist(out, on, off, on),
		},
		{
			ID: "integration.service", Class: ClassIntegration, Binary: BinaryNode,
			Title:     "System service",
			About:     "Writes a systemd unit and runs systemctl through `observer service` (Linux only). A launchd plist ships for manual install; Observer writes no service on macOS or Windows.",
			Levels:    lv(onlyL3, l2l3, "P1"),
			Packages:  []string{"internal/service"},
			Surface:   Surface{FilesWritten: []string{"systemd unit file"}, Processes: []string{"systemctl"}, OSIntegration: []string{"systemd unit (Linux)"}},
			Teardown:  []TeardownStep{{Owner: "observer service uninstall", What: "Stops and removes the unit."}},
			Verbs:     []string{"service"},
			Stability: StabilityStable,
			Distros:   dist(off, off, off, off),
			Note:      "The plan table shows this out of minimal, but the plan gives it no L1 level, so minimal compiles it in and inactive (L2) until a phase adds L1.",
		},
		{
			ID: "ingest.otlp", Class: ClassFeature, Binary: BinaryNode,
			Title:         "OTLP ingest",
			About:         "Receives OpenTelemetry traces and logs from AI tools (Claude Code OTel, vendor OTLP) on loopback gRPC and HTTP, and the opt-in edit that points a tool's own exporter at it.",
			Levels:        lv([]Level{L1, L3}, l1l2l3, "P2"),
			Packages:      []string{"internal/otlpvendor", "internal/telemetrywire"},
			DirectModules: []string{"go.opentelemetry.io/proto/otlp", "github.com/BurntSushi/toml"},
			Recommends:    []string{"adapter.claude-code"},
			Surface: Surface{
				Listeners: []Listener{
					loopback("127.0.0.1:4317", "token file", "ingest.otel.grpc_addr", TriggerOptIn),
					loopback("127.0.0.1:4318", "token file", "ingest.otel.http_addr", TriggerOptIn),
				},
				VendorConfigs: []string{"AI tool OTel exporter settings (opt-in)"},
			},
			Tables:     []string{"otel_content"},
			Routes:     []string{"/api/setup/telemetry"},
			Teardown:   []TeardownStep{{Owner: "internal/clientwire Remover", What: "Removes the telemetry exporter entries Observer wrote."}},
			ConfigKeys: []string{"ingest"},
			Stability:  StabilityPreview,
			Distros:    dist(out, off, off, off),
			Note:       "Ruling D-436: the generic OTLP receiver (internal/ingest/otlp) is core, because it also serves Plane A traces and the observer-edge; this component is the vendor mapping table (internal/otlpvendor), the logs handler and the exporter-config edit (internal/telemetrywire). Compiled out by -tags sbo_omit_ingest_otlp (P2, MP-L1C): the cmd/observer wiring pair (otlp_ingest.go, otlp_proxyroute.go, init_otlp.go, otlp_route_marker.go vs their _omit.go files), the clientwire writer (telemetry_wire.go vs telemetry_omit.go) and the dashboard setup route; [ingest.otel] is then reported at start, never obeyed, and `observer init --otlp-export` refuses. Such a build cannot remove exporter entries an earlier full build wrote: remove ingest.otlp in a build that has it first.",
		},
		{
			ID: "export.otel", Class: ClassFeature, Binary: BinaryNode,
			Title:         "OpenTelemetry export",
			About:         "Exports Observer's own traces to a configured OTLP endpoint, the guard-event feed, and the self-observability decision-run sink.",
			Levels:        lv([]Level{L1, L3}, l1l2l3, "P2"),
			Packages:      []string{"internal/exporter/otel", "internal/selfobs/emit/otelsink"},
			DirectModules: []string{"go.opentelemetry.io/otel", "go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp", "go.opentelemetry.io/otel/sdk", "go.opentelemetry.io/otel/trace"},
			Surface: Surface{Egress: []Egress{
				{Host: "<configured OTLP endpoint>", Purpose: "Trace export", Gate: "config", Trigger: TriggerOptIn},
				{Host: "<configured self-observability gateway>", Purpose: "Self-observability decision runs", Gate: "config", Trigger: TriggerOptIn},
			}},
			ConfigKeys: []string{"exporter", "selfobs"},
			Stability:  StabilityPreview,
			Distros:    dist(out, off, off, off),
			Note:       "Compiled out by -tags sbo_omit_export_otel (P2, MP-L1C): the cmd/observer wiring pair (otel.go, guardotel.go, selfobs_sink.go vs otel_omit.go, guardotel_omit.go, selfobs_sink_omit.go) drops internal/exporter/otel, the self-observability OTLP sink (internal/selfobs/emit/otelsink) and the OpenTelemetry SDK; [exporter.otel] and [selfobs] are then reported at start, never obeyed. Ruling D-436: the neutral self-observability model (selfobs run, keys, shape, conformance) and the emit.Sink contract with its Nop are core, so every producer keeps compiling and emits through Nop.",
		},
		{
			ID: "obs", Class: ClassFeature, Binary: BinaryNode,
			Title:  "Plane A observability",
			About:  "General observability of a hosted LLM app: span ingest, input admission, evals, alerts and self-observability. The admin plane, never shown on the node dashboard.",
			Levels: lv([]Level{L1, L3}, l1l2l3, "P2"),
			Packages: []string{
				"internal/obs", "internal/obs/admission", "internal/obs/egress", "internal/obs/eval", "internal/obs/span",
				"internal/obs/ingest", "internal/obs/alert", "internal/obs/store", "internal/obs/httpapi", "internal/evalcore",
				"internal/orgserver/gateway/classify", "internal/orgserver/gateway/convention",
			},
			DirectModules: []string{"go.opentelemetry.io/proto/otlp", "google.golang.org/protobuf", "golang.org/x/text"},
			// Self-observability is emitted through the core emit.Sink
			// (D-436), so obs no longer needs export.otel compiled: without
			// it, decision runs go to emit.Nop.
			RequiresCompiled: []string{"judge.client"},
			Surface: Surface{Egress: []Egress{
				{Host: "<configured judge endpoint>", Purpose: "Admission and eval judging", Gate: "config", Trigger: TriggerOptIn},
				{Host: "<configured webhook>", Purpose: "Alert delivery", Gate: "config", Trigger: TriggerOptIn},
			}},
			// Registered through ExtraRoutes by the obs wiring only.
			Routes:     []string{"/api/obs"},
			Verbs:      []string{"obs", "eval"},
			ConfigKeys: []string{"observability"},
			Stability:  StabilityPreview,
			Distros:    dist(out, out, out, off),
			Note:       "Out of developer per D-389. Compiled out by -tags sbo_omit_obs (no_obs is a one-release alias) and by sbo_omit_judge_client, which obs requires; the binary then reports obs omitted. The self-observability model (internal/selfobs run, keys, shape, conformance, emit) is core and the [selfobs] key belongs to export.otel (D-436); with those packages no longer counted as obs, a -tags sbo_omit_obs build carries none of obs's packages, so obs claims L1 (MP-L1C).",
		},
		{
			ID: "processobs", Class: ClassFeature, Binary: BinaryNode,
			Title:  "Process observation",
			About:  "Observes the processes AI tools spawn: eBPF on Linux (needs CAP_BPF), an ETW bridge on Windows behind an elevated scheduled task, and the pid bridge.",
			Levels: lv([]Level{L1, L3}, l1l2l3, "P2"),
			Packages: []string{
				"internal/processobs", "internal/processobs/bridge", "internal/processobs/poll", "internal/processobs/linuxebpf",
				"internal/processbridge/setup", "internal/processobs/etw",
			},
			DirectModules: []string{"github.com/cilium/ebpf", "golang.org/x/sys"},
			Surface: Surface{
				Listeners:     []Listener{loopback("127.0.0.1:8823", "local token", "observer.process.etw.listen_addr", TriggerOptIn)},
				OSIntegration: []string{"eBPF programs (CAP_BPF, Linux)", "elevated Scheduled Task for the ETW bridge (Windows)"},
			},
			Tables:     []string{"process_runs", "process_events", "process_network_bodies"},
			Routes:     []string{"/api/process"},
			Verbs:      []string{"process", "process-bridge"},
			ConfigKeys: []string{"observer"},
			Stability:  StabilityExperimental,
			Distros:    dist(out, off, off, off),
			Note:       "Phase PB moved its model to internal/procmodel and the boot id to internal/bootid (both core), so store, proxy, metrics, diag, arena and intervention no longer import it. internal/pidbridge and its session_pid_bridge table are core, not this component (T-671 audit): they are the proxy's session attribution written by the hook path (spec 9 and 14, migration 004), used with or without process observation. Compiled out by -tags sbo_omit_processobs (P2, MP-L1B): the cmd/observer wiring pair (processobs.go, process_cmd.go, process_bridge*.go vs processobs_omit.go) and the dashboard feature files (process_enable.go, process_etw_*.go, process_feature.go vs process_feature_omit.go); the process verbs then refuse with exit code 3 and the process setup routes answer 404 component_not_installed. The read-only process findings pages stay and show no rows.",
		},
		{
			ID: "org.client", Class: ClassFeature, Binary: BinaryNode,
			Title:  "Org client",
			About:  "Enrolment with an org server, the push loop, the signed policy rails, grants, budgets, pricing, display and intel. Talks only to the org server the operator enrolled with.",
			Levels: lv(onlyL3, l2l3, "P1"),
			Packages: []string{
				"internal/orgclient", "internal/orgclient/gen", "internal/orgenrol", "internal/orgbudget", "internal/orgintel",
				"internal/orgpricing", "internal/pushsched", "internal/policyfresh", "internal/policystate", "internal/govern",
				"internal/govern/sidecar", "internal/grantverify", "internal/policyfam", "internal/policyfam/nodegov",
				"internal/policyfam/nodefeatures", "internal/policyfam/nodefeatures/lkgsidecar", "internal/policyfam/egress",
				"internal/policyfam/admission", "internal/policyfam/planebadmission", "internal/policyfam/providers",
			},
			DirectModules: []string{"github.com/oapi-codegen/runtime", "github.com/zalando/go-keyring"},
			Surface: Surface{
				Egress:      []Egress{{Host: "<enrolled org server>", Purpose: "Push, policy and grant rails", Gate: "enrolment", Trigger: TriggerAutomatic}},
				Credentials: []string{"OS keychain (enrolment credential)"},
			},
			Tables:     []string{"org_enrolment", "org_push_log", "org_push_changes", "org_push_deletions", "org_policy_resource_state"},
			Routes:     []string{"/api/enrolment", "/api/requests", "/api/intel"},
			Verbs:      []string{"enroll", "unenroll", "org", "request", "requests", "intel"},
			ConfigKeys: []string{"org_client"},
			Stability:  StabilityStable,
			Distros:    dist(off, cond, on, on),
			Note:       "L2 only (rev S1): compiled into every distro. Catalog validation refuses any distro or manifest that omits it or targets L1 for it.",
		},
		{
			ID: "org.update", Class: ClassFeature, Binary: BinaryNode,
			Title:      "Self-update",
			About:      "Applies updates fetched only from the enrolled org server's mirror, with a fork-exec-and-watch handshake and rollback.",
			Levels:     lv([]Level{L1, L3}, l1l2l3, "P2"),
			Packages:   []string{"internal/update"},
			Requires:   []string{"org.client"},
			Surface:    Surface{Egress: []Egress{{Host: "<enrolled org server>", Purpose: "Update manifest and artifacts", Gate: "enrolment", Trigger: TriggerAutomatic}}, FilesWritten: []string{"the observer binary (atomic swap)"}},
			Tables:     []string{"update_state", "update_events"},
			Migrations: []MigrationRef{{File: "internal/db/migrations/105_update_state.sql", Class: MigrationSingletonSeed, Note: "Seeds the update_state singleton."}},
			Routes:     []string{"/api/update"},
			Verbs:      []string{"update"},
			ConfigKeys: []string{"update"},
			Ledger:     []string{"UPD-6"},
			Stability:  StabilityStable,
			Distros:    dist(out, on, on, on),
		},
		{
			ID: "agentaccess.relay", Class: ClassFeature, Binary: BinaryNode,
			Title:  "Agent Access relay",
			About:  "The MCP relay with a local policy decision point and record chain. Rewrites MCP client config to route through the relay.",
			Levels: lv([]Level{L1, L3}, l1l2l3, "P2"),
			// Core since lane MP-L1I (T-715), each a pure vocabulary shared
			// with the org server or the org client: internal/dpop/jose (the
			// JOSE primitives the org client signs its source-node key proof
			// with), internal/mcpintel/correlate (the MCP-call correlation the
			// node session panel and the org drawer must agree on) and
			// internal/mcpintel/discover (the MCP inventory identity hashes the
			// push wire and ingest must agree on). The component owns the
			// relay, its PDP, record chain, projector and egress.
			Packages: []string{
				"internal/mcprelay", "internal/mcprelay/localpdp", "internal/mcprelay/coverage", "internal/mcprelay/project",
				"internal/mcprelay/record", "internal/mcpaccess", "internal/policyfam/mcpaccess", "internal/agentid", "internal/dpop",
				"internal/mcpegress", "internal/netpolicy",
			},
			DirectModules: []string{"cel.dev/cel-go"},
			Requires:      []string{"org.client"},
			Surface: Surface{
				VendorConfigs: []string{"MCP client config entries (wrapped through the relay)"},
				Credentials:   []string{"OS keychain"},
			},
			Tables:   []string{"mcp_relay_state", "mcp_relay_record", "mcp_relay_launch_spec"},
			Teardown: []TeardownStep{{Owner: "observer mcp unwrap", What: "Restores the original MCP client entries."}},
			Routes:   []string{"/api/mcp-access"},
			// `observer mcp` is the canonical Agent Access node tree
			// (mcp_cmd.go); `observer mcp-relay` is its older spelling (T-661).
			Verbs:      []string{"mcp", "mcp-relay"},
			ConfigKeys: []string{"mcp_relay"},
			Stability:  StabilityPreview,
			Distros:    dist(out, off, off, off),
		},
		{
			ID: "cloud.intelligence", Class: ClassFeature, Binary: BinaryNode,
			Title:  "Cloud Intelligence",
			About:  "The signed-in personal cloud plane: consent-gated evidence upload and enrichment, run as a manual or opt-in subprocess.",
			Levels: lv([]Level{L1, L3}, l1l2l3, "P2"),
			// internal/cloudcontract (the versioned wire and result
			// vocabulary) is core since lane MP-L1I (T-715, ruling D-512):
			// the org wire (orgcontract, orgintel, the store's cloud and org
			// intel tables, the dashboard's views) speaks it. The component
			// owns the evidence builder, the network lane, the credential
			// store and the consent gateway.
			Packages:      []string{"internal/cloudevidence", "internal/cloudclient", "internal/cloudpop", "internal/cloudcred", "internal/cloudgateway"},
			DirectModules: []string{"github.com/zalando/go-keyring"},
			Surface: Surface{
				Egress:      []Egress{{Host: "<hosted cloud base URL>", Purpose: "Evidence upload and results", Gate: "consent", Trigger: TriggerOptIn}},
				Credentials: []string{"OS keychain (cloud credential)"},
			},
			Tables:     []string{"cloud_outbox", "cloud_results", "cloud_consent_receipts"},
			Routes:     []string{"/api/cloud"},
			Verbs:      []string{"cloud"},
			ConfigKeys: []string{"cloud"},
			Stability:  StabilityPreview,
			Distros:    dist(out, off, out, off),
		},
		{
			ID: "egress.pricing-feed", Class: ClassFeature, Binary: BinaryNode,
			Title:    "Pricing feed",
			About:    "Fetches the model price table from superbased.app every 24 hours so cost and budgets stay accurate. On by default (D-374).",
			Levels:   lv([]Level{L1, L3}, l1l2l3, "P2"),
			Packages: []string{"internal/pricingfeed", "internal/pricingfeed/client", "internal/pricingfeedgate"},
			Surface: Surface{Egress: []Egress{
				{Host: "superbased.app", Purpose: "Model price table refresh every 24 hours", Gate: "default on, opt out in [pricing.feed]", Trigger: TriggerAutomatic},
			}},
			Tables:     []string{"pricing_feed_cache"},
			Verbs:      []string{"pricing"},
			ConfigKeys: []string{"pricing"},
			Stability:  StabilityStable,
			Distros:    dist(out, on, out, on),
			Note:       "Stays on by default in developer (operator ruling D-374: a stale feed breaks budgets). Managed nodes use the org pricing rail.",
		},
		{
			ID: "egress.aggregate-share", Class: ClassFeature, Binary: BinaryNode,
			Title:      "Aggregate share",
			About:      "Submits consented, aggregate-only usage statistics to aggregate.superbased.app.",
			Levels:     lv([]Level{L1, L3}, l1l2l3, "P2"),
			Packages:   []string{"internal/aggregate", "internal/aggregateclient", "internal/aggregatesource", "internal/aggregatesvc"},
			Surface:    Surface{Egress: []Egress{{Host: "aggregate.superbased.app", Purpose: "Aggregate usage submission", Gate: "consent", Trigger: TriggerOptIn}}},
			Tables:     []string{"aggregate_consent", "aggregate_submissions"},
			Verbs:      []string{"aggregate"},
			ConfigKeys: []string{"aggregate_share"},
			Stability:  StabilityPreview,
			Distros:    dist(out, off, out, off),
		},
		{
			ID: "notify.email", Class: ClassFeature, Binary: BinaryNode,
			Title:      "Email and digest",
			About:      "Sends email notifications and the periodic digest over a configured SMTP server.",
			Levels:     lv([]Level{L1, L3}, l1l2l3, "P2"),
			Packages:   []string{"internal/notify/email", "internal/notify/digest"},
			Surface:    Surface{Egress: []Egress{{Host: "<configured SMTP server>", Purpose: "Email delivery", Gate: "config", Trigger: TriggerOptIn}}},
			Tables:     []string{"digest_state"},
			Verbs:      []string{"digest"},
			ConfigKeys: []string{"email", "digest"},
			Stability:  StabilityStable,
			Distros:    dist(out, off, off, off),
		},
		{
			ID: "projects", Class: ClassFeature, Binary: BinaryNode,
			Title:  "Projects and lines of code",
			About:  "Per-project spend and ROI, a read-only HEAD-only commit scanner, lines-of-code tracking with a loopback editor endpoint, and skills history.",
			Levels: lv(onlyL3, l2l3, "P1"),
			Packages: []string{
				"internal/commitscan", "internal/commitlog", "internal/projectroi", "internal/loc",
				"internal/skillhistory", "internal/skillscan",
			},
			Surface: Surface{
				Processes:    []string{"git (read-only log)"},
				FilesWritten: []string{"~/.observer/loc-editor-token"},
			},
			Tables:     []string{"file_changes", "project_commits", "project_commit_files", "project_commit_scan"},
			Routes:     []string{"/api/projects", "/api/loc", "/api/project"},
			Verbs:      []string{"project", "loc"},
			ConfigKeys: []string{"projects", "loc"},
			Ledger:     []string{"COMMIT-1", "COMMIT-2"},
			Stability:  StabilityStable,
			Distros:    dist(on, on, on, on),
		},
		{
			ID: "judge.client", Class: ClassFeature, Binary: BinaryNode,
			Title:     "Judge client",
			About:     "The one LLM-as-judge client: an OpenAI-compatible chat completions caller with secret redaction and a payload cap for any non-loopback judge. Used by the commit alignment judge and, in builds with obs, the eval and admission judges.",
			Levels:    lv([]Level{L1, L3}, l1l2l3, "P2"),
			Packages:  []string{"internal/judgeclient"},
			Surface:   Surface{Egress: []Egress{{Host: "<configured judge endpoint>", Purpose: "LLM judge call for a configured judge", Gate: "config", Trigger: TriggerOptIn}}},
			Stability: StabilityPreview,
			Distros:   dist(out, off, out, off),
			Note:      "Split out of obs in phase PB (D-408) so the alignment judge stays in developer while obs leaves it (D-389). Required only as code: it starts nothing and is active only through a component that calls it. Compiled out by -tags sbo_omit_judge_client (P2, MP-L1A), which also compiles out obs.",
		},
		{
			ID: "projects.alignment-judge", Class: ClassFeature, Binary: BinaryNode,
			Title:            "Commit alignment judge",
			About:            "The opt-in tier-J grade of delivered versus missed work, using the judge client.",
			Levels:           lv(onlyL3, l2l3, "P1"),
			HostedIn:         []string{"internal/intelligence/alignment"},
			Requires:         []string{"projects"},
			RequiresCompiled: []string{"judge.client"},
			Surface:          Surface{Egress: []Egress{{Host: "<configured judge endpoint>", Purpose: "Commit alignment grading", Gate: "config", Trigger: TriggerOptIn}}},
			ConfigKeys:       []string{"projects"},
			Stability:        StabilityPreview,
			Distros:          dist(out, off, out, off),
			Note:             "Stays in developer (D-408): the judge client is its own component since phase PB, so this tier no longer follows obs out of developer (D-389).",
		},
		{
			ID: "archive", Class: ClassFeature, Binary: BinaryNode,
			Title:         "Corpus archive",
			About:         "Moves cold rows into a separate archive.db with copy, verify, then delete, and an operator-invoked reclaim.",
			Levels:        lv([]Level{L1, L3}, l1l2l3, "P5"),
			DirectModules: []string{"modernc.org/sqlite"},
			// internal/archive (the pure row, marker, digest and leftover
			// vocabulary) is core since lane MP-L1H (T-715): the store's
			// hot-side seam, codeintel's archived-project read and the
			// dashboard's storage leftovers speak it. The component owns the
			// cold store and the mover.
			Packages:   []string{"internal/archivestore", "internal/archivestore/migrations", "internal/archivesvc"},
			Surface:    Surface{FilesWritten: []string{"~/.observer/archive.db"}},
			Tables:     []string{"codeintel_archived_projects", "process_archived_windows"},
			Verbs:      []string{"archive"},
			ConfigKeys: []string{"archive"},
			Stability:  StabilityPreview,
			Distros:    dist(out, off, off, off),
		},
		{
			ID: "benchmark", Class: ClassFeature, Binary: BinaryNode,
			Title:         "Benchmarks and arena",
			About:         "Runs AI agents against benchmark tasks and side-by-side arena comparisons in git worktrees.",
			Levels:        lv([]Level{L1, L3}, l1l2l3, "P2"),
			DirectModules: []string{"github.com/BurntSushi/toml"},
			Packages:      []string{"internal/benchmark", "internal/arena", "internal/arenamodels"},
			Requires:      []string{"terminals"},
			Surface:       Surface{Processes: []string{"AI agent CLIs", "git (worktrees)"}},
			Tables:        []string{"benchmark_runs", "benchmark_attempts", "benchmark_scores", "arena_runs", "arena_candidates"},
			Routes:        []string{"/api/benchmarks", "/api/arena"},
			Verbs:         []string{"benchmark"},
			ConfigKeys:    []string{"benchmark"},
			Stability:     StabilityPreview,
			Distros:       dist(out, on, out, on),
		},
		{
			ID: "intervention", Class: ClassFeature, Binary: BinaryNode,
			Title:         "Intervention",
			About:         "Signals or stops vendor AI tool processes when an authority allows it.",
			Levels:        lv(onlyL3, l2l3, "P1"),
			Packages:      []string{"internal/intervention", "internal/intervention/inspectionbroker", "internal/jobobject", "internal/codexipc"},
			DirectModules: []string{"golang.org/x/sys"},
			Surface:       Surface{OSIntegration: []string{"process signals", "Windows Job Objects"}},
			Stability:     StabilityPreview,
			Distros:       dist(off, off, off, off),
			Note:          "The plan table shows this out of minimal, but the plan gives it no L1 level, so minimal compiles it in and inactive (L2) until a phase adds L1.",
		},
		{
			ID: "demo", Class: ClassFeature, Binary: BinaryNode,
			Title:    "Demo data",
			About:    "Embedded demo fixtures the dashboard can load to show the product without real sessions.",
			Levels:   lv([]Level{L1, L3}, l1l2l3, "P2"),
			Packages: []string{"internal/demo"},
			// The demo fixtures are parsed with the Claude Code parser.
			RequiresCompiled: []string{"adapter.claude-code"},
			Routes:           []string{"/api/demo"},
			Stability:        StabilityStable,
			Distros:          dist(out, on, out, on),
		},
	}
}
