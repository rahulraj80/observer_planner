//go:build !sbo_omit_adapter_opencode

package defaults

import (
	"github.com/marmutapp/superbased-observer/internal/adapter"
	"github.com/marmutapp/superbased-observer/internal/adapter/opencode"
)

// adapter.opencode (package internal/adapter/opencode). A build with any tag in the
// constraint above leaves the package out of the binary, and
// adapter_opencode_omit.go reports the component compiled out (L1).
func init() {
	register(11, func() adapter.Adapter { return opencode.New() })
}
