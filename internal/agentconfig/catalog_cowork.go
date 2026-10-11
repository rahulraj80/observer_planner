package agentconfig

// Claude Cowork (phase P2, doc-authored). Cowork runs inside Claude Desktop;
// its settings are Claude Desktop's: the user claude_desktop_config.json
// (MCP servers, preferences) and the Desktop policy keys an admin sets
// through MDM (macOS plist, Windows registry, Linux managed-settings.json)
// or, for third-party (gateway / Bedrock / Vertex / Foundry) deployments,
// a local configLibrary. The inner Claude Code reads ~/.claude/settings.json
// (catalogued under claude-code). Hooks do not fire in Cowork agent mode.
// Sources: audit docs/audits/adapter-config-surface-2026-10-03.md section 5
// "cowork" and the pages below, re-fetched 2026-10-04.

const (
	coworkRef     = "https://claude.com/docs/third-party/claude-desktop/configuration.md"
	coworkEntRef  = "https://support.claude.com/en/articles/12622667-enterprise-configuration-for-claude-desktop"
	coworkMCPRef  = "https://modelcontextprotocol.io/docs/develop/connect-local-servers"
	coworkAudit   = "docs/audits/adapter-config-surface-2026-10-03.md section 5 cowork (Claude Desktop docs, 2026-10-03)"
	coworkUnverNt = "Named by the 2026-10-03 audit; not found on the third-party configuration page re-fetched 2026-10-04."
	coworkMDMOnly = "MDM only: a local configuration cannot set it."
)

var coworkManagedOnly = []Scope{ScopeManaged}

