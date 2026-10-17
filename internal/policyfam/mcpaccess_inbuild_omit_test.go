//go:build sbo_omit_org_agentaccess || sbo_omit_agentaccess_relay

package policyfam

// mcpAccessInBuild is false: an Agent Access omit tag leaves
// family_mcpaccess.go out, so tools.mcp_access answers ErrFamilyNotLinked
// (D-535).
const mcpAccessInBuild = false
