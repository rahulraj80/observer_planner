//go:build !sbo_omit_integration_shell_wrap

package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/db"
	"github.com/marmutapp/superbased-observer/internal/shellwrap"
	"github.com/marmutapp/superbased-observer/internal/shellwrapsvc"
)

// fakeShellWrap records calls; it never touches a file.
type fakeShellWrap struct {
	applied         []shellwrapsvc.Request
	disabled        []bool
	disabledTargets []shellwrapsvc.Target
	statusTargets   []shellwrapsvc.Target
	windowsMissing  bool
}

func (f *fakeShellWrap) StatusFor(_ context.Context, t shellwrapsvc.Target) (shellwrap.Status, error) {
	f.statusTargets = append(f.statusTargets, t)
	if t == shellwrapsvc.TargetWindows {
		if f.windowsMissing {
			return shellwrap.Status{}, fmt.Errorf("%w: observer is not running inside WSL", shellwrapsvc.ErrTargetUnavailable)
		}
		return shellwrap.Status{GOOS: "windows", ShimDir: `C:\Users\u\.observer\shims`, Bridge: &shellwrap.WSLBridge{Distro: "Ubuntu"}}, nil
	}
	return shellwrap.Status{GOOS: "linux", ShimDir: "/h/.observer/shims"}, nil
}

func (f *fakeShellWrap) Apply(_ context.Context, req shellwrapsvc.Request) (shellwrapsvc.Outcome, error) {
	f.applied = append(f.applied, req)
	return shellwrapsvc.Outcome{DryRun: req.DryRun}, nil
}

func (f *fakeShellWrap) DisableFor(_ context.Context, t shellwrapsvc.Target, dry bool) (shellwrapsvc.Outcome, error) {
	f.disabled = append(f.disabled, dry)
	f.disabledTargets = append(f.disabledTargets, t)
	return shellwrapsvc.Outcome{DryRun: dry}, nil
}

func (f *fakeShellWrap) Targets(context.Context) ([]shellwrapsvc.TargetInfo, error) {
	return []shellwrapsvc.TargetInfo{
		{ID: shellwrapsvc.TargetNative, Label: "WSL (Ubuntu)", GOOS: "linux", Available: true},
		{ID: shellwrapsvc.TargetWindows, Label: "Windows (via WSL Ubuntu)", GOOS: "windows", Available: !f.windowsMissing},
	}, nil
}

func shellWrapServer(t *testing.T, svc ShellWrapService) *Server {
	t.Helper()
	dir := t.TempDir()
	database, err := openTestDB(context.Background(), db.Options{Path: filepath.Join(dir, "d.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	s, err := New(Options{DB: database, ConfigPath: filepath.Join(dir, "config.toml"), ShellWrap: svc})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func shellWrapToken(t *testing.T, s *Server) (string, *http.Cookie) {
	t.Helper()
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/shell-wrap/status", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d %s", rr.Code, rr.Body.String())
	}
	var body struct {
		Token  string           `json:"confirm_token"`
		Status shellwrap.Status `json:"status"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Token == "" || body.Status.ShimDir == "" {
		t.Fatalf("status body %+v", body)
	}
	for _, c := range rr.Result().Cookies() {
		if c.Name == remoteConfirmCookie {
			return body.Token, c
		}
	}
	t.Fatal("no confirm cookie")
	return "", nil
}

func TestShellWrapRoutes(t *testing.T) {
	fake := &fakeShellWrap{}
	s := shellWrapServer(t, fake)
	tok, ck := shellWrapToken(t, s)

	if rr := shellWrapPost(s, "/api/shell-wrap/apply", `{"tools":["claude-code"]}`, "", nil); rr.Code != http.StatusForbidden {
		t.Fatalf("apply without the confirm token: %d", rr.Code)
	}
	if len(fake.applied) != 0 {
		t.Fatal("a refused request reached the service")
	}
	rr := shellWrapPost(s, "/api/shell-wrap/apply", `{"tools":["claude-code"],"shells":["zsh"],"dry_run":true}`, tok, ck)
	if rr.Code != http.StatusOK {
		t.Fatalf("apply: %d %s", rr.Code, rr.Body.String())
	}
	if len(fake.applied) != 1 || !fake.applied[0].DryRun || fake.applied[0].Shells[0] != "zsh" {
		t.Fatalf("service saw %+v", fake.applied)
	}
	if rr := shellWrapPost(s, "/api/shell-wrap/disable", `{}`, tok, ck); rr.Code != http.StatusOK {
		t.Fatalf("disable: %d %s", rr.Code, rr.Body.String())
	}
	if len(fake.disabled) != 1 || fake.disabled[0] {
		t.Fatalf("disable calls %+v", fake.disabled)
	}
}

func TestShellWrapRoutesNotWired(t *testing.T) {
	s := shellWrapServer(t, nil)
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/shell-wrap/status", nil))
	if rr.Code != http.StatusNotImplemented {
		t.Fatalf("got %d", rr.Code)
	}
}

// TestShellWrapRoutesCarryTheTarget pins the WSL bridge target (T-419) on all
// three routes: status takes ?target=, apply and disable take "target", an
// unknown target is a 400 that never reaches the service, and an unavailable
// Windows target is a 409 with the reason.
func TestShellWrapRoutesCarryTheTarget(t *testing.T) {
	fake := &fakeShellWrap{}
	s := shellWrapServer(t, fake)
	tok, ck := shellWrapToken(t, s)

	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/shell-wrap/status?target=windows", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status windows: %d %s", rr.Code, rr.Body.String())
	}
	var body struct {
		Status  shellwrap.Status          `json:"status"`
		Target  string                    `json:"target"`
		Targets []shellwrapsvc.TargetInfo `json:"targets"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Target != "windows" || body.Status.Bridge == nil || len(body.Targets) != 2 {
		t.Fatalf("status body %+v", body)
	}

	rr = httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/shell-wrap/status?target=mars", nil))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("unknown target: %d", rr.Code)
	}

	if rr := shellWrapPost(s, "/api/shell-wrap/apply", `{"target":"windows","tools":["muse"],"dry_run":true}`, tok, ck); rr.Code != http.StatusOK {
		t.Fatalf("apply windows: %d %s", rr.Code, rr.Body.String())
	}
	if len(fake.applied) != 1 || fake.applied[0].Target != shellwrapsvc.TargetWindows {
		t.Fatalf("service saw %+v", fake.applied)
	}
	if rr := shellWrapPost(s, "/api/shell-wrap/apply", `{"target":"mars","tools":["muse"]}`, tok, ck); rr.Code != http.StatusBadRequest {
		t.Fatalf("apply unknown target: %d", rr.Code)
	}
	if len(fake.applied) != 1 {
		t.Fatal("an unknown target reached the service")
	}
	if rr := shellWrapPost(s, "/api/shell-wrap/disable", `{"target":"windows"}`, tok, ck); rr.Code != http.StatusOK {
		t.Fatalf("disable windows: %d %s", rr.Code, rr.Body.String())
	}
	if len(fake.disabledTargets) != 1 || fake.disabledTargets[0] != shellwrapsvc.TargetWindows {
		t.Fatalf("disable targets %+v", fake.disabledTargets)
	}

	fake.windowsMissing = true
	rr = httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/shell-wrap/status?target=windows", nil))
	if rr.Code != http.StatusConflict || !strings.Contains(rr.Body.String(), "not running inside WSL") {
		t.Fatalf("unavailable windows target: %d %s", rr.Code, rr.Body.String())
	}
}
