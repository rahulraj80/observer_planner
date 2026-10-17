//go:build sbo_omit_adapter_deepseek

package defaults

import "github.com/marmutapp/superbased-observer/internal/components"

// This build leaves internal/adapter/deepseek out: the binary reports
// adapter.deepseek compiled out (L1), so every gate resolves it as not in this build.
func init() {
	components.MarkOmitted("adapter.deepseek")
}