var coworkFiles = []FileRow{
	{
		ID: "cowork/desktop-config", Tool: "cowork", Shape: "cowork/desktop-config", Kind: KindSettings, Scope: ScopeUser,
		Paths: OSPaths{
			Darwin:  "~/Library/Application Support/Claude/claude_desktop_config.json",
			Windows: `%APPDATA%\Claude\claude_desktop_config.json`,
		},
		Format: FormatJSON, Precedence: 20, WriteNeeds: PrivUser, Grounding: doc(coworkMCPRef),
		Note: "Claude Desktop user config (mcpServers, preferences). The MSIX build redirects it to %LOCALAPPDATA%\\Packages\\Claude_*\\LocalCache\\Roaming\\Claude\\. " +
			"No official first-party Desktop on Linux.",
	},
	{
		ID: "cowork/desktop-internal-config", Tool: "cowork", Shape: "cowork/desktop-internal-config", Kind: KindToolState, Scope: ScopeUser,
		Paths: OSPaths{
			Darwin:  "~/Library/Application Support/Claude/config.json",
			Windows: `%APPDATA%\Claude\config.json`,
		},
		Format: FormatJSON, Precedence: 0, WriteNeeds: PrivNever, Grounding: doc(coworkAudit),
		Note: "Desktop internal state (OAuth token cache, sandbox flags): holds tokens, never read or written.",
	},
	{
		ID: "cowork/managed-policy", Tool: "cowork", Shape: "cowork/policy", Kind: KindSettings, Scope: ScopeManaged,
		Paths: OSPaths{Linux: "/etc/claude-desktop/managed-settings.json"}, Format: FormatJSON, Precedence: 100, WriteNeeds: PrivAdminRoot,
		Grounding: doc(coworkRef),
		Note: "Desktop policy. Linux: this root-owned file (native JSON). macOS: plist domain com.anthropic.claudefordesktop under /Library/Managed Preferences " +
			"(per-user subdirectory too); Windows: HKLM\\SOFTWARE\\Policies\\Claude (HKCU ignored when HKLM is present), values as strings. Re-checked every " +
			"configRecheckIntervalMinutes.",
	},
	{
		ID: "cowork/config-library", Tool: "cowork", Shape: "cowork/policy", Kind: KindSettings, Scope: ScopeUser,
		Paths: OSPaths{
			Linux:   "~/.config/Claude-3p/configLibrary/",
			Darwin:  "~/Library/Application Support/Claude-3p/configLibrary/",
			Windows: `%LOCALAPPDATA%\Claude-3p\configLibrary\`,
		},
		Format: FormatJSON, Precedence: 20, WriteNeeds: PrivNever, Grounding: doc(coworkRef),
		Note: "Third-party deployments only: local configurations (_meta.json + <id>.json) written by the app's setup. Never written by the editor: " +
			"a configuration may carry inference credentials. MDM wins over it except for the app-behavior keys.",
	},
}

func coworkRows(rows []SettingRow) []SettingRow {
	for i := range rows {
		r := &rows[i]
		r.Tool = "cowork"
		r.ID = r.Tool + "/" + r.Path.String()
		if r.FileID == "" {
			r.FileID = "cowork/policy"
		}
		if r.Owner == "" {
			r.Owner = OwnerUser
		}
		if r.Verified == "" {
			r.Verified = VerifiedDoc
		}
		if r.Source == "" {
			r.Source = coworkRef
		}
	}
	return rows
}

func coworkKey(k string) KeyPath { return KeyPath{k} }

var coworkSettings = coworkRows([]SettingRow{
	// --- claude_desktop_config.json
	{
		FileID: "cowork/desktop-config", Path: coworkKey("mcpServers"), Type: TypeMCPMap, Source: coworkMCPRef,
		Desc: "Local MCP servers by name: {command, args, env}.",
	},
	{
		FileID: "cowork/desktop-config", Path: coworkKey("preferences"), Type: TypeObject, Source: coworkAudit,
		Desc: "Desktop app preferences.",
	},
	{
		FileID: "cowork/desktop-config", Path: KeyPath{"preferences", "theme"}, Type: TypeString, Source: coworkAudit,
		Desc: "App theme.", Verified: VerifiedUnverified, Note: "UNVERIFIED in the audit (from a third-party summary).",
	},

	// --- connection and credentials (third-party)
	{
		Path: coworkKey("inferenceProvider"), Type: TypeEnum, Enum: []string{"gateway", "anthropic", "bedrock", "mantle", "vertex", "foundry"},
		Desc: "Inference backend.",
	},
	{
		Path: coworkKey("inferenceCustomHeaders"), Type: TypeObject, Desc: "Extra headers on inference requests (no credentials).",
	},
	{Path: coworkKey("inferenceSessionLifetimeSec"), Type: TypeInt, Desc: "Sign-in session lifetime in seconds."},
	{
		Path: coworkKey("inferenceCredentialKind"), Type: TypeEnum,
		Enum: []string{"static", "helper-script", "interactive", "vendor-profile", "workforce", "external-idp"}, Desc: "Where the inference credential comes from.",
	},
	{
		Path: coworkKey("inferenceCredentialHelper"), Type: TypeString, Desc: "Absolute path of an executable that prints the credential.",
		Danger: DangerConfirm, Note: "Runs an arbitrary executable.",
	},
	{
		Path: coworkKey("inferenceCredentialHelperWindows"), Type: TypeString, Desc: "Windows-only credential helper path.",
		Danger: DangerConfirm, Note: "Runs an arbitrary executable.",
	},
	{Path: coworkKey("inferenceCredentialHelperArgs"), Type: TypeStringList, Desc: "Arguments for the credential helper."},
	{Path: coworkKey("inferenceCredentialHelperTtlSec"), Type: TypeInt, Min: f64(1), Default: "3600", Desc: "Credential helper output cache TTL in seconds."},
	{Path: coworkKey("inferenceCredentialHelperTimeoutSec"), Type: TypeInt, Min: f64(1), Max: f64(600), Default: "60", Desc: "Credential helper timeout in seconds."},
	{Path: coworkKey("inferenceCredentialHelperSilentRefreshEnabled"), Type: TypeBool, Default: "true", Desc: "Re-run the helper silently on expiry."},
	{
		Path: coworkKey("egressProxyUrl"), Scopes: coworkManagedOnly, Type: TypeString, Desc: "HTTP proxy for app and agent traffic.",
		Danger: DangerConfirm, Note: coworkMDMOnly + " Pins all traffic through another host.",
	},
	{
		Path: coworkKey("egressProxyPacUrl"), Scopes: coworkManagedOnly, Type: TypeString, Desc: "PAC file URL (overrides egressProxyUrl).",
		Danger: DangerConfirm, Note: coworkMDMOnly,
	},
	{Path: coworkKey("coworkVmIpv6Enabled"), Type: TypeBool, Desc: "IPv6 in the Cowork workspace VM."},
	{Path: coworkKey("userContentRendererUrl"), Type: TypeString, Desc: "HTTPS origin for artifact and file previews."},
	{Path: coworkKey("inferenceAnthropicApiKey"), Type: TypeString, Desc: "Claude API key.", Secret: true},
	{Path: coworkKey("inferenceBedrockRegion"), Type: TypeString, Desc: "AWS region of the Bedrock endpoint."},
	{
		Path: coworkKey("inferenceBedrockBaseUrl"), Type: TypeString, Desc: "Bedrock base URL (VPC endpoint or gateway).",
		Danger: DangerConfirm, Note: "Base URL override.",
	},
	{Path: coworkKey("inferenceBedrockServiceTier"), Type: TypeEnum, Enum: []string{"flex", "priority"}, Desc: "Bedrock service tier."},
	{Path: coworkKey("inferenceBedrockBearerToken"), Type: TypeString, Desc: "Static Bedrock bearer token.", Secret: true},
	{Path: coworkKey("inferenceBedrockSsoStartUrl"), Type: TypeString, Desc: "AWS SSO start URL."},
	{Path: coworkKey("inferenceBedrockSsoRegion"), Type: TypeString, Desc: "AWS SSO region."},
	{Path: coworkKey("inferenceBedrockSsoAccountId"), Type: TypeString, Desc: "AWS account id in IAM Identity Center."},
	{Path: coworkKey("inferenceBedrockSsoRoleName"), Type: TypeString, Desc: "IAM Identity Center permission-set name."},
	{Path: coworkKey("inferenceBedrockProfile"), Type: TypeString, Desc: "AWS named profile for Bedrock credentials."},
	{Path: coworkKey("inferenceBedrockAwsDir"), Type: TypeString, Desc: "AWS config directory (default ~/.aws)."},
	{Path: coworkKey("inferenceBedrockAwsCliPath"), Type: TypeString, Desc: "Path of the aws CLI executable."},
	{Path: coworkKey("inferenceFoundryResource"), Type: TypeString, Desc: "Azure AI Foundry resource name."},
	{
		Path: coworkKey("inferenceFoundryBaseUrl"), Type: TypeString, Desc: "Full Foundry base URL (gateway or proxy).",
		Danger: DangerConfirm, Note: "Base URL override.",
	},
	{Path: coworkKey("inferenceFoundryApiKey"), Type: TypeString, Desc: "Azure AI Foundry API key.", Secret: true},
	{Path: coworkKey("inferenceFoundryTenantId"), Type: TypeString, Desc: "Entra ID tenant id."},
	{Path: coworkKey("inferenceFoundryClientId"), Type: TypeString, Desc: "Entra ID client id."},
	{Path: coworkKey("inferenceFoundryAuthFlow"), Type: TypeEnum, Enum: []string{"device-code", "browser", "broker"}, Desc: "Entra sign-in flow."},
	{
		Path: coworkKey("inferenceGatewayBaseUrl"), Type: TypeString, Desc: "Inference gateway endpoint (with inferenceProvider gateway).",
		Danger: DangerConfirm, Note: "Base URL override. A proxy route for Cowork must go here, not ANTHROPIC_BASE_URL.",
	},
	{Path: coworkKey("inferenceStreamIdleTimeoutSec"), Type: TypeInt, Min: f64(300), Max: f64(1800), Desc: "Extra wait for streamed output (gateway only)."},
	{Path: coworkKey("inferenceGatewayApiKey"), Type: TypeString, Desc: "Inference gateway API key.", Secret: true},
	{Path: coworkKey("inferenceGatewayAuthScheme"), Type: TypeEnum, Enum: []string{"bearer", "x-api-key"}, Default: "\"bearer\"", Desc: "How the gateway credential is sent."},
	{Path: coworkKey("inferenceGatewayOidcAuthFlow"), Type: TypeEnum, Enum: []string{"browser", "broker"}, Desc: "Gateway IdP sign-in flow."},
	{Path: coworkKey("inferenceGatewayOidc"), Type: TypeObject, Desc: "External IdP for gateway sign-in.", Deprecated: true, Note: "Superseded by inferenceIdpOidc."},
	{Path: coworkKey("inferenceIdpAuthFlow"), Type: TypeEnum, Enum: []string{"browser", "broker"}, Desc: "Identity provider sign-in flow."},
	{Path: coworkKey("inferenceIdpOidc"), Type: TypeObject, Desc: "Organization OIDC identity provider (credential kind external-idp)."},
	{Path: coworkKey("inferenceVertexProjectId"), Type: TypeString, Desc: "GCP project id for Vertex AI."},
	{Path: coworkKey("inferenceVertexRegion"), Type: TypeString, Desc: "GCP region of the Claude models."},
	{
		Path: coworkKey("inferenceVertexBaseUrl"), Type: TypeString, Desc: "Vertex AI PSC endpoint.",
		Danger: DangerConfirm, Note: "Base URL override.",
	},
	{Path: coworkKey("inferenceVertexOAuthClientId"), Type: TypeString, Desc: "Desktop OAuth client id (Sign in with Google)."},
	{Path: coworkKey("inferenceVertexOAuthClientSecret"), Type: TypeString, Desc: "OAuth client secret.", Secret: true, Note: "Google calls it non-confidential; still treated as a secret."},
	{Path: coworkKey("inferenceVertexOAuthScopes"), Type: TypeString, Desc: "Google OAuth scopes override."},
	{Path: coworkKey("inferenceVertexOAuthLoginHint"), Type: TypeString, Desc: "Pre-fill for the Google account chooser."},
	{Path: coworkKey("inferenceVertexWorkforceAudience"), Type: TypeString, Desc: "Workforce-pool provider audience."},
	{Path: coworkKey("inferenceVertexWorkforceUserProject"), Type: TypeString, Desc: "GCP project billed for STS."},
	{Path: coworkKey("inferenceVertexWorkforceAuthFlow"), Type: TypeEnum, Enum: []string{"browser", "broker"}, Desc: "Workforce IdP sign-in flow."},
	{Path: coworkKey("inferenceVertexWorkforceOidc"), Type: TypeObject, Desc: "Workforce Identity OIDC config."},
	{Path: coworkKey("inferenceVertexCredentialsFile"), Type: TypeString, Desc: "Service-account JSON path (blank = ADC)."},

	// --- models
	{Path: coworkKey("modelDiscoveryEnabled"), Type: TypeBool, Desc: "Fill the model picker from the provider at launch."},
	{Path: coworkKey("modelPrefer1mContext"), Type: TypeBool, Desc: "Start on the 1M-context variant of the default model."},
	{Path: coworkKey("inferenceModels"), Type: TypeObject, Desc: "Model list (an array; the first entry is the default): name, labelOverride, supports1m, prefer1m, isFamilyDefault, maxEffort."},
	{
		Path: coworkKey("defaultModelEffort"), Type: TypeEnum, Enum: []string{"low", "medium", "high", "xhigh", "max"},
		Desc: "Starting effort for the default model (third-party).",
	},
	{
		Path: coworkKey("effortLevel"), Type: TypeEnum, Enum: []string{"low", "medium", "high", "xhigh", "max"},
		Desc: "Claude Code effort level (passed as CLAUDE_CODE_EFFORT_LEVEL).",
	},
	{Path: coworkKey("alwaysStartWithDefaultModel"), Type: TypeBool, Desc: "Every new conversation starts on the default model."},
	{Path: coworkKey("inferenceModelPricingEnabled"), Type: TypeBool, Desc: "Show estimated cost on the Usage page."},
	{Path: coworkKey("inferenceModelPricingMultiplier"), Type: TypeFloat, Min: f64(0), Max: f64(1), Desc: "Multiplier on cost estimates."},
	{Path: coworkKey("inferenceModelPricing"), Type: TypeObject, Desc: "Per-model pricing rates (an array)."},
	{Path: coworkKey("modelCatalogEnabled"), Type: TypeBool, Desc: "Use the published Claude Code model catalog for picker labels."},
	{Path: coworkKey("modelCatalogUrl"), Type: TypeString, Desc: "Custom model catalog mirror (https or loopback)."},

	// --- deployment, surfaces
	{Path: coworkKey("disableDeploymentModeChooser"), Type: TypeBool, Default: "false", Desc: "Hide the Claude.ai sign-in option (third-party mode)."},
	{Path: coworkKey("disableDeepLinkRegistration"), Type: TypeBool, Default: "false", Desc: "Disable claude:// deep links."},
	{Path: coworkKey("builtinBrowserEnabled"), Type: TypeBool, Default: "false", Desc: "Offer the built-in browser in Cowork and Code."},
	{Path: coworkKey("builtinBrowserDefaultDomainPolicy"), Type: TypeEnum, Enum: []string{"allow", "block"}, Default: "\"allow\"", Desc: "Built-in browser site policy."},
	{Path: coworkKey("builtinBrowserAllowedDomains"), Type: TypeStringList, Desc: "Sites allowed when the policy is block."},
	{Path: coworkKey("builtinBrowserBlockedDomains"), Type: TypeStringList, Desc: "Sites blocked when the policy is allow."},
	{Path: coworkKey("chatTabEnabled"), Type: TypeBool, Desc: "Chat tab."},
	{Path: coworkKey("chatAdvancedFileAnalysisEnabled"), Type: TypeBool, Desc: "Sandboxed code for file analysis in Chat."},
	{Path: coworkKey("isClaudeCodeForDesktopEnabled"), Type: TypeBool, Default: "true", Desc: "Code tab."},
	{
		Path: coworkKey("sshHostAllowlist"), Type: TypeStringList, Desc: "SSH hosts Code sessions may use (\"*\" = any, empty = off).",
		Danger: DangerConfirm, Note: "Dangerous: [\"*\"].",
	},
	{Path: coworkKey("sshClientPath"), Type: TypeString, Desc: "Path of the OpenSSH client."},
	{Path: coworkKey("sshTransport"), Type: TypeEnum, Enum: []string{"auto", "system-openssh", "builtin"}, Desc: "SSH engine."},
	{Path: coworkKey("coworkTabEnabled"), Type: TypeBool, Default: "true", Desc: "Cowork tab."},
	{
		Path: coworkKey("secureVmFeaturesEnabled"), Type: TypeBool, Source: coworkEntRef, Desc: "Cowork on or off (first-party enterprise).",
	},
	{Path: coworkKey("isDesktopExtensionEnabled"), Type: TypeBool, Source: coworkEntRef, Desc: "Desktop extensions."},
	{Path: coworkKey("isDesktopExtensionDirectoryEnabled"), Type: TypeBool, Source: coworkEntRef, Desc: "Desktop extension directory."},
	{Path: coworkKey("forceLoginOrgUUID"), Type: TypeString, Source: coworkEntRef, Desc: "Organization the user must sign in to."},

	// --- workspace and permissions
	{Path: coworkKey("userPluginMarketplacesEnabled"), Type: TypeBool, Desc: "Let users add plugin marketplaces."},
	{Path: coworkKey("userPluginUploadsEnabled"), Type: TypeBool, Desc: "Let users add their own plugins."},
	{
		Path: coworkKey("disabledBuiltinTools"), Type: TypeRuleList,
		Desc: "Denied tools or Claude Code-style rules (Bash, Read, Write, Edit, WebFetch, Bash(curl *), Read(//**/.env), ...).",
	},
	{Path: coworkKey("disableBundledSkills"), Type: TypeBool, Desc: "Disable bundled skills and workflows."},
	{Path: coworkKey("skillCreationEnabled"), Type: TypeBool, Desc: "Let users create and upload skills."},
	{Path: coworkKey("scheduledTasksEnabled"), Type: TypeBool, Desc: "Scheduled tasks in Cowork and Code."},
	{Path: coworkKey("keepAwakeEnabled"), Type: TypeBool, Desc: "Let Claude keep the computer awake."},
	{
		Path: coworkKey("builtinToolPolicy"), Type: TypeObject, Desc: "Per-tool approval policy: tool or rule mapped to allow (default) or ask.",
		Danger: DangerConfirm, Note: "Dangerous: tools set to allow.",
	},
	{
		Path: coworkKey("autoModeEnabled"), Type: TypeBool, Desc: "Offer Auto mode (true also adds it to Cowork).",
		Danger: DangerConfirm, Note: "Dangerous: true together with disableBypassPermissionsMode false.",
	},
	{
		Path: coworkKey("disableBypassPermissionsMode"), Type: TypeBool, Desc: "Remove bypass permissions mode from Code and Cowork.",
		Danger: DangerConfirm, Note: "Dangerous: false (bypass stays available).",
	},
	{Path: coworkKey("toolSearchEnabled"), Type: TypeBool, Default: "false", Desc: "Load MCP tool schemas on demand."},
	{Path: coworkKey("skipWebFetchPreflight"), Type: TypeBool, Desc: "Skip the WebFetch domain check."},
	{Path: coworkKey("allowedWorkspaceFolders"), Type: TypeObject, Desc: "Folders Claude may work in (an array of {path, writable, isLocalDevMcpEnabled}); unset = unrestricted."},
	{Path: coworkKey("blockReadsOutsideWorkingDirectories"), Type: TypeBool, Desc: "Block reads outside the session working directories."},
	{
		Path: coworkKey("coworkEgressAllowedHosts"), Type: TypeStringList, Desc: "Hosts agent tools may reach (hostname, *.domain, *).",
		Danger: DangerConfirm, Note: "Dangerous: [\"*\"].",
	},
	{
		Path: coworkKey("requireCoworkFullVmSandbox"), Type: TypeBool, Default: "false", Desc: "Run tools in an isolated VM.",
		Danger: DangerConfirm, Note: "true: the VM network cannot reach a host proxy on 127.0.0.1:8820, and the inner Claude Code stops reading device policy.",
	},
	{Path: coworkKey("organizationInstructions"), Type: TypeString, Desc: "Instructions appended to the system prompt (max 3,000 characters)."},

	// --- plugins and MCP
	{Path: coworkKey("allowedPluginMarketplaces"), Type: TypeObject, Desc: "Organization-provisioned plugin marketplaces (an array)."},
	{Path: coworkKey("managedMcpServers"), Type: TypeObject, Desc: "Managed MCP servers (an array of {name, disabled, alwaysAllow, command, args, env, stdio})."},
	{
		Path: coworkKey("isLocalDevMcpEnabled"), Type: TypeBool, Desc: "Local development MCP server.",
		Danger: DangerConfirm, Note: "false blocks a local MCP server such as Observer's.",
	},
	{
		Path: coworkKey("allowedMcpCommandExtensions"), Type: TypeStringList, Desc: "MCP command extensions allowed.",
		Source: coworkAudit, Verified: VerifiedUnverified, Note: coworkUnverNt,
	},

	// --- telemetry
	{Path: coworkKey("otlpEndpoint"), Type: TypeString, Desc: "OpenTelemetry endpoint for metrics and traces."},
	{Path: coworkKey("otlpHeaders"), Type: TypeObject, Desc: "Extra OTLP headers (authentication).", Secret: true, Note: "Usually carries an auth header: the secret posture applies."},
	{
		Path: coworkKey("otlpEnabled"), Type: TypeBool, Desc: "OpenTelemetry export on or off.",
		Source: coworkAudit, Verified: VerifiedUnverified, Note: coworkUnverNt,
	},
	{
		Path: coworkKey("diagnosticsDataCollectionEnabled"), Type: TypeBool, Desc: "Diagnostics data collection.",
		Source: coworkAudit, Verified: VerifiedUnverified, Note: coworkUnverNt,
	},

	// --- updates and lifecycle
	{Path: coworkKey("disableAutoUpdates"), Scopes: coworkManagedOnly, Type: TypeBool, Desc: "Disable automatic app updates.", Note: coworkMDMOnly},
	{Path: coworkKey("autoUpdaterEnforcementHours"), Scopes: coworkManagedOnly, Type: TypeInt, Min: f64(1), Max: f64(72), Desc: "Hours before an update restart is forced.", Note: coworkMDMOnly},
	{Path: coworkKey("updateViaUpdatesHost"), Scopes: coworkManagedOnly, Type: TypeBool, Desc: "Update through the updates host.", Note: coworkMDMOnly},
	{Path: coworkKey("configRecheckIntervalMinutes"), Type: TypeInt, Min: f64(1), Max: f64(1440), Default: "10", Desc: "Managed configuration re-check interval in minutes."},
	{Path: coworkKey("relaunchEnforcementHours"), Type: TypeInt, Default: "24", Desc: "Hours before a restart is forced after a configuration change."},

	// --- bootstrap server
	{Path: coworkKey("bootstrapUrl"), Type: TypeString, Desc: "Server that delivers dynamic configuration after sign-in."},
	{Path: coworkKey("bootstrapEnabled"), Type: TypeBool, Desc: "Use the bootstrap server."},
	{Path: coworkKey("bootstrapCustomCa"), Type: TypeString, Desc: "PEM CA certificate for the bootstrap server."},
})

// coworkRuleGrammar: disabledBuiltinTools takes Claude Code tool names and
// rules; MCP rules are not listed for it.
var coworkRuleGrammar = RuleGrammar{
	"shell":  {Name: "Bash", Style: RuleCall},
	"read":   {Name: "Read", Style: RuleCall},
	"edit":   {Name: "Edit", Style: RuleCall},
	"write":  {Name: "Write", Style: RuleCall},
	"fetch":  {Name: "WebFetch", Style: RuleCall, PatternPrefix: "domain:"},
	"search": {Name: "WebSearch", Style: RuleBare},
}

// coworkMCPShape: claude_desktop_config.json mcpServers are local servers
// only ({command, args, env}).
var coworkMCPShape = MCPShape{EnvKey: "env", TypeNames: map[string]string{"stdio": ""}}

var coworkMap = []MapRow{
	{
		Key: KeyReasoningEffort, Tool: "cowork", Settings: []string{"cowork/effortLevel"}, Transform: TransformEnumMap,
		Params: TransformParams{Enum: []EnumPair{
			{"low", []any{"low"}}, {"medium", []any{"medium"}}, {"high", []any{"high"}}, {"xhigh", []any{"xhigh"}}, {"max", []any{"max"}},
		}}, Source: coworkRef, Note: "Desktop policy (admin-authored). none and minimal are refused.",
	},
	{
		Key: KeyAutoUpdate, Tool: "cowork", Settings: []string{"cowork/disableAutoUpdates"}, Transform: TransformBoolInvert, Source: coworkRef,
		Note: "MDM only.",
	},
	{
		Key: KeyDeniedTools, Tool: "cowork", Settings: []string{"cowork/disabledBuiltinTools"}, Transform: TransformRuleTranslate,
		Params: TransformParams{Grammar: coworkRuleGrammar}, Source: coworkRef, Note: "Built-in tools only; MCP rules are refused.",
	},
	{
		Key: KeyMCPServers, Tool: "cowork", Settings: []string{"cowork/mcpServers"}, Transform: TransformMCPShape,
		Params: TransformParams{MCP: coworkMCPShape}, Lossy: true, Source: coworkMCPRef,
		Note: "Local servers only; remote servers are refused. " + mcpLossyNote,
	},
	{
		Key: KeyAPIKeyRef, Tool: "cowork", Settings: []string{"cowork/inferenceCredentialHelper"}, Transform: TransformIdentity, Source: coworkRef,
		Note: "A helper executable that prints the credential; the key itself is never stored.",
	},
	{
		Key: KeyBaseURL, Tool: "cowork", Settings: []string{"cowork/inferenceGatewayBaseUrl"}, Transform: TransformIdentity, Source: coworkRef,
		Note: "Third-party gateway deployments only; the first-party OAuth path has no base URL knob.",
	},
}

var coworkCatalog = toolCatalog{
	Tool: "cowork",
	VerifiedAgainst: "Claude Desktop third-party configuration reference fetched 2026-10-04, the enterprise configuration support article and the MCP " +
		"local-servers page (audit section 5); no live install on node 1. Hooks do not fire in Cowork agent mode.",
	Files: coworkFiles,
	Extra: coworkSettings,
	Map:   coworkMap,
}
