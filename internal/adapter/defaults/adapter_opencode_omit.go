//go:build sbo_omit_adapter_opencode

package defaults

import "github.com/marmutapp/superbased-observer/internal/components"

// This build leaves internal/adapter/opencode out: the binary reports
// adapter.opencode compiled out (L1), so every gate resolves it as not in this build.
func init() {
	components.MarkOmitted("adapter.opencode")
}
