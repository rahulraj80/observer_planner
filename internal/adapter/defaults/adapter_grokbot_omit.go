//go:build sbo_omit_adapter_grokbot

package defaults

import "github.com/marmutapp/superbased-observer/internal/components"

// This build leaves internal/adapter/grokbot out: the binary reports
// adapter.grokbot compiled out (L1), so every gate resolves it as not in this build.
func init() {
	components.MarkOmitted("adapter.grokbot")
}
