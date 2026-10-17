//go:build !sbo_omit_adapter_chatgpt_web && !sbo_omit_adapter_claude_web && !sbo_omit_adapter_perplexity_web && !sbo_omit_adapter_gemini_web && !sbo_omit_adapter_copilot_web

package main

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/adapter/browserchat"
)

// TestBrowserGranularityMatchesTheAdapter pins browser.go's spelled-out
// granularity vocabulary to internal/adapter/browserchat's constants.
func TestBrowserGranularityMatchesTheAdapter(t *testing.T) {
	for got, want := range map[string]browserchat.Granularity{
		browserGranularityUsageOnly: browserchat.GranularityUsageOnly,
		browserGranularityRedacted:  browserchat.GranularityRedacted,
		browserGranularityFull:      browserchat.GranularityFull,
	} {
		if got != string(want) {
			t.Errorf("browser.go spells %q, the adapter %q", got, want)
		}
	}
}
