//go:build sbo_omit_adapter_antigravity || sbo_omit_adapter_antigravity_cli

package defaults

import "github.com/marmutapp/superbased-observer/internal/components"

// This build leaves internal/adapter/antigravity out: the binary reports
// adapter.antigravity and adapter.antigravity-cli compiled out (L1), so every gate resolves it as not in this build.
func init() {
	components.MarkOmitted("adapter.antigravity")
	components.MarkOmitted("adapter.antigravity-cli")
}
