//go:build sbo_omit_proxy_routing

package mcp

// tools_routing_templates_omit.go is the off side of
// tools_routing_templates.go: in a `-tags sbo_omit_proxy_routing` build the
// policy compiler is not linked, so get_routing_status reports no templates
// (lane MP-L1I, T-715). The tier table and the recommendation tool keep
// working from the core internal/routingcore.

// routingTemplateInfo is one built-in policy template as
// get_routing_status reports it.
type routingTemplateInfo struct {
	Name  string `json:"name"`
	Hash  string `json:"hash"`
	Rules int    `json:"rules"`
}

// routingTemplateInfos is empty: no policy template is in this build.
func routingTemplateInfos() []routingTemplateInfo { return []routingTemplateInfo{} }
