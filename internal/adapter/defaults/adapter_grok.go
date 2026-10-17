//go:build !sbo_omit_adapter_grok

package defaults

import (
	"github.com/marmutapp/superbased-observer/internal/adapter"
	"github.com/marmutapp/superbased-observer/internal/adapter/grok"
)

// adapter.grok (package internal/adapter/grok). A build with any tag in the
// constraint above leaves the package out of the binary, and
// adapter_grok_omit.go reports the component compiled out (L1).
func init() {
	register(23, func() adapter.Adapter { return grok.New() })
}
