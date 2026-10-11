//go:build sbo_omit_adapter_copilot_cli

package defaults

import "github.com/marmutapp/superbased-observer/internal/components"

// This build leaves internal/adapter/copilotcli out: the binary reports
// adapter.copilot-cli compiled out (L1), so every gate resolves it as not in this build.
func init() {
	components.MarkOmitted("adapter.copilot-cli")
}
