package github

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

func sfx(t *testing.T, id vs.SurfaceID, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "vendorapi", "github", string(id), name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func ok200(body []byte) vendorapi.Response {
	return vendorapi.Response{
		Status: 200, Origin: "https://api.github.com:443", Body: body,
		Params: vendorapi.Params{"org": "acme", "enterprise": "acme"},
	}
}

func validActivity(t *testing.T, p vendorapi.Page) {
	t.Helper()
	p.Snapshot = &vendorapi.SnapshotManifest{ManifestID: "run-1", ObservedAt: time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)}
	if err := vendorapi.Validate(p); err != nil {
		t.Fatalf("page does not validate: %v", err)
	}
}

func isDrift(err error) bool { return err != nil && errors.Is(err, vendorapi.ErrSchemaDrift) }

func fact(p vendorapi.Page, metric, segment, identity, feature string) (vendorapi.FactRow, bool) {
	for _, f := range p.Facts {
		if f.Metric == metric && f.TrafficSegment == segment && f.Identity == identity && f.Feature == feature {
			return f, true
		}
	}
	return vendorapi.FactRow{}, false
}

// TestMetricsReportsParseCorpus (lane L8): every built metrics report reads
// its corpus first hop (the download links) and parses its corpus report
// file into valid facts; the per-line values land as micro-units of their
// unit, per user, per enterprise, per client segment and per feature.
func TestMetricsReportsParseCorpus(t *testing.T) {
	day := time.Date(2025, 10, 1, 0, 0, 0, 0, time.UTC)
	for id, newDriver := range MetricsReports() {
		t.Run(string(id), func(t *testing.T) {
			d := newDriver()
			links, err := d.Report.Links(ok200(sfx(t, id, "report_link.json")))
			if err != nil || len(links) != 2 || !strings.HasPrefix(links[0], "https://example.com/") {
				t.Fatalf("links %v err %v", links, err)
			}
			p, err := d.ParseResponse(ok200(sfx(t, id, "report_file.ndjson")))
			if err != nil {
				t.Fatal(err)
			}
			validActivity(t, p)
			if len(p.Facts) == 0 || p.SourceRecords == 0 || p.NextCursor != "" {
				t.Fatalf("facts %d records %d next %q", len(p.Facts), p.SourceRecords, p.NextCursor)
			}
			for _, f := range p.Facts {
				if f.SourceTZ != "UTC" || f.IntervalEnd.Sub(f.IntervalStart) != 24*time.Hour || f.Final || f.ValueMicros < 0 {
					t.Fatalf("fact %+v", f)
				}
			}
		})
	}
	ent, _ := MetricsReports()[SurfaceMetricsEnterprise1d]().ParseResponse(ok200(sfx(t, SurfaceMetricsEnterprise1d, "report_file.ndjson")))
	if f, ok := fact(ent, "daily_active_users", "enterprise:1", "", ""); !ok || f.Unit != "users" || f.ValueMicros != 2_000_000 ||
		!f.IntervalStart.Equal(day) {
		t.Fatalf("enterprise daily_active_users %+v %v", f, ok)
	}
	if f, ok := fact(ent, "prompt_tokens_sum", "enterprise:1:cli", "", ""); !ok || f.Unit != "tokens" {
		t.Fatalf("cli tokens %+v %v", f, ok)
	}
	if _, ok := fact(ent, "daily_active_users", "enterprise:2", "", ""); !ok {
		t.Fatal("the second enterprise line folded into the first")
	}
	if f, ok := fact(ent, "loc_added_sum", "enterprise:1", "", "code_completion"); !ok || f.Unit != "lines" {
		t.Fatalf("feature lines %+v %v", f, ok)
	}
	users, _ := MetricsReports()[SurfaceMetricsUsers1d]().ParseResponse(ok200(sfx(t, SurfaceMetricsUsers1d, "report_file.ndjson")))
	var credits vendorapi.FactRow
	for _, f := range users.Facts {
		if f.IdentityKind != "user_id" || f.Identity == "" {
			t.Fatalf("user fact without its user id: %+v", f)
		}
		if f.Metric == "ai_credits_used" && f.Feature == "" && f.TrafficSegment == "" {
			credits = f
		}
	}
	if credits.Unit != "credits" || credits.ValueMicros != 12_500_000 {
		t.Fatalf("ai_credits_used %+v", credits)
	}
	repos, _ := MetricsReports()[SurfaceMetricsRepos1d]().ParseResponse(ok200(sfx(t, SurfaceMetricsRepos1d, "report_file.ndjson")))
	for _, f := range repos.Facts {
		if len(f.RepoHash) != 32 || f.Identity != "" || strings.Contains(f.RepoHash, "example") {
			t.Fatalf("repo fact %+v", f)
		}
	}
	e28, _ := MetricsReports()[SurfaceMetricsEnterprise28d]().ParseResponse(ok200(sfx(t, SurfaceMetricsEnterprise28d, "report_file.ndjson")))
	if _, ok := fact(e28, "monthly_active_users", "enterprise:1", "", ""); !ok {
		t.Fatal("28-day day_totals did not land")
	}
}

