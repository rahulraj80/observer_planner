package cursor

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/vendorapi"
)

var d3Window = vendorapi.Window{
	From: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
	To:   time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC),
}

// TestPostSurfacesBuildClosedBodies: each read-only POST surface builds the
// closed vendorapi body its egress POST table row names, from the window and
// cursor only (never a caller-supplied body).
func TestPostSurfacesBuildClosedBodies(t *testing.T) {
	from, to := d3Window.From.UnixMilli(), d3Window.To.UnixMilli()-1
	r, err := buildUsageEvents(nil, d3Window, "")
	if err != nil {
		t.Fatal(err)
	}
	if r.Method != "POST" || r.Path != "/teams/filtered-usage-events" ||
		r.Body != (vendorapi.EpochRangePage{StartMillis: from, EndMillis: to, Page: 1, PageSize: UsagePageSize}) {
		t.Fatalf("usage events request %+v", r)
	}
	if r, err = buildUsageEvents(nil, d3Window, "3"); err != nil || r.Body.(vendorapi.EpochRangePage).Page != 3 {
		t.Fatalf("page 3: %+v %v", r, err)
	}
	r, err = buildDailyUsage(nil, d3Window, "")
	if err != nil || r.Path != "/teams/daily-usage-data" || r.Body != (vendorapi.EpochRange{StartMillis: from, EndMillis: to}) {
		t.Fatalf("daily usage request %+v %v", r, err)
	}
	r, err = buildSpend(nil, vendorapi.Window{}, "")
	if err != nil || r.Path != "/teams/spend" || r.Body != (vendorapi.PageQuery{Page: 1, PageSize: SpendPageSize}) {
		t.Fatalf("spend request %+v %v", r, err)
	}
	for _, b := range []vendorapi.PostBody{
		vendorapi.EpochRangePage{StartMillis: from, EndMillis: to, Page: 1, PageSize: UsagePageSize},
		vendorapi.EpochRange{StartMillis: from, EndMillis: to},
		vendorapi.PageQuery{Page: 1, PageSize: SpendPageSize},
	} {
		if _, err := vendorapi.EncodePostBody(b); err != nil {
			t.Errorf("%T does not encode: %v", b, err)
		}
	}
	long := vendorapi.Window{From: d3Window.From, To: d3Window.From.Add(31 * 24 * time.Hour)}
	for name, fn := range map[string]func() error{
		"usage empty window": func() error { _, err := buildUsageEvents(nil, vendorapi.Window{}, ""); return err },
		"usage long window":  func() error { _, err := buildUsageEvents(nil, long, ""); return err },
		"usage bad cursor":   func() error { _, err := buildUsageEvents(nil, d3Window, "1"); return err },
		"usage text cursor":  func() error { _, err := buildUsageEvents(nil, d3Window, "x"); return err },
		"daily long window":  func() error { _, err := buildDailyUsage(nil, long, ""); return err },
		"daily paged":        func() error { _, err := buildDailyUsage(nil, d3Window, "2"); return err },
		"spend bad cursor":   func() error { _, err := buildSpend(nil, d3Window, "02"); return err },
	} {
		if fn() == nil {
			t.Errorf("%s: built", name)
		}
	}
}

