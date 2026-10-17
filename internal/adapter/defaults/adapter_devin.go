//go:build !sbo_omit_adapter_devin

package defaults

import (
	"github.com/marmutapp/superbased-observer/internal/adapter"
	"github.com/marmutapp/superbased-observer/internal/adapter/devin"
)

// adapter.devin (package internal/adapter/devin). A build with any tag in the
// constraint above leaves the package out of the binary, and
// adapter_devin_omit.go reports the component compiled out (L1).
func init() {
	register(24, func() adapter.Adapter { return devin.New() })
}
