//go:build sbo_omit_adapter_hermes

package defaults

import "github.com/marmutapp/superbased-observer/internal/components"

// This build leaves internal/adapter/hermes out: the binary reports
// adapter.hermes compiled out (L1), so every gate resolves it as not in this build.
func init() {
	components.MarkOmitted("adapter.hermes")
}
