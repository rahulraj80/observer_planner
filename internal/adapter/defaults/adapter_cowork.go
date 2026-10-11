//go:build !sbo_omit_adapter_cowork

package defaults

import (
	"github.com/marmutapp/superbased-observer/internal/adapter"
	"github.com/marmutapp/superbased-observer/internal/adapter/cowork"
)

// adapter.cowork (package internal/adapter/cowork). A build with any tag in the
// constraint above leaves the package out of the binary, and
// adapter_cowork_omit.go reports the component compiled out (L1).
func init() {
	register(7, func() adapter.Adapter { return cowork.New() })
}
