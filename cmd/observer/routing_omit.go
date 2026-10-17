//go:build sbo_omit_proxy_routing

package main

import (
	"github.com/spf13/cobra"

	"github.com/marmutapp/superbased-observer/internal/components"
)

// routing_omit.go stands in for the routing*.go wiring in a build without
// proxy.routing (-tags sbo_omit_proxy_routing, lane MP-L1I, T-715, ruling
// D-512): internal/routing (the router engine, policy compiler, bases,
// Simulate and lint), internal/routingconfig and internal/routingapply are
// not linked. The pure tier table and turn-kind classifier stay in the core
// internal/routingcore, so the Model Value Report and advise keep working.
//
// Operator rule D-514: a [routing] block this build cannot honour never
// stops `observer start`. The proxy starts with no model router and says so
// once in the log; `observer routing` answers "not in this build" (exit 3)
// and names where to get a build with it.
func init() { components.MarkOmitted("proxy.routing") }

// newRoutingCmd registers `observer routing` as a compiled-out verb.
func newRoutingCmd() *cobra.Command {
	return compiledOutVerb("routing", "Model routing (not in this build)", "proxy.routing")
}
