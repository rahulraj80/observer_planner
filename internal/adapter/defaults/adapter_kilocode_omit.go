//go:build sbo_omit_adapter_kilo_code || sbo_omit_adapter_kilo_code_cli || sbo_omit_adapter_cline

package defaults

import "github.com/marmutapp/superbased-observer/internal/components"

// This build leaves internal/adapter/kilocode out: the binary reports
// adapter.kilo-code and adapter.kilo-code-cli compiled out (L1), so every gate resolves it as not in this build.
func init() {
	components.MarkOmitted("adapter.kilo-code")
	components.MarkOmitted("adapter.kilo-code-cli")
}
