package agentconfig

// Codex and Open Interpreter overlays. Open Interpreter is a rebadged
// Codex CLI build whose vendor names Codex's config.schema.json as the
// source of truth, so both get the Codex-lineage overlay; the fork's own
// documented differences are layered on top (plan section 9).
// Sources: learn.chatgpt.com/docs/config-file/config-reference,
// learn.chatgpt.com/docs/enterprise/managed-configuration,
// openinterpreter.com/docs/terminal/config-reference (fetched 2026-10-03;
// audit section 5 "codex" / "open-interpreter").
const (
	codexRef     = "https://learn.chatgpt.com/docs/config-file/config-reference"
	codexManaged = "https://learn.chatgpt.com/docs/enterprise/managed-configuration"
	oiRef        = "https://www.openinterpreter.com/docs/terminal/config-reference"
	notProject   = "Cannot be set from a project .codex/config.toml (user, system or managed only)."
)

var codexNonProjectScopes = []Scope{ScopeSystem, ScopeUser, ScopeManaged}

// codexLineageOverlays is the overlay set every Codex-config tool shares,
// keyed by the tool id prefix of its row ids.
func codexLineageOverlays(tool, ref string) []Overlay {
	id := func(k string) string { return tool + "/" + k }
	return []Overlay{
		{ID: id("approval_policy"), Danger: DangerConfirm, Source: ref, Note: "\"never\" runs every command without asking."},
		{ID: id("approvals_reviewer"), Danger: DangerConfirm, Source: ref, Note: "\"auto_review\" lets a model approve."},
		{ID: id("sandbox_mode"), Danger: DangerConfirm, Source: ref, Note: "\"danger-full-access\" disables the sandbox."},
		{ID: id("sandbox_workspace_write.network_access"), Danger: DangerConfirm, Source: ref},
		{ID: id("shell_environment_policy.inherit"), Danger: DangerConfirm, Source: ref, Note: "\"all\" passes every variable (secrets included) to commands."},
		{ID: id("notify"), Danger: DangerConfirm, Scopes: codexNonProjectScopes, Source: ref, Note: "Runs an arbitrary command. " + notProject},
		{ID: id("projects"), Danger: DangerConfirm, Source: ref, Note: "trust_level = \"trusted\" on an untrusted repository loads its project config."},
		{ID: id("features.hooks"), Danger: DangerRefuseWhileIntegrated, Source: ref, Note: "false switches off hooks, Observer's included."},
		{ID: id("features.codex_hooks"), Danger: DangerRefuseWhileIntegrated, Source: ref, Note: "Deprecated alias of features.hooks."},
		{ID: id("hooks"), Prefix: true, Source: ref, Note: "Observer's hook registration owns its own entries (stamped as an Observer-owned span in phase P3)."},
		{ID: id("mcp_servers"), Type: TypeMCPMap, Source: ref},
		{ID: id("model_provider"), Scopes: codexNonProjectScopes, Source: ref, Note: notProject},
		{
			ID: id("model_providers"), Scopes: codexNonProjectScopes, Source: ref,
			Note: "base_url here is Observer's proxy route seam; env_key names a variable, never a value. " + notProject,
		},
		{
			ID: id("openai_base_url"), Scopes: codexNonProjectScopes, Source: ref,
			Note: "Moving it off Observer while the proxy route is on breaks capture. " + notProject,
		},
		{ID: id("profile"), Scopes: codexNonProjectScopes, Source: ref, Note: notProject},
		{ID: id("profiles"), Scopes: codexNonProjectScopes, Source: ref, Note: notProject},
		{ID: id("otel"), Prefix: true, Scopes: codexNonProjectScopes, Source: ref, Note: notProject},
		{ID: id("cli_auth_credentials_store"), Source: codexManaged, Note: "Where credentials are stored; never a credential itself."},
	}
}

var codexCatalog = toolCatalog{
	Tool:            "codex",
	Map:             codexMap,
	VerifiedAgainst: "codex-rs/core/config.schema.json on main, fetched 2026-10-03 (the schema carries no release version)",
	Overlays:        codexLineageOverlays("codex", codexRef),
}

var openInterpreterCatalog = toolCatalog{
	Tool:            "open-interpreter",
	Map:             oiMap,
	VerifiedAgainst: "Codex config.schema.json (the vendor's named source of truth), fetched 2026-10-03; version skew between the fork and Codex main is unverified",
	Overlays: append(codexLineageOverlays("open-interpreter", oiRef),
		Overlay{
			ID: "open-interpreter/approval_policy", Enum: []string{"untrusted", "on-request", "never"}, Verified: VerifiedDoc, Source: oiRef,
			Note: "The fork still accepts \"untrusted\", which current Codex rejects.",
		},
		Overlay{
			ID: "open-interpreter/model_reasoning_effort", Type: TypeEnum, Enum: []string{"minimal", "low", "medium", "high", "xhigh"}, Verified: VerifiedDoc, Source: oiRef,
			Note: "The fork documents a closed set with \"minimal\" (Codex: any value the model advertises).",
		},
		Overlay{ID: "open-interpreter/features.hooks", Source: oiRef, Note: "Hooks are ENABLED by default in this fork (opt-in in Codex)."},
	),
}