// TestReportFieldPolicyMatrix (lane L8, plan 12A.5): a missing links field,
// a link that is not a string, a non-200 answer, a line that is not an
// object, a missing day, user id, repository id or enterprise id, a wrong
// JSON type, a negative value and a repeated subject fail the page; an
// unknown member is ignored and an unknown feature lands as other with a
// canary signal.
func TestReportFieldPolicyMatrix(t *testing.T) {
	users := MetricsReports()[SurfaceMetricsUsers1d]()
	agg := MetricsReports()[SurfaceMetricsEnterprise1d]()
	repos := MetricsReports()[SurfaceMetricsRepos1d]()
	e28 := MetricsReports()[SurfaceMetricsEnterprise28d]()
	parse := func(d vendorapi.Driver, body string) error {
		_, err := d.ParseResponse(ok200([]byte(body)))
		return err
	}
	links := func(body string) error {
		_, err := users.Report.Links(ok200([]byte(body)))
		return err
	}
	cases := []struct {
		name string
		err  error
	}{
		{"links missing", links(`{"report_day":"2025-10-01"}`)},
		{"link not a string", links(`{"download_links":[1]}`)},
		{"links answer not an object", links(`[]`)},
		{"links status", func() error {
			r := ok200([]byte(`{"download_links":[]}`))
			r.Status = 204
			_, err := users.Report.Links(r)
			return err
		}()},
		{"line not an object", parse(users, `[1,2]`)},
		{"day missing", parse(users, `{"user_id":1}`)},
		{"day not a date", parse(users, `{"user_id":1,"day":"yesterday"}`)},
		{"user id missing", parse(users, `{"day":"2025-10-01"}`)},
		{"user id not numeric", parse(users, `{"day":"2025-10-01","user_id":"octocat"}`)},
		{"metric wrong type", parse(users, `{"day":"2025-10-01","user_id":1,"loc_added_sum":"12"}`)},
		{"metric negative", parse(users, `{"day":"2025-10-01","user_id":1,"loc_added_sum":-1}`)},
		{"repeated subject", parse(users, "{\"day\":\"2025-10-01\",\"user_id\":1,\"loc_added_sum\":1}\n{\"day\":\"2025-10-01\",\"user_id\":1,\"loc_added_sum\":2}")},
		{"segment not an object", parse(users, `{"day":"2025-10-01","user_id":1,"totals_by_cli":[1]}`)},
		{"features not an array", parse(users, `{"day":"2025-10-01","user_id":1,"totals_by_feature":{}}`)},
		{"enterprise id missing", parse(agg, `{"day":"2025-10-01","daily_active_users":2}`)},
		{"day_totals missing", parse(e28, `{"report_start_day":"2025-10-01"}`)},
		{"repo id missing", parse(repos, `{"day":"2025-10-01","pull_requests":{"total_created":1}}`)},
		{"repo id a string", parse(repos, `{"day":"2025-10-01","repo_id":"x","pull_requests":{"total_created":1}}`)},
		{"download status", func() error {
			r := ok200([]byte(`{}`))
			r.Status = 206
			_, err := users.ParseResponse(r)
			return err
		}()},
	}
	for _, c := range cases {
		if !isDrift(c.err) {
			t.Errorf("%s: err %v, want schema drift", c.name, c.err)
		}
	}
	p, err := users.ParseResponse(ok200([]byte(`{"day":"2025-10-01","user_id":7,"user_login":"never-lands","new_field":{"x":1},` +
		`"totals_by_feature":[{"feature":"Brand New Feature!","code_generation_activity_count":3}]}`)))
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Facts) != 1 || p.Facts[0].Feature != "other" || len(canaryReport(p)) != 1 {
		t.Fatalf("lenient parse: %+v canary %v", p.Facts, canaryReport(p))
	}
	// SEC-SWEEP, ledger VCONN-12: two unknown feature names on one line both
	// fold into "other"; they are summed, never a repeated-key drift that
	// fails the whole file on every run.
	p, err = users.ParseResponse(ok200([]byte(`{"day":"2025-10-01","user_id":7,"totals_by_feature":[` +
		`{"feature":"Chat-Panel","code_generation_activity_count":3},{"feature":"New-Agent","code_generation_activity_count":4}]}`)))
	if err != nil {
		t.Fatalf("two unknown features on one line: %v", err)
	}
	if len(p.Facts) != 1 || p.Facts[0].Feature != "other" || p.Facts[0].ValueMicros != 7_000_000 || len(canaryReport(p)) != 1 {
		t.Fatalf("two unknown features: %+v", p.Facts)
	}
	for _, f := range p.Facts {
		if strings.Contains(f.Identity+f.Feature+f.Metric, "never-lands") {
			t.Fatal("the login landed")
		}
	}
	if len(canaryReport(vendorapi.Page{})) != 0 {
		t.Error("canary on a clean page")
	}
}

