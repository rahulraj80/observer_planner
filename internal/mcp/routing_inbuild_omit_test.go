//go:build sbo_omit_proxy_routing

package mcp

// wantRoutingTemplates is 0 in a build without proxy.routing: the policy
// compiler is not linked, so get_routing_status reports no templates
// (tools_routing_templates_omit.go, D-535).
const wantRoutingTemplates = 0
