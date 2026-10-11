//go:build sbo_omit_adapter_freebuff

package defaults

import "github.com/marmutapp/superbased-observer/internal/components"

// This build leaves internal/adapter/freebuff out: the binary reports
// adapter.freebuff compiled out (L1), so every gate resolves it as not in this build.
func init() {
	components.MarkOmitted("adapter.freebuff")
}
