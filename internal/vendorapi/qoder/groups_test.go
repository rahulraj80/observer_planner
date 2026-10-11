package qoder

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/vendorapi"
	vs "github.com/marmutapp/superbased-observer/internal/vendorsurface"
)

func surfaceFixture(t *testing.T, s vs.SurfaceID, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "vendorapi", "qoder", string(s), name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func validated(t *testing.T, p vendorapi.Page) {
	t.Helper()
	if p.Snapshot != nil {
		p.Snapshot.ManifestID, p.Snapshot.ObservedAt = "run-1", time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	}
	if err := vendorapi.Validate(p); err != nil {
		t.Fatal(err)
	}
}

// TestGroupsAndBillingGroups (lane ENTAPI-L16C, T-710): the documented
// examples land; nextToken pages and is omitted on the last page; the
// description never lands; the limit amount reads -1 as unlimited and 0 as
// frozen; every field-policy row is schema drift.
func TestGroupsAndBillingGroups(t *testing.T) {
	p, err := parseGroups(resp(surfaceFixture(t, SurfaceGroups, "normal_page.json")))
	if err != nil {
		t.Fatal(err)
	}
	validated(t, p)
	if len(p.Resources) != 1 || p.NextCursor != "cursor_from_response" {
		t.Fatalf("groups page %+v", p)
	}
	g := p.Resources[0]
	if g.Type != GroupType || g.DisplayName != "Platform Team" || g.Attrs["source"] != "manual" ||
		g.Attrs["is_managed"] != "false" || g.Attrs["member_count"] != "2" || g.Attrs["created_at"] != "2026-08-01T08:00:00Z" {
		t.Errorf("group %+v", g)
	}
	if _, ok := g.Attrs["description"]; ok {
		t.Error("the description landed")
	}
	r := resp(surfaceFixture(t, SurfaceGroups, "last_page.json"))
	r.Cursor = p.NextCursor
	p2, err := parseGroups(r)
	if err != nil || p2.NextCursor != "" || len(p2.Resources) != 2 || p2.Resources[0].Attrs["sync_type"] != "scim" {
		t.Fatalf("groups last page %+v %v", p2, err)
	}

	b, err := parseBillingGroups(resp(surfaceFixture(t, SurfaceBillingGroups, "normal_page.json")))
	if err != nil {
		t.Fatal(err)
	}
	validated(t, b)
	if len(b.Resources) != 2 || b.NextCursor != "cursor_from_response" {
		t.Fatalf("billing page %+v", b)
	}
	un, plat := b.Resources[0], b.Resources[1]
	if un.ID.Value != "unassigned" || un.Attrs["limit_state"] != "unlimited" || un.Attrs["limit_amount"] != "" ||
		un.Attrs["current_used"] != "320" || un.Attrs["period_start"] != "2026-08-01T00:00:00Z" {
		t.Errorf("unassigned %+v", un)
	}
	if plat.Attrs["limit_state"] != "set" || plat.Attrs["limit_amount"] != "10000" || plat.Attrs["member_count"] != "12" ||
		plat.Attrs["is_blocked"] != "false" || plat.Attrs["unit"] != "credits" {
		t.Errorf("platform %+v", plat)
	}
	r = resp(surfaceFixture(t, SurfaceBillingGroups, "last_page.json"))
	r.Cursor = b.NextCursor
	b2, err := parseBillingGroups(r)
	if err != nil || b2.NextCursor != "" || len(b2.Resources) != 1 || b2.Resources[0].Attrs["limit_state"] != "frozen" ||
		b2.Resources[0].Attrs["is_blocked"] != "true" {
		t.Fatalf("billing last page %+v %v", b2, err)
	}

	for name, c := range map[string]struct {
		parse func(vendorapi.Response) (vendorapi.Page, error)
		body  string
	}{
		"groups missing":       {parseGroups, `{"maxResults":20}`},
		"group id missing":     {parseGroups, `{"groups":[{"displayName":"a","source":"manual","isManaged":false,"memberCount":1}]}`},
		"member count quoted":  {parseGroups, `{"groups":[{"id":"g","displayName":"a","source":"manual","isManaged":false,"memberCount":"1"}]}`},
		"member count missing": {parseGroups, `{"groups":[{"id":"g","displayName":"a","source":"manual","isManaged":false}]}`},
		"group time":           {parseGroups, `{"groups":[{"id":"g","displayName":"a","source":"manual","isManaged":false,"memberCount":1,"createdAt":"x"}]}`},
		"repeated group":       {parseGroups, `{"groups":[{"id":"g","displayName":"a","source":"manual","isManaged":false,"memberCount":1},{"id":"g","displayName":"a","source":"manual","isManaged":false,"memberCount":1}]}`},
		"token type":           {parseGroups, `{"groups":[],"nextToken":5}`},
		"billing missing":      {parseBillingGroups, `{"maxResults":20}`},
		"limit missing":        {parseBillingGroups, `{"billingGroups":[{"id":"b","name":"B","isUnassigned":false,"memberCount":1,"currentUsed":0,"isBlocked":false}]}`},
		"limit undocumented":   {parseBillingGroups, `{"billingGroups":[{"id":"b","name":"B","isUnassigned":false,"memberCount":1,"limitAmount":-2,"currentUsed":0,"isBlocked":false}]}`},
		"used negative":        {parseBillingGroups, `{"billingGroups":[{"id":"b","name":"B","isUnassigned":false,"memberCount":1,"limitAmount":5,"currentUsed":-1,"isBlocked":false}]}`},
		"unassigned mismatch":  {parseBillingGroups, `{"billingGroups":[{"id":"b","name":"B","isUnassigned":true,"memberCount":1,"limitAmount":5,"currentUsed":1,"isBlocked":false}]}`},
		"period inverted":      {parseBillingGroups, `{"billingGroups":[],"selectedPeriod":{"periodStart":"2026-09-01T00:00:00Z","periodEnd":"2026-08-01T00:00:00Z"}}`},
	} {
		if _, err := c.parse(resp([]byte(c.body))); !errors.Is(err, vendorapi.ErrSchemaDrift) {
			t.Errorf("%s: err %v, want schema drift", name, err)
		}
	}
	rr := resp([]byte(`{"billingGroups":[],"nextToken":"t"}`))
	rr.Cursor = "t"
	if _, err := parseBillingGroups(rr); err == nil {
		t.Error("a repeated token was accepted")
	}
	for _, d := range []vendorapi.Driver{Groups(), BillingGroups()} {
		req, err := d.Build(vendorapi.Params{ParamOrganization: "org_xxx"}, vendorapi.Window{}, "tok")
		if err != nil || req.Query["maxResults"][0] != "100" || req.Query["nextToken"][0] != "tok" ||
			req.Query["keyword"] != nil || req.Query["periodEnd"] != nil {
			t.Errorf("%s request %+v %v", d.Surface, req, err)
		}
		if _, err := d.Build(vendorapi.Params{ParamOrganization: "../x"}, vendorapi.Window{}, ""); err == nil {
			t.Errorf("%s accepted a path-breaking organization", d.Surface)
		}
	}
}

// TestDailyTrend (lane ENTAPI-L16C, T-710): the documented example lands the
// lines, commits and Tab completion counts per day; deprecated aliases,
// rates and extItems never land; every field-policy row is schema drift.
func TestDailyTrend(t *testing.T) {
	p, err := parseDailyTrend(resp(surfaceFixture(t, SurfaceDailyTrend, "normal_page.json")))
	if err != nil {
		t.Fatal(err)
	}
	validated(t, p)
	want := map[string]int64{
		"total_lines_changed": 2000, "ai_lines_changed": 500, "other_lines_changed": 1500, "other_lines_added": 1500,
		"commits": 30, "tab_suggestions": 200, "tab_acceptances": 120,
	}
	day := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	if len(p.Facts) != len(want) {
		t.Fatalf("facts %+v", p.Facts)
	}
	for _, f := range p.Facts {
		if want[f.Metric] != f.ValueMicros/1_000_000 || !f.IntervalStart.Equal(day) || f.CoverageDomain != Domain {
			t.Errorf("fact %+v", f)
		}
	}
	for name, body := range map[string]string{
		"items missing":   `{"nextItems":[]}`,
		"next missing":    `{"items":[]}`,
		"date offset":     `{"items":[{"date":"2025-06-01T08:00:00+05:00","totalLinesChanged":1,"aiLinesChanged":1,"otherLinesChanged":0,"otherLinesAdded":0,"commitCount":1}],"nextItems":[]}`,
		"date not day":    `{"items":[{"date":"2025-06-01T03:00:00Z","totalLinesChanged":1,"aiLinesChanged":1,"otherLinesChanged":0,"otherLinesAdded":0,"commitCount":1}],"nextItems":[]}`,
		"count missing":   `{"items":[{"date":"2025-06-01T00:00:00Z","totalLinesChanged":1,"aiLinesChanged":1,"otherLinesChanged":0,"otherLinesAdded":0}],"nextItems":[]}`,
		"count quoted":    `{"items":[],"nextItems":[{"date":"2025-06-01T00:00:00Z","nextSuggestedCount":"2","nextAcceptedCount":1}]}`,
		"count fraction":  `{"items":[],"nextItems":[{"date":"2025-06-01T00:00:00Z","nextSuggestedCount":2.5,"nextAcceptedCount":1}]}`,
		"repeated day":    `{"items":[],"nextItems":[{"date":"2025-06-01T00:00:00Z","nextSuggestedCount":2,"nextAcceptedCount":1},{"date":"2025-06-01T00:00:00Z","nextSuggestedCount":2,"nextAcceptedCount":1}]}`,
		"negative count":  `{"items":[],"nextItems":[{"date":"2025-06-01T00:00:00Z","nextSuggestedCount":-2,"nextAcceptedCount":1}]}`,
		"date not string": `{"items":[],"nextItems":[{"date":20250601,"nextSuggestedCount":2,"nextAcceptedCount":1}]}`,
	} {
		if _, err := parseDailyTrend(resp([]byte(body))); !errors.Is(err, vendorapi.ErrSchemaDrift) {
			t.Errorf("%s: err %v, want schema drift", name, err)
		}
	}
	from := time.Date(2026, 9, 10, 5, 0, 0, 0, time.UTC)
	req, err := buildDailyTrend(vendorapi.Params{ParamOrganization: "org_xxx"}, vendorapi.Window{From: from, To: from.Add(48 * time.Hour)}, "")
	if err != nil || req.Path != "/v1/organizations/org_xxx/ai-code/stats/daily-trend" ||
		req.Query["start_date"][0] != "2026-09-09T00:00:00Z" || req.Query["end_date"][0] != "2026-09-12T05:00:00Z" {
		t.Fatalf("request %+v %v", req, err)
	}
	if _, err := buildDailyTrend(vendorapi.Params{ParamOrganization: "org_xxx"}, vendorapi.Window{From: from, To: from.Add(40 * 24 * time.Hour)}, ""); err == nil {
		t.Error("a window over 31 days was accepted")
	}
}
