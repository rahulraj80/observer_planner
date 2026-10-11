//go:build !sbo_omit_adapter_hermes

package defaults

import (
	"github.com/marmutapp/superbased-observer/internal/adapter"
	"github.com/marmutapp/superbased-observer/internal/adapter/hermes"
)

// adapter.hermes (package internal/adapter/hermes). A build with any tag in the
// constraint above leaves the package out of the binary, and
// adapter_hermes_omit.go reports the component compiled out (L1).
func init() {
	register(16, func() adapter.Adapter { return hermes.New() })
}
