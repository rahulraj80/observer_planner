package integration

// Built-in provider coverage for a route (T-035, D-033 / D-273 / D-423).
//
// pi and Prime Agent launch on the session-scoped "observer" provider, but a
// developer can switch to one of the tool's BUILT-IN providers mid-session,
// and those talk to their vendor hosts directly, past the proxy's prompt
// guard and budget admission (security ledger PROVIDER-SWITCH-1). Both tools
// read a models.json in which a provider entry that carries only a `baseUrl`
// overrides that built-in provider's endpoint while keeping its models and
// its own credential (pi docs/models.md "Overriding Built-in Providers").
// Grounded 2026-10-08 against pi 0.74.2 (@earendil-works/pi-coding-agent)
// and prime-agent 0.9.8, both installed in a throwaway HOME and pointed at a
// local mock upstream: every covered row below reached the mock at exactly
// the path the proxy expects (docs/proxy-routing-blockers.md "pi and Prime
// Agent built-in providers").
//
// ProviderOverride is the data a launcher needs to point one built-in
// provider at the proxy, or the grounded reason it cannot. The launcher
// writes the covered rows on a managed node only; it never names a tool.

// ProviderOverride is one built-in provider of a tool and how the proxy can
// carry it.
type ProviderOverride struct {
	// Provider is the tool's own built-in provider id (the models.json key).
	Provider string
	// API is the wire shape the tool speaks to this provider (informational;
	// it decides which proxy parser sees the turn).
	API string
	// Path is the base path, relative to the proxy root, that replaces the
	// provider's base URL: "" for Anthropic's fixed upstream, "/v1" for
	// OpenAI's, "/v1beta" for Gemini's, "/up/<lane>/<vendor path>" for a
	// lane. Meaningful only when Uncovered is empty.
	Path string
	// Lane is the `/up/<id>/` lane Path addresses (nil for a fixed
	// upstream). Its DefaultUpstream is a built-in agent lane, so a fresh
	// install routes the provider with no hand-written config.
	Lane *ProxyLane
	// Uncovered, when not empty, is why this provider cannot be pointed at
	// the proxy. Such a provider still bypasses it and is flagged after send.
	Uncovered string
	// Caveat names a part of a covered provider the override does not reach
	// ("" = none).
	Caveat string
}

// Covered reports whether the launcher can point the provider at the proxy.
func (o ProviderOverride) Covered() bool { return o.Uncovered == "" }

// laneOverride builds a covered row that rides a built-in agent lane.
func laneOverride(provider, api, lane, upstream, vendorPath string) ProviderOverride {
	return ProviderOverride{
		Provider: provider, API: api,
		Path: "/up/" + lane + vendorPath,
		Lane: &ProxyLane{ID: lane, DefaultUpstream: upstream},
	}
}

// Reasons shared by the uncovered rows.
const (
	uncoveredTemplatedHost = "its base URL is a per-account template (account, gateway or region placeholders), so there is no fixed host for a proxy lane"
	uncoveredMixedBase     = "its models use different base paths per API, and a provider-level base URL replaces all of them with one value"
)

