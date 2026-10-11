//go:build !sbo_omit_remote

package dashboard

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/db"
)

// fakeLedger is an in-memory ToolInstallLedger. uninstallArgv is the argv it
// "derives" for an uninstall (the real derivation is internal/toolinstall's,
// tested there); the test asserts the handler spawns exactly that.
type fakeLedger struct {
	recorded      []string // tool|handle|display
	recordedArgv  [][]string
	rec           *ToolInstallRecord
	uninstallArgv []string
	uninstallErr  error
	forgetErr     error
	forgot        []int64
	lastToolArg   string
	lastIDArg     int64
}

func (f *fakeLedger) RecordInstall(_ context.Context, tool, handle string, argv []string, display string) error {
	f.recorded = append(f.recorded, tool+"|"+handle+"|"+display)
	f.recordedArgv = append(f.recordedArgv, argv)
	return nil
}

func (f *fakeLedger) Lookup(_ context.Context, tool string) (ToolInstallRecord, bool, error) {
	if f.rec == nil || f.rec.Tool != tool {
		return ToolInstallRecord{}, false, nil
	}
	return *f.rec, true, nil
}

func (f *fakeLedger) List(context.Context) ([]ToolInstallRecord, error) {
	if f.rec == nil {
		return nil, nil
	}
	return []ToolInstallRecord{*f.rec}, nil
}

func (f *fakeLedger) Uninstall(_ context.Context, tool string, id int64, spawn func([]string) (string, error)) (ToolUninstallStarted, error) {
	f.lastToolArg, f.lastIDArg = tool, id
	if f.uninstallErr != nil {
		return ToolUninstallStarted{}, f.uninstallErr
	}
	h, err := spawn(f.uninstallArgv)
	if err != nil {
		return ToolUninstallStarted{}, err
	}
	return ToolUninstallStarted{RecordID: id, Handle: h, Tool: tool, Command: "npm uninstall --global @openai/codex"}, nil
}

func (f *fakeLedger) Forget(_ context.Context, _ string, id int64) error {
	if f.forgetErr != nil {
		return f.forgetErr
	}
	f.forgot = append(f.forgot, id)
	return nil
}

