package agentconfig

import "sort"

// The common vocabulary (plan section 2.4 and section 4): one closed set of
// keys, each with a canonical type and canonical values, mapped onto the
// catalogue rows of every tool that exposes it (MapRow, vocab_map.go). A
// common key owns no file location of its own.

// CommonKey is the closed enum of common-vocabulary keys.
type CommonKey string

// The 21 common keys: 13 Core keys (section 4 groups them into 11 table
// rows: allowed/denied tools and context limit/auto compact share a row),
// 6 Secondary keys, and the 2 Observer-owned keys that stay read-only in
// the common plane.
const (
	KeyModel             CommonKey = "model"
	KeyApprovalMode      CommonKey = "approval_mode"
	KeyAllowedTools      CommonKey = "allowed_tools"
	KeyDeniedTools       CommonKey = "denied_tools"
	KeyMCPServers        CommonKey = "mcp_servers"
	KeyTelemetryOff      CommonKey = "telemetry_off"
	KeyAutoUpdate        CommonKey = "auto_update"
	KeyInstructionsFiles CommonKey = "instructions_files"
	KeyAPIKeyRef         CommonKey = "api_key_ref" //nolint:gosec // G101: a vocabulary key name; the key holds a reference, never a credential.
	KeyContextLimit      CommonKey = "context_limit"
	KeyAutoCompact       CommonKey = "auto_compact"
	KeyReasoningEffort   CommonKey = "reasoning_effort"
	KeyTheme             CommonKey = "theme"

	KeyAdditionalDirs  CommonKey = "additional_dirs"
	KeyEnv             CommonKey = "env"
	KeySmallModel      CommonKey = "small_model"
	KeySandboxMode     CommonKey = "sandbox_mode"
	KeyMaxOutputTokens CommonKey = "max_output_tokens"
	KeyTemperature     CommonKey = "temperature"

	KeyBaseURL CommonKey = "base_url"
	KeyHooks   CommonKey = "hooks"
)

// CommonTier groups the keys the way the common form ships them.
type CommonTier string

const (
	// TierCore keys ship first in the common form; each reaches at least
	// 26 tools (section 4).
	TierCore CommonTier = "core"
	// TierSecondary keys are per-tool fields the common form can also set.
	TierSecondary CommonTier = "secondary"
	// TierObserverOwned keys render read-only in the common plane: Observer
	// writes them (proxy route, hook registration) and edits go through
	// those owners' verbs.
	TierObserverOwned CommonTier = "observer_owned"
)

// OrderKind says how values of an ordered-danger key compare when several
// org layers set it (section 3.2: strictest wins across org layers, deny
// lists union, allow lists intersect). OrderNone keys resolve by layer
// specificity alone.
type OrderKind string

const (
	OrderNone OrderKind = ""
	// OrderEnum: Enum is listed strictest first; the strictest value wins.
	OrderEnum OrderKind = "enum_strictest"
	// OrderTrueStrict: true is stricter than false (telemetry_off).
	OrderTrueStrict OrderKind = "true_strict"
	// OrderUnion: list values are unioned (deny lists).
	OrderUnion OrderKind = "union"
	// OrderIntersect: list values are intersected (allow lists).
	OrderIntersect OrderKind = "intersect"
)

// CommonSpec is one common key's canonical definition.
type CommonSpec struct {
	Key  CommonKey  `json:"key"`
	Tier CommonTier `json:"tier"`
	Type ValueType  `json:"type"`
	// Enum lists the canonical values; for an OrderEnum key, strictest
	// first.
	Enum []string `json:"enum,omitempty"`
	// Danger lists the canonical values that are dangerous (the section 5.4
	// table); a write of one needs the confirm step.
	Danger []string `json:"danger,omitempty"`
	// Unit names the canonical unit ("tokens", "percent").
	Unit  string    `json:"unit,omitempty"`
	Order OrderKind `json:"order,omitempty"`
	Desc  string    `json:"desc"`
}

