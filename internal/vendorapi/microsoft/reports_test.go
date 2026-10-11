package microsoft

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/vendorapi"
	vs "github.com/marmutapp/superbased-observer/internal/vendorsurface"
)

func fixture(t *testing.T, surface vs.SurfaceID, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "vendorapi", "microsoft", string(surface), name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func resp(path string, body []byte) vendorapi.Response {
	return vendorapi.Response{Status: 200, Origin: "https://graph.microsoft.com:443", Path: path, Body: body}
}

const detailPath = "/v1.0/copilot/reports/getMicrosoft365CopilotUsageUserDetail(period='D7')"

func TestBuildPicksThePeriodAndFollowsOnlyItsOwnNextLink(t *testing.T) {
	b := UsageUserDetail().Build
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	for span, want := range map[time.Duration]string{
		24 * time.Hour: "D7", 7 * day: "D7", 8 * day: "D28", 28 * day: "D28", 60 * day: "D90", 120 * day: "D180",
	} {
		r, err := b(nil, vendorapi.Window{From: start, To: start.Add(span)}, "")
		if err != nil || r.Method != "GET" || !strings.HasSuffix(r.Path, "(period='"+want+"')") ||
			r.Query["$format"][0] != "application/json" || r.Body != nil {
			t.Errorf("span %s: %+v %v, want %s", span, r, err, want)
		}
	}
	r, err := b(nil, vendorapi.Window{}, detailPath+"?$format=application/json&$skiptoken=abc")
	if err != nil || r.Path != detailPath || len(r.Query["$skiptoken"]) != 1 || r.Query["$skiptoken"][0] != "abc" {
		t.Fatalf("next link cursor: %+v %v", r, err)
	}
	for name, cursor := range map[string]string{
		"other report":    "/v1.0/copilot/reports/getMicrosoft365CopilotUserCountTrend(period='D7')?$skiptoken=a",
		"other api":       "/v1.0/users?$skiptoken=a",
		"no query":        detailPath,
		"no skip":         detailPath + "?$format=application/json",
		"unknown member":  detailPath + "?$skiptoken=a&$filter=x",
		"unparsed query":  detailPath + "?%zz",
		"empty first win": "",
	} {
		if _, err := b(nil, vendorapi.Window{}, cursor); err == nil {
			t.Errorf("%s: built", name)
		}
	}
}

func TestUserDetailParsesCorpusFixtures(t *testing.T) {
	p, err := parseUserDetail(resp(detailPath, fixture(t, SurfaceUsageUserDetail, "normal_page.json")))
	if err != nil {
		t.Fatal(err)
	}
	if err := vendorapi.Validate(p); err != nil {
		t.Fatalf("parsed page does not validate: %v", err)
	}
	if len(p.Snapshots) != 1 || p.NextCursor != detailPath+"?$format=application/json&$skiptoken=SYNTHETIC-SKIPTOKEN-1" {
		t.Fatalf("snapshots %d next %q", len(p.Snapshots), p.NextCursor)
	}
	s := p.Snapshots[0]
	if s.Class != UserActivityClass || s.Subject != "dc8c64d6ec3a3aa17481d7e5eb5b68a6" ||
		s.Body["report_refresh_date"] != "2025-08-20" || s.Body["report_period"] != "7" ||
		s.Body["teams_last_activity_date"] != "2025-08-20" || s.Body["powerpoint_last_activity_date"] != "2025-03-26" {
		t.Fatalf("snapshot = %+v", s)
	}
	if _, ok := s.Body["excel_last_activity_date"]; ok {
		t.Error("an empty date (no activity) lands as a value")
	}
	for k, v := range s.Body {
		if strings.Contains(k, "display") || strings.Contains(v, "C65E8D8A") {
			t.Errorf("the display name lands: %s=%s", k, v)
		}
	}
	last, err := parseUserDetail(resp(detailPath, fixture(t, SurfaceUsageUserDetail, "last_page.json")))
	if err != nil || last.NextCursor != "" || last.Snapshots[0].Subject != "adelev@contoso.com" {
		t.Fatalf("last page: %+v %v", last, err)
	}
}

func TestNextLinkStaysOnTheRequestOriginAndPath(t *testing.T) {
	page := func(link string) []byte { return []byte(`{"value":[],"@odata.nextLink":` + link + `}`) }
	for name, link := range map[string]string{
		"foreign host":   `"https://evil.example` + detailPath + `?$skiptoken=a"`,
		"foreign port":   `"https://graph.microsoft.com:8443` + detailPath + `?$skiptoken=a"`,
		"http downgrade": `"http://graph.microsoft.com` + detailPath + `?$skiptoken=a"`,
		"userinfo":       `"https://u:p@graph.microsoft.com` + detailPath + `?$skiptoken=a"`,
		"other path":     `"https://graph.microsoft.com/v1.0/users?$skiptoken=a"`,
		"relative":       `"` + detailPath + `?$skiptoken=a"`,
		"fragment":       `"https://graph.microsoft.com` + detailPath + `?$skiptoken=a#x"`,
	} {
		if _, err := parseUserDetail(resp(detailPath, page(link))); !errors.Is(err, vendorapi.ErrSchemaDrift) {
			t.Errorf("%s: err %v, want schema drift", name, err)
		}
	}
	if _, err := parseUserDetail(resp(detailPath, page(`1`))); err == nil {
		t.Error("a numeric next link decoded")
	}
	p, err := parseUserDetail(resp(detailPath, page(`"https://GRAPH.microsoft.com:443`+detailPath+`?$skiptoken=a"`)))
	if err != nil || p.NextCursor != detailPath+"?$skiptoken=a" {
		t.Fatalf("explicit default port: %+v %v", p, err)
	}
}

func TestUserCountSummaryParsesCorpusFixture(t *testing.T) {
	p, err := parseSummary(resp("/x", fixture(t, SurfaceUserCountSummary, "normal_page.json")))
	if err != nil {
		t.Fatal(err)
	}
	if err := vendorapi.Validate(p); err != nil {
		t.Fatalf("parsed page does not validate: %v", err)
	}
	if len(p.Facts) != 2*len(apps) {
		t.Fatalf("facts %d", len(p.Facts))
	}
	end := time.Date(2025, 8, 21, 0, 0, 0, 0, time.UTC)
	for _, f := range p.Facts {
		if !f.IntervalEnd.Equal(end) || !f.IntervalStart.Equal(end.Add(-7*day)) || f.Identity != "" ||
			f.CoverageDomain != CoverageDomain || f.Unit != "users" {
			t.Fatalf("fact %+v", f)
		}
		if f.Feature == "copilot_chat" && f.Metric == "active_users" && f.ValueMicros != 65480_000_000 {
			t.Fatalf("copilot chat active %+v", f)
		}
	}
}

func TestUserCountTrendParsesCorpusFixture(t *testing.T) {
	p, err := parseTrend(resp("/x", fixture(t, SurfaceUserCountTrend, "normal_page.json")))
	if err != nil {
		t.Fatal(err)
	}
	if err := vendorapi.Validate(p); err != nil {
		t.Fatalf("parsed page does not validate: %v", err)
	}
	if len(p.Facts) != 2*len(apps) {
		t.Fatalf("facts %d", len(p.Facts))
	}
	d := time.Date(2025, 8, 20, 0, 0, 0, 0, time.UTC)
	for _, f := range p.Facts {
		if !f.IntervalStart.Equal(d) || !f.IntervalEnd.Equal(d.Add(day)) {
			t.Fatalf("fact %+v", f)
		}
		if f.Feature == "any_app" && f.Metric == "enabled_users" && f.ValueMicros != 357584_000_000 {
			t.Fatalf("any app enabled %+v", f)
		}
	}
}

func TestCountReportsRefuseDrift(t *testing.T) {
	for name, body := range map[string]string{
		"no value":        `{}`,
		"no refresh":      `{"value":[{"adoptionByProduct":[]}]}`,
		"no products":     `{"value":[{"reportRefreshDate":"2025-08-20"}]}`,
		"bad period":      `{"value":[{"reportRefreshDate":"2025-08-20","adoptionByProduct":[{"reportPeriod":0}]}]}`,
		"quoted count":    `{"value":[{"reportRefreshDate":"2025-08-20","adoptionByProduct":[{"reportPeriod":7,"wordActiveUsers":"5"}]}]}`,
		"negative count":  `{"value":[{"reportRefreshDate":"2025-08-20","adoptionByProduct":[{"reportPeriod":7,"wordActiveUsers":-5}]}]}`,
		"repeated period": `{"value":[{"reportRefreshDate":"2025-08-20","adoptionByProduct":[{"reportPeriod":7},{"reportPeriod":7}]}]}`,
	} {
		if _, err := parseSummary(resp("/x", []byte(body))); !errors.Is(err, vendorapi.ErrSchemaDrift) {
			t.Errorf("summary %s: err %v, want schema drift", name, err)
		}
	}
	for name, body := range map[string]string{
		"no dates":     `{"value":[{"reportRefreshDate":"2025-08-20","reportPeriod":7}]}`,
		"no period":    `{"value":[{"reportRefreshDate":"2025-08-20","adoptionByDate":[]}]}`,
		"bad date":     `{"value":[{"reportRefreshDate":"2025-08-20","reportPeriod":7,"adoptionByDate":[{"reportDate":"20/08/2025"}]}]}`,
		"repeated day": `{"value":[{"reportRefreshDate":"2025-08-20","reportPeriod":7,"adoptionByDate":[{"reportDate":"2025-08-20"},{"reportDate":"2025-08-20"}]}]}`,
	} {
		if _, err := parseTrend(resp("/x", []byte(body))); !errors.Is(err, vendorapi.ErrSchemaDrift) {
			t.Errorf("trend %s: err %v, want schema drift", name, err)
		}
	}
	for name, body := range map[string]string{
		"no upn":       `{"value":[{"reportRefreshDate":"2025-08-20"}]}`,
		"bad activity": `{"value":[{"reportRefreshDate":"2025-08-20","userPrincipalName":"a@x.test","wordCopilotLastActivityDate":"yesterday"}]}`,
		"repeat user":  `{"value":[{"reportRefreshDate":"2025-08-20","userPrincipalName":"a@x.test"},{"reportRefreshDate":"2025-08-20","userPrincipalName":"A@x.test"}]}`,
	} {
		if _, err := parseUserDetail(resp(detailPath, []byte(body))); !errors.Is(err, vendorapi.ErrSchemaDrift) {
			t.Errorf("detail %s: err %v, want schema drift", name, err)
		}
	}
}

// TestReportsReplaceNoLegacyRail pins the per-surface ownership reconciliation
// with m365copilotanalytics: that package's Graph rail reads interaction
// content (getAllEnterpriseInteractions) and the reports rail none of these
// three report functions, so no report driver names a legacy poller and the
// ownership route keeps refusing the m365 rails (nothing here can cut them
// over). Every driver authenticates through the token step.
func TestReportsReplaceNoLegacyRail(t *testing.T) {
	for _, d := range []vendorapi.Driver{UsageUserDetail(), UserCountSummary(), UserCountTrend()} {
		if d.LegacyPoller != "" {
			t.Errorf("%s names legacy poller %q", d.Surface, d.LegacyPoller)
		}
		if d.Auth.Scheme != vendorapi.SchemeOAuthClientCredentials || d.Auth.Kind != vs.AuthOAuthClientCredentials {
			t.Errorf("%s auth %+v", d.Surface, d.Auth)
		}
		if d.Tool != Tool || d.MaxLookback != MaxLookback {
			t.Errorf("%s tool %q lookback %s", d.Surface, d.Tool, d.MaxLookback)
		}
	}
}
