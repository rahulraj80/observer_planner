//go:build sbo_omit_adapter_muse

package defaults

import "github.com/marmutapp/superbased-observer/internal/components"

// This build leaves internal/adapter/muse out: the binary reports
// adapter.muse compiled out (L1), so every gate resolves it as not in this build.
func init() {
	components.MarkOmitted("adapter.muse")
}
