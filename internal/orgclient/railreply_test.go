package orgclient

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/orgcontract"
	"github.com/marmutapp/superbased-observer/internal/policyfam"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// spaPage is what an org server that predates a rail answers for an unknown
// GET: its dashboard's index.html with a 200 (demo org server, 2026-10-03).
const spaPage = "<!doctype html><html><head><title>Observer</title></head><body><div id=\"root\"></div></body></html>"

func resetRailNotSupportedLog() {
	railNotSupportedLogged.Range(func(k, _ any) bool {
		railNotSupportedLogged.Delete(k)
		return true
	})
}

func TestClassifyRailReply(t *testing.T) {
	big := `{"a":"` + strings.Repeat("x", 2000) + `"}`
	cases := []struct {
		name        string
		status      int
		contentType string
		body        string
		want        railReply
		reason      string
	}{
		{"json document", 200, "application/json", `{"version":1}`, railReplyAnswered, ""},
		{"json after whitespace, no content type", 200, "", "\n  [1]", railReplyAnswered, ""},
		{"large json", 200, "application/json", big, railReplyAnswered, ""},
		{"spa page by content type", 200, "text/html; charset=utf-8", spaPage, railReplyNotSupported, "html page"},
		{"spa page by first byte", 200, "", "\n" + spaPage, railReplyNotSupported, "html page"},
		{"html mislabelled as json", 200, "application/json", spaPage, railReplyNotSupported, "html page"},
		{"plain text", 200, "text/plain", "OK", railReplyNotSupported, "non-JSON body"},
		{"empty 200 stays the rail's decode error", 200, "application/json", "", railReplyAnswered, ""},
		{"404", 404, "text/plain", "404 page not found", railReplyNotSupported, "status 404"},
		{"405", 405, "", "", railReplyNotSupported, "status 405"},
		{"501", 501, "", "", railReplyNotSupported, "status 501"},
		{"304", 304, "", "", railReplyAnswered, ""},
		{"401", 401, "application/json", `{"error":"x"}`, railReplyAnswered, ""},
		{"409", 409, "application/json", `{"error":"x"}`, railReplyAnswered, ""},
		{"503 html from a proxy is an outage, not a missing rail", 503, "text/html", spaPage, railReplyAnswered, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := &http.Response{StatusCode: tc.status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(tc.body))}
			if tc.contentType != "" {
				resp.Header.Set("Content-Type", tc.contentType)
			}
			got, reason := classifyRailReply(resp)
			if got != tc.want || reason != tc.reason {
				t.Fatalf("classify = %v %q, want %v %q", got, reason, tc.want, tc.reason)
			}
			// The body must reach the rail's decoder byte for byte.
			rest, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatal(err)
			}
			if string(rest) != tc.body {
				t.Fatalf("body after classify = %d bytes, want the original %d", len(rest), len(tc.body))
			}
		})
	}
}

// olderOrgServer answers EVERY request the way a server that predates the
// node's rails does: the SPA's index.html with a 200.
func olderOrgServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(spaPage))
	}))
	t.Cleanup(srv.Close)
	return srv
}

type logSink struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *logSink) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func (l *logSink) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}

// TestRailsTreatAnOlderServersPageAsNotSupported runs every node-pulled
// signed rail against an older server's SPA page: each one keeps its state,
// returns NO error (no per-cycle spam), and the rail logs at most one quiet
// line per process. Before the shared classifier every rail tried to decode
// the page ("invalid character '<' looking for beginning of value").
func TestRailsTreatAnOlderServersPageAsNotSupported(t *testing.T) {
	resetRailNotSupportedLog()
	t.Cleanup(resetRailNotSupportedLog)
	ctx := context.Background()
	srv := olderOrgServer(t)
	c, s, bundlePath := enrolledIntelClientWithBundle(t, srv.URL)
	sink := &logSink{}
	c.logger = slog.New(slog.NewTextHandler(sink, nil))
	enabledPricing(c)

	rails := []struct {
		rail  string
		fetch func() error
	}{
		{displayPolicyRail, func() error {
			changed, err := c.FetchOrgDisplayPolicy(ctx)
			if changed {
				t.Error("display policy changed on a page")
			}
			return err
		}},
		{announcementRail, func() error {
			changed, err := c.FetchOrgAnnouncement(ctx)
			if changed {
				t.Error("announcement changed on a page")
			}
			return err
		}},
		{routingPolicyRail, func() error {
			changed, out, err := c.FetchRoutingPolicy(ctx)
			if changed || out == nil || !out.OK {
				t.Errorf("routing outcome = changed %v %+v, want unchanged OK (none published)", changed, out)
			}
			return err
		}},
		{pricingPolicyRail, func() error {
			out, err := c.FetchPricingPolicy(ctx)
			if out.State != orgcontract.PricingFetchNotSupported {
				t.Errorf("pricing state = %q, want not_supported", out.State)
			}
			return err
		}},
		{budgetPolicyRail, func() error {
			out, err := c.FetchBudgetPolicy(ctx)
			if out.HaveBody {
				t.Errorf("budget applied a body from a page: %+v", out)
			}
			return err
		}},
		{intelResultsRail, func() error {
			out, err := c.FetchIntelResults(ctx)
			if out.State != IntelFetchNotSupported {
				t.Errorf("intel state = %q, want not_supported", out.State)
			}
			return err
		}},
		{policyBundleRail, func() error {
			res, err := c.FetchPolicyBundle(ctx, bundlePath)
			if res.Status != PolicyNone {
				t.Errorf("policy bundle status = %v, want none", res.Status)
			}
			return err
		}},
		{policyResourceRail, func() error {
			res, err := c.FetchAndAcceptPolicyResource(ctx, policyfam.FamilyMCPAccess,
				PolicyResourceOptions{CacheDir: filepath.Join(t.TempDir(), "res")})
			if res.Status != PRNone {
				t.Errorf("policy resource status = %v, want none", res.Status)
			}
			return err
		}},
	}
	// The update rail exists only in a build with the org.update component.
	rails = append(rails, updateRailPageCases(ctx, t, c)...)
	for _, r := range rails {
		for pass := 0; pass < 2; pass++ {
			// Two passes: the second proves the quiet line is not repeated.
			c.routingNotFound.Reset()
			c.announcementNotFound.Reset()
			c.intelNotFound.Reset()
			if err := r.fetch(); err != nil {
				t.Errorf("%s pass %d: error on an older server's page: %v", r.rail, pass, err)
			}
		}
		if n := strings.Count(sink.String(), "rail="+r.rail+" "); n != 1 {
			t.Errorf("%s: logged %d not-supported lines, want exactly 1", r.rail, n)
		}
	}
	if _, ok, _ := s.GetOrgDisplayPolicy(ctx); ok {
		t.Error("a display policy was cached from a page")
	}
	if strings.Contains(sink.String(), "invalid character") {
		t.Errorf("a rail still decoded the page: %s", sink.String())
	}
}

// enrolledIntelClientWithBundle is enrolledIntelClient plus a bundle cache
// path for the policy-bundle rail.
func enrolledIntelClientWithBundle(t *testing.T, srvURL string) (*Client, *store.Store, string) {
	t.Helper()
	c, s := enrolledIntelClient(t, srvURL)
	// The policy-resource rail reads the enrolment generation (see
	// enrolledPRClient).
	if _, err := s.BumpEnrolmentGeneration(context.Background(), OrgKey(srvURL, "org-1"), false); err != nil {
		t.Fatalf("BumpEnrolmentGeneration: %v", err)
	}
	return c, s, filepath.Join(t.TempDir(), "org-policy-bundle.json")
}
