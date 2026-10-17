//go:build !sbo_omit_adapter_cline

package defaults

import (
	"github.com/marmutapp/superbased-observer/internal/adapter"
	"github.com/marmutapp/superbased-observer/internal/adapter/cline"
)

// adapter.cline (package internal/adapter/cline). A build with any tag in the
// constraint above leaves the package out of the binary, and
// adapter_cline_omit.go reports the component compiled out (L1).
func init() {
	register(3, func() adapter.Adapter { return cline.New() })
}
