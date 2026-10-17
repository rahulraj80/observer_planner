package dashboard

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/guard/notify"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// T-496: `observer guard mcp reputation` as a dashboard control. The route
// runs notify.LookupNPMReputation, the function the CLI verb runs.

const reputationOptIn = `[guard.cloud]
enabled = true

[guard.cloud.reputation]
enabled = true
`

// stubRegistry answers every request with one status and body and records
// the URLs it was asked for.
type stubRegistry struct {
	status int
	body   string
	urls   []string
}

func (s *stubRegistry) Do(r *http.Request) (*http.Response, error) {
	s.urls = append(s.urls, r.URL.String())
	return &http.Response{StatusCode: s.status, Body: io.NopCloser(strings.NewReader(s.body)), Header: http.Header{}}, nil
}

func withRegistry(t *testing.T, reg *stubRegistry) {
	t.Helper()
	orig := guardReputationDoer
	guardReputationDoer = reg
	t.Cleanup(func() { guardReputationDoer = orig })
}

func postReputation(t *testing.T, s *Server, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := armLocalWrite(httptest.NewRequest(http.MethodPost, "/api/guard/mcp/reputation", strings.NewReader(body)))
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, r)
	return rr
}

func TestGuardMCPReputation_StatusReportsOptIn(t *testing.T) {
	off, _ := newSecretTestServer(t, "[observer]\nlog_level = \"info\"\n")
	rr := httptest.NewRecorder()
	off.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/guard/mcp/reputation", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status: %d %s", rr.Code, rr.Body.String())
	}
	m := decodeMap(t, rr)
	if m["available"] != false || !strings.Contains(m["off_reason"].(string), "opt in") || m["confirm_token"] == "" {
		t.Fatalf("off by default (D1) with the honest reason and a confirm token: %v", m)
	}

	on, _ := newSecretTestServer(t, reputationOptIn)
	rr = httptest.NewRecorder()
	on.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/guard/mcp/reputation", nil))
	if m := decodeMap(t, rr); m["available"] != true || m["off_reason"] != "" {
		t.Fatalf("opted in: %v", m)
	}
}

func TestGuardMCPReputation_RefusedWithoutOptIn(t *testing.T) {
	reg := &stubRegistry{status: 200, body: `{"name":"x"}`}
	withRegistry(t, reg)
	s, _ := newSecretTestServer(t, "[observer]\nlog_level = \"info\"\n")
	rr := postReputation(t, s, `{"package":"left-pad"}`)
	if rr.Code != http.StatusConflict || decodeMap(t, rr)["error"] != "reputation_off" {
		t.Fatalf("without the D1 opt-in: %d %s", rr.Code, rr.Body.String())
	}
	if len(reg.urls) != 0 {
		t.Fatalf("a refused lookup must make no request: %v", reg.urls)
	}
}

func TestGuardMCPReputation_LookupMatchesTheCLI(t *testing.T) {
	doc := `{"name":"@scope/pkg","description":"an MCP server","dist-tags":{"latest":"1.2.3"},` +
		`"versions":{"1.0.0":{},"1.2.3":{}},"time":{"created":"2020-01-01T00:00:00Z","modified":"2020-02-01T00:00:00Z"},` +
		`"maintainers":[{"name":"a"}]}`
	reg := &stubRegistry{status: 200, body: doc}
	withRegistry(t, reg)
	s, _ := newSecretTestServer(t, reputationOptIn)
	rr := postReputation(t, s, `{"package":"@scope/pkg"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("lookup: %d %s", rr.Code, rr.Body.String())
	}
	if len(reg.urls) != 1 || reg.urls[0] != "https://registry.npmjs.org/@scope%2Fpkg" {
		t.Fatalf("exactly one registry request through the egress allowlist: %v", reg.urls)
	}
	var got guardReputationResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	info, err := notify.ParseNPMMetadata([]byte(doc), s.now())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got.Lines, "\n") != strings.Join(notify.FormatNPMInfo(info), "\n") {
		t.Fatalf("the dashboard must render the CLI's own lines:\n%v", got.Lines)
	}
	if got.Young || got.Warning != "" || got.Latest != "1.2.3" || got.VersionCount != 2 || got.CreatedAt != "2020-01-01" {
		t.Fatalf("response: %+v", got)
	}
	rows, err := store.New(s.opts.DB).RecentRemoteAudit(context.Background(), 5)
	if err != nil || len(rows) == 0 || !strings.Contains(rows[0].Detail, "guard_mcp_reputation package=@scope/pkg result=ok") {
		t.Fatalf("the lookup is audited: %+v %v", rows, err)
	}
}

func TestGuardMCPReputation_YoungPackageWarns(t *testing.T) {
	s, _ := newSecretTestServer(t, reputationOptIn)
	created := s.now().AddDate(0, 0, -3).UTC().Format("2006-01-02T15:04:05Z")
	withRegistry(t, &stubRegistry{status: 200, body: `{"name":"fresh","dist-tags":{"latest":"0.0.1"},"time":{"created":"` + created + `"}}`})
	rr := postReputation(t, s, `{"package":"fresh"}`)
	var got guardReputationResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &got)
	if rr.Code != http.StatusOK || !got.Young || got.Warning != notify.YoungPackageWarning {
		t.Fatalf("young package: %d %s", rr.Code, rr.Body.String())
	}
}

func TestGuardMCPReputation_ErrorsAreHonest(t *testing.T) {
	cases := []struct {
		name, body string
		reg        *stubRegistry
		status     int
		code       string
	}{
		{"not found", `{"package":"no-such-pkg"}`, &stubRegistry{status: 404, body: `{"error":"Not found"}`}, http.StatusNotFound, "not_found"},
		{"registry error", `{"package":"left-pad"}`, &stubRegistry{status: 503, body: ""}, http.StatusBadGateway, "lookup_failed"},
		{"empty package", `{"package":"  "}`, &stubRegistry{status: 200}, http.StatusBadRequest, "invalid_package"},
		{"two names", `{"package":"a b"}`, &stubRegistry{status: 200}, http.StatusBadRequest, "invalid_package"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withRegistry(t, tc.reg)
			s, _ := newSecretTestServer(t, reputationOptIn)
			rr := postReputation(t, s, tc.body)
			if rr.Code != tc.status || decodeMap(t, rr)["error"] != tc.code {
				t.Fatalf("%d %s, want %d %s", rr.Code, rr.Body.String(), tc.status, tc.code)
			}
			if tc.status == http.StatusBadRequest && len(tc.reg.urls) != 0 {
				t.Fatal("an invalid name must make no request")
			}
		})
	}
}
