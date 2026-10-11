//go:build !sbo_omit_processobs

package dashboard

import "github.com/marmutapp/superbased-observer/internal/config"

// fixedCapability returns a processCapabilityFn that ignores the ProcessConfig
// and always reports the given capability: the injected-IO seam that lets the
// enable-capture tests exercise every GOOS-shaped capability set without the
// real platform. It carries !sbo_omit_processobs like processCapability
// (process_enable.go); moved from testhelpers_shared_test.go (REVIEW-4 P4-08).
func fixedCapability(c processCapability) func(config.ProcessConfig) processCapability {
	return func(config.ProcessConfig) processCapability { return c }
}
