//go:build sbo_omit_adapter_aider

package defaults

import "github.com/marmutapp/superbased-observer/internal/components"

// This build leaves internal/adapter/aider out: the binary reports
// adapter.aider compiled out (L1), so every gate resolves it as not in this build.
func init() {
	components.MarkOmitted("adapter.aider")
}
