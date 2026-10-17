package anthropic

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/vendorapi"
)

// TestUserCostParsesCorpusFixtures: the corpus pages land cost, list cost and
// requests for the seat user by ASCII e-mail on one UTC day, and neither the
// actor's name nor any other undocumented-for-landing field reaches the page.
func TestUserCostParsesCorpusFixtures(t *testing.T) {
	p := mustParse(t, parseUserCost, fixture(t, SurfaceUserCost, "normal_page.json"))
	if p.NextCursor != "next_page" || p.SourceRecords != 1 || len(p.Facts) != 3 {
		t.Fatalf("next %q records %d facts %d", p.NextCursor, p.SourceRecords, len(p.Facts))
	}
	day := time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)
	f := facts(p)
	for metric, want := range map[string]struct {
		unit   string
		micros int64
	}{
		// "41280.000000" cents = 412.80 USD; "51600.000000" cents = 516.00 USD.
		"cost":      {"usd", 412_800_000},
		"list_cost": {"usd", 516_000_000},
		"requests":  {"count", 128_000_000},
	} {
		r, ok := f["jane@example.com||product:chat||"+metric]
		if !ok || r.Unit != want.unit || r.ValueMicros != want.micros || r.IdentityKind != "email" ||
			!r.IntervalStart.Equal(day) || !r.IntervalEnd.Equal(day.Add(24*time.Hour)) || r.SourceTZ != "UTC" ||
			r.CoverageDomain != "anthropic.enterprise.seat_users" || r.ModelRaw != "" {
			t.Errorf("%s = %+v (ok %v)", metric, r, ok)
		}
	}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, never := range []string{"Jane Smith", "Jane", "user_01AbCdEfGhIjKlMnOpQrSt", "org_013FP9SaFPBg7Kw7fetjn6cF"} {
		if strings.Contains(string(b), never) {
			t.Errorf("page carries %q", never)
		}
	}
	last := mustParse(t, parseUserCost, fixture(t, SurfaceUserCost, "last_page.json"))
	if last.NextCursor != "" || last.SourceRecords != 1 || len(last.Facts) != 3 {
		t.Fatalf("last page next %q records %d facts %d", last.NextCursor, last.SourceRecords, len(last.Facts))
	}
}

// userCostBody builds a one-page body from row JSON fragments.
func userCostBody(rows ...string) []byte {
	return []byte(`{"data":[` + strings.Join(rows, ",") + `],"data_refreshed_at":null,"has_more":false,"next_page":null,"organization_id":"org_x"}`)
}

// userCostRowJSON is a documented row with one substitution applied.
func userCostRowJSON(actor, extra string) string {
	return `{"actor":` + actor + `,"amount":"150","list_amount":"200.5","currency":"USD",` +
		`"starting_at":"2026-10-06T00:00:00Z","ending_at":"2026-10-07T00:00:00Z","product":"claude_code"` + extra + `}`
}

const ucUser = `{"type":"user_actor","user_id":"user_01X","email_address":"Ann@Example.COM","name":"Ann Person","email":"Ann@Example.COM","deleted":false}`

func TestUserCostIdentityAndFields(t *testing.T) {
	cases := []struct {
		name, actor, extra string
		kind, identity     string
		facts              int
	}{
		{"ascii email folds", ucUser, `,"requests":3`, "email", "ann@example.com", 3},
		{
			"kelvin sign falls back to user id",
			`{"type":"user_actor","user_id":"user_01X","email_address":"Kate@example.com","name":"Kate"}`, "", "user_id", "user_01X", 2,
		},
		{
			"non-ascii local part falls back",
			`{"type":"user_actor","user_id":"user_01X","email_address":"jörg@example.com"}`, "", "user_id", "user_01X", 2,
		},
		{
			"deleted account with null email",
			`{"type":"user_actor","user_id":"user_01Y","email_address":null,"name":"Gone","deleted":true}`, `,"requests":null`, "user_id", "user_01Y", 2,
		},
		{"empty email", `{"type":"user_actor","user_id":"user_01Z","email_address":"  "}`, "", "user_id", "user_01Z", 2},
		{"deprecated email alone never lands", `{"type":"user_actor","user_id":"user_01W","email":"w@example.com"}`, "", "user_id", "user_01W", 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := mustParse(t, parseUserCost, userCostBody(userCostRowJSON(c.actor, c.extra)))
			if len(p.Facts) != c.facts {
				t.Fatalf("facts %d, want %d", len(p.Facts), c.facts)
			}
			for _, f := range p.Facts {
				if f.IdentityKind != c.kind || f.Identity != c.identity || f.Feature != "product:claude_code" {
					t.Errorf("fact %+v", f)
				}
				if f.Metric == "list_cost" && f.ValueMicros != 2_005_000 {
					t.Errorf("list cost %d", f.ValueMicros)
				}
			}
			b, _ := json.Marshal(p)
			for _, never := range []string{"Ann Person", "Kate", "Gone", "w@example.com"} {
				if strings.Contains(string(b), never) {
					t.Errorf("page carries %q", never)
				}
			}
		})
	}
	// A null product keeps the bare feature.
	p := mustParse(t, parseUserCost, userCostBody(strings.Replace(userCostRowJSON(ucUser, ""), `"product":"claude_code"`, `"product":null`, 1)))
	if p.Facts[0].Feature != "product:" {
		t.Errorf("null product feature %q", p.Facts[0].Feature)
	}
	// Two products on one user day are two groups.
	two := userCostBody(userCostRowJSON(ucUser, ""), strings.Replace(userCostRowJSON(ucUser, ""), `"claude_code"`, `"chat"`, 1))
	if p := mustParse(t, parseUserCost, two); len(p.Facts) != 4 {
		t.Errorf("two products: %d facts", len(p.Facts))
	}
}

