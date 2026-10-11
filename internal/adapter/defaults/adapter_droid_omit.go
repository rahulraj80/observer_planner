//go:build sbo_omit_adapter_droid

package defaults

import "github.com/marmutapp/superbased-observer/internal/components"

// This build leaves internal/adapter/droid out: the binary reports
// adapter.droid compiled out (L1), so every gate resolves it as not in this build.
func init() {
	components.MarkOmitted("adapter.droid")
}
