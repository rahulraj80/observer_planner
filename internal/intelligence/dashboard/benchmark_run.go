//go:build !sbo_omit_benchmark

package dashboard

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/marmutapp/superbased-observer/internal/benchmark"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// benchmark_run.go is the dashboard twin of `observer benchmark run` and
// `observer benchmark delete` (backlog item 6, sweep row 10).
//
// A run is THE CLI COMMAND: the posted spec is checked with
// benchmark.ParseSpec (the parser the CLI uses), written to a file beside the
// config, and `observer benchmark run <file> [--confirm-spend] --config <cfg>`
// runs as a job. The spend gate is the CLI's own (dry run by default, budget
// caps, enforceBenchmarkBudget) plus one dashboard rule: a spend run needs a
// FINISHED dry run of the very same spec in this daemon, so the operator has
// seen the estimate for exactly what they confirm.
//
// A claude-code harness needs an isolated benchmark daemon. The spend run
// passes --isolate-if-needed, so the CLI stands one up for a spec that needs
// it even though the dashboard passes its own config, and copies the finished
// run (attempts, sessions, billed api_turns) into this node's database before
// the throwaway one is deleted (T-664). Codex runs record into this node's
// database directly. Both appear on the Benchmarks page.

const (
	benchmarkModeDryRun = benchmarkJobModePrefix + "dry-run"
	benchmarkModeRun    = benchmarkJobModePrefix + "run"
	// benchmarkSpecMax caps a posted spec (the shipped corpus is ~5 KiB).
	benchmarkSpecMax = 256 << 10
)

// benchmarkRunRequest is the POST /api/benchmarks/run body.
type benchmarkRunRequest struct {
	Spec         string `json:"spec"`
	ConfirmSpend bool   `json:"confirm_spend"`
	// DryRunJobID names the finished dry run a spend run rides on.
	DryRunJobID string `json:"dry_run_job_id"`
	// AllowUnpriced is `--allow-unpriced`: spend even when a config's model
	// has no billed history to estimate from. It rides on the spend run only;
	// a dry run never spends, so it has nothing to permit there.
	AllowUnpriced bool `json:"allow_unpriced"`
}

// handleBenchmarkRun serves POST /api/benchmarks/run.
func (s *Server) handleBenchmarkRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	// Review M3-05: a run spends API money; it carries the confirm token.
	if !requireJSONConfirm(w, r) {
		return
	}
	if s.opts.ConfigPath == "" {
		writeJSONStatus(w, http.StatusConflict, map[string]string{
			"error": "this dashboard was started without a config file, so it cannot run a benchmark",
		})
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, benchmarkSpecMax+4096))
	if err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "read body: " + err.Error()})
		return
	}
	var req benchmarkRunRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "decode body: " + err.Error()})
		return
	}
	spec, err := benchmark.ParseSpec(req.Spec)
	if err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	sum := sha256.Sum256([]byte(req.Spec))
	digest := hex.EncodeToString(sum[:])

	if req.ConfirmSpend {
		s.backfillMu.Lock()
		job, ok := s.backfillJobs[req.DryRunJobID]
		dryDigest := s.benchDryRuns[req.DryRunJobID]
		var status, mode string
		if ok {
			status, mode = job.Status, job.Mode
		}
		s.backfillMu.Unlock()
		if !ok || mode != benchmarkModeDryRun || status != "done" || dryDigest != digest {
			writeJSONStatus(w, http.StatusConflict, map[string]string{
				"error": "run a dry run of this exact spec first and review its estimate; the spend run needs it to have finished",
			})
			return
		}
	}

	dir := filepath.Join(filepath.Dir(s.opts.ConfigPath), "benchmark-specs")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		writeErr(w, fmt.Errorf("benchmark spec dir: %w", err))
		return
	}
	path := filepath.Join(dir, digest[:16]+".toml")
	if err := os.WriteFile(path, []byte(req.Spec), 0o600); err != nil {
		writeErr(w, fmt.Errorf("write benchmark spec: %w", err))
		return
	}

	mode := benchmarkModeDryRun
	args := []string{"benchmark", "run", path}
	if req.ConfirmSpend {
		mode = benchmarkModeRun
		args = append(args, "--confirm-spend", "--isolate-if-needed")
		if req.AllowUnpriced {
			args = append(args, "--allow-unpriced")
		}
	}
	job, err := s.startJob(mode, args)
	if err != nil {
		writeErr(w, err)
		return
	}
	if req.ConfirmSpend {
		s.recordManageAudit(r, "benchmark_run", fmt.Sprintf("spec=%s digest=%s job=%s allow_unpriced=%t", spec.Name, digest[:16], job.ID, req.AllowUnpriced))
	} else {
		s.backfillMu.Lock()
		if s.benchDryRuns == nil {
			s.benchDryRuns = map[string]string{}
		}
		s.benchDryRuns[job.ID] = digest
		s.backfillMu.Unlock()
	}
	writeJSON(w, map[string]any{
		"job_id":      job.ID,
		"mode":        mode,
		"status":      "running",
		"started_at":  job.StartedAt.Format(time.RFC3339),
		"spec_name":   spec.Name,
		"spec_digest": digest,
	})
}

// handleBenchmarkDelete serves POST /api/benchmarks/delete {"run_id"}: the
// same store.DeleteBenchmarkRun `observer benchmark delete` calls, after the
// same existence check.
func (s *Server) handleBenchmarkDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	// Review M3-05: deleting run rows carries the confirm token.
	if !requireJSONConfirm(w, r) {
		return
	}
	var req struct {
		RunID string `json:"run_id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil || req.RunID == "" {
		http.Error(w, "run_id required", http.StatusBadRequest)
		return
	}
	st := store.New(s.opts.DB)
	if _, ok, err := st.LoadBenchmarkRun(r.Context(), req.RunID); err != nil {
		writeErr(w, err)
		return
	} else if !ok {
		http.Error(w, "no benchmark run "+req.RunID, http.StatusNotFound)
		return
	}
	if err := st.DeleteBenchmarkRun(r.Context(), req.RunID); err != nil {
		writeErr(w, err)
		return
	}
	s.recordManageAudit(r, "benchmark_delete", "run="+req.RunID)
	writeJSON(w, map[string]any{"deleted": req.RunID})
}
