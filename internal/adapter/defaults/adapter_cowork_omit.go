//go:build sbo_omit_adapter_cowork

package defaults

import "github.com/marmutapp/superbased-observer/internal/components"

// This build leaves internal/adapter/cowork out: the binary reports
// adapter.cowork compiled out (L1), so every gate resolves it as not in this build.
func init() {
	components.MarkOmitted("adapter.cowork")
}
