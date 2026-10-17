//go:build !sbo_omit_agentaccess_relay && !sbo_omit_capture_proxy

package main

import (
	"strings"

	"github.com/marmutapp/superbased-observer/internal/mcpaccess"
	"github.com/marmutapp/superbased-observer/internal/mcprelay/localpdp"
	"github.com/marmutapp/superbased-observer/internal/proxy"
)

// mcprelay_proxy.go is the proxy half of the node MCP relay (lane MP-L1L,
// T-715): the tools[] allow-list seam the relay binds into the proxy. A build
// without capture.proxy (-tags sbo_omit_capture_proxy) has no proxy request
// path, so this file is not linked; the relay keeps its stdio wrapper,
// loopback listener and the guard's MCP-access lookup.

// bindMCPRelayProxy binds the relay's tools[] seam onto the proxy options.
// A nil handle ([mcp_relay].enabled=false) binds nothing.
func bindMCPRelayProxy(h *mcpRelayHandle, opts *proxy.Options) {
	if h == nil || opts == nil {
		return
	}
	opts.ToolsAllowlist = h.ToolsSeam()
	h.logger.Info("mcp-relay: proxy tools filter wired")
}

// connectorApproved reports whether a provider-hosted connector URL is one
// the org table names (a vserver audience or a server target).
func connectorApproved(reg mcpaccess.Registry, c proxy.HostedMCPConnector) bool {
	if c.URL == "" {
		return false
	}
	u := strings.TrimRight(c.URL, "/")
	for _, v := range reg.VServers {
		if strings.TrimRight(reg.AudienceOf(v), "/") == u {
			return true
		}
		for _, s := range v.Servers {
			if strings.TrimRight(s.Target, "/") == u {
				return true
			}
		}
	}
	return false
}

// ToolsSeam is the proxy seam: strip every `mcp__*` declaration the table
// does not allow (R-306); refuse the request for a hosted connector the org
// does not approve (R-307, org-authoritative nodes only, an individual
// node's unknown connector is ungoverned).
func (h *mcpRelayHandle) ToolsSeam() proxy.ToolsAllowlistSeam {
	return func(in proxy.ToolsAllowlistInput) proxy.ToolsAllowlistResult {
		t := h.Table()
		if t == nil {
			return proxy.ToolsAllowlistResult{Keep: in.Decls}
		}
		orgAuth := h.orgAuth()
		for _, c := range in.Connectors {
			if connectorApproved(t.Node.Registry, c) {
				continue
			}
			if !orgAuth {
				continue
			}
			if t.Mode == localpdp.ModeObserve {
				h.logger.Info("mcp-relay: observe would-deny hosted MCP connector", "connector", c.Name, "url", c.URL, "session_id", in.SessionID)
				continue
			}
			name := c.Name
			if name == "" {
				name = c.ConnectorID
			}
			return proxy.ToolsAllowlistResult{
				DenyRuleID: "R-307",
				DenyReason: "hosted MCP connector " + name + " is not approved by the organization's tools.mcp_access grant; remote MCP must route through the org gateway",
			}
		}
		keep := make([]proxy.MCPToolDecl, 0, len(in.Decls))
		for _, d := range in.Decls {
			v := h.decide(d.Server, d.Tool)
			if !v.Known || v.Allowed {
				keep = append(keep, d)
				continue
			}
			h.logger.Info("mcp-relay: stripping disallowed MCP tool declaration", "tool", d.Name, "reason", v.Reason, "session_id", in.SessionID)
		}
		return proxy.ToolsAllowlistResult{Keep: keep}
	}
}
