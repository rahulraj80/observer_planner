//go:build !sbo_omit_adapter_zed

package defaults

import (
	"github.com/marmutapp/superbased-observer/internal/adapter"
	"github.com/marmutapp/superbased-observer/internal/adapter/zed"
)

// adapter.zed (package internal/adapter/zed). A build with any tag in the
// constraint above leaves the package out of the binary, and
// adapter_zed_omit.go reports the component compiled out (L1).
func init() {
	register(40, func() adapter.Adapter { return zed.New() })
}