// piFamilyProviderOverrides is the built-in provider table shared by pi and
// Prime Agent (a hard fork of the same pi-mono upstream; both bundle the same
// provider catalogue apart from the rows added per tool below).
var piFamilyProviderOverrides = []ProviderOverride{
	{Provider: "anthropic", API: "anthropic-messages", Path: ""},
	{Provider: "openai", API: "openai-responses", Path: "/v1"},
	{Provider: "google", API: "google-generative-ai", Path: "/v1beta"},
	laneOverride("openrouter", "openai-completions", "openrouter", "https://openrouter.ai", "/api/v1"),
	laneOverride("xai", "openai-completions", "xai", "https://api.x.ai", "/v1"),
	laneOverride("groq", "openai-completions", "groq", "https://api.groq.com", "/openai/v1"),
	laneOverride("cerebras", "openai-completions", "cerebras", "https://api.cerebras.ai", "/v1"),
	laneOverride("deepseek", "openai-completions", "deepseek", "https://api.deepseek.com", ""),
	laneOverride("zai", "openai-completions", "zai", "https://api.z.ai", "/api/coding/paas/v4"),
	laneOverride("moonshotai", "openai-completions", "moonshotai", "https://api.moonshot.ai", "/v1"),
	laneOverride("moonshotai-cn", "openai-completions", "moonshotai-cn", "https://api.moonshot.cn", "/v1"),
	laneOverride("huggingface", "openai-completions", "huggingface", "https://router.huggingface.co", "/v1"),
	laneOverride("fireworks", "anthropic-messages", "fireworks", "https://api.fireworks.ai", "/inference"),
	laneOverride("vercel-ai-gateway", "anthropic-messages", "vercel-ai-gateway", "https://ai-gateway.vercel.sh", ""),
	laneOverride("kimi-coding", "anthropic-messages", "kimi-coding", "https://api.kimi.com", "/coding"),
	laneOverride("minimax", "anthropic-messages", "minimax", "https://api.minimax.io", "/anthropic"),
	laneOverride("minimax-cn", "anthropic-messages", "minimax-cn", "https://api.minimaxi.com", "/anthropic"),
	laneOverride("xiaomi", "anthropic-messages", "xiaomi", "https://api.xiaomimimo.com", "/anthropic"),
	laneOverride("xiaomi-token-plan-ams", "anthropic-messages", "xiaomi-token-plan-ams", "https://token-plan-ams.xiaomimimo.com", "/anthropic"),
	laneOverride("xiaomi-token-plan-cn", "anthropic-messages", "xiaomi-token-plan-cn", "https://token-plan-cn.xiaomimimo.com", "/anthropic"),
	laneOverride("xiaomi-token-plan-sgp", "anthropic-messages", "xiaomi-token-plan-sgp", "https://token-plan-sgp.xiaomimimo.com", "/anthropic"),
	{Provider: "amazon-bedrock", API: "bedrock-converse-stream", Uncovered: "requests are AWS SigV4-signed to a regional host chosen per model, and the proxy has no Bedrock lane or parser"},
	{Provider: "azure-openai-responses", API: "azure-openai-responses", Uncovered: "its endpoint is the developer's own Azure resource URL, which has no built-in host"},
	{Provider: "google-vertex", API: "google-vertex", Uncovered: uncoveredTemplatedHost},
	{Provider: "cloudflare-ai-gateway", API: "mixed", Uncovered: uncoveredTemplatedHost},
	{Provider: "cloudflare-workers-ai", API: "openai-completions", Uncovered: uncoveredTemplatedHost},
	{Provider: "mistral", API: "mistral-conversations", Uncovered: "the proxy does not parse Mistral's conversations API, so a routed turn would carry no prompt guard or token capture"},
	{Provider: "openai-codex", API: "openai-codex-responses", Uncovered: "it is the ChatGPT sign-in backend; grounding it needs a ChatGPT sign-in, which D-423 excludes (no vendor credentials)"},
	{Provider: "github-copilot", API: "mixed", Uncovered: "a GitHub Copilot sign-in rewrites the provider's base URL from its token after models.json is applied, and its models use three APIs"},
	{Provider: "opencode", API: "mixed", Uncovered: uncoveredMixedBase},
	{Provider: "opencode-go", API: "mixed", Uncovered: uncoveredMixedBase},
}

// piOnlyProviderOverrides are built-in providers pi 0.74.2 bundles and
// Prime Agent 0.9.8 does not.
var piOnlyProviderOverrides = []ProviderOverride{
	laneOverride("together", "openai-completions", "together", "https://api.together.ai", "/v1"),
}

// primeAgentOnlyProviderOverrides are Prime Agent's own built-in providers.
var primeAgentOnlyProviderOverrides = []ProviderOverride{
	func() ProviderOverride {
		o := laneOverride("prime-inference", "openai-completions", "prime-inference", "https://api.pinference.ai", "/api/v1")
		o.Caveat = "private Prime Inference models granted to a signed-in account are added after models.json overrides apply, so they keep their own base URL"
		return o
	}(),
}

// joinProviderOverrides concatenates provider tables into a fresh slice so
// no two registry rows share a backing array.
func joinProviderOverrides(tables ...[]ProviderOverride) []ProviderOverride {
	var out []ProviderOverride
	for _, t := range tables {
		out = append(out, t...)
	}
	return out
}
