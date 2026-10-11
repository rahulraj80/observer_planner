package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/diag"
)

type fakeWindowsHomes struct {
	wsl     bool
	targets []diag.WindowsHomeTarget
	applied []WindowsHomeApply
}

func (f *fakeWindowsHomes) Targets(context.Context) (bool, []diag.WindowsHomeTarget, error) {
	return f.wsl, f.targets, nil
}

func (f *fakeWindowsHomes) Apply(_ context.Context, req WindowsHomeApply) ([]string, error) {
	f.applied = append(f.applied, req)
	return []string{req.Target + " route would register in " + req.Home}, nil
}

func windowsHomesServer(t *testing.T, fake *fakeWindowsHomes) *Server {
	t.Helper()
	opts := Options{}
	if fake != nil {
		opts.WindowsHomes = fake
	}
	return newRemoteTestServer(t, opts)
}

func windowsHomesDo(s *Server, method, path, body string, armed bool) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if armed {
		r = armLocalWrite(r)
	} else if method == http.MethodPost {
		r.Header.Set("Content-Type", "application/json")
	}
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, r)
	return rr
}

// TestWindowsHomesControl pins T-421 item 4: the GET lists the detected
// homes per cross-OS target with the CLI flag; the apply previews / writes
// only for a detected home, carries the confirm token, and refuses off WSL.
func TestWindowsHomesControl(t *testing.T) {
	fake := &fakeWindowsHomes{wsl: true, targets: []diag.WindowsHomeTarget{{
		Target: "codex-windows", Subdir: ".codex",
		Candidates: []diag.WindowsHomeCandidate{{Home: "/mnt/c/Users/a"}, {Home: "/mnt/c/Users/b"}},
	}}}
	s := windowsHomesServer(t, fake)

	rr := windowsHomesDo(s, http.MethodGet, "/api/setup/windows-homes", "", false)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET: %d %s", rr.Code, rr.Body.String())
	}
	var got struct {
		ConfirmToken string `json:"confirm_token"`
		WSL          bool   `json:"wsl"`
		Targets      []struct {
			Target     string `json:"target"`
			Flag       string `json:"flag"`
			Command    string `json:"command"`
			Candidates []struct {
				Home string `json:"home"`
			} `json:"candidates"`
		} `json:"targets"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.ConfirmToken == "" || !got.WSL || len(got.Targets) != 1 || got.Targets[0].Flag != "--windows-codex-home" ||
		got.Targets[0].Command != "observer init --windows-codex-home <path>" || len(got.Targets[0].Candidates) != 2 {
		t.Fatalf("GET body = %+v", got)
	}

	body := `{"target":"codex-windows","home":"/mnt/c/Users/b","dry_run":true}`
	if rr := windowsHomesDo(s, http.MethodPost, "/api/setup/windows-homes/apply", body, false); rr.Code != http.StatusForbidden {
		t.Fatalf("no confirm token: %d", rr.Code)
	}
	rr = windowsHomesDo(s, http.MethodPost, "/api/setup/windows-homes/apply", body, true)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "would register") {
		t.Fatalf("preview: %d %s", rr.Code, rr.Body.String())
	}
	if len(fake.applied) != 1 || !fake.applied[0].DryRun || fake.applied[0].Home != "/mnt/c/Users/b" {
		t.Fatalf("applied = %+v", fake.applied)
	}
	for _, bad := range []string{
		`{"target":"codex-windows","home":"/etc"}`,
		`{"target":"claude-code-windows","home":"/mnt/c/Users/b"}`,
	} {
		if rr := windowsHomesDo(s, http.MethodPost, "/api/setup/windows-homes/apply", bad, true); rr.Code != http.StatusBadRequest {
			t.Errorf("%s: %d, want 400", bad, rr.Code)
		}
	}
	if len(fake.applied) != 1 {
		t.Errorf("a refused request still applied: %+v", fake.applied)
	}

	fake.wsl = false
	if rr := windowsHomesDo(s, http.MethodPost, "/api/setup/windows-homes/apply", body, true); rr.Code != http.StatusConflict {
		t.Errorf("off WSL: %d, want 409", rr.Code)
	}
}

// TestWindowsHomesNotWired: no seam is the honest 501.
func TestWindowsHomesNotWired(t *testing.T) {
	s := windowsHomesServer(t, nil)
	if rr := windowsHomesDo(s, http.MethodGet, "/api/setup/windows-homes", "", false); rr.Code != http.StatusNotImplemented {
		t.Errorf("GET unwired: %d", rr.Code)
	}
}
