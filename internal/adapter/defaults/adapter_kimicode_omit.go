//go:build sbo_omit_adapter_kimi_code

package defaults

import "github.com/marmutapp/superbased-observer/internal/components"

// This build leaves internal/adapter/kimicode out: the binary reports
// adapter.kimi-code compiled out (L1), so every gate resolves it as not in this build.
func init() {
	components.MarkOmitted("adapter.kimi-code")
}
