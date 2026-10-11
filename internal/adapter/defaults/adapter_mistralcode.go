//go:build !sbo_omit_adapter_mistral_code

package defaults

import (
	"github.com/marmutapp/superbased-observer/internal/adapter"
	"github.com/marmutapp/superbased-observer/internal/adapter/mistralcode"
)

// adapter.mistral-code (package internal/adapter/mistralcode). A build with any tag in the
// constraint above leaves the package out of the binary, and
// adapter_mistralcode_omit.go reports the component compiled out (L1).
func init() {
	register(35, func() adapter.Adapter { return mistralcode.New() })
}
