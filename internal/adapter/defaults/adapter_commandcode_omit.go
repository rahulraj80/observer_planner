//go:build sbo_omit_adapter_command_code

package defaults

import "github.com/marmutapp/superbased-observer/internal/components"

// This build leaves internal/adapter/commandcode out: the binary reports
// adapter.command-code compiled out (L1), so every gate resolves it as not in this build.
func init() {
	components.MarkOmitted("adapter.command-code")
}
