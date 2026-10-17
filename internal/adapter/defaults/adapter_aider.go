//go:build !sbo_omit_adapter_aider

package defaults

import (
	"github.com/marmutapp/superbased-observer/internal/adapter"
	"github.com/marmutapp/superbased-observer/internal/adapter/aider"
)

// adapter.aider (package internal/adapter/aider). A build with any tag in the
// constraint above leaves the package out of the binary, and
// adapter_aider_omit.go reports the component compiled out (L1).
func init() {
	register(26, func() adapter.Adapter { return aider.New() })
}
