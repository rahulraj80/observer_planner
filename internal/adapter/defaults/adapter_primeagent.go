//go:build !sbo_omit_adapter_prime_agent

package defaults

import (
	"github.com/marmutapp/superbased-observer/internal/adapter"
	"github.com/marmutapp/superbased-observer/internal/adapter/primeagent"
)

// adapter.prime-agent (package internal/adapter/primeagent). A build with any tag in the
// constraint above leaves the package out of the binary, and
// adapter_primeagent_omit.go reports the component compiled out (L1).
func init() {
	register(32, func() adapter.Adapter { return primeagent.New() })
}
