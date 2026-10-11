//go:build sbo_omit_adapter_devin

package defaults

import "github.com/marmutapp/superbased-observer/internal/components"

// This build leaves internal/adapter/devin out: the binary reports
// adapter.devin compiled out (L1), so every gate resolves it as not in this build.
func init() {
	components.MarkOmitted("adapter.devin")
}
