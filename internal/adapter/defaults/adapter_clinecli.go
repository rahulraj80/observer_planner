//go:build !sbo_omit_adapter_cline_cli

package defaults

import (
	"github.com/marmutapp/superbased-observer/internal/adapter"
	"github.com/marmutapp/superbased-observer/internal/adapter/clinecli"
)

// adapter.cline-cli (package internal/adapter/clinecli). A build with any tag in the
// constraint above leaves the package out of the binary, and
// adapter_clinecli_omit.go reports the component compiled out (L1).
func init() {
	register(4, func() adapter.Adapter { return clinecli.New() })
}
