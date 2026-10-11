package anthropic

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
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "vendorapi", "anthropic", string(surface), name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func resp(body []byte) vendorapi.Response {
	return vendorapi.Response{Status: 200, Origin: "https://api.anthropic.com:443", Body: body, Params: vendorapi.Params{}}
}

func stamp(p vendorapi.Page) vendorapi.Page {
	if p.Snapshot == nil {
		p.Snapshot = &vendorapi.SnapshotManifest{}
	}
	p.Snapshot.ManifestID = "run-1"
	p.Snapshot.ObservedAt = time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	return p
}

// facts indexes a page's facts by identity, model, feature, segment and metric.
func facts(p vendorapi.Page) map[string]vendorapi.FactRow {
	out := map[string]vendorapi.FactRow{}
	for _, f := range p.Facts {
		out[f.Identity+"|"+f.ModelRaw+"|"+f.Feature+"|"+f.TrafficSegment+"|"+f.Metric] = f
	}
	return out
}

func mustParse(t *testing.T, parse func(vendorapi.Response) (vendorapi.Page, error), body []byte) vendorapi.Page {
	t.Helper()
	p, err := parse(resp(body))
	if err != nil {
		t.Fatal(err)
	}
	if err := vendorapi.Validate(stamp(p)); err != nil {
		t.Fatalf("parsed page does not validate: %v", err)
	}
	return p
}

func TestUsageParsesCorpusFixtures(t *testing.T) {
	p := mustParse(t, parseUsage, fixture(t, SurfaceUsageMessages, "normal_page.json"))
	if p.NextCursor != "page_MjAyNS0wNS0xNFQwMDowMDowMFo=" || p.SourceRecords != 1 || len(p.Facts) != 6 {
		t.Fatalf("next %q records %d facts %d", p.NextCursor, p.SourceRecords, len(p.Facts))
	}
	f := facts(p)
	day := time.Date(2025, 8, 1, 0, 0, 0, 0, time.UTC)
	for metric, want := range map[string]int64{
		"input_tokens_uncached": 1500, "input_tokens_cache_read": 200, "input_tokens_cache_write_5m": 0,
		"input_tokens_cache_write_1h": 0, "output_tokens": 500, "web_search_requests": 10,
	} {
		r, ok := f["|claude-opus-5|||"+metric]
		if !ok || r.ValueMicros != want*1_000_000 || !r.IntervalStart.Equal(day) || !r.IntervalEnd.Equal(day.Add(24*time.Hour)) ||
			r.IdentityKind != "" {
			t.Errorf("%s = %+v", metric, r)
		}
	}
	last := mustParse(t, parseUsage, fixture(t, SurfaceUsageMessages, "last_page.json"))
	if last.NextCursor != "" || last.SourceRecords != 2 || len(last.Facts) != 6 {
		t.Fatalf("last page: next %q records %d facts %d", last.NextCursor, last.SourceRecords, len(last.Facts))
	}
	if r := facts(last)["||||input_tokens_cache_write_5m"]; r.ValueMicros != 512_000_000 {
		t.Errorf("ungrouped cache write = %+v", r)
	}
}

func TestCostParsesCorpusFixtures(t *testing.T) {
	p := mustParse(t, parseCost, fixture(t, SurfaceCostReport, "normal_page.json"))
	if len(p.Facts) != 1 || p.NextCursor == "" || p.SourceRecords != 1 {
		t.Fatalf("facts %d next %q", len(p.Facts), p.NextCursor)
	}
	r := p.Facts[0]
	// "123.78912" cents = 1.2378912 USD = 1,237,891.2 micro-dollars, rounded.
	if r.Unit != "usd" || r.Metric != "cost" || r.ValueMicros != 1_237_891 || r.ModelRaw != "claude-opus-5" ||
		r.Feature != "tokens|cache_creation.ephemeral_1h_input_tokens|standard|0-200k|global" {
		t.Fatalf("cost fact %+v", r)
	}
	sweep := mustParse(t, parseCost, fixture(t, SurfaceCostReport, "enum_sweep.json"))
	if len(sweep.Facts) != 4 || sweep.NextCursor != "" {
		t.Fatalf("enum sweep facts %d", len(sweep.Facts))
	}
	if r := facts(sweep)["|claude-sonnet-5|tokens|output_tokens|batch|200k-1M|us||cost"]; r.ValueMicros != 9_876_543_211_235 {
		t.Errorf("max-size amount = %+v", r)
	}
}

