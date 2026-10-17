package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// fakeOrgDigests adds the optional digest seam to the scripted manager.
type fakeOrgDigests struct {
	fakeOrgIntel
	out   OrgIntelDigests
	calls int
}

func (f *fakeOrgDigests) ProjectDigests(*http.Request) (OrgIntelDigests, error) {
	f.calls++
	return f.out, nil
}

// T-490: the developer's digest view answers through the optional seam, is
// 501 for a manager without it, and is GET only.
func TestIntelDigestsRoute(t *testing.T) {
	serveWith := func(opts Options, r *http.Request) *httptest.ResponseRecorder {
		s := newRemoteTestServer(t, opts)
		rec := httptest.NewRecorder()
		s.guardedHandler("127.0.0.1:8081").ServeHTTP(rec, r)
		return rec
	}

	if rec := serveWith(Options{OrgIntel: &fakeOrgIntel{}}, loopbackRequest(http.MethodGet, "/api/intel/digests")); rec.Code != http.StatusNotImplemented {
		t.Fatalf("no seam = %d, want 501", rec.Code)
	}

	fake := &fakeOrgDigests{out: OrgIntelDigests{State: "ok", Projects: 1, Digests: []OrgIntelDigestView{{ProjectID: "p1", Headline: "h"}}}}
	rec := serveWith(Options{OrgIntel: fake}, loopbackRequest(http.MethodGet, "/api/intel/digests"))
	if rec.Code != http.StatusOK {
		t.Fatalf("digests = %d %s", rec.Code, rec.Body.String())
	}
	var got OrgIntelDigests
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || got.State != "ok" || len(got.Digests) != 1 {
		t.Fatalf("body = %s (%v)", rec.Body.String(), err)
	}

	empty := &fakeOrgDigests{out: OrgIntelDigests{State: "not_supported"}}
	rec = serveWith(Options{OrgIntel: empty}, loopbackRequest(http.MethodGet, "/api/intel/digests"))
	if rec.Code != http.StatusOK || !json.Valid(rec.Body.Bytes()) {
		t.Fatalf("not supported = %d %s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || got.Digests == nil {
		t.Fatalf("digests must be a list, never null: %s", rec.Body.String())
	}

	if rec := serveWith(Options{OrgIntel: fake}, loopbackRequest(http.MethodPost, "/api/intel/digests")); rec.Code == http.StatusOK {
		t.Fatalf("POST answered 200")
	}
}