func TestUsageEventsParsesCorpusFixtures(t *testing.T) {
	p, err := parseUsageEvents(resp(fixture(t, SurfaceUsageEvents, "normal_page.json")))
	if err != nil {
		t.Fatal(err)
	}
	if err := vendorapi.Validate(p); err != nil {
		t.Fatalf("parsed page does not validate: %v", err)
	}
	if len(p.Usage) != 3 || p.NextCursor != "2" {
		t.Fatalf("usage %d next %q", len(p.Usage), p.NextCursor)
	}
	u := p.Usage[0]
	if u.JoinNamespace != UsageNamespace || u.JoinKind != UsageJoinKind || len(u.JoinValue) != 32 ||
		u.IdentityKind != "email" || u.Identity != "developer@company.com" || u.Model != "claude-4.5-sonnet" ||
		u.SessionRef != "8f2e4a1b-6c3d-4e5f-9a7b-2d1c8e6f4a3b" || !u.At.Equal(time.UnixMilli(1750979225854)) ||
		u.InputTokens != 126 || u.OutputTokens != 450 || u.CacheWriteTokens != 6112 || u.CacheReadTokens != 11964 ||
		u.ChargedUnit != "cents" || u.ChargedMicros != 21_362_320 ||
		u.Flags["is_headless"] != "false" || u.Flags["billing_basis"] != "tokens" {
		t.Fatalf("event 0 = %+v", u)
	}
	b := u.Billing
	if b == nil || b.Category != "Usage-based" || b.MaxMode == nil || !*b.MaxMode || b.Chargeable == nil || !*b.Chargeable ||
		*b.RequestUnitsMicros != 5_000_000 || *b.ModelCostMicros != 20_182_320 || *b.FeeMicros != 1_180_000 ||
		b.DiscountPercentMicros != nil {
		t.Fatalf("event 0 billing = %+v", b)
	}
	if d := p.Usage[1].Billing.DiscountPercentMicros; d == nil || *d != 10_000_000 {
		t.Fatalf("event 1 discount = %v", d)
	}
	third := p.Usage[2]
	if third.SessionRef != "" || third.Billing.Category != "Included in Business" || *third.Billing.Chargeable ||
		*third.Billing.RequestUnitsMicros != 1_400_000 || third.Billing.ModelCostMicros != nil || third.ChargedMicros != 8_000_000 {
		t.Fatalf("event 2 = %+v billing %+v", third, third.Billing)
	}
	if p.Usage[0].JoinValue == p.Usage[1].JoinValue {
		t.Error("two distinct events share a join value")
	}

	last, err := parseUsageEvents(resp(fixture(t, SurfaceUsageEvents, "last_page.json")))
	if err != nil {
		t.Fatal(err)
	}
	if last.NextCursor != "" || len(last.Usage) != 1 {
		t.Fatalf("last page next %q usage %d", last.NextCursor, len(last.Usage))
	}
	if sa := last.Usage[0]; sa.Identity != "agent-runner@company.com" || sa.Billing.ServiceAccountName != "Nightly CI Agent" ||
		sa.Flags["is_headless"] != "true" {
		t.Fatalf("service-account event = %+v billing %+v", sa, sa.Billing)
	}
}

// TestUsageEventsJoinValueIsStableAndRepeatSafe: the join value is a pure
// function of the composite key, so the same event in a later run merges by
// revision; two events identical in every key field inside one page get
// distinct values (the n-th repeat suffix) rather than a repeated-key refusal.
func TestUsageEventsJoinValueIsStableAndRepeatSafe(t *testing.T) {
	body := []byte(`{"usageEvents":[` +
		`{"timestamp":"1750979225854","userEmail":"A@x.test","model":"m","tokenUsage":{"outputTokens":3}},` +
		`{"timestamp":"1750979225854","userEmail":"a@x.test","model":"m","tokenUsage":{"outputTokens":3}},` +
		`{"timestamp":1750979225855,"serviceAccountId":"sa_1","model":"m"}` +
		`],"pagination":{"currentPage":4,"hasNextPage":false}}`)
	p, err := parseUsageEvents(resp(body))
	if err != nil {
		t.Fatal(err)
	}
	if err := vendorapi.Validate(p); err != nil {
		t.Fatalf("repeat page refused: %v", err)
	}
	if p.Usage[0].JoinValue == p.Usage[1].JoinValue {
		t.Fatal("repeats share a join value")
	}
	again, _ := parseUsageEvents(resp(body))
	for i := range p.Usage {
		if again.Usage[i].JoinValue != p.Usage[i].JoinValue {
			t.Fatalf("event %d join value is not stable", i)
		}
	}
	if sa := p.Usage[2]; sa.IdentityKind != "account_id" || sa.Identity != "sa_1" {
		t.Fatalf("service account identity = %+v", sa)
	}
}

