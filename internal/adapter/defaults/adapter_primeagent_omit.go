//go:build sbo_omit_adapter_prime_agent

package defaults

import "github.com/marmutapp/superbased-observer/internal/components"

// This build leaves internal/adapter/primeagent out: the binary reports
// adapter.prime-agent compiled out (L1), so every gate resolves it as not in this build.
func init() {
	components.MarkOmitted("adapter.prime-agent")
}
