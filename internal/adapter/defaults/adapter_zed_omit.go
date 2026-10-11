//go:build sbo_omit_adapter_zed

package defaults

import "github.com/marmutapp/superbased-observer/internal/components"

// This build leaves internal/adapter/zed out: the binary reports
// adapter.zed compiled out (L1), so every gate resolves it as not in this build.
func init() {
	components.MarkOmitted("adapter.zed")
}
