//go:build !sbo_omit_adapter_crush

package defaults

import (
	"github.com/marmutapp/superbased-observer/internal/adapter"
	"github.com/marmutapp/superbased-observer/internal/adapter/crush"
)

// adapter.crush (package internal/adapter/crush). A build with any tag in the
// constraint above leaves the package out of the binary, and
// adapter_crush_omit.go reports the component compiled out (L1).
func init() {
	register(21, func() adapter.Adapter { return crush.New() })
}
