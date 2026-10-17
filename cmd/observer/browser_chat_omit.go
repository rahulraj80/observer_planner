//go:build sbo_omit_adapter_chatgpt_web || sbo_omit_adapter_claude_web || sbo_omit_adapter_perplexity_web || sbo_omit_adapter_gemini_web || sbo_omit_adapter_copilot_web

package main

import (
	"errors"

	"github.com/marmutapp/superbased-observer/internal/models"
)

// normalizeBrowserChatTurn refuses every captured turn: this build compiled
// the browser-chat adapters out, so the bridge records the drop (with this
// reason, in browser-health.json) and stores nothing.
func normalizeBrowserChatTurn([]byte, string) ([]models.ToolEvent, []models.TokenEvent, error) {
	return nil, nil, errors.New("the browser-chat adapters (adapter.chatgpt-web and its bundled *-web rows) are not in this build")
}

// browserChatIDFlags has no adapter rule to apply; the flags only qualify an
// ingested turn, and this build ingests none.
func browserChatIDFlags(string, string, string) (synthetic, idSourceNone bool) {
	return false, false
}
