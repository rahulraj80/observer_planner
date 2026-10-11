package factory

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/vendorapi"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "vendorapi", "factory", string(SurfaceTokens), name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func resp(body []byte) vendorapi.Response {
	return vendorapi.Response{Status: 200, Origin: "https://api.factory.ai:443", Body: body}
}

func stamp(p vendorapi.Page) vendorapi.Page {
	p.Snapshot = &vendorapi.SnapshotManifest{ManifestID: "run-1", ObservedAt: time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)}
	return p
}

// TestTokensParsesCorpusFixtures: per model per UTC day, Standard Credits
// in credits and the four token counts in tokens; a null cache count is
// not reported and never lands as zero.
func TestTokensParsesCorpusFixtures(t *testing.T) {
	p, err := parseTokens(resp(fixture(t, "normal_page.json")))
	if err != nil {
		t.Fatal(err)
	}
	if err := vendorapi.Validate(stamp(p)); err != nil {
		t.Fatalf("parsed page does not validate: %v", err)
	}
	if len(p.Facts) != 5+3 || p.SourceRecords != 2 {
		t.Fatalf("facts %d records %d", len(p.Facts), p.SourceRecords)
	}
	day := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	got := map[string]vendorapi.FactRow{}
	for _, f := range p.Facts {
		if !f.IntervalStart.Equal(day) || !f.IntervalEnd.Equal(day.Add(24*time.Hour)) || f.CoverageDomain != TokensDomain {
			t.Fatalf("fact %+v", f)
		}
		got[f.ModelRaw+"/"+f.Metric] = f
	}
	if f := got["claude-sonnet-4-5-20250929/standard_credits"]; f.Unit != UnitCredits || f.ValueMicros != 450000*1_000_000 {
		t.Errorf("credits fact %+v", f)
	}
	if f := got["claude-sonnet-4-5-20250929/cache_read_tokens"]; f.Unit != "tokens" || f.ValueMicros != 900000*1_000_000 {
		t.Errorf("cache read fact %+v", f)
	}
	if _, ok := got["gpt-5-codex/cache_read_tokens"]; ok {
		t.Error("a null cache count landed")
	}
	empty, err := parseTokens(resp(fixture(t, "empty_page.json")))
	if err != nil || len(empty.Facts) != 0 {
		t.Fatalf("empty page %+v %v", empty, err)
	}
}

// TestTokensBuildEndsYesterday: the request never asks for today (a
// documented 400) and re-reads the day before the window.
func TestTokensBuildEndsYesterday(t *testing.T) {
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	req, err := buildTokens(nil, vendorapi.Window{From: now.Add(-time.Hour), To: now}, "")
	if err != nil {
		t.Fatal(err)
	}
	if req.Path != "/api/v1/analytics/tokens" || req.Query["startDate"][0] != "2026-10-07" || req.Query["endDate"][0] != "2026-10-07" ||
		req.Query["group_by"][0] != "model" {
		t.Fatalf("request %+v", req)
	}
	req, err = buildTokens(nil, vendorapi.Window{From: now.Add(-72 * time.Hour), To: now}, "")
	if err != nil || req.Query["startDate"][0] != "2026-10-04" || req.Query["endDate"][0] != "2026-10-07" {
		t.Fatalf("three-day request %+v %v", req, err)
	}
	for _, w := range []vendorapi.Window{{}, {From: now.Add(-40 * 24 * time.Hour), To: now}} {
		if _, err := buildTokens(nil, w, ""); err == nil {
			t.Errorf("window %v accepted", w)
		}
	}
	if _, err := buildTokens(nil, vendorapi.Window{From: now.Add(-time.Hour), To: now}, "2"); err == nil {
		t.Error("the surface is not paged")
	}
	if d := Tokens(); d.Tool != "droid" || d.Auth.Scheme != vendorapi.SchemeBearer {
		t.Errorf("driver %+v", d)
	}
}

// TestTokensFieldPolicy (plan 12A.5).
func TestTokensFieldPolicy(t *testing.T) {
	row := func(extra string) string {
		return `{"data":[{"date":"2026-01-15","group_key":"m","billable_tokens":1,"input_tokens":1,"output_tokens":1` + extra + `}]}`
	}
	for name, body := range map[string]string{
		"data missing":       `{"meta":{}}`,
		"date missing":       `{"data":[{"group_key":"m","billable_tokens":1,"input_tokens":1,"output_tokens":1}]}`,
		"date format":        `{"data":[{"date":"15/01/2026","group_key":"m","billable_tokens":1,"input_tokens":1,"output_tokens":1}]}`,
		"group key missing":  `{"data":[{"date":"2026-01-15","billable_tokens":1,"input_tokens":1,"output_tokens":1}]}`,
		"credits missing":    `{"data":[{"date":"2026-01-15","group_key":"m","input_tokens":1,"output_tokens":1}]}`,
		"negative count":     row(`,"cache_read_tokens":-1`),
		"quoted count":       row(`,"cache_write_tokens":"5"`),
		"exponent":           row(`,"cache_write_tokens":1e5`),
		"repeated day model": `{"data":[` + row("")[9:len(row(""))-2] + `,` + row("")[9:len(row(""))-2] + `]}`,
	} {
		if _, err := parseTokens(resp([]byte(body))); err == nil || !errors.Is(err, vendorapi.ErrSchemaDrift) {
			t.Errorf("%s: err %v, want schema drift", name, err)
		}
	}
	r := resp([]byte(`{"data":[]}`))
	r.Status = 202
	if _, err := parseTokens(r); err == nil {
		t.Error("a 202 was accepted")
	}
	if _, err := parseTokens(resp([]byte(row(`,"newField":true`)))); err != nil {
		t.Errorf("unknown members must be ignored: %v", err)
	}
}
