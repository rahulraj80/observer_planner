//go:build sbo_omit_adapter_copilot

package defaults

import "github.com/marmutapp/superbased-observer/internal/components"

// This build leaves internal/adapter/copilot out: the binary reports
// adapter.copilot compiled out (L1), so every gate resolves it as not in this build.
func init() {
	components.MarkOmitted("adapter.copilot")
}
