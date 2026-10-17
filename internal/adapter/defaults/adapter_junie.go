//go:build !sbo_omit_adapter_junie

package defaults

import (
	"github.com/marmutapp/superbased-observer/internal/adapter"
	"github.com/marmutapp/superbased-observer/internal/adapter/junie"
)

// adapter.junie (package internal/adapter/junie). A build with any tag in the
// constraint above leaves the package out of the binary, and
// adapter_junie_omit.go reports the component compiled out (L1).
func init() {
	register(33, func() adapter.Adapter { return junie.New() })
}
