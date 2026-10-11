//go:build sbo_omit_adapter_chatgpt_web || sbo_omit_adapter_claude_web || sbo_omit_adapter_perplexity_web || sbo_omit_adapter_gemini_web || sbo_omit_adapter_copilot_web

package defaults

import "github.com/marmutapp/superbased-observer/internal/components"

// This build leaves internal/adapter/browserchat out: the binary reports
// the five browser-chat rows compiled out (L1), so every gate resolves them
// as not in this build.
func init() {
	components.MarkOmitted("adapter.chatgpt-web")
	components.MarkOmitted("adapter.claude-web")
	components.MarkOmitted("adapter.perplexity-web")
	components.MarkOmitted("adapter.gemini-web")
	components.MarkOmitted("adapter.copilot-web")
}
