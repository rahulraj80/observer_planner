//go:build sbo_omit_benchmark

package main

import (
	"github.com/spf13/cobra"

	"github.com/marmutapp/superbased-observer/internal/components"
)

// benchmark_omit.go stands in for the benchmark*.go wiring in a build
// without the benchmark component (-tags sbo_omit_benchmark, lane MP-L1E,
// T-715): internal/benchmark, internal/arena and internal/arenamodels are not
// linked. `observer benchmark` answers "not in this build" (exit 3) and the
// benchmark and Agent Arena routes answer 404 component_not_installed.
func init() { components.MarkOmitted("benchmark") }

// newBenchmarkCmd registers `observer benchmark` as a compiled-out verb.
func newBenchmarkCmd() *cobra.Command {
	return compiledOutVerb("benchmark", "Benchmarks and Agent Arena (not in this build)", "benchmark")
}
