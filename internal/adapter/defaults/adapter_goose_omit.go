//go:build sbo_omit_adapter_goose

package defaults

import "github.com/marmutapp/superbased-observer/internal/components"

// This build leaves internal/adapter/goose out: the binary reports
// adapter.goose compiled out (L1), so every gate resolves it as not in this build.
func init() {
	components.MarkOmitted("adapter.goose")
}
