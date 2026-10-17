package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// startJobForTest POSTs /api/jobs/<name> and returns the job id.
func startJobForTest(t *testing.T, server *Server, name string) string {
	t.Helper()
	rr := httptest.NewRecorder()
	server.Handler().ServeHTTP(rr, armLocalWrite(httptest.NewRequest(http.MethodPost, "/api/jobs/"+name, nil)))
	var resp struct {
		JobID string `json:"job_id"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil || resp.JobID == "" {
		t.Fatalf("POST /api/jobs/%s = %d %s", name, rr.Code, rr.Body.String())
	}
	return resp.JobID
}

func cancelJobForTest(server *Server, name, id string) *httptest.ResponseRecorder {
	rr := httptest.NewRecorder()
	server.Handler().ServeHTTP(rr, armLocalWrite(httptest.NewRequest(http.MethodPost,
		"/api/jobs/"+name+"/cancel", strings.NewReader(`{"job_id":"`+id+`"}`))))
	return rr
}

// TestDoctorIntegrityJobIsCancelable pins T-421 item 2: the Health page's
// forced integrity check runs `observer doctor db --integrity` through the
// command-jobs runner, and the operator can stop it: the subprocess's context
// is cancelled and the job ends "canceled", not "failed".
func TestDoctorIntegrityJobIsCancelable(t *testing.T) {
	started := make(chan []string, 1)
	fake := func(ctx context.Context, args []string, onChunk func([]byte)) (int, error) {
		started <- append([]string(nil), args...)
		onChunk([]byte("checking...\n"))
		<-ctx.Done()
		return -1, ctx.Err()
	}
	server := newServerWithFakeExec(t, fake)
	id := startJobForTest(t, server, "doctor-integrity")
	select {
	case args := <-started:
		if strings.Join(args[:3], " ") != "doctor db --integrity" {
			t.Fatalf("argv = %v", args)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the integrity job never started")
	}

	rr := cancelJobForTest(server, "doctor-integrity", id)
	if rr.Code != http.StatusOK {
		t.Fatalf("cancel = %d %s", rr.Code, rr.Body.String())
	}
	job := pollCommandJob(t, server, id)
	if job.Status != "canceled" || !strings.Contains(job.Output, "checking") {
		t.Fatalf("job after cancel = %+v", job)
	}
	// A finished job cannot be cancelled again.
	if rr := cancelJobForTest(server, "doctor-integrity", id); rr.Code != http.StatusConflict {
		t.Fatalf("second cancel = %d, want 409", rr.Code)
	}
	// A read-only job leaves no manage audit row (start or cancel).
	var n int
	if err := server.opts.DB.QueryRow(`SELECT COUNT(*) FROM remote_audit WHERE detail LIKE 'job_doctor-integrity%'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("read-only integrity job wrote %d audit rows", n)
	}
}

// TestCommandJobCancelRefusals: the cancel route only stops a running job of
// its own mode, carries the confirm token, and exists only for cancelable
// rows.
func TestCommandJobCancelRefusals(t *testing.T) {
	server := newServerWithFakeExec(t, newFakeExec(&fakeExecResult{out: []byte("ok\n")}))
	other := startJobForTest(t, server, "score")
	pollCommandJob(t, server, other)

	if rr := cancelJobForTest(server, "doctor-integrity", other); rr.Code != http.StatusNotFound {
		t.Fatalf("cancel of another mode's job = %d, want 404", rr.Code)
	}
	if rr := cancelJobForTest(server, "doctor-integrity", "nope"); rr.Code != http.StatusNotFound {
		t.Fatalf("cancel of an unknown job = %d, want 404", rr.Code)
	}
	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/jobs/doctor-integrity/cancel", strings.NewReader(`{"job_id":"x"}`))
	r.Header.Set("Content-Type", "application/json")
	server.Handler().ServeHTTP(rr, r)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("cancel without the confirm token = %d, want 403", rr.Code)
	}
	_, capMap, _ := server.registerRoutes(nil)
	for name, row := range commandJobs {
		_, has := capMap["/api/jobs/"+name+"/cancel"]
		if has != row.cancelable {
			t.Errorf("%s: cancel route registered = %v, cancelable = %v", name, has, row.cancelable)
		}
	}
}

// TestCommandJobCancelBeforeStart: a cancel that lands before the runner
// publishes its cancel func still stops the job before the subprocess runs.
func TestCommandJobCancelBeforeStart(t *testing.T) {
	ran := make(chan struct{}, 1)
	server := newServerWithFakeExec(t, func(context.Context, []string, func([]byte)) (int, error) {
		ran <- struct{}{}
		return 0, nil
	})
	server.backfillMu.Lock()
	server.backfillJobs["j1"] = &backfillJob{ID: "j1", Mode: "doctor-integrity", Status: "running", StartedAt: time.Now().UTC(), canceled: true}
	server.backfillMu.Unlock()
	server.runBackfillJob("j1", []string{"doctor", "db", "--integrity"})
	select {
	case <-ran:
		t.Fatal("a job cancelled before it started still ran the CLI")
	default:
	}
	server.backfillMu.Lock()
	status := server.backfillJobs["j1"].Status
	server.backfillMu.Unlock()
	if status != "canceled" {
		t.Fatalf("status = %q, want canceled", status)
	}
}