// TestReportDaysCoverEveryDay (lane L8): a 1-day report run asks for the
// days from its window start less the lag to its window end less the lag;
// hourly runs over three days ask for every day at least once and never skip
// one, the cursor walks the days in order, and the 28-day report is one
// request.
func TestReportDaysCoverEveryDay(t *testing.T) {
	d := MetricsReports()[SurfaceMetricsUsers1d]()
	t0 := time.Date(2026, 10, 1, 0, 30, 0, 0, time.UTC)
	asked := map[string]int{}
	from := t0
	for i := 0; i < 72; i++ {
		to := from.Add(time.Hour)
		w := vendorapi.Window{From: from, To: to}
		cur := ""
		for n := 0; ; n++ {
			req, err := d.Build(vendorapi.Params{"enterprise": "acme"}, w, cur)
			if err != nil {
				t.Fatalf("run %d cursor %q: %v", i, cur, err)
			}
			if req.Path != "/enterprises/acme/copilot/metrics/reports/users-1-day" {
				t.Fatalf("path %s", req.Path)
			}
			asked[req.Query["day"][0]]++
			cur = d.Report.Next(nil, w, cur)
			if cur == "" {
				break
			}
			if n > 30 {
				t.Fatal("cursor never ends")
			}
		}
		from = to
	}
	first, _ := time.Parse(time.DateOnly, "2026-09-29")
	for day := first; day.Before(time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)); day = day.Add(24 * time.Hour) {
		if asked[day.Format(time.DateOnly)] == 0 {
			t.Errorf("day %s was never asked for (asked %v)", day.Format(time.DateOnly), asked)
		}
	}
	w := vendorapi.Window{From: time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC), To: time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)}
	var walked []string
	for cur := ""; ; {
		req, _ := d.Build(vendorapi.Params{"enterprise": "acme"}, w, cur)
		walked = append(walked, req.Query["day"][0])
		if cur = d.Report.Next(nil, w, cur); cur == "" {
			break
		}
	}
	if strings.Join(walked, ",") != "2026-10-03,2026-10-04,2026-10-05" {
		t.Fatalf("walked %v", walked)
	}
	for _, bad := range []struct {
		p   vendorapi.Params
		w   vendorapi.Window
		cur string
	}{
		{vendorapi.Params{"enterprise": "acme/../x"}, w, ""},
		{vendorapi.Params{}, w, ""},
		{vendorapi.Params{"enterprise": "acme"}, vendorapi.Window{}, ""},
		{vendorapi.Params{"enterprise": "acme"}, w, "2026-10-09"},
		{vendorapi.Params{"enterprise": "acme"}, w, "2026-10-4"},
	} {
		if _, err := d.Build(bad.p, bad.w, bad.cur); err == nil {
			t.Errorf("build accepted %+v", bad)
		}
	}
	latest := MetricsReports()[SurfaceMetricsEnterprise28d]()
	req, err := latest.Build(vendorapi.Params{"enterprise": "acme"}, w, "")
	if err != nil || req.Path != "/enterprises/acme/copilot/metrics/reports/enterprise-28-day/latest" || len(req.Query) != 0 ||
		latest.Report.Next != nil || latest.MaxWindow != 0 {
		t.Fatalf("latest: %+v %v", req, err)
	}
	if _, err := latest.Build(vendorapi.Params{"enterprise": "acme"}, w, "2026-10-04"); err == nil {
		t.Error("the latest report accepted a cursor")
	}
	if d.LegacyPoller != LegacyEngagementKey || d.Tool != Tool || d.Auth.Scheme != vendorapi.SchemeBearer || d.Report.MaxLinks == 0 {
		t.Errorf("driver %+v", d)
	}
}

