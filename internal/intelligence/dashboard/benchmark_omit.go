//go:build sbo_omit_benchmark

package dashboard

import "net/http"

// benchmark_omit.go is the off side of benchmarks.go, benchmark_run.go,
// arena.go and arena_models.go: in a `-tags sbo_omit_benchmark` build
// (component benchmark compiled out) the benchmark and Agent Arena routes
// stay registered and answer 404 component_not_installed, reason
// not_compiled, and this package does not import internal/benchmark,
// internal/arena or internal/arenamodels (lane MP-L1E, T-715).

// handleBenchmarks refuses: benchmark is not in this build.
func (s *Server) handleBenchmarks(w http.ResponseWriter, _ *http.Request) {
	writeCompiledOut(w, "benchmark")
}

// handleBenchmarkDetail refuses: benchmark is not in this build.
func (s *Server) handleBenchmarkDetail(w http.ResponseWriter, _ *http.Request) {
	writeCompiledOut(w, "benchmark")
}

// handleBenchmarkRun refuses: benchmark is not in this build.
func (s *Server) handleBenchmarkRun(w http.ResponseWriter, _ *http.Request) {
	writeCompiledOut(w, "benchmark")
}

// handleBenchmarkDelete refuses: benchmark is not in this build.
func (s *Server) handleBenchmarkDelete(w http.ResponseWriter, _ *http.Request) {
	writeCompiledOut(w, "benchmark")
}

// handleArenaRuns refuses: benchmark (Agent Arena) is not in this build.
func (s *Server) handleArenaRuns(w http.ResponseWriter, _ *http.Request) {
	writeCompiledOut(w, "benchmark")
}

// handleArenaModels refuses: benchmark (Agent Arena) is not in this build.
func (s *Server) handleArenaModels(w http.ResponseWriter, _ *http.Request) {
	writeCompiledOut(w, "benchmark")
}

// handleArenaRunDetail refuses: benchmark (Agent Arena) is not in this build.
func (s *Server) handleArenaRunDetail(w http.ResponseWriter, _ *http.Request, _ string) {
	writeCompiledOut(w, "benchmark")
}

// handleArenaRunDiff refuses: benchmark (Agent Arena) is not in this build.
func (s *Server) handleArenaRunDiff(w http.ResponseWriter, _ *http.Request, _, _ string) {
	writeCompiledOut(w, "benchmark")
}

// handleArenaCandidateAction refuses: benchmark (Agent Arena) is not in this build.
func (s *Server) handleArenaCandidateAction(w http.ResponseWriter, _ *http.Request, _, _ string) {
	writeCompiledOut(w, "benchmark")
}
