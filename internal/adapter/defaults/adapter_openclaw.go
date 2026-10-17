//go:build !sbo_omit_adapter_openclaw

package defaults

import (
	"github.com/marmutapp/superbased-observer/internal/adapter"
	"github.com/marmutapp/superbased-observer/internal/adapter/openclaw"
)

// adapter.openclaw (package internal/adapter/openclaw). A build with any tag in the
// constraint above leaves the package out of the binary, and
// adapter_openclaw_omit.go reports the component compiled out (L1).
func init() {
	register(10, func() adapter.Adapter { return openclaw.New() })
}
