package orgclient

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/orgcontract"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// displayPolicyServer serves GET /api/agent/display-policy with a scripted
// (status, body) the test changes between fetches.
type displayPolicyServer struct {
	srv    *httptest.Server
	status int
	doc    any // JSON-encoded on a 200; ignored otherwise
	raw    string
}

func newDisplayPolicyServer(t *testing.T) *displayPolicyServer {
	t.Helper()
	ds := &displayPolicyServer{status: http.StatusNotFound}
	ds.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/agent/display-policy" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if ds.raw != "" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(ds.raw))
			return
		}
		if ds.status != http.StatusOK {
			writeTestJSON(w, ds.status, map[string]string{"error": "scripted"})
			return
		}
		writeTestJSON(w, http.StatusOK, ds.doc)
	}))
	t.Cleanup(ds.srv.Close)
	return ds
}

func mustSignDisplay(t *testing.T, priv ed25519.PrivateKey, org string, version int64, value string) orgcontract.DisplayPolicyDoc {
	t.Helper()
	d, err := orgcontract.SignDisplayPolicy(priv, org, version, orgcontract.DisplayPolicyBody{DeveloperCostDisplay: value})
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// TestFetchOrgDisplayPolicy walks the rail's state rules in order against ONE
// node: each step scripts the server, fetches once, and asserts what the node
// now holds. The load-bearing rows: a 404 / 5xx / unsigned / foreign-org /
// foreign-key / replayed document NEVER clears or changes the last verified
// value, and the version is monotonic.
func TestFetchOrgDisplayPolicy(t *testing.T) {
	ctx := context.Background()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	_, otherPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ds := newDisplayPolicyServer(t)
	c, s, _ := enrolledClient(t, ds.srv.URL)

	v1Tokens := mustSignDisplay(t, priv, "org-1", 1, "tokens")
	inflated := v1Tokens
	inflated.Version = 50
	unsigned := mustSignDisplay(t, priv, "org-1", 2, "usd")
	unsigned.Signature = ""

	type want struct {
		has     bool
		value   string
		version int64
	}
	steps := []struct {
		name        string
		status      int
		doc         any
		raw         string
		wantChanged bool
		wantErr     bool
		want        want
	}{
		{name: "older server (404), nothing cached: still nothing", status: 404, want: want{}},
		{name: "signed v1 tokens is cached", status: 200, doc: v1Tokens, wantChanged: true, want: want{true, "tokens", 1}},
		{name: "same version again is a no-op", status: 200, doc: v1Tokens, want: want{true, "tokens", 1}},
		{name: "404 keeps the last verified value", status: 404, want: want{true, "tokens", 1}},
		{name: "5xx keeps the last verified value", status: 503, wantErr: true, want: want{true, "tokens", 1}},
		{name: "unsigned document refused, value kept", status: 200, doc: unsigned, wantErr: true, want: want{true, "tokens", 1}},
		{name: "document for another org refused", status: 200, doc: mustSignDisplay(t, priv, "org-2", 2, "usd"), wantErr: true, want: want{true, "tokens", 1}},
		{name: "document signed by a key that is not the org's refused", status: 200, doc: mustSignDisplay(t, otherPriv, "org-1", 2, "usd"), wantErr: true, want: want{true, "tokens", 1}},
		{name: "captured v1 replayed at an inflated version refused", status: 200, doc: inflated, wantErr: true, want: want{true, "tokens", 1}},
		// A non-JSON 200 is an older server's answer (railreply.go): nothing
		// applied, nothing cleared, and no per-cycle error.
		{name: "non-JSON body is an older server: value kept, no error", raw: "not json", want: want{true, "tokens", 1}},
		{name: "a malformed JSON body is still refused", raw: `{"version":`, wantErr: true, want: want{true, "tokens", 1}},
		{name: "signed v2 dollars applies", status: 200, doc: mustSignDisplay(t, priv, "org-1", 2, "usd"), wantChanged: true, want: want{true, "usd", 2}},
		{name: "an OLDER genuine document is not re-applied", status: 200, doc: v1Tokens, want: want{true, "usd", 2}},
		{name: "signed v3 tokens applies", status: 200, doc: mustSignDisplay(t, priv, "org-1", 3, "tokens"), wantChanged: true, want: want{true, "tokens", 3}},
	}
	for _, st := range steps {
		ds.status, ds.doc, ds.raw = st.status, st.doc, st.raw
		changed, err := c.FetchOrgDisplayPolicy(ctx)
		if (err != nil) != st.wantErr {
			t.Fatalf("%s: err = %v, wantErr %v", st.name, err, st.wantErr)
		}
		if changed != st.wantChanged {
			t.Errorf("%s: changed = %v, want %v", st.name, changed, st.wantChanged)
		}
		row, ok, gerr := s.GetOrgDisplayPolicy(ctx)
		if gerr != nil {
			t.Fatalf("%s: GetOrgDisplayPolicy: %v", st.name, gerr)
		}
		got := want{has: ok}
		if ok {
			got.value, got.version = row.DeveloperCostDisplay, row.Version
			if row.OrgID != "org-1" {
				t.Errorf("%s: cached org = %q", st.name, row.OrgID)
			}
		}
		if got != st.want {
			t.Errorf("%s: node holds %+v, want %+v", st.name, got, st.want)
		}
	}
}

// TestFetchOrgDisplayPolicyHonoursTheCrossRailPin: on a node whose
// announcement rail already pinned the org key, a FIRST display-policy fetch
// offering a different key is refused (no per-rail TOFU on a long-enrolled
// node), and nothing is cached.
func TestFetchOrgDisplayPolicyHonoursTheCrossRailPin(t *testing.T) {
	ctx := context.Background()
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	_, otherPriv, _ := ed25519.GenerateKey(rand.Reader)
	ds := newDisplayPolicyServer(t)
	c, s, _ := enrolledClient(t, ds.srv.URL)

	ann := signAnnouncementDoc(1, testAnnBody, priv, pub)
	if err := s.UpsertOrgAnnouncement(ctx, store.OrgAnnouncementRow{
		Version: 1, Body: ann.Body, BodyHash: ann.BodyHash, Signature: ann.Signature,
		ServerPubkey: ann.PublicKey, ReceivedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}

	ds.status, ds.doc = 200, mustSignDisplay(t, otherPriv, "org-1", 1, "tokens")
	if _, err := c.FetchOrgDisplayPolicy(ctx); err == nil {
		t.Fatal("a key that disagrees with the announcement rail's pin was accepted")
	}
	if _, ok, _ := s.GetOrgDisplayPolicy(ctx); ok {
		t.Fatal("a refused first fetch cached a row")
	}

	ds.doc = mustSignDisplay(t, priv, "org-1", 1, "tokens")
	if changed, err := c.FetchOrgDisplayPolicy(ctx); err != nil || !changed {
		t.Fatalf("the org's own key: changed=%v err=%v", changed, err)
	}
}

// TestDisplayPolicyClearedOnUnenrol: leaving the org drops its display choice
// (the dashboard is back to dollars, like any solo install).
func TestDisplayPolicyClearedOnUnenrol(t *testing.T) {
	ctx := context.Background()
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	ds := newDisplayPolicyServer(t)
	c, s, _ := enrolledClient(t, ds.srv.URL)
	ds.status, ds.doc = 200, mustSignDisplay(t, priv, "org-1", 1, "tokens")
	if _, err := c.FetchOrgDisplayPolicy(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteEnrolment(ctx); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.GetOrgDisplayPolicy(ctx); ok {
		t.Fatal("display policy survived unenrolment")
	}
}
