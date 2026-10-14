package planlimits

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/integration"
)

// TestProviderToolsAreRegistryTools pins every provider's Tools entry to a
// row of the integration capability registry, so a renamed tool id cannot
// silently drop a plan's "estimated from your usage" spend.
func TestProviderToolsAreRegistryTools(t *testing.T) {
	known := map[string]bool{}
	for _, tool := range integration.Tools() {
		known[tool] = true
	}
	for _, p := range Providers {
		for _, tool := range p.Tools {
			if !known[tool] {
				t.Errorf("provider %s names tool %q, which is not in the integration registry", p.ID, tool)
			}
		}
	}
	for tool := range PassiveLogTools {
		if !known[tool] {
			t.Errorf("passive log tool %q is not in the integration registry", tool)
		}
	}
}
