//go:build !sbo_omit_adapter_chatgpt_web && !sbo_omit_adapter_claude_web && !sbo_omit_adapter_perplexity_web && !sbo_omit_adapter_gemini_web && !sbo_omit_adapter_copilot_web

package defaults

import (
	"github.com/marmutapp/superbased-observer/internal/adapter"
	"github.com/marmutapp/superbased-observer/internal/adapter/browserchat"
	"github.com/marmutapp/superbased-observer/internal/models"
)

// adapter.chatgpt-web, adapter.claude-web, adapter.perplexity-web,
// adapter.gemini-web and adapter.copilot-web (package
// internal/adapter/browserchat). A build with any tag in the constraint
// above leaves the package out of the binary, and
// adapter_browserchat_omit.go reports the five components compiled out
// (L1).
func init() {
	// Browser-chatbot rail. Hook-only: no WatchPaths (capture arrives via
	// the browser extension's native-messaging bridge / loopback listener,
	// not the file watcher). One adapter per *-web site; the site is a DATA
	// discriminator inside browserchat, so these share a single package +
	// normalizer. Registered here so the integration-registry coverage
	// guardrail is satisfied and each tool name is declared.
	register(41, func() adapter.Adapter { return browserchat.New() }) // chatgpt-web
	register(42, func() adapter.Adapter { return browserchat.NewFor(models.ToolClaudeWeb) })
	register(43, func() adapter.Adapter { return browserchat.NewFor(models.ToolPerplexityWeb) })
	register(44, func() adapter.Adapter { return browserchat.NewFor(models.ToolGeminiWeb) })
	register(45, func() adapter.Adapter { return browserchat.NewFor(models.ToolCopilotWeb) })
}