// TestAICreditParsesAndBooksTheEchoedInterval (lane L8): usage items land as
// quantity and usd facts over the interval timePeriod echoes (the official
// example is a year), summed per product, sku, model and unit; a missing or
// impossible period, a missing required member and a negative amount fail
// the page; the request asks for the day before the window.
func TestAICreditParsesAndBooksTheEchoedInterval(t *testing.T) {
	for _, id := range []vs.SurfaceID{SurfaceAICreditOrg, SurfaceAICreditEnt} {
		d := AICreditReports()[id]()
		p, err := d.ParseResponse(ok200(sfx(t, id, "normal_page.json")))
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		validActivity(t, p)
		var q, usd vendorapi.FactRow
		for _, f := range p.Facts {
			switch f.Metric {
			case "net_quantity":
				q = f
			case "net_amount":
				usd = f
			}
		}
		if q.Unit != "credits" || q.ValueMicros != 100_000_000 || usd.Unit != "usd" || usd.ValueMicros != 1_000_000 ||
			!usd.IntervalStart.Equal(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)) ||
			!usd.IntervalEnd.Equal(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)) || usd.ModelRaw != "GPT-5" {
			t.Fatalf("%s: quantity %+v usd %+v", id, q, usd)
		}
	}
	d := AICreditReports()[SurfaceAICreditEnt]()
	item := `{"product":"Copilot","sku":"Copilot AI Credits","model":"GPT-5","unitType":"ai-credits","netQuantity":10,"netAmount":0.1}`
	p, err := d.ParseResponse(ok200([]byte(`{"timePeriod":{"year":2026,"month":10,"day":7},"usageItems":[` + item + `,` + item + `]}`)))
	if err != nil {
		t.Fatal(err)
	}
	validActivity(t, p)
	if len(p.Facts) != 2 || p.Facts[0].IntervalEnd.Sub(p.Facts[0].IntervalStart) != 24*time.Hour {
		t.Fatalf("facts %+v", p.Facts)
	}
	for _, f := range p.Facts {
		if (f.Metric == "net_amount" && f.ValueMicros != 200_000) || (f.Metric == "net_quantity" && (f.ValueMicros != 20_000_000 || f.Unit != "ai_credits")) {
			t.Fatalf("summed fact %+v", f)
		}
	}
	month, _ := d.ParseResponse(ok200([]byte(`{"timePeriod":{"year":2026,"month":2},"usageItems":[]}`)))
	if len(month.Facts) != 0 {
		t.Fatal("empty items")
	}
	for name, body := range map[string]string{
		"no period":       `{"usageItems":[]}`,
		"no items":        `{"timePeriod":{"year":2026}}`,
		"month 13":        `{"timePeriod":{"year":2026,"month":13},"usageItems":[]}`,
		"february 30":     `{"timePeriod":{"year":2026,"month":2,"day":30},"usageItems":[]}`,
		"day without mon": `{"timePeriod":{"year":2026,"day":3},"usageItems":[]}`,
		"no product":      `{"timePeriod":{"year":2026},"usageItems":[{"sku":"x","unitType":"credits","netQuantity":1,"netAmount":1}]}`,
		"negative amount": `{"timePeriod":{"year":2026},"usageItems":[{"product":"p","sku":"x","unitType":"credits","netQuantity":1,"netAmount":-1}]}`,
		"amount a string": `{"timePeriod":{"year":2026},"usageItems":[{"product":"p","sku":"x","unitType":"credits","netQuantity":1,"netAmount":"1"}]}`,
		"not an object":   `[]`,
	} {
		if _, err := d.ParseResponse(ok200([]byte(body))); !isDrift(err) {
			t.Errorf("%s: err %v, want schema drift", name, err)
		}
	}
	w := vendorapi.Window{From: time.Date(2026, 10, 8, 3, 0, 0, 0, time.UTC), To: time.Date(2026, 10, 8, 4, 0, 0, 0, time.UTC)}
	req, err := d.Build(vendorapi.Params{"enterprise": "acme"}, w, "")
	if err != nil || req.Path != "/enterprises/acme/settings/billing/ai_credit/usage" || req.Query["day"][0] != "7" ||
		req.Query["month"][0] != "10" || req.Query["year"][0] != "2026" {
		t.Fatalf("build %+v %v", req, err)
	}
	org := AICreditReports()[SurfaceAICreditOrg]()
	if req, err := org.Build(vendorapi.Params{"org": "acme"}, w, ""); err != nil || !strings.HasPrefix(req.Path, "/organizations/acme/") {
		t.Fatalf("org build %+v %v", req, err)
	}
	if _, err := org.Build(vendorapi.Params{"org": "acme"}, w, "2"); err == nil {
		t.Error("a paging cursor was accepted")
	}
	if org.LegacyPoller != LegacyBillingKey || org.MaxWindow != 24*time.Hour {
		t.Errorf("driver %+v", org)
	}
}

