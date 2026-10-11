//go:build !sbo_omit_adapter_copilot_cli

package defaults

import (
	"github.com/marmutapp/superbased-observer/internal/adapter"
	"github.com/marmutapp/superbased-observer/internal/adapter/copilotcli"
)

// adapter.copilot-cli (package internal/adapter/copilotcli). A build with any tag in the
// constraint above leaves the package out of the binary, and
// adapter_copilotcli_omit.go reports the component compiled out (L1).
func init() {
	register(6, func() adapter.Adapter { return copilotcli.New() })
}
