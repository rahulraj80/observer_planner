//go:build sbo_omit_adapter_cursor

package defaults

import "github.com/marmutapp/superbased-observer/internal/components"

// This build leaves internal/adapter/cursor out: the binary reports
// adapter.cursor compiled out (L1), so every gate resolves it as not in this build.
func init() {
	components.MarkOmitted("adapter.cursor")
}
