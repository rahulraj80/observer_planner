//go:build !sbo_omit_adapter_kilo_code && !sbo_omit_adapter_kilo_code_cli && !sbo_omit_adapter_cline

package defaults

import (
	"github.com/marmutapp/superbased-observer/internal/adapter"
	"github.com/marmutapp/superbased-observer/internal/adapter/kilocode"
)

// adapter.kilo-code and adapter.kilo-code-cli (package internal/adapter/kilocode). A build with any tag in the
// constraint above leaves the package out of the binary, and
// adapter_kilocode_omit.go reports the component compiled out (L1).
func init() {
	register(17, func() adapter.Adapter { return kilocode.NewLegacy() })
	register(18, func() adapter.Adapter { return kilocode.NewCLI() })
}