func newUninstallServer(t *testing.T, lm LaunchManager, allowInstall func() bool, ledger ToolInstallLedger) http.Handler {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "observer.db")
	database, err := openTestDB(context.Background(), db.Options{Path: dbPath})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	cfgPath := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(cfgPath, []byte("[observer]\ndb_path = \""+filepath.ToSlash(dbPath)+"\"\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	rc, _ := newReadyRemoteController(t)
	opts := Options{
		DB:               database,
		ConfigPath:       cfgPath,
		Remote:           rc,
		LaunchManager:    lm,
		AllowToolInstall: allowInstall,
		ToolInstallHint:  codexHint,
		ToolPreflight: func(tool string) (ToolPreflight, bool) {
			return ToolPreflight{Tool: tool, Verdict: "ok", Bin: "/x/" + tool}, tool == "codex"
		},
	}
	if ledger != nil {
		opts.ToolInstalls = ledger
	}
	s, err := New(opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s.Handler()
}

func postToolInstallAction(t *testing.T, h http.Handler, path string, body any, withConfirm bool) *httptest.ResponseRecorder {
	t.Helper()
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw))
	req.Host = "127.0.0.1:8080"
	req.Header.Set("Content-Type", "application/json")
	if withConfirm {
		ck, ctok := getConfirm(t, h)
		req.AddCookie(ck)
		req.Header.Set(remoteConfirmHeader, ctok)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

var codexUninstallArgv = []string{"npm", "uninstall", "--global", "@openai/codex"}

// TestTerminalInstallRecordsIntoLedger: a guided install is recorded with the
// handle it spawned and the SERVER argv it ran.
func TestTerminalInstallRecordsIntoLedger(t *testing.T) {
	lm := &fakeLaunchManager{}
	led := &fakeLedger{}
	h := newUninstallServer(t, lm, func() bool { return true }, led)
	rec := postInstall(t, h, "codex")
	if rec.Code != http.StatusOK {
		t.Fatalf("install = %d: %s", rec.Code, rec.Body.String())
	}
	if len(led.recorded) != 1 || !strings.HasPrefix(led.recorded[0], "codex|") || !strings.HasSuffix(led.recorded[0], "|npm install -g @openai/codex") {
		t.Fatalf("recorded = %v", led.recorded)
	}
	if !reflect.DeepEqual(led.recordedArgv[0], codexArgv) {
		t.Errorf("recorded argv = %v, want %v", led.recordedArgv[0], codexArgv)
	}
}

// TestTerminalUninstallHappyPath: the spawned argv is the LEDGER-derived one,
// verbatim, under the "uninstall:<tool>" setup label — and a client-supplied
// argv field is ignored.
func TestTerminalUninstallHappyPath(t *testing.T) {
	lm := &fakeLaunchManager{}
	led := &fakeLedger{uninstallArgv: codexUninstallArgv}
	h := newUninstallServer(t, lm, func() bool { return true }, led)
	rec := postToolInstallAction(t, h, "/api/terminal/uninstall", map[string]any{
		"tool": "codex", "record_id": 7, "argv": []string{"rm", "-rf", "/"},
	}, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("uninstall = %d: %s", rec.Code, rec.Body.String())
	}
	if !reflect.DeepEqual(lm.lastSetupSpec.Argv, codexUninstallArgv) {
		t.Errorf("spawned argv = %v, want %v", lm.lastSetupSpec.Argv, codexUninstallArgv)
	}
	if lm.lastSetupSpec.Label != "uninstall:codex" {
		t.Errorf("label = %q", lm.lastSetupSpec.Label)
	}
	if led.lastToolArg != "codex" || led.lastIDArg != 7 {
		t.Errorf("ledger saw tool=%q id=%d", led.lastToolArg, led.lastIDArg)
	}
	var out ToolUninstallStarted
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out.Handle == "" || out.RecordID != 7 {
		t.Errorf("response = %s (%v)", rec.Body.String(), err)
	}
}

// TestTerminalUninstallGates walks every refusal, in the install route's
// order, plus the ledger error mapping.
func TestTerminalUninstallGates(t *testing.T) {
	on := func() bool { return true }
	off := func() bool { return false }
	cases := []struct {
		name    string
		lm      LaunchManager
		allow   func() bool
		ledger  *fakeLedger
		body    map[string]any
		confirm bool
		want    int
	}{
		{"no confirm token", &fakeLaunchManager{}, on, &fakeLedger{}, map[string]any{"tool": "codex", "record_id": 1}, false, http.StatusForbidden},
		{"no launch manager", nil, on, &fakeLedger{}, map[string]any{"tool": "codex", "record_id": 1}, true, http.StatusServiceUnavailable},
		{"kill-switch off", &fakeLaunchManager{}, off, &fakeLedger{}, map[string]any{"tool": "codex", "record_id": 1}, true, http.StatusForbidden},
		{"kill-switch seam nil", &fakeLaunchManager{}, nil, &fakeLedger{}, map[string]any{"tool": "codex", "record_id": 1}, true, http.StatusForbidden},
		{"no ledger", &fakeLaunchManager{}, on, nil, map[string]any{"tool": "codex", "record_id": 1}, true, http.StatusNotImplemented},
		{"missing tool", &fakeLaunchManager{}, on, &fakeLedger{}, map[string]any{"record_id": 1}, true, http.StatusBadRequest},
		{"missing record id", &fakeLaunchManager{}, on, &fakeLedger{}, map[string]any{"tool": "codex"}, true, http.StatusBadRequest},
		{"not installed from here", &fakeLaunchManager{}, on, &fakeLedger{uninstallErr: ErrToolInstallNotFound}, map[string]any{"tool": "codex", "record_id": 1}, true, http.StatusNotFound},
		{"stale record", &fakeLaunchManager{}, on, &fakeLedger{uninstallErr: ErrToolInstallStale}, map[string]any{"tool": "codex", "record_id": 1}, true, http.StatusConflict},
		{"vendor script", &fakeLaunchManager{}, on, &fakeLedger{uninstallErr: ErrToolUninstallUnsupported}, map[string]any{"tool": "codex", "record_id": 1}, true, http.StatusConflict},
		{"setup in flight", &fakeLaunchManager{setupErr: ErrLaunchSetupInFlight}, on, &fakeLedger{uninstallArgv: codexUninstallArgv}, map[string]any{"tool": "codex", "record_id": 1}, true, http.StatusConflict},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var ledger ToolInstallLedger
			if tc.ledger != nil {
				ledger = tc.ledger
			}
			h := newUninstallServer(t, tc.lm, tc.allow, ledger)
			rec := postToolInstallAction(t, h, "/api/terminal/uninstall", tc.body, tc.confirm)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tc.want, rec.Body.String())
			}
			if fl, ok := tc.lm.(*fakeLaunchManager); ok && tc.want != http.StatusOK && tc.name != "setup in flight" && fl.lastSetupSpec.Argv != nil {
				t.Errorf("a refused uninstall spawned %v", fl.lastSetupSpec.Argv)
			}
		})
	}
}

