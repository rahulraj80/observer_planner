package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestCommandJobs_RunTheCLIArgv pins every row of the commandJobs table: a POST
// to /api/jobs/<name> runs exactly the observer argv the matching CLI command
// takes, plus the dashboard's own --config. One case per table row, so a new
// row without a case (or an argv drift) is loud.
func TestCommandJobs_RunTheCLIArgv(t *testing.T) {
	want := map[string][]string{
		"score":             {"score"},
		"patterns":          {"patterns"},
		"calibration":       {"model-value", "--save-calibration"},
		"summarize":         {"summarize"},
		"pricing-sync":      {"pricing", "sync"},
		"mcp-wrap":          {"mcp", "wrap"},
		"mcp-unwrap":        {"mcp", "unwrap"},
		"mcp-relay-enable":  {"mcp-relay", "enable"},
		"mcp-relay-disable": {"mcp-relay", "disable"},
		"mcp-doctor":        {"mcp", "doctor"},
		"doctor-integrity":  {"doctor", "db", "--integrity"},
		"reclaim-cleanup":   {"archive", "reclaim-cleanup", "--delete"},
	}
	if len(want) != len(commandJobs) {
		t.Fatalf("commandJobs has %d rows, the test pins %d: add a case for every row", len(commandJobs), len(want))
	}
	for name, argv := range want {
		t.Run(name, func(t *testing.T) {
			seen := make(chan []string, 1)
			fake := func(_ context.Context, args []string, onChunk func([]byte)) (int, error) {
				seen <- append([]string(nil), args...)
				onChunk([]byte("ok\n"))
				return 0, nil
			}
			server := newServerWithFakeExec(t, fake)
			rr := httptest.NewRecorder()
			server.Handler().ServeHTTP(rr, armLocalWrite(httptest.NewRequest(http.MethodPost, "/api/jobs/"+name, nil)))
			if rr.Code != http.StatusOK {
				t.Fatalf("POST status %d body=%s", rr.Code, rr.Body.String())
			}
			var resp struct {
				JobID string `json:"job_id"`
				Mode  string `json:"mode"`
			}
			if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
				t.Fatal(err)
			}
			if resp.Mode != name || resp.JobID == "" {
				t.Fatalf("response %+v", resp)
			}
			var got []string
			select {
			case got = <-seen:
			case <-time.After(5 * time.Second):
				t.Fatal("subprocess never invoked")
			}
			wantArgs := append(append([]string{}, argv...), "--config", server.opts.ConfigPath)
			if strings.Join(got, " ") != strings.Join(wantArgs, " ") {
				t.Errorf("argv = %v, want %v", got, wantArgs)
			}
			job := pollCommandJob(t, server, resp.JobID)
			if job.Status != "done" || !strings.Contains(job.Output, "ok") {
				t.Errorf("job = %+v", job)
			}
		})
	}
}

// TestCommandJobs_RefusesUnknownAndGet: only the table's names run, and only
// on POST; nothing reaches os/exec otherwise.
func TestCommandJobs_RefusesUnknownAndGet(t *testing.T) {
	var called atomic.Bool
	fake := func(context.Context, []string, func([]byte)) (int, error) {
		called.Store(true)
		return 0, nil
	}
	server := newServerWithFakeExec(t, fake)
	for _, tc := range []struct {
		method, path string
		want         int
	}{
		{http.MethodGet, "/api/jobs/score", http.StatusMethodNotAllowed},
		{http.MethodPost, "/api/jobs/rm-rf", http.StatusNotFound},
		{http.MethodPost, "/api/jobs/backfill", http.StatusNotFound},
	} {
		rr := httptest.NewRecorder()
		server.Handler().ServeHTTP(rr, armLocalWrite(httptest.NewRequest(tc.method, tc.path, nil)))
		if rr.Code != tc.want {
			t.Errorf("%s %s = %d, want %d", tc.method, tc.path, rr.Code, tc.want)
		}
	}
	if called.Load() {
		t.Error("execBackfill must not run for a refused request")
	}
}

// TestCommandJobs_PollIsScopedToCommandJobs: /api/jobs/<id> answers for a
// command job and 404s for a backfill job (that one stays behind the Settings
// governance of /api/backfill/jobs/), and the list carries command jobs only.
func TestCommandJobs_PollIsScopedToCommandJobs(t *testing.T) {
	fake := newFakeExec(&fakeExecResult{out: []byte("done\n")})
	server := newServerWithFakeExec(t, fake)

	post := func(path, body string) string {
		t.Helper()
		rr := httptest.NewRecorder()
		var req *http.Request
		if body == "" {
			req = httptest.NewRequest(http.MethodPost, path, nil)
		} else {
			req = httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		}
		server.Handler().ServeHTTP(rr, armLocalWrite(req))
		if rr.Code != http.StatusOK {
			t.Fatalf("POST %s = %d %s", path, rr.Code, rr.Body.String())
		}
		var resp struct {
			JobID string `json:"job_id"`
		}
		_ = json.NewDecoder(rr.Body).Decode(&resp)
		return resp.JobID
	}
	scoreID := post("/api/jobs/score", "")
	backfillID := post("/api/backfill/run", `{"mode":"message-id"}`)
	pollCommandJob(t, server, scoreID)

	rr := httptest.NewRecorder()
	server.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/jobs/"+backfillID, nil))
	if rr.Code != http.StatusNotFound {
		t.Errorf("GET /api/jobs/<backfill id> = %d, want 404", rr.Code)
	}

	rr = httptest.NewRecorder()
	server.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/jobs", nil))
	var list struct {
		Jobs []backfillJob `json:"jobs"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&list); err != nil {
		t.Fatal(err)
	}
	if len(list.Jobs) != 1 || list.Jobs[0].Mode != "score" {
		t.Errorf("GET /api/jobs = %+v, want only the score job", list.Jobs)
	}
}

// TestCommandJobs_WritesAreAudited: a job that writes (every row but the
// read-only doctor) leaves one manage row in remote_audit, like the other
// dashboard writes.
func TestCommandJobs_WritesAreAudited(t *testing.T) {
	fake := newFakeExec(&fakeExecResult{})
	server := newServerWithFakeExec(t, fake)
	for _, name := range []string{"patterns", "mcp-doctor"} {
		rr := httptest.NewRecorder()
		server.Handler().ServeHTTP(rr, armLocalWrite(httptest.NewRequest(http.MethodPost, "/api/jobs/"+name, nil)))
		if rr.Code != http.StatusOK {
			t.Fatalf("POST %s = %d", name, rr.Code)
		}
	}
	var n int
	if err := server.opts.DB.QueryRow(`SELECT COUNT(*) FROM remote_audit WHERE kind = 'manage' AND detail LIKE 'job_patterns%'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("patterns audit rows = %d, want 1", n)
	}
	if err := server.opts.DB.QueryRow(`SELECT COUNT(*) FROM remote_audit WHERE detail LIKE 'job_mcp-doctor%'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("read-only doctor wrote %d audit rows, want 0", n)
	}
}

func pollCommandJob(t *testing.T, server *Server, id string) backfillJob {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		rr := httptest.NewRecorder()
		server.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/jobs/"+id, nil))
		if rr.Code != http.StatusOK {
			t.Fatalf("poll %s = %d %s", id, rr.Code, rr.Body.String())
		}
		var job backfillJob
		if err := json.NewDecoder(rr.Body).Decode(&job); err != nil {
			t.Fatal(err)
		}
		if job.Status != "running" {
			return job
		}
		if time.Now().After(deadline) {
			t.Fatalf("job %s still running", id)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
