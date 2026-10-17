//go:build sbo_omit_adapter_crush

package defaults

import "github.com/marmutapp/superbased-observer/internal/components"

// This build leaves internal/adapter/crush out: the binary reports
// adapter.crush compiled out (L1), so every gate resolves it as not in this build.
func init() {
	components.MarkOmitted("adapter.crush")
}
