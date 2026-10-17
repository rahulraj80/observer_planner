//go:build !sbo_omit_adapter_copilot

package defaults

import (
	"github.com/marmutapp/superbased-observer/internal/adapter"
	"github.com/marmutapp/superbased-observer/internal/adapter/copilot"
)

// adapter.copilot (package internal/adapter/copilot). A build with any tag in the
// constraint above leaves the package out of the binary, and
// adapter_copilot_omit.go reports the component compiled out (L1).
func init() {
	register(5, func() adapter.Adapter { return copilot.New() })
}
