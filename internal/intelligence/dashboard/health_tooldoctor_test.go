package dashboard

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/diag"
)

// TestToolDoctorRunsTheInjectedBuilder pins T-421 item 3: GET
// /api/health/doctor/tool?tool=X runs Options.ToolDoctor (cmd/observer's
// buildDoctorReport, the one `observer doctor <tool>` prints) for exactly
// that tool, on the real install's database, and renders its checks with the
// counts and the CLI command.
func TestToolDoctorRunsTheInjectedBuilder(t *testing.T) {
	server := newServerWithFakeExec(t, newFakeExec(&fakeExecResult{}))
	var gotTool string
	var gotDB *sql.DB
	server.opts.ToolDoctor = func(_ context.Context, _ config.Config, database *sql.DB, tool string) (diag.Report, error) {
		gotTool, gotDB = tool, database
		return diag.Report{Checks: []diag.Check{
			{Name: tool, Status: diag.StatusWarn, Message: tool + " - see notes below", Details: []string{"launcher binary not found"}},
			{Name: tool + ".control-coverage", Status: diag.StatusOK, Message: "tool-call=hook-block"},
		}}, nil
	}
	rr := httptest.NewRecorder()
	server.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/health/doctor/tool?tool=cursor", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET = %d %s", rr.Code, rr.Body.String())
	}
	var out toolDoctorReport
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if gotTool != "cursor" || gotDB != server.opts.DB {
		t.Fatalf("builder ran for %q on %p, want cursor on the real DB", gotTool, gotDB)
	}
	if out.Tool != "cursor" || out.Command != "observer doctor cursor" || len(out.Checks) != 2 ||
		out.Warn != 1 || out.OK != 1 || out.AllOK || out.Checks[0].Status != "warn" || out.Checks[0].Details[0] != "launcher binary not found" {
		t.Fatalf("report = %+v", out)
	}
}

// TestToolDoctorRefusals: only a registry tool id runs, only on GET; a build
// without the seam says so with the CLI command; a builder error is honest.
func TestToolDoctorRefusals(t *testing.T) {
	server := newServerWithFakeExec(t, newFakeExec(&fakeExecResult{}))
	get := func(path string) *httptest.ResponseRecorder {
		rr := httptest.NewRecorder()
		server.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, path, nil))
		return rr
	}
	if rr := get("/api/health/doctor/tool?tool=cursor"); rr.Code != http.StatusNotImplemented || !strings.Contains(rr.Body.String(), "observer doctor cursor") {
		t.Fatalf("nil seam = %d %s", rr.Code, rr.Body.String())
	}
	called := false
	server.opts.ToolDoctor = func(context.Context, config.Config, *sql.DB, string) (diag.Report, error) {
		called = true
		return diag.Report{}, errors.New("no doctor checks or adapters match")
	}
	for _, path := range []string{"/api/health/doctor/tool", "/api/health/doctor/tool?tool=not-a-tool", "/api/health/doctor/tool?tool=../etc"} {
		if rr := get(path); rr.Code != http.StatusBadRequest {
			t.Fatalf("%s = %d, want 400", path, rr.Code)
		}
	}
	if called {
		t.Fatal("an unknown tool reached the builder")
	}
	if rr := get("/api/health/doctor/tool?tool=cursor"); rr.Code != http.StatusUnprocessableEntity || !strings.Contains(rr.Body.String(), "no doctor checks") {
		t.Fatalf("builder error = %d %s", rr.Code, rr.Body.String())
	}
	rr := httptest.NewRecorder()
	server.Handler().ServeHTTP(rr, armLocalWrite(httptest.NewRequest(http.MethodPost, "/api/health/doctor/tool?tool=cursor", nil)))
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST = %d, want 405", rr.Code)
	}
}
