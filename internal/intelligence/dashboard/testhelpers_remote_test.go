//go:build !sbo_omit_remote

package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/remoteauth"
)

// testhelpers_remote_test.go is the remote-in half of the shared test
// helpers (testhelpers_manage_test.go): a real, Ready() controller and the
// confirm probe through GET /api/remote/config.

// testManageRemote is the controller newManageServer wires in: a ready one.
func testManageRemote(t *testing.T) RemoteController {
	rc, _ := newReadyRemoteController(t)
	return rc
}

// newReadyRemoteController builds a real, Ready() controller with a known
// pairing secret; returns the controller + the base64url secret to pair with.
func newReadyRemoteController(t *testing.T) (RemoteController, string) {
	return newReadyRemoteControllerWithTerminal(t, false)
}

func newReadyRemoteControllerWithTerminal(t *testing.T, allowTerminal bool) (RemoteController, string) {
	t.Helper()
	raw, enc, err := remoteauth.GenerateSecret()
	if err != nil {
		t.Fatalf("GenerateSecret: %v", err)
	}
	hash, err := remoteauth.HashSecret(raw)
	if err != nil {
		t.Fatalf("HashSecret: %v", err)
	}
	rc := NewRemoteController(RemoteOptions{
		HashedSecret:    hash,
		AllowedHosts:    []string{testRemoteHost},
		RateLimitPerMin: 6,
		AllowTerminal:   allowTerminal,
		Session:         remoteauth.SessionParams{TTL: time.Hour, Idle: time.Hour, Max: 5},
	})
	if !rc.Ready() {
		t.Fatal("controller not Ready with secret + allowlist")
	}
	return rc, enc
}

// getConfirm performs GET /api/remote/config and returns the confirm cookie +
// token the SPA would echo.
func getConfirm(t *testing.T, h http.Handler) (*http.Cookie, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/remote/config", nil)
	req.Host = "127.0.0.1:8080"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/remote/config = %d", rec.Code)
	}
	var resp struct {
		ConfirmToken string `json:"confirm_token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	var ck *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == remoteConfirmCookie {
			ck = c
		}
	}
	if ck == nil || resp.ConfirmToken == "" {
		t.Fatalf("no confirm cookie/token: cookie=%v token=%q", ck, resp.ConfirmToken)
	}
	if ck.Value != resp.ConfirmToken {
		t.Fatalf("cookie/token mismatch: %q vs %q", ck.Value, resp.ConfirmToken)
	}
	return ck, resp.ConfirmToken
}
