//go:build !sbo_omit_adapter_kiro_crew

package defaults

import (
	"github.com/marmutapp/superbased-observer/internal/adapter"
	"github.com/marmutapp/superbased-observer/internal/adapter/kirocrew"
)

// adapter.kiro-crew (package internal/adapter/kirocrew). A build with any tag in the
// constraint above leaves the package out of the binary, and
// adapter_kirocrew_omit.go reports the component compiled out (L1).
func init() {
	register(38, func() adapter.Adapter { return kirocrew.New() })
}