func TestClaudeCodeParsesCorpusFixtures(t *testing.T) {
	p := mustParse(t, parseClaudeCode, fixture(t, SurfaceClaudeCodeUsage, "normal_page.json"))
	if p.SourceRecords != 1 || p.NextCursor == "" {
		t.Fatalf("records %d next %q", p.SourceRecords, p.NextCursor)
	}
	f := facts(p)
	user := "user@emaildomain.com"
	for k, want := range map[string]int64{
		user + "|||local|sessions":                         15,
		user + "|||local|lines_added":                      342,
		user + "|||local|lines_removed":                    128,
		user + "|||local|commits":                          8,
		user + "|||local|pull_requests":                    2,
		user + "|claude-opus-5||local|input_tokens":        45230,
		user + "|claude-sonnet-5||local|cache_read_tokens": 3420,
		user + "||tool:edit_tool|local|tool_accepted":      25,
		user + "||tool:write_tool|local|tool_rejected":     0,
	} {
		r, ok := f[k]
		if !ok || r.ValueMicros != want*1_000_000 || r.IdentityKind != "email" {
			t.Errorf("%s = %+v (ok %v)", k, r, ok)
		}
	}
	// 186 cents = 1.86 USD.
	if r := f[user+"|claude-opus-5||local|estimated_cost"]; r.Unit != "usd" || r.ValueMicros != 1_860_000 {
		t.Errorf("estimated cost %+v", r)
	}
	last := mustParse(t, parseClaudeCode, fixture(t, SurfaceClaudeCodeUsage, "last_page.json"))
	if last.NextCursor != "" || last.SourceRecords != 2 {
		t.Fatalf("last page next %q records %d", last.NextCursor, last.SourceRecords)
	}
	lf := facts(last)
	if r, ok := lf[user+"|||remote|sessions"]; !ok || r.TrafficSegment != "remote" {
		t.Errorf("remote segment %+v", r)
	}
	if r, ok := lf["ci-key|||local|sessions"]; !ok || r.IdentityKind != "api_key_name" {
		t.Errorf("api actor %+v", r)
	}
	if canaryClaudeCode(last) != nil {
		t.Error("no canary on documented tool names")
	}
}

func TestUsersParsesCorpusFixtures(t *testing.T) {
	p := mustParse(t, parseUsers, fixture(t, SurfaceUsers, "normal_page.json"))
	if len(p.Resources) != 1 || p.NextCursor != "last_id" {
		t.Fatalf("resources %d next %q", len(p.Resources), p.NextCursor)
	}
	r := p.Resources[0]
	if r.Type != UserType || r.ID.Namespace != NamespaceUser || r.ID.Value != "user_01WCz1FkmYMm4gnmykNKUu3Q" ||
		r.DisplayName != "Jane Doe" || r.Attrs["role"] != "admin" || r.Attrs["email"] != "user@emaildomain.com" {
		t.Fatalf("user %+v", r)
	}
	sweep := mustParse(t, parseUsers, fixture(t, SurfaceUsers, "enum_sweep.json"))
	if len(sweep.Resources) != 9 || sweep.NextCursor != "" {
		t.Fatalf("enum sweep %d next %q", len(sweep.Resources), sweep.NextCursor)
	}
}

