//go:build !sbo_omit_proxy_routing

package mcp

// wantRoutingTemplates is the number of built-in routing policy templates
// get_routing_status reports: the six of §R6.4 when proxy.routing is
// compiled in (routing_inbuild_omit_test.go holds the other side, D-535).
const wantRoutingTemplates = 6