// commonSpecs is the closed table, in section 4 order.
var commonSpecs = []CommonSpec{
	{
		Key: KeyModel, Tier: TierCore, Type: TypeString,
		Desc: `Main model. Canonical form "<provider>/<model>" (e.g. anthropic/claude-sonnet-4-6); a bare id is accepted where the provider is unambiguous. Written through model_alias, which refuses a model the tool's catalogue cannot serve.`,
	},
	{
		Key: KeyApprovalMode, Tier: TierCore, Type: TypeEnum,
		Enum:   []string{"plan", "ask", "ask_edits", "auto_edits", "full_auto"},
		Danger: []string{"full_auto"}, Order: OrderEnum,
		Desc: "How much the agent may do without asking, strictest first: plan (read-only, no edits or commands), ask (prompt for every tool, reads included), ask_edits (reads run, edits and commands prompt), auto_edits (edits run, commands prompt), full_auto (nothing prompts).",
	},
	{
		Key: KeyAllowedTools, Tier: TierCore, Type: TypeRuleList, Order: OrderIntersect,
		Desc: `Rules that run without a prompt. Canonical rule "<kind>" or "<kind>:<pattern>", kind one of shell, read, edit, write, fetch, search, mcp; translated per tool by rule_translate, which refuses what the tool's grammar cannot express.`,
	},
	{
		Key: KeyDeniedTools, Tier: TierCore, Type: TypeRuleList, Order: OrderUnion,
		Desc: "Rules that never run. Same canonical rule form as allowed_tools.",
	},
	{
		Key: KeyMCPServers, Tier: TierCore, Type: TypeMCPMap,
		Desc: "MCP servers by name: {command, args, env} for a local server or {type, url} for a remote one. Observer's own entry is an owned span and never written through this key.",
	},
	{
		Key: KeyTelemetryOff, Tier: TierCore, Type: TypeBool, Order: OrderTrueStrict,
		Desc: "true turns the tool's own usage telemetry off.",
	},
	{
		Key: KeyAutoUpdate, Tier: TierCore, Type: TypeBool,
		Desc: "true lets the tool update itself.",
	},
	{
		Key: KeyInstructionsFiles, Tier: TierCore, Type: TypeStringList,
		Desc: "Extra instruction file names or globs the tool loads. Most tools read fixed names: writable only where the tool has an override knob.",
	},
	{
		Key: KeyAPIKeyRef, Tier: TierCore, Type: TypeString,
		Desc: "A REFERENCE to the API key (an env var name, a ${VAR} reference or a helper command), never the key itself.",
	},
	{
		Key: KeyContextLimit, Tier: TierCore, Type: TypeInt, Unit: "tokens",
		Desc: "Context window the tool budgets for, in tokens.",
	},
	{
		Key: KeyAutoCompact, Tier: TierCore, Type: TypeInt, Unit: "percent",
		Desc: "Context usage, as a percent of the window, at which the tool compacts the conversation.",
	},
	{
		Key: KeyReasoningEffort, Tier: TierCore, Type: TypeEnum,
		Enum: []string{"none", "minimal", "low", "medium", "high", "xhigh", "max"},
		Desc: "Reasoning effort. A tool that only takes a thinking-token budget gets one through effort_to_budget (lossy).",
	},
	{
		Key: KeyTheme, Tier: TierCore, Type: TypeEnum, Enum: []string{"light", "dark"},
		Desc: "Light or dark. Named themes stay per tool.",
	},

	{
		Key: KeyAdditionalDirs, Tier: TierSecondary, Type: TypeStringList,
		Desc: "Directories outside the project the agent may read or write.",
	},
	{
		Key: KeyEnv, Tier: TierSecondary, Type: TypeStringMap,
		Desc: "Environment variables the tool passes to its commands. The secret posture (section 5.3) applies to every value.",
	},
	{
		Key: KeySmallModel, Tier: TierSecondary, Type: TypeString,
		Desc: "Model for background and low-complexity tasks; same canonical form as model.",
	},
	{
		Key: KeySandboxMode, Tier: TierSecondary, Type: TypeEnum,
		Enum:   []string{"read_only", "workspace_write", "off"},
		Danger: []string{"off"}, Order: OrderEnum,
		Desc: "Command sandbox, strictest first: read_only, workspace_write, off.",
	},
	{
		Key: KeyMaxOutputTokens, Tier: TierSecondary, Type: TypeInt, Unit: "tokens",
		Desc: "Maximum output tokens per model response.",
	},
	{
		Key: KeyTemperature, Tier: TierSecondary, Type: TypeFloat,
		Desc: "Sampling temperature.",
	},

	{
		Key: KeyBaseURL, Tier: TierObserverOwned, Type: TypeString,
		Desc: "Provider base URL. Observer-owned while the proxy route is on: edited through the route verbs, never the common form.",
	},
	{
		Key: KeyHooks, Tier: TierObserverOwned, Type: TypeObject,
		Desc: "Hook configuration. Observer-owned (hook registration); never a common-plane write.",
	},
}

// CommonSpecs returns the closed common-key table (copies).
func CommonSpecs() []CommonSpec {
	out := make([]CommonSpec, len(commonSpecs))
	for i, s := range commonSpecs {
		s.Enum = append([]string(nil), s.Enum...)
		s.Danger = append([]string(nil), s.Danger...)
		out[i] = s
	}
	return out
}

// CommonSpecFor returns one key's spec.
func CommonSpecFor(k CommonKey) (CommonSpec, bool) {
	for _, s := range CommonSpecs() {
		if s.Key == k {
			return s, true
		}
	}
	return CommonSpec{}, false
}

// CommonKeys returns every key, sorted.
func CommonKeys() []CommonKey {
	out := make([]CommonKey, 0, len(commonSpecs))
	for _, s := range commonSpecs {
		out = append(out, s.Key)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