// TestFieldPolicyMatrix (Anthropic drivers, plan 12A.5): a missing required
// field, a wrong JSON type, a non-daily bucket, a repeated group, a
// non-USD currency, an undocumented actor and a non-200 status fail the page
// as schema drift; unknown members are ignored; an unknown tool_actions key
// lands under tool:other with a canary signal.
func TestFieldPolicyMatrix(t *testing.T) {
	bucket := func(results string) []byte {
		return []byte(`{"data":[{"starting_at":"2025-08-01T00:00:00Z","ending_at":"2025-08-02T00:00:00Z","results":[` +
			results + `]}],"has_more":false,"next_page":null}`)
	}
	usage := `{"uncached_input_tokens":1,"cache_read_input_tokens":2,"cache_creation":{"ephemeral_5m_input_tokens":3,"ephemeral_1h_input_tokens":4},"output_tokens":5,"model":"m"}`
	cc := func(actor, extra string) []byte {
		return []byte(`{"data":[{"date":"2025-08-08T00:00:00Z","actor":` + actor + `,"is_remote":false,` +
			`"core_metrics":{"num_sessions":1,"lines_of_code":{"added":1,"removed":1},"commits_by_claude_code":1,"pull_requests_by_claude_code":1},` +
			`"model_breakdown":[],"tool_actions":{` + extra + `}}],"has_more":false,"next_page":null}`)
	}
	user := `{"type":"user_actor","email_address":"A@B.com"}`
	drift := []struct {
		name  string
		parse func(vendorapi.Response) (vendorapi.Page, error)
		body  []byte
	}{
		{"usage container", parseUsage, []byte(`{"buckets":[],"has_more":false}`)},
		{"usage flag type", parseUsage, []byte(`{"data":[],"has_more":"false"}`)},
		{"usage more without cursor", parseUsage, []byte(`{"data":[],"has_more":true,"next_page":null}`)},
		{"usage missing metric", parseUsage, bucket(`{"uncached_input_tokens":1,"output_tokens":1}`)},
		{"usage negative", parseUsage, bucket(strings.Replace(usage, `"output_tokens":5`, `"output_tokens":-5`, 1))},
		{"usage float count", parseUsage, bucket(strings.Replace(usage, `"output_tokens":5`, `"output_tokens":5.5`, 1))},
		{"usage repeated model", parseUsage, bucket(usage + "," + usage)},
		{"usage not daily", parseUsage, []byte(`{"data":[{"starting_at":"2025-08-01T06:00:00Z","ending_at":"2025-08-02T06:00:00Z","results":[]}],"has_more":false}`)},
		{"usage results missing", parseUsage, []byte(`{"data":[{"starting_at":"2025-08-01T00:00:00Z","ending_at":"2025-08-02T00:00:00Z"}],"has_more":false}`)},
		{"cost currency", parseCost, bucket(`{"amount":"1","currency":"EUR"}`)},
		{"cost amount", parseCost, bucket(`{"amount":"1e3","currency":"USD"}`)},
		{"cost amount type", parseCost, bucket(`{"amount":12,"currency":"USD"}`)},
		{"cost repeated line", parseCost, bucket(`{"amount":"1","currency":"USD","cost_type":"tokens"},{"amount":"2","currency":"USD","cost_type":"tokens"}`)},
		{"cc actor", parseClaudeCode, cc(`{"type":"robot_actor","name":"x"}`, "")},
		{"cc metric missing", parseClaudeCode, []byte(`{"data":[{"date":"2025-08-08T00:00:00Z","actor":` + user + `,"core_metrics":{},"model_breakdown":[]}],"has_more":false}`)},
		{"cc date", parseClaudeCode, []byte(`{"data":[{"date":"2025-08-08T05:00:00Z","actor":` + user + `}],"has_more":false}`)},
		{"cc repeated actor day", parseClaudeCode, []byte(strings.Replace(string(cc(user, "")), `}}],"has_more"`,
			`}},{"date":"2025-08-08T00:00:00Z","actor":{"type":"user_actor","email_address":"a@b.com"},"is_remote":false,"core_metrics":{"num_sessions":1,"lines_of_code":{"added":1,"removed":1},"commits_by_claude_code":1,"pull_requests_by_claude_code":1},"model_breakdown":[]}],"has_more"`, 1))},
		{"cc tool type", parseClaudeCode, cc(user, `"edit_tool":{"accepted":true,"rejected":0}`)},
		{"users id missing", parseUsers, []byte(`{"data":[{"email":"a@b.c"}],"has_more":false}`)},
		{"users repeated", parseUsers, []byte(`{"data":[{"id":"u1"},{"id":"u1"}],"has_more":false}`)},
		{"users more without last id", parseUsers, []byte(`{"data":[],"has_more":true}`)},
	}
	for _, c := range drift {
		if _, err := c.parse(resp(c.body)); err == nil || !errors.Is(err, vendorapi.ErrSchemaDrift) {
			t.Errorf("%s: err %v, want schema drift", c.name, err)
		}
	}
	st := resp([]byte(`{"data":[],"has_more":false}`))
	st.Status = 202
	for _, parse := range []func(vendorapi.Response) (vendorapi.Page, error){parseUsage, parseCost, parseClaudeCode, parseUsers} {
		if _, err := parse(st); !errors.Is(err, vendorapi.ErrSchemaDrift) {
			t.Errorf("status 202 accepted: %v", err)
		}
	}
	p, err := parseClaudeCode(resp(cc(user, `"edit_tool":{"accepted":2,"rejected":1},"Weird Tool!":{"accepted":3,"rejected":4},"x-y":{"accepted":1,"rejected":1}`)))
	if err != nil {
		t.Fatal(err)
	}
	f := facts(p)
	if f["a@b.com||tool:other|local|tool_accepted"].ValueMicros != 4_000_000 ||
		f["a@b.com||tool:other|local|tool_rejected"].ValueMicros != 5_000_000 ||
		f["a@b.com||tool:edit_tool|local|tool_accepted"].ValueMicros != 2_000_000 {
		t.Errorf("tool facts %v", f)
	}
	if len(canaryClaudeCode(p)) != 1 {
		t.Error("unknown tool key: no canary")
	}
	for _, fr := range p.Facts {
		if strings.Contains(fr.Feature, "Weird") || strings.Contains(fr.Feature, "x-y") {
			t.Errorf("raw unknown key stored: %q", fr.Feature)
		}
	}
	// Unknown members are ignored.
	if _, err := parseUsage(resp(bucket(strings.Replace(usage, `"model":"m"`, `"model":"m","new_field":{"x":1}`, 1)))); err != nil {
		t.Errorf("unknown member refused: %v", err)
	}
}