func TestUsageEventsRefusesDrift(t *testing.T) {
	for name, body := range map[string]string{
		"no events":          `{"pagination":{"currentPage":1,"hasNextPage":false}}`,
		"no pagination":      `{"usageEvents":[]}`,
		"flag type":          `{"usageEvents":[],"pagination":{"currentPage":1,"hasNextPage":"yes"}}`,
		"bad page":           `{"usageEvents":[],"pagination":{"currentPage":0,"hasNextPage":false}}`,
		"no timestamp":       `{"usageEvents":[{"userEmail":"a@x.test","model":"m"}],"pagination":{"currentPage":1,"hasNextPage":false}}`,
		"no identity":        `{"usageEvents":[{"timestamp":"1","model":"m"}],"pagination":{"currentPage":1,"hasNextPage":false}}`,
		"negative tokens":    `{"usageEvents":[{"timestamp":"1","userEmail":"a@x.test","model":"m","tokenUsage":{"inputTokens":-1}}],"pagination":{"currentPage":1,"hasNextPage":false}}`,
		"negative charge":    `{"usageEvents":[{"timestamp":"1","userEmail":"a@x.test","model":"m","chargedCents":-2}],"pagination":{"currentPage":1,"hasNextPage":false}}`,
		"negative fee":       `{"usageEvents":[{"timestamp":"1","userEmail":"a@x.test","model":"m","cursorTokenFee":-2}],"pagination":{"currentPage":1,"hasNextPage":false}}`,
		"exponent cost":      `{"usageEvents":[{"timestamp":"1","userEmail":"a@x.test","model":"m","requestsCosts":1e3}],"pagination":{"currentPage":1,"hasNextPage":false}}`,
		"not an object":      `[]`,
		"timestamp not time": `{"usageEvents":[{"timestamp":"soon","userEmail":"a@x.test","model":"m"}],"pagination":{"currentPage":1,"hasNextPage":false}}`,
	} {
		if _, err := parseUsageEvents(resp([]byte(body))); !errors.Is(err, vendorapi.ErrSchemaDrift) {
			t.Errorf("%s: err %v, want schema drift", name, err)
		}
	}
	if _, err := parseUsageEvents(vendorapi.Response{Status: 401, Body: []byte(`{}`)}); !errors.Is(err, vendorapi.ErrSchemaDrift) {
		t.Errorf("non-200: %v", err)
	}
}

func TestDailyUsageParsesCorpusFixture(t *testing.T) {
	p, err := parseDailyUsage(resp(fixture(t, SurfaceDailyUsage, "normal_page.json")))
	if err != nil {
		t.Fatal(err)
	}
	if err := vendorapi.Validate(p); err != nil {
		t.Fatalf("parsed page does not validate: %v", err)
	}
	if p.SourceRecords != 1 || len(p.Facts) != len(dailyMetrics) || p.NextCursor != "" {
		t.Fatalf("records %d facts %d next %q", p.SourceRecords, len(p.Facts), p.NextCursor)
	}
	day := time.Date(2024, 3, 18, 0, 0, 0, 0, time.UTC)
	got := map[string]vendorapi.FactRow{}
	for _, f := range p.Facts {
		if !f.IntervalStart.Equal(day) || !f.IntervalEnd.Equal(day.Add(24*time.Hour)) || f.Identity != "developer@company.com" ||
			f.IdentityKind != "email" || f.SourceTZ != "UTC" {
			t.Fatalf("fact %+v", f)
		}
		got[f.Metric] = f
	}
	if f := got["accepted_lines_added"]; f.ValueMicros != 1102_000_000 || f.Unit != "lines" {
		t.Fatalf("accepted lines %+v", f)
	}
	if f := got["chat_requests"]; f.ValueMicros != 128_000_000 || f.Unit != "requests" {
		t.Fatalf("chat requests %+v", f)
	}
	if f, ok := got["api_key_requests"]; !ok || f.ValueMicros != 0 {
		t.Fatalf("a documented zero lands as zero: %+v", f)
	}
	for name, body := range map[string]string{
		"no data":      `{"period":{}}`,
		"no day":       `{"data":[{"email":"a@x.test"}]}`,
		"bad day":      `{"data":[{"day":"18/03/2024","email":"a@x.test"}]}`,
		"no email":     `{"data":[{"day":"2024-03-18"}]}`,
		"quoted count": `{"data":[{"day":"2024-03-18","email":"a@x.test","chatRequests":"5"}]}`,
		"negative":     `{"data":[{"day":"2024-03-18","email":"a@x.test","chatRequests":-5}]}`,
		"fraction":     `{"data":[{"day":"2024-03-18","email":"a@x.test","chatRequests":1.5}]}`,
		"repeat":       `{"data":[{"day":"2024-03-18","email":"a@x.test"},{"day":"2024-03-18","email":"A@x.test"}]}`,
	} {
		if _, err := parseDailyUsage(resp([]byte(body))); !errors.Is(err, vendorapi.ErrSchemaDrift) {
			t.Errorf("%s: err %v, want schema drift", name, err)
		}
	}
}

