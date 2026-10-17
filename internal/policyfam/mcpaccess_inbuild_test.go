//go:build !sbo_omit_org_agentaccess && !sbo_omit_agentaccess_relay

package policyfam

// mcpAccessInBuild mirrors family_mcpaccess.go's build line: the
// tools.mcp_access compiler is linked (mcpaccess_inbuild_omit_test.go holds
// the other side, D-535).
const mcpAccessInBuild = true
