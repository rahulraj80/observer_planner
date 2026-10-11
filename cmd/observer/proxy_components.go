package main

import (
	"github.com/marmutapp/superbased-observer/internal/config"
)

// proxyInertOverlay is the start-time switch overlay of every component
// buildProxy wires a seam for (modular packaging, T-659 / T-661, lane
// MP-BND; completed by lane MP-ADP, T-598). It extends inertOverlay
// (components_gate.go) for buildProxy, which loads its own config rather
// than the daemon's overlaid copy, so without this gate an inactive
// component would still be wired into the proxy by its own config switch.
// One row per component, walked in order; guard is deliberately absent:
// an inactive guard keeps its proxy-time decisions (ledger MODPKG-11).
var proxyInertOverlay = []struct {
	component string
	apply     func(*config.Config)
}{
	// Conversation compression and the post-compaction injector: both
	// rewrite the upstream request body.
	{"proxy.compression", func(c *config.Config) {
		c.Compression.Conversation.Enabled = false
		c.Compression.Conversation.Compaction.InjectPostCompact = false
	}},
	{"proxy.routing", func(c *config.Config) { c.Routing.Enabled = false }},
	// The node MCP relay's proxy seams (the tools[] allow-list and the
	// guard's MCP-access lookup) and its process-wide handle.
	{"agentaccess.relay", func(c *config.Config) { c.MCPRelay.Enabled = false }},
	// Process observation's network-capture seam on the proxy.
	{"processobs", func(c *config.Config) { c.Observer.Process.Network.Enabled = false }},
	// The self-observability OTLP sink is export.otel's (ruling D-436).
	{"export.otel", func(c *config.Config) { c.SelfObs.Enabled = false }},
	// The node-local [proxy.org_route] bootstrap to the org AI Gateway: no
	// org egress from the proxy while the org client is inactive.
	{"org.client", func(c *config.Config) { c.Proxy.OrgRoute = config.ProxyOrgRouteConfig{} }},
}

// applyProxyComponentGate returns cfg with the switches of every inactive
// component turned off: the daemon-wide inertOverlay (so an inactive obs
// builds no admission service here either) plus proxyInertOverlay. With no
// [components] block the snapshot is the legacy everything-active one and
// cfg is returned unchanged, so a default install behaves exactly as before.
func applyProxyComponentGate(cfg config.Config) config.Config {
	snap := componentSnapshotForConfig(cfg)
	cfg = applyInertOverlay(cfg, snap)
	for _, row := range proxyInertOverlay {
		if !snap.Active(row.component) {
			row.apply(&cfg)
		}
	}
	return cfg
}