func TestUserCostFieldPolicy(t *testing.T) {
	row := userCostRowJSON(ucUser, "")
	sub := func(old, repl string) []byte { return userCostBody(strings.Replace(row, old, repl, 1)) }
	drift := []struct {
		name string
		body []byte
	}{
		{"currency", sub(`"currency":"USD"`, `"currency":"EUR"`)},
		{"currency missing", sub(`"currency":"USD",`, ``)},
		{"amount exponent", sub(`"amount":"150"`, `"amount":"1e3"`)},
		{"amount number", sub(`"amount":"150"`, `"amount":150`)},
		{"amount missing", sub(`"amount":"150",`, ``)},
		{"list amount bad", sub(`"list_amount":"200.5"`, `"list_amount":"abc"`)},
		{"not a daily bucket", sub(`"starting_at":"2026-10-06T00:00:00Z"`, `"starting_at":"2026-10-06T06:00:00Z"`)},
		{"placeholder bounds", sub(`"starting_at":"2026-10-06T00:00:00Z","ending_at":"2026-10-07T00:00:00Z"`,
			`"starting_at":"2019-12-27T18:11:19.117Z","ending_at":"2019-12-27T18:11:19.117Z"`)},
		{"bounds missing", sub(`"starting_at":"2026-10-06T00:00:00Z",`, ``)},
		{"actor missing", sub(`"actor":`+ucUser+`,`, ``)},
		{"actor type", sub(`"type":"user_actor"`, `"type":"api_actor"`)},
		{"user id missing", sub(`"user_id":"user_01X",`, ``)},
		{"user id empty", sub(`"user_id":"user_01X"`, `"user_id":""`)},
		{"requests negative", userCostBody(userCostRowJSON(ucUser, `,"requests":-1`))},
		{"requests fractional", userCostBody(userCostRowJSON(ucUser, `,"requests":1.5`))},
		{"repeated user day product", userCostBody(row, strings.Replace(row, `"Ann@Example.COM","name"`, `"ann@example.com","name"`, 1))},
		{"has_more without cursor", []byte(`{"data":[],"has_more":true,"next_page":null}`)},
		{"data missing", []byte(`{"has_more":false}`)},
	}
	for _, c := range drift {
		if _, err := parseUserCost(resp(c.body)); err == nil || !errors.Is(err, vendorapi.ErrSchemaDrift) {
			t.Errorf("%s: err %v, want schema drift", c.name, err)
		}
	}
	st := resp(userCostBody())
	st.Status = 202
	if _, err := parseUserCost(st); !errors.Is(err, vendorapi.ErrSchemaDrift) {
		t.Errorf("status 202 accepted: %v", err)
	}
	if _, err := parseUserCost(resp(userCostBody(strings.Replace(row, `"product"`, `"new_field":{"x":1},"product"`, 1)))); err != nil {
		t.Errorf("unknown member refused: %v", err)
	}
}

func TestUserCostBuild(t *testing.T) {
	from := time.Date(2026, 10, 7, 1, 0, 0, 0, time.UTC)
	w := vendorapi.Window{From: from, To: from.Add(5 * time.Hour)}
	req, err := buildUserCost(nil, w, "")
	if err != nil {
		t.Fatal(err)
	}
	// From minus the 24h settle lag, truncated to the UTC day.
	if req.Method != "GET" || req.Path != "/v1/organizations/analytics/user_cost_report" ||
		req.Query["starting_at"][0] != "2026-10-06T00:00:00Z" || req.Query["ending_at"][0] != "2026-10-07T06:00:00Z" ||
		req.Query["bucket_width"][0] != "1d" || req.Query["group_by[]"][0] != "product" || len(req.Query["group_by[]"]) != 1 ||
		req.Query["limit"][0] != "1000" || req.Query["page"] != nil {
		t.Fatalf("request %+v", req)
	}
	req, err = buildUserCost(nil, w, "cursor_abc=")
	if err != nil || req.Query["page"][0] != "cursor_abc=" {
		t.Fatalf("cursor request %+v %v", req, err)
	}
	// A window reaching back past the documented floor starts at the floor.
	early := vendorapi.Window{From: time.Date(2026, 1, 1, 3, 0, 0, 0, time.UTC), To: time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)}
	req, err = buildUserCost(nil, early, "")
	if err != nil || req.Query["starting_at"][0] != "2026-01-01T00:00:00Z" {
		t.Fatalf("clamped request %+v %v", req, err)
	}
	for _, c := range []struct {
		name string
		w    vendorapi.Window
		cur  string
	}{
		{"empty window", vendorapi.Window{}, ""},
		{"inverted window", vendorapi.Window{From: from, To: from}, ""},
		{"window before the floor", vendorapi.Window{From: time.Date(2025, 12, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}, ""},
		{"malformed cursor", w, "a b"},
		{"cursor with ampersand", w, "x&y"},
	} {
		if _, err := buildUserCost(nil, c.w, c.cur); err == nil {
			t.Errorf("%s accepted", c.name)
		}
	}
	d := UserCostReport()
	if d.Surface != SurfaceUserCost || d.LegacyPoller != "" || d.Tool != Tool || d.MaxWindow != ReportMaxWindow ||
		d.MaxLookback != 365*24*time.Hour || len(d.Supplies) != 1 || d.ParseResponse == nil {
		t.Errorf("driver %+v", d)
	}
}
