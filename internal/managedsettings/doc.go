// Package managedsettings generates the Anthropic managed-settings artifacts an
// admin deploys to point a fleet of Claude Code nodes at this Observer install
// (native-console integration, Workstream B / Phase 4).
//
// It produces two distinct artifacts that do different jobs and must both ship
// (template §4.2):
//
//   - managed-mcp.json — pins Observer's MCP server fleet-wide (in-session tool
//     presence). It does NOT deliver telemetry.
//   - managed-settings env block — points Claude Code's native OTel exporter at
//     Observer's OTLP receiver. THIS is what delivers telemetry.
//
// The package is pure: given options it returns marshaled JSON and a human
// README explaining the MCP-vs-telemetry split. The CLI command
// (observer org emit-managed-settings) injects the endpoint from config and
// handles file/stdout I/O.
//
// It also renders lever A of the org AI tool policy (D-100; T-119, T-433,
// T-434): FromPolicy takes the composed policy body a managed node receives
// and places each enforced value in the vendor's administrator-managed
// layer (layers_table.go, copilot.go), each policy default in the vendor's
// lowest administrator layer, with the per-OS hold strength stated honestly,
// and renders every layer as a root-owned file, an Apple configuration
// profile and a Windows policy registry import. The catalogue
// (internal/agentconfig) owns each file's path and key shape; nothing here
// branches on a tool name. The org server serves it at
// GET /api/org/agent-config/mdm.
package managedsettings