// TestAuditParsesClosedDTOAndPagesPerEndpoint (lane L8): entries land once
// per _document_id with the actor login, the action, the target and only the
// allowlisted members (a hashed token never decodes); the enterprise log
// pages by page number or cursor, the org log by cursor only; a foreign or
// unreadable next link and the documented failures fail the page.
func TestAuditParsesClosedDTOAndPagesPerEndpoint(t *testing.T) {
	org := AuditLogs()[SurfaceAuditOrg]()
	ent := AuditLogs()[SurfaceAuditEnt]()
	p, err := org.ParseResponse(ok200(sfx(t, SurfaceAuditOrg, "normal_page.json")))
	if err != nil {
		t.Fatal(err)
	}
	validActivity(t, p)
	if len(p.Audit) != 3 || p.NextCursor != "" {
		t.Fatalf("events %d next %q", len(p.Audit), p.NextCursor)
	}
	e := p.Audit[0]
	if e.Namespace != AuditNamespace || e.EventID != "xJJFlFOhQ6b-5vaAFy9Rjw" || e.Action != "team.add_member" ||
		e.ActorKind != "login" || e.ActorIdentity != "octocat" || e.Target != "monalisa" || e.Detail["team"] != "octo-corp/example-team" ||
		!e.At.Equal(time.UnixMilli(1606929874512).UTC()) {
		t.Fatalf("event %+v", e)
	}
	empty, err := org.ParseResponse(ok200(sfx(t, SurfaceAuditOrg, "empty_page.json")))
	if err != nil || len(empty.Audit) != 0 {
		t.Fatalf("empty %+v %v", empty, err)
	}
	withLink := func(link string) vendorapi.Response {
		r := ok200([]byte(`[]`))
		r.Header = map[string]string{"link": link}
		return r
	}
	ep, err := ent.ParseResponse(withLink(`<https://api.github.com/enterprises/acme/audit-log?page=2>; rel="next"`))
	if err != nil || ep.NextCursor != "page:2" {
		t.Fatalf("enterprise page link %q %v", ep.NextCursor, err)
	}
	op, err := org.ParseResponse(withLink(`<https://api.github.com/orgs/acme/audit-log?after=MS42MDQ0NjQ2NTM2NTMyMTN8eEpKRmxGT2hRNmItNXZhQUZ5OVJqdw%3D%3D>; rel="next"`))
	if err != nil || !strings.HasPrefix(op.NextCursor, "after:MS42") {
		t.Fatalf("org cursor link %q %v", op.NextCursor, err)
	}
	for name, err := range map[string]error{
		"org page number": func() error {
			_, err := org.ParseResponse(withLink(`<https://api.github.com/orgs/acme/audit-log?page=2>; rel="next"`))
			return err
		}(),
		"foreign link": func() error {
			_, err := ent.ParseResponse(withLink(`<https://evil.example.com/x?page=2>; rel="next"`))
			return err
		}(),
		"no document id": func() error {
			_, err := org.ParseResponse(ok200([]byte(`[{"@timestamp":1,"action":"a"}]`)))
			return err
		}(),
		"timestamp a word": func() error {
			_, err := org.ParseResponse(ok200([]byte(`[{"@timestamp":"soon","_document_id":"d","action":"a"}]`)))
			return err
		}(),
		"repeated id": func() error {
			_, err := org.ParseResponse(ok200([]byte(`[{"@timestamp":1,"_document_id":"d","action":"a"},{"@timestamp":2,"_document_id":"d","action":"a"}]`)))
			return err
		}(),
		"not an array": func() error {
			_, err := org.ParseResponse(ok200([]byte(`{"events":[]}`)))
			return err
		}(),
	} {
		if !isDrift(err) {
			t.Errorf("%s: err %v, want schema drift", name, err)
		}
	}
	odd, err := org.ParseResponse(ok200([]byte(`[{"@timestamp":5,"_document_id":"d1","action":"Weird Action!","actor":"octocat",` +
		`"hashed_token":"never-decoded","token_scopes":"repo","repo":"acme/x","data":{"a":1}}]`)))
	if err != nil {
		t.Fatal(err)
	}
	if ev := odd.Audit[0]; ev.Action != "other" || len(ev.Detail) != 1 || ev.Detail["repo"] != "acme/x" || ev.Target != "acme/x" ||
		len(canaryAudit(odd)) != 1 {
		t.Fatalf("closed DTO: %+v", ev)
	}
	w := vendorapi.Window{From: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)}
	req, err := ent.Build(vendorapi.Params{"enterprise": "acme"}, w, "page:3")
	if err != nil || req.Query["phrase"][0] != "created:2026-10-01T00:00:00Z..2026-10-02T00:00:00Z" || req.Query["page"][0] != "3" ||
		req.Query["per_page"][0] != "100" {
		t.Fatalf("enterprise build %+v %v", req, err)
	}
	for _, c := range []struct {
		d   vendorapi.Driver
		p   vendorapi.Params
		cur string
	}{
		{org, vendorapi.Params{"org": "acme"}, "page:2"},
		{org, vendorapi.Params{"org": "acme"}, "after:bad cursor"},
		{org, vendorapi.Params{"org": "acme"}, "x"},
		{org, vendorapi.Params{"org": "a/b"}, ""},
	} {
		if _, err := c.d.Build(c.p, w, c.cur); err == nil {
			t.Errorf("build accepted %v %q", c.p, c.cur)
		}
	}
	if !org.ClosedDTO || org.MaxLookback != AuditLookback {
		t.Errorf("driver %+v", org)
	}
}
