package vendorapi

import "strings"

// Gateway attribution (lane ENTAPI-L16C, T-710). A gateway's activity
// (OpenRouter's activity report, Kilo Gateway's usage analytics) is traffic
// any client may have sent through that gateway: a coding agent, a script, an
// unrelated app. It is never attributed to one registry tool. Its facts land
// UNATTRIBUTED under the reserved tool value "gateway:<vendor>", which is not
// a registry tool id (the registry grammar has no colon), so:
//
//   - no tool figure arbitrates onto it (display arbitration and provenance
//     look up registry tools only);
//   - no identity can be held or linked under it (the identity routes accept
//     registry tool ids only; gateway facts are org-level, never per member);
//   - no spend, budget, alert or enforcement path reads it (vendor rows never
//     reach the live spend product, invariant I4; a comparison asked on such a
//     connection finds no node spend under that value and lists it as
//     unmatched, display only).
//
// A gateway fact stays "Segment" coverage until a RouteBinding is live proven
// (plan OQ-15).

// GatewayToolPrefix starts every reserved gateway attribution value.
const GatewayToolPrefix = "gateway:"

// GatewayTool returns the reserved attribution value of a gateway vendor.
func GatewayTool(vendor string) string { return GatewayToolPrefix + vendor }

// IsGatewayTool reports whether a fact's tool value is a reserved gateway
// attribution rather than a registry tool.
func IsGatewayTool(tool string) bool { return strings.HasPrefix(tool, GatewayToolPrefix) }