// TestTerminalPreflightCarriesDashboardInstall: the preflight reply carries
// the tool's record, and the kill-switch clears can_uninstall.
func TestTerminalPreflightCarriesDashboardInstall(t *testing.T) {
	for _, allow := range []bool{true, false} {
		led := &fakeLedger{rec: &ToolInstallRecord{ID: 3, Tool: "codex", Channel: "npm", Status: "installed", CanUninstall: true, UninstallCommand: "npm uninstall --global @openai/codex", CanForget: true}}
		h := newUninstallServer(t, &fakeLaunchManager{}, func() bool { return allow }, led)
		req := httptest.NewRequest(http.MethodGet, "/api/terminal/launch/preflight?tool=codex", nil)
		req.Host = "127.0.0.1:8080"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("preflight = %d: %s", rec.Code, rec.Body.String())
		}
		var pf ToolPreflight
		if err := json.Unmarshal(rec.Body.Bytes(), &pf); err != nil {
			t.Fatal(err)
		}
		if pf.DashboardInstall == nil || pf.DashboardInstall.ID != 3 {
			t.Fatalf("dashboard_install = %+v", pf.DashboardInstall)
		}
		if pf.DashboardInstall.CanUninstall != allow {
			t.Errorf("allow=%v: can_uninstall = %v", allow, pf.DashboardInstall.CanUninstall)
		}
	}
	// No record -> field omitted.
	h := newUninstallServer(t, &fakeLaunchManager{}, func() bool { return true }, &fakeLedger{})
	req := httptest.NewRequest(http.MethodGet, "/api/terminal/launch/preflight?tool=codex", nil)
	req.Host = "127.0.0.1:8080"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if strings.Contains(rec.Body.String(), "dashboard_install") {
		t.Errorf("no record must omit dashboard_install: %s", rec.Body.String())
	}
}

func TestTerminalInstallsListAndForget(t *testing.T) {
	led := &fakeLedger{rec: &ToolInstallRecord{ID: 9, Tool: "claude-code", Channel: "script", Status: "installed", UninstallGuidance: "x", CanForget: true}}
	h := newUninstallServer(t, &fakeLaunchManager{}, func() bool { return true }, led)
	req := httptest.NewRequest(http.MethodGet, "/api/terminal/installs", nil)
	req.Host = "127.0.0.1:8080"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"claude-code"`) || !strings.Contains(rec.Body.String(), `"available":true`) {
		t.Fatalf("list = %d %s", rec.Code, rec.Body.String())
	}
	if r := postToolInstallAction(t, h, "/api/terminal/installs/forget", map[string]any{"tool": "claude-code", "record_id": 9}, false); r.Code != http.StatusForbidden {
		t.Fatalf("forget without confirm = %d", r.Code)
	}
	if r := postToolInstallAction(t, h, "/api/terminal/installs/forget", map[string]any{"tool": "claude-code", "record_id": 9}, true); r.Code != http.StatusOK {
		t.Fatalf("forget = %d %s", r.Code, r.Body.String())
	}
	if len(led.forgot) != 1 || led.forgot[0] != 9 {
		t.Errorf("forgot = %v", led.forgot)
	}
	led.forgetErr = ErrToolInstallStale
	if r := postToolInstallAction(t, h, "/api/terminal/installs/forget", map[string]any{"tool": "claude-code", "record_id": 9}, true); r.Code != http.StatusConflict {
		t.Fatalf("stale forget = %d", r.Code)
	}
	led.forgetErr = errors.New("boom")
	if r := postToolInstallAction(t, h, "/api/terminal/installs/forget", map[string]any{"tool": "claude-code", "record_id": 9}, true); r.Code != http.StatusInternalServerError {
		t.Fatalf("broken forget = %d", r.Code)
	}
	// No ledger -> list reports unavailable, not an error.
	h2 := newUninstallServer(t, &fakeLaunchManager{}, func() bool { return true }, nil)
	req = httptest.NewRequest(http.MethodGet, "/api/terminal/installs", nil)
	req.Host = "127.0.0.1:8080"
	rec = httptest.NewRecorder()
	h2.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"available":false`) {
		t.Fatalf("no-ledger list = %d %s", rec.Code, rec.Body.String())
	}
}
