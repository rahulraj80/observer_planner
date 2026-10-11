//go:build sbo_omit_adapter_codex || sbo_omit_adapter_open_interpreter

package defaults

import "github.com/marmutapp/superbased-observer/internal/components"

// This build leaves internal/adapter/codex out: the binary reports
// adapter.codex and adapter.open-interpreter compiled out (L1), so every gate resolves it as not in this build.
func init() {
	components.MarkOmitted("adapter.codex")
	components.MarkOmitted("adapter.open-interpreter")
}
