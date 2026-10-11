// Package mcpregister writes and removes Observer's MCP server entry (and the
// Agent Access relay's stdio-wrap entries) in each AI tool's own MCP config
// file. It is split out of internal/mcp (lane MP-L1H, T-715) so the
// registrar and its remover stay in every build: a binary without the
// integration.mcp-server component (-tags sbo_omit_integration_mcp_server)
// links no MCP server, but can still remove an entry a previous build wrote.
package mcpregister
