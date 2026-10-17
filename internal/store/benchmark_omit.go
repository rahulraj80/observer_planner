//go:build sbo_omit_benchmark

package store

import (
	"context"

	"github.com/marmutapp/superbased-observer/internal/orgcontract"
)

// benchmark_omit.go is the off side of benchmark.go and benchmarkorgrows.go
// in a `-tags sbo_omit_benchmark` build (lane MP-L1E, T-715): the store does
// not import internal/benchmark, and the org push carries no benchmark rows
// (a build without the component runs none). The benchmark_* tables stay in
// the schema (migrations are never build-tagged).

// SelectBenchmarkOrgRows returns no rows: benchmark is not in this build.
func (s *Store) SelectBenchmarkOrgRows(context.Context) ([]orgcontract.BenchmarkRunRow, []orgcontract.BenchmarkAttemptRow, error) {
	return nil, nil, nil
}
