//go:build sbo_omit_adapter_pi

package defaults

import "github.com/marmutapp/superbased-observer/internal/components"

// This build leaves internal/adapter/pi out: the binary reports
// adapter.pi compiled out (L1), so every gate resolves it as not in this build.
func init() {
	components.MarkOmitted("adapter.pi")
}
