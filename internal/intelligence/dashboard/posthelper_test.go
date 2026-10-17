package dashboard

import (
	"net/http"
	"net/http/httptest"
	"strings"
)

// shellWrapPost POSTs a JSON body with the optional confirm token and cookie.
// It lives outside shellwrap_test.go (which compiles out with
// integration.shell-wrap) because reprice_test.go uses it too.
func shellWrapPost(s *Server, path, body, token string, cookie *http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set(remoteConfirmHeader, token)
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)
	return rr
}