func TestBuildWindowsAndPaging(t *testing.T) {
	from := time.Date(2026, 10, 7, 1, 0, 0, 0, time.UTC)
	w := vendorapi.Window{From: from, To: from.Add(5 * time.Hour)}
	req, err := buildUsage(nil, w, "")
	if err != nil {
		t.Fatal(err)
	}
	// From minus the settle lag crosses midnight: the request starts the day before.
	if req.Path != "/v1/organizations/usage_report/messages" || req.Query["starting_at"][0] != "2026-10-06T00:00:00Z" ||
		req.Query["ending_at"][0] != "2026-10-07T06:00:00Z" || req.Query["bucket_width"][0] != "1d" ||
		req.Query["group_by[]"][0] != "model" || req.Query["limit"][0] != "31" || req.Query["page"] != nil {
		t.Fatalf("usage request %+v", req)
	}
	req, err = buildCost(nil, w, "page_abc=")
	if err != nil || req.Query["page"][0] != "page_abc=" || req.Query["group_by[]"][0] != "description" || req.Path != "/v1/organizations/cost_report" {
		t.Fatalf("cost request %+v %v", req, err)
	}
	req, err = buildClaudeCode(nil, w, "")
	if err != nil || req.Query["starting_at"][0] != "2026-10-06" || req.Query["limit"][0] != "1000" {
		t.Fatalf("claude code request %+v %v", req, err)
	}
	req, err = buildClaudeCode(nil, vendorapi.Window{From: from.Add(3 * time.Hour), To: from.Add(4 * time.Hour)}, "")
	if err != nil || req.Query["starting_at"][0] != "2026-10-07" {
		t.Fatalf("claude code settled day %+v %v", req, err)
	}
	req, err = buildUsers(nil, vendorapi.Window{}, "user_01X")
	if err != nil || req.Query["after_id"][0] != "user_01X" || req.Query["limit"][0] != "1000" {
		t.Fatalf("users request %+v %v", req, err)
	}
	for _, c := range []struct {
		name  string
		build func(vendorapi.Params, vendorapi.Window, string) (vendorapi.Request, error)
		w     vendorapi.Window
		cur   string
	}{
		{"usage empty window", buildUsage, vendorapi.Window{}, ""},
		{"cost empty window", buildCost, vendorapi.Window{}, ""},
		{"cc empty window", buildClaudeCode, vendorapi.Window{}, ""},
		{"usage bad cursor", buildUsage, w, "a b"},
		{"users bad cursor", buildUsers, w, "x&y"},
	} {
		if _, err := c.build(nil, c.w, c.cur); err == nil {
			t.Errorf("%s accepted", c.name)
		}
	}
	for _, d := range []vendorapi.Driver{UsageMessages(), CostReport(), ClaudeCodeUsage(), Users(), UserCostReport()} {
		if d.Auth.Kind != vs.AuthHeaderKey || d.Auth.Scheme != vendorapi.SchemeRawHeader || d.Auth.Header != "X-Api-Key" ||
			d.Headers["anthropic-version"] != APIVersion {
			t.Errorf("%s auth %+v headers %v", d.Surface, d.Auth, d.Headers)
		}
		for k := range d.Headers {
			if strings.EqualFold(k, "x-api-key") || strings.EqualFold(k, "authorization") {
				t.Errorf("%s: static header %q would carry a credential", d.Surface, k)
			}
		}
	}
	if d := ClaudeCodeUsage(); d.LegacyPoller != LegacyClaudeCodeKey || d.MaxWindow != 24*time.Hour || d.Tool != Tool {
		t.Errorf("claude code driver %+v", d)
	}
	if d := Users(); !d.AuthoritativeSnapshot || d.Tool != "" {
		t.Errorf("users driver %+v", d)
	}
}
