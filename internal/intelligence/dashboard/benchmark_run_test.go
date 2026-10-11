//go:build !sbo_omit_benchmark

package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

const testBenchSpec = `name = "ui-spec"
repeats = 1
[budget]
  max_total_usd = 1.0
  max_cell_usd = 0.5
[analysis]
  baseline_config = "codex-a"
[[tasks]]
  id = "t1"
  repo = "https://example.com/r.git"
  ref = "abc"
  prompt = "do it"
  [tasks.success]
    scorer = "contains"
    value = "ok"
[[configs]]
  id = "codex-a"
  harness = "codex"
  model = "gpt-5.6-sol"
`

type benchRunResp struct {
	JobID      string `json:"job_id"`
	Mode       string `json:"mode"`
	SpecDigest string `json:"spec_digest"`
	Error      string `json:"error"`
}

func postBench(t *testing.T, srv *Server, body string) (int, benchRunResp) {
	t.Helper()
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, armLocalWrite(httptest.NewRequest(http.MethodPost, "/api/benchmarks/run", strings.NewReader(body))))
	var out benchRunResp
	_ = json.NewDecoder(rr.Body).Decode(&out)
	return rr.Code, out
}

func benchBody(t *testing.T, spec string, spend bool, dryRunJob string) string {
	t.Helper()
	b, err := json.Marshal(map[string]any{"spec": spec, "confirm_spend": spend, "dry_run_job_id": dryRunJob})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestBenchmarkRun is the dashboard twin of `observer benchmark run` (sweep
// row 10): the spec is validated with benchmark.ParseSpec, written to a file,
// and the CLI itself runs it as a job. A spend run is refused until a dry run
// of the SAME spec has finished in this daemon, and the spend is audited.
func TestBenchmarkRun(t *testing.T) {
	mu := make(chan []string, 4)
	fake := func(_ context.Context, args []string, onChunk func([]byte)) (int, error) {
		mu <- append([]string(nil), args...)
		onChunk([]byte("estimate: $0.40\n"))
		return 0, nil
	}
	srv := newServerWithFakeExec(t, fake)

	if code, out := postBench(t, srv, benchBody(t, "name = ", false, "")); code != http.StatusBadRequest || out.Error == "" {
		t.Fatalf("bad spec = %d %+v, want 400 with the parse error", code, out)
	}

	// Spend without a dry run first: refused, nothing runs.
	if code, _ := postBench(t, srv, benchBody(t, testBenchSpec, true, "")); code != http.StatusConflict {
		t.Fatalf("spend without dry run = %d, want 409", code)
	}

	code, dry := postBench(t, srv, benchBody(t, testBenchSpec, false, ""))
	if code != http.StatusOK || dry.Mode != "benchmark-dry-run" || dry.SpecDigest == "" {
		t.Fatalf("dry run = %d %+v", code, dry)
	}
	args := <-mu
	if len(args) < 3 || args[0] != "benchmark" || args[1] != "run" || (strings.Contains(strings.Join(args, " "), "--confirm-spend") || strings.Contains(strings.Join(args, " "), "--isolate-if-needed")) {
		t.Fatalf("dry-run argv = %v", args)
	}
	specFile := args[2]
	if b, err := os.ReadFile(specFile); err != nil || string(b) != testBenchSpec {
		t.Fatalf("spec file %s = %q (%v)", specFile, b, err)
	}
	pollCommandJob(t, srv, dry.JobID)

	// A different spec cannot ride on that dry run.
	other := strings.Replace(testBenchSpec, "ui-spec", "other", 1)
	if code, _ := postBench(t, srv, benchBody(t, other, true, dry.JobID)); code != http.StatusConflict {
		t.Fatalf("spend with another spec's dry run = %d, want 409", code)
	}

	code, run := postBench(t, srv, benchBody(t, testBenchSpec, true, dry.JobID))
	if code != http.StatusOK || run.Mode != "benchmark-run" {
		t.Fatalf("spend = %d %+v", code, run)
	}
	args = <-mu
	if !strings.Contains(strings.Join(args, " "), "--confirm-spend") {
		t.Fatalf("spend argv = %v", args)
	}
	// T-664: the spend run asks the CLI for an isolated daemon when the spec
	// needs one, so a claude-code run is not refused for the dashboard's own
	// --config; the dry run never stands one up.
	if !strings.Contains(strings.Join(args, " "), "--isolate-if-needed") {
		t.Fatalf("spend argv = %v, want --isolate-if-needed", args)
	}
	var n int
	if err := srv.opts.DB.QueryRow(`SELECT COUNT(*) FROM remote_audit WHERE detail LIKE 'benchmark_run%'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("audit rows = %d, want 1 (only the spend run)", n)
	}
	select {
	case extra := <-mu:
		t.Errorf("unexpected extra run %v", extra)
	case <-time.After(50 * time.Millisecond):
	}
}

// TestBenchmarkDelete is `observer benchmark delete <run-id>`: the same
// store.DeleteBenchmarkRun, 404 for an unknown run, audited.
func TestBenchmarkDelete(t *testing.T) {
	srv := newServerWithFakeExec(t, nil)
	runID := seedBenchmarkRun(t, srv)
	del := func(id string) int {
		rr := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rr, armLocalWrite(httptest.NewRequest(http.MethodPost, "/api/benchmarks/delete",
			strings.NewReader(`{"run_id":`+jsonEscape(id)+`}`))))
		return rr.Code
	}
	if code := del("no-such-run"); code != http.StatusNotFound {
		t.Errorf("unknown run = %d, want 404", code)
	}
	if code := del(runID); code != http.StatusOK {
		t.Fatalf("delete = %d", code)
	}
	if code, _ := benchmarkGET(t, srv, "/api/benchmarks/"+runID); code != http.StatusNotFound {
		t.Errorf("run still readable after delete: %d", code)
	}
	var n int
	if err := srv.opts.DB.QueryRow(`SELECT COUNT(*) FROM remote_audit WHERE detail LIKE 'benchmark_delete%'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("audit rows = %d, want 1", n)
	}
}

// TestBenchmarkRunAllowUnpriced: the CLI refuses to spend on a model with no
// billed history unless --allow-unpriced is passed. The dashboard carries the
// same opt-in on the spend run only (a dry run never spends), and the audit
// row records it.
func TestBenchmarkRunAllowUnpriced(t *testing.T) {
	runs := make(chan []string, 4)
	fake := func(_ context.Context, args []string, onChunk func([]byte)) (int, error) {
		runs <- append([]string(nil), args...)
		onChunk([]byte("ok\n"))
		return 0, nil
	}
	srv := newServerWithFakeExec(t, fake)
	body := func(spend, allow bool, dry string) string {
		b, err := json.Marshal(map[string]any{
			"spec": testBenchSpec, "confirm_spend": spend, "dry_run_job_id": dry, "allow_unpriced": allow,
		})
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	code, dry := postBench(t, srv, body(false, true, ""))
	if code != http.StatusOK {
		t.Fatalf("dry run = %d", code)
	}
	if args := strings.Join(<-runs, " "); strings.Contains(args, "--allow-unpriced") {
		t.Errorf("dry-run argv carries --allow-unpriced: %s", args)
	}
	pollCommandJob(t, srv, dry.JobID)
	if code, _ := postBench(t, srv, body(true, true, dry.JobID)); code != http.StatusOK {
		t.Fatalf("spend = %d", code)
	}
	if args := strings.Join(<-runs, " "); !strings.Contains(args, "--confirm-spend") || !strings.Contains(args, "--allow-unpriced") {
		t.Errorf("spend argv = %s, want --confirm-spend and --allow-unpriced", args)
	}
	var detail string
	if err := srv.opts.DB.QueryRow(`SELECT detail FROM remote_audit WHERE detail LIKE 'benchmark_run%'`).Scan(&detail); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(detail, "allow_unpriced=true") {
		t.Errorf("audit detail = %q, want allow_unpriced=true", detail)
	}
}
