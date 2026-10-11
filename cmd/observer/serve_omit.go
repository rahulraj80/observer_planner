//go:build sbo_omit_integration_mcp_server

package main

import (
	"github.com/spf13/cobra"

	"github.com/marmutapp/superbased-observer/internal/components"
)

// serve_omit.go stands in for serve.go and contract.go in a build without the
// integration.mcp-server component (-tags sbo_omit_integration_mcp_server,
// lane MP-L1H, T-715): internal/mcp (the stdio MCP server and its tools) is
// not linked. `observer serve` and `observer contract` answer "not in this
// build" (exit 3). The client-config registrar is core
// (internal/mcpregister), so `observer uninstall` and the components teardown
// still remove an MCP entry a previous build wrote.
func init() { components.MarkOmitted("integration.mcp-server") }

// newServeCmd registers `observer serve` as a compiled-out verb.
func newServeCmd() *cobra.Command {
	return compiledOutVerb("serve", "MCP server (not in this build)", "integration.mcp-server")
}

// newContractCmd registers `observer contract` as a compiled-out verb.
func newContractCmd() *cobra.Command {
	return compiledOutVerb("contract", "MCP tool contract (not in this build)", "integration.mcp-server")
}