func TestSpendParsesCorpusFixture(t *testing.T) {
	r := resp(fixture(t, SurfaceSpend, "normal_page.json"))
	p, err := parseSpend(r)
	if err != nil {
		t.Fatal(err)
	}
	if err := vendorapi.Validate(p); err != nil {
		t.Fatalf("parsed page does not validate: %v", err)
	}
	if len(p.Snapshots) != 2 || p.NextCursor != "" {
		t.Fatalf("snapshots %d next %q", len(p.Snapshots), p.NextCursor)
	}
	s := p.Snapshots[0]
	if s.Class != SpendSnapshotClass || s.Subject != "developer@company.com" || s.Body["spend_cents"] != "2450.125487" ||
		s.Body["role"] != "member" || s.Body["monthly_limit_dollars"] != "200" || s.Body["cycle_start"] != "2024-02-27T00:00:00Z" {
		t.Fatalf("snapshot 0 = %+v", s)
	}
	for k := range s.Body {
		if strings.Contains(k, "name") {
			t.Errorf("the member name lands as %q", k)
		}
	}
	if _, ok := p.Snapshots[1].Body["monthly_limit_dollars"]; ok {
		t.Error("a null limit lands as a value")
	}
	// The response does not echo its page: the driver pages from the cursor
	// it sent.
	multi := []byte(`{"teamMemberSpend":[],"subscriptionCycleStart":1708992000000,"totalPages":3}`)
	for cursor, want := range map[string]string{"": "2", "2": "3", "3": ""} {
		p, err := parseSpend(vendorapi.Response{Status: 200, Body: multi, Cursor: cursor})
		if err != nil || p.NextCursor != want {
			t.Errorf("cursor %q: next %q (%v), want %q", cursor, p.NextCursor, err, want)
		}
	}
	for name, body := range map[string]string{
		"no members": `{"subscriptionCycleStart":1,"totalPages":1}`,
		"no pages":   `{"teamMemberSpend":[],"subscriptionCycleStart":1}`,
		"no cycle":   `{"teamMemberSpend":[],"totalPages":1}`,
		"no email":   `{"teamMemberSpend":[{"spendCents":1}],"subscriptionCycleStart":1,"totalPages":1}`,
		"negative":   `{"teamMemberSpend":[{"email":"a@x.test","spendCents":-1}],"subscriptionCycleStart":1,"totalPages":1}`,
		"repeat":     `{"teamMemberSpend":[{"email":"a@x.test"},{"email":"A@x.test"}],"subscriptionCycleStart":1,"totalPages":1}`,
	} {
		if _, err := parseSpend(resp([]byte(body))); !errors.Is(err, vendorapi.ErrSchemaDrift) {
			t.Errorf("%s: err %v, want schema drift", name, err)
		}
	}
}

func TestPostDriversDeclareTheirContracts(t *testing.T) {
	if d := UsageEvents(); d.LegacyPoller != LegacyUsageKey || d.MaxWindow != UsageMaxWindow || !d.ClosedDTO {
		t.Errorf("usage events driver %+v", d)
	}
	if d := DailyUsage(); d.LegacyPoller != "" || d.MaxWindow != DailyMaxWindow {
		t.Errorf("daily usage driver: legacy %q window %s", d.LegacyPoller, d.MaxWindow)
	}
	if d := Spend(); d.LegacyPoller != "" || d.MaxWindow != 0 {
		t.Errorf("spend driver: legacy %q window %s", d.LegacyPoller, d.MaxWindow)
	}
}
