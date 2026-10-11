//go:build !sbo_omit_adapter_command_code

package defaults

import (
	"github.com/marmutapp/superbased-observer/internal/adapter"
	"github.com/marmutapp/superbased-observer/internal/adapter/commandcode"
)

// adapter.command-code (package internal/adapter/commandcode). A build with any tag in the
// constraint above leaves the package out of the binary, and
// adapter_commandcode_omit.go reports the component compiled out (L1).
func init() {
	register(30, func() adapter.Adapter { return commandcode.New() })
}
