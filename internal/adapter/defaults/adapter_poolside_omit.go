//go:build sbo_omit_adapter_poolside

package defaults

import "github.com/marmutapp/superbased-observer/internal/components"

// This build leaves internal/adapter/poolside out: the binary reports
// adapter.poolside compiled out (L1), so every gate resolves it as not in this build.
func init() {
	components.MarkOmitted("adapter.poolside")
}
