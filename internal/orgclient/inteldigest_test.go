package orgclient

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/orgcontract"
)

// T-490: the developer digest pull signs the constant path with an empty
// query bound to the org id, relays the server's rows, and maps a server that
// predates the rail (a 404, or the SPA page) onto not_supported.
func TestFetchProjectDigests(t *testing.T) {
	var (
		status  = http.StatusOK
		html    bool
		pub     ed25519.PublicKey
		proofOK bool
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if html {
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<!doctype html><html></html>"))
			return
		}
		if r.URL.Path != orgcontract.IntelDigestsPath || r.URL.RawQuery != "" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		ts, _ := strconv.ParseInt(r.Header.Get(orgcontract.HeaderTimestamp), 10, 64)
		sig, _ := base64.RawURLEncoding.DecodeString(r.Header.Get(orgcontract.HeaderAgentSignature))
		proofOK = r.Header.Get("Authorization") == "Bearer bearer-xyz" &&
			ed25519.Verify(pub, orgcontract.IntelRailSigningMessage(ts, http.MethodGet, orgcontract.IntelDigestsPath, "", "org-1"), sig)
		if status != http.StatusOK {
			w.WriteHeader(status)
			return
		}
		writeTestJSON(w, http.StatusOK, orgcontract.IntelDigestsResponse{
			Projects: 2, Digests: []orgcontract.IntelDigestRow{{ProjectID: "p1", WeekStart: "2026-09-21", Headline: "h"}},
		})
	}))
	t.Cleanup(srv.Close)
	c, _ := enrolledIntelClient(t, srv.URL)
	key, err := c.bearers.LoadAgentKey()
	if err != nil {
		t.Fatal(err)
	}
	pub = key.Public().(ed25519.PublicKey)
	ctx := context.Background()

	got, err := c.FetchProjectDigests(ctx)
	if err != nil || got.State != IntelFetchOK || got.Projects != 2 || len(got.Digests) != 1 || got.Digests[0].ProjectID != "p1" {
		t.Fatalf("ok = %+v, %v", got, err)
	}
	if !proofOK {
		t.Fatal("the proof did not verify over the digest path")
	}

	status = http.StatusNotFound
	if got, _ := c.FetchProjectDigests(ctx); got.State != IntelFetchNotSupported {
		t.Fatalf("404 = %+v", got)
	}
	status = http.StatusForbidden
	if got, _ := c.FetchProjectDigests(ctx); got.State != IntelFetchAuthFailed {
		t.Fatalf("403 = %+v", got)
	}
	html = true
	if got, _ := c.FetchProjectDigests(ctx); got.State != IntelFetchNotSupported {
		t.Fatalf("SPA page = %+v", got)
	}
}
