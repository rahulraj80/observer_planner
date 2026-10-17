package agentconfig

// Crush overlay (legacy crush.json; crushrc is raw text with no rows).
// Sources: github.com/charmbracelet/crush README.md, docs/config/README.md,
// schema.json (fetched 2026-10-03; audit section 5 "crush").
const crushRef = "https://raw.githubusercontent.com/charmbracelet/crush/main/docs/config/README.md"

var crushCatalog = toolCatalog{
	Tool:            "crush",
	Map:             crushMap,
	VerifiedAgainst: "schema.json on main for the legacy crush.json format, fetched 2026-10-03",
	Overlays: []Overlay{
		{
			ID: "crush/permissions.allowed_tools", Type: TypeRuleList, Danger: DangerConfirm, Source: crushRef,
			Note: "Listed tools run without asking.",
		},
		{ID: "crush/options.disabled_tools", Type: TypeRuleList, Source: crushRef, Note: "Disabled tools are hidden, not refused at run time."},
		{ID: "crush/mcp", Type: TypeMCPMap, Source: crushRef},
		{
			ID: "crush/providers", Source: crushRef,
			Note: "providers.<id>.base_url is Observer's proxy route seam; api_key must be a $ENV reference, never a literal key. $(...) in a value runs at load.",
		},
		{ID: "crush/env", Source: crushRef, Note: "Values are shell-expanded ($VAR, $(cmd)) at load."},
		{
			ID: "crush/hooks", Source: crushRef,
			Note: "Observer's hook registration owns its own entries (stamped as an Observer-owned span in phase P3).",
		},
	},
}
