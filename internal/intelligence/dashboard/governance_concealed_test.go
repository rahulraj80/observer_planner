package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// TestGovernanceConcealedRoutesMatchGuard pins T-401. On a governed node the
// SPA polled routes the visibility posture hides (/api/cloud/status from the
// TopBar, Sessions and the session drawer; /api/config/backup from the
// Settings rail) and logged a 404 for each. GET /api/governance now lists
// those routes (concealed_routes) for the SPA's one shared fetch guard, and
// the list is computed from the SAME concealment function the guard refuses
// with: for every registered route, a GET is refused as hidden exactly when
// the route is listed.
func TestGovernanceConcealedRoutesMatchGuard(t *testing.T) {
	s := newRemoteTestServer(t, Options{
		Governance: governedProvider(t,
			[]string{"benchmarks"}, // hidden nav page
			nil,
			[]string{"cloud"}, // hidden Settings subsection
			nil,
		),
	})
	rec := httptest.NewRecorder()
	s.guardedHandler("127.0.0.1:8081").ServeHTTP(rec, loopbackRequest(http.MethodGet, "/api/governance"))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/governance = %d: %s", rec.Code, rec.Body.String())
	}
	var gov struct {
		ConcealedRoutes []string `json:"concealed_routes"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &gov); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"/api/cloud/status", "/api/config/backup", "/api/benchmarks", "/api/config/section/cloud"} {
		if !slices.Contains(gov.ConcealedRoutes, want) {
			t.Errorf("concealed_routes lacks %s: %v", want, gov.ConcealedRoutes)
		}
	}
	for _, never := range []string{"/api/governance", "/api/status", "/api/config", "/api/config/section/enrolment"} {
		if slices.Contains(gov.ConcealedRoutes, never) {
			t.Errorf("concealed_routes lists %s, which the posture does not hide", never)
		}
	}

	// The list and the guard agree on every registered route. The guard is
	// driven with a stub next handler, so no route handler runs.
	mux, _, sections := s.registerRoutes(nil)
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) })
	guard := s.governanceGuard(mux, sections, ok)
	wildcard := regexp.MustCompile(`\{[^}]*\}`)
	for pattern := range sections {
		path := pattern
		if method, rest, ok := strings.Cut(path, " "); ok {
			if method != http.MethodGet {
				continue
			}
			path = rest
		}
		if path == configSectionPrefix || !strings.HasPrefix(path, "/api/") {
			continue
		}
		req := wildcard.ReplaceAllString(path, "x")
		rec := httptest.NewRecorder()
		guard.ServeHTTP(rec, loopbackRequest(http.MethodGet, req))
		refused := rec.Code == http.StatusNotFound && strings.Contains(rec.Body.String(), "governance_hidden")
		if listed := slices.Contains(gov.ConcealedRoutes, path); listed != refused {
			t.Errorf("%s: listed=%v but guard refused-as-hidden=%v (code %d)", pattern, listed, refused, rec.Code)
		}
	}
}

// TestGovernanceConcealedRoutesAbsentWhenUngoverned: an ungoverned node's
// /api/governance response is unchanged (the key is omitted).
func TestGovernanceConcealedRoutesAbsentWhenUngoverned(t *testing.T) {
	s := newRemoteTestServer(t, Options{})
	rec := httptest.NewRecorder()
	s.guardedHandler("127.0.0.1:8081").ServeHTTP(rec, loopbackRequest(http.MethodGet, "/api/governance"))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/governance = %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "concealed_routes") {
		t.Fatalf("ungoverned response carries concealed_routes: %s", rec.Body.String())
	}
}
