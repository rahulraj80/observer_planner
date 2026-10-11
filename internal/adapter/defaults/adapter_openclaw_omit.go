//go:build sbo_omit_adapter_openclaw

package defaults

import "github.com/marmutapp/superbased-observer/internal/components"

// This build leaves internal/adapter/openclaw out: the binary reports
// adapter.openclaw compiled out (L1), so every gate resolves it as not in this build.
func init() {
	components.MarkOmitted("adapter.openclaw")
}
