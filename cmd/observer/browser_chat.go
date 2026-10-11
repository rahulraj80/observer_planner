//go:build !sbo_omit_adapter_chatgpt_web && !sbo_omit_adapter_claude_web && !sbo_omit_adapter_perplexity_web && !sbo_omit_adapter_gemini_web && !sbo_omit_adapter_copilot_web

package main

import (
	"github.com/marmutapp/superbased-observer/internal/adapter/browserchat"
	"github.com/marmutapp/superbased-observer/internal/models"
	"github.com/marmutapp/superbased-observer/internal/scrub"
)

// normalizeBrowserChatTurn is the browser-chat adapter family's half of
// ingestBrowserTurn (adapter.chatgpt-web and its four bundled *-web rows,
// package internal/adapter/browserchat): built unless one of those rows is
// compiled out (lane MP-L1D, T-598), when browser_chat_omit.go refuses
// every turn instead.
func normalizeBrowserChatTurn(body []byte, ceiling string) ([]models.ToolEvent, []models.TokenEvent, error) {
	return browserchat.NormalizeWith(body, browserchat.Options{
		Scrubber:           scrub.New(),
		GranularityCeiling: browserchat.Granularity(ceiling),
	})
}

// browserChatIDFlags is peekBrowserTurn's adapter half: whether a turn is
// keyed under the synthesized session key, and whether its id_source
// normalizes to "none", each decided by the adapter's own rule.
func browserChatIDFlags(conversationID, messageID, idSource string) (synthetic, idSourceNone bool) {
	return browserchat.IsSyntheticSessionKey(conversationID, messageID),
		browserchat.NormalizeIDSource(idSource) == "none"
}
