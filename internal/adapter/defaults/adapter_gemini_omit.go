//go:build sbo_omit_adapter_gemini_cli

package defaults

import "github.com/marmutapp/superbased-observer/internal/components"

// This build leaves internal/adapter/gemini out: the binary reports
// adapter.gemini-cli compiled out (L1), so every gate resolves it as not in this build.
func init() {
	components.MarkOmitted("adapter.gemini-cli")
}
