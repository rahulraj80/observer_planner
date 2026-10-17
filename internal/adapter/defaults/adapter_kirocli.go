//go:build !sbo_omit_adapter_kiro_cli

package defaults

import (
	"github.com/marmutapp/superbased-observer/internal/adapter"
	"github.com/marmutapp/superbased-observer/internal/adapter/kirocli"
)

// adapter.kiro-cli (package internal/adapter/kirocli). A build with any tag in the
// constraint above leaves the package out of the binary, and
// adapter_kirocli_omit.go reports the component compiled out (L1).
func init() {
	register(20, func() adapter.Adapter { return kirocli.New() })
}
