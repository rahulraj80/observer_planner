//go:build sbo_omit_adapter_kiro_crew

package defaults

import "github.com/marmutapp/superbased-observer/internal/components"

// This build leaves internal/adapter/kirocrew out: the binary reports
// adapter.kiro-crew compiled out (L1), so every gate resolves it as not in this build.
func init() {
	components.MarkOmitted("adapter.kiro-crew")
}
