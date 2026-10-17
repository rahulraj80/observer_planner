//go:build !sbo_omit_proxy_routing

package mcp

import "github.com/marmutapp/superbased-observer/internal/routing"

// tools_routing_templates.go is the proxy.routing half of
// get_routing_status: the built-in policy templates live in
// internal/routing (the policy compiler), which a `-tags
// sbo_omit_proxy_routing` build leaves out (tools_routing_templates_omit.go,
// lane MP-L1I, T-715). Everything else the routing tools read is the core
// internal/routingcore.

// routingTemplateInfo is one built-in policy template as
// get_routing_status reports it.
type routingTemplateInfo struct {
	Name  string `json:"name"`
	Hash  string `json:"hash"`
	Rules int    `json:"rules"`
}

// routingTemplateInfos lists the built-in policy templates with their
// content hashes.
func routingTemplateInfos() []routingTemplateInfo {
	templates := make([]routingTemplateInfo, 0, len(routing.Templates()))
	for _, p := range routing.Templates() {
		templates = append(templates, routingTemplateInfo{Name: p.Name, Hash: p.Hash(), Rules: len(p.Rules)})
	}
	return templates
}
