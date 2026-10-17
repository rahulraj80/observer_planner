// Package clientwire is the ONE owner of which per-tool AI-client
// integration writes Observer offers (hooks, the MCP server entry, the
// persisted proxy route) and of the removal `observer uninstall` performs.
//
// Two halves:
//
//   - The capability predicates (HookSupported, HookRemovable, MCPSupported,
//     RouteSupported, RouteRemovable, HookRegistrationWanted). They read only
//     the tool's integration registry row (internal/integration) and the hook
//     registry's writer table (hook.RegistryWrites), never a tool name
//     (CLAUDE.md module rules 3 and 5). `observer init`, `observer
//     uninstall`, `observer start`'s auto-registration and the dashboard's
//     Connected tools panel all ask these, so a tool the CLI can wire is
//     exactly a tool the dashboard can wire.
//   - The exporter wiring (T-537, opt-in): TelemetrySupported and
//     WireTelemetry point a tool's own OpenTelemetry exporter at the node's
//     OTLP receiver through internal/telemetrywire's pure edit, under the
//     same settings-file lock and credential-file rules; the Remover removes
//     it as PartTelemetry.
//   - The Remover: the removal `observer uninstall` runs, as one seam both
//     the CLI and the dashboard call. It drives the same hook.Registry,
//     mcp.Registrar and proxyroute.Registrar undo paths; only Observer's own
//     entries are removed, user-authored entries are kept, and a shared
//     credential file is edited inside Observer's own subtree only (the
//     registries' credential-file guards, D-268 / D-270).
//
// The package does no network I/O and owns no table; its file writes are
// the registries' own.
package clientwire
