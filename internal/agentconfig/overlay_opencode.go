package agentconfig

// OpenCode-lineage overlays: OpenCode itself and its fork Kilo (the Kilo
// CLI and the Kilo Code 7.x extension, which share one config). Sources:
// opencode.ai/docs/{config,permissions,enterprise} (repo mirror
// anomalyco/opencode packages/web/src/content/docs), kilo.ai/docs and
// app.kilo.ai/config.json (fetched 2026-10-03; audit section 5 "opencode",
// "kilo-code", "kilo-code-cli").
const (
	opencodeRef   = "https://opencode.ai/docs/config/"
	opencodePerms = "https://opencode.ai/docs/permissions/"
	kiloRef       = "https://kilo.ai/docs/contributing/architecture/config-schema"
)

// opencodeLineageOverlays is the overlay set every OpenCode-config tool
// shares, keyed by the tool id prefix of its row ids.
func opencodeLineageOverlays(tool, ref, permsRef string) []Overlay {
	id := func(k string) string { return tool + "/" + k }
	return []Overlay{
		{ID: id("permission"), Danger: DangerConfirm, Source: permsRef, Note: "\"allow\" approves every tool call."},
		{ID: id("permission"), Prefix: true, Danger: DangerConfirm, Source: permsRef, Note: "\"allow\" (or a broad pattern) approves without asking."},
		{ID: id("share"), Danger: DangerConfirm, Source: ref, Note: "\"auto\" uploads conversations to the vendor's share service."},
		{ID: id("plugin"), Danger: DangerConfirm, Source: ref, Note: "Plugins run in-process code."},
		{ID: id("mcp"), Type: TypeMCPMap, Source: ref, Note: "command is an argv array; the env key is environment."},
		{
			ID: id("provider"), Source: ref,
			Note: "provider.<id>.options.baseURL is Observer's proxy route seam; options.apiKey must be an {env:NAME} reference, never a literal key.",
		},
		{ID: id("disabled_providers"), Source: ref, Note: "Can drop the provider Observer proxies."},
		{ID: id("enabled_providers"), Source: ref, Note: "Can drop the provider Observer proxies."},
	}
}

var opencodeCatalog = toolCatalog{
	Tool:            "opencode",
	Map:             opencodeMap,
	VerifiedAgainst: "opencode.ai/config.json fetched 2026-10-03 (the schema carries no release version)",
	Overlays:        opencodeLineageOverlays("opencode", opencodeRef, opencodePerms),
}

// kiloOverlays adds the Kilo-only keys to the shared lineage set.
func kiloOverlays(tool string) []Overlay {
	id := func(k string) string { return tool + "/" + k }
	return append(opencodeLineageOverlays(tool, kiloRef, kiloRef),
		Overlay{ID: id("snapshot"), Danger: DangerConfirm, Source: kiloRef, Note: "false removes undo."},
		Overlay{ID: id("remote_control"), Danger: DangerConfirm, Source: kiloRef},
		Overlay{ID: id("retention.enabled"), Danger: DangerConfirm, Source: kiloRef, Note: "Permanently deletes old sessions."},
	)
}

var kiloCodeCLICatalog = toolCatalog{
	Tool:            "kilo-code-cli",
	Map:             kiloCodeCLIMap,
	VerifiedAgainst: "app.kilo.ai/config.json fetched 2026-10-03 (the schema carries no release version)",
	Overlays:        kiloOverlays("kilo-code-cli"),
}

var kiloCodeCatalog = toolCatalog{
	Tool:            "kilo-code",
	Map:             kiloCodeMap,
	VerifiedAgainst: "app.kilo.ai/config.json fetched 2026-10-03 (Kilo Code 7.x shares the Kilo CLI config; the pre-7 extension's settings live in VS Code state)",
	Overlays:        kiloOverlays("kilo-code"),
}
