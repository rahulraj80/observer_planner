//go:build sbo_omit_remote

package dashboard

import (
	"net/http"
	"testing"
)

// testhelpers_remote_omit_test.go is the remote-out half of the shared test
// helpers (testhelpers_manage_test.go): no controller exists, and the confirm
// token is minted here the way setConfirmCookie mints it, because
// GET /api/remote/config answers component_not_installed in this build.

// testManageRemote is nil: the remote component is not in this build.
func testManageRemote(*testing.T) RemoteController { return nil }

// getConfirm returns a double-submit confirm cookie + token pair the
// management writes accept (requireJSONConfirm compares the two).
func getConfirm(t *testing.T, _ http.Handler) (*http.Cookie, string) {
	t.Helper()
	tok := mintConfirmToken()
	if tok == "" {
		t.Fatal("mintConfirmToken returned empty")
	}
	return &http.Cookie{Name: remoteConfirmCookie, Value: tok, Path: "/"}, tok
}
