package kilo

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/vendorapi"
)

// TestTable (lane ENTAPI-L16D, T-713): the closed input groups by user and
// model per day at the documented limit and matches the corpus request; each
// row lands one fact per column under the Kilo user id with the model, cost
// in micro-units of usd; a cut, mis-grouped or malformed answer is drift.
func TestTable(t *testing.T) {
	w := vendorapi.Window{From: time.Date(2026, 10, 6, 7, 0, 0, 0, time.UTC), To: time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)}
	req, err := buildTable(vendorapi.Params{ParamOrganization: orgID, "projects": "x"}, w, "")
	if err != nil {
		t.Fatal(err)
	}
	var side struct {
		HTTP struct {
			Query map[string]string `json:"query"`
		} `json:"http"`
	}
	if err := json.Unmarshal(fixture(t, SurfaceTable, "normal_page.source.json"), &side); err != nil {
		t.Fatal(err)
	}
	if req.Path != "/api/trpc/usageAnalytics.getTable" || len(req.Query) != 1 || side.HTTP.Query["input"] != req.Query["input"][0] {
		t.Fatalf("request %+v, corpus %s", req, side.HTTP.Query["input"])
	}
	if strings.Contains(req.Query["input"][0], "projects") {
		t.Fatal("a caller parameter reached the input")
	}
	for name, c := range map[string]struct {
		p vendorapi.Params
		w vendorapi.Window
		c string
	}{
		"no org":    {vendorapi.Params{}, w, ""},
		"cursor":    {vendorapi.Params{ParamOrganization: orgID}, w, "x"},
		"long":      {vendorapi.Params{ParamOrganization: orgID}, vendorapi.Window{From: w.To.Add(-8 * day), To: w.To}, ""},
		"empty win": {vendorapi.Params{ParamOrganization: orgID}, vendorapi.Window{From: w.To, To: w.To}, ""},
	} {
		if _, err := buildTable(c.p, c.w, c.c); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}

	p, err := parseTable(resp(fixture(t, SurfaceTable, "normal_page.json")))
	if err != nil {
		t.Fatal(err)
	}
	if err := vendorapi.Validate(p); err != nil {
		t.Fatal(err)
	}
	if len(p.Facts) != 3*len(tableMetrics) || p.SourceRecords != 1 || p.NextCursor != "" {
		t.Fatalf("page: %d facts, %d records", len(p.Facts), p.SourceRecords)
	}
	got := map[string]vendorapi.FactRow{}
	for _, f := range p.Facts {
		if f.IdentityKind != "user_id" || f.ModelRaw == "" || f.CoverageDomain != TableDomain {
			t.Fatalf("fact %+v", f)
		}
		got[f.IntervalStart.Format("2006-01-02")+"|"+f.Identity+"|"+f.Metric] = f
	}
	if f := got["2026-10-06|user_7c1f2a|cost"]; f.Unit != "usd" || f.ValueMicros != 184250 {
		t.Errorf("cost fact %+v", f)
	}
	if f := got["2026-10-06|user_9d3e4b|input_tokens"]; f.Unit != "tokens" || f.ValueMicros != 21000*1_000_000 {
		t.Errorf("tokens fact %+v", f)
	}

	row := `{"datetime":"2026-10-06T00:00:00Z","dimensions":{"user":"u","model":"m"},"costMicrodollars":1,` +
		`"requestCount":1,"inputTokens":1,"outputTokens":1,"cacheWriteTokens":0,"cacheHitTokens":0,"errorCount":0}`
	body := func(rows, gran string) string {
		return `{"result":{"data":{"rows":[` + rows + `],"effectiveGranularity":"` + gran + `"}}}`
	}
	if _, err := parseTable(resp([]byte(body(row, "day")))); err != nil {
		t.Fatalf("base row: %v", err)
	}
	for name, b := range map[string]string{
		"hour buckets":     body(row, "hour"),
		"repeated key":     body(row+","+row, "day"),
		"extra dimension":  body(strings.Replace(row, `"model":"m"`, `"model":"m","project":"p"`, 1), "day"),
		"no user":          body(strings.Replace(row, `"user":"u"`, `"mode":"u"`, 1), "day"),
		"fractional count": body(strings.Replace(row, `"inputTokens":1`, `"inputTokens":1.5`, 1), "day"),
		"negative cost":    body(strings.Replace(row, `"costMicrodollars":1`, `"costMicrodollars":-1`, 1), "day"),
		"quoted cost":      body(strings.Replace(row, `"costMicrodollars":1`, `"costMicrodollars":"1"`, 1), "day"),
		"missing errors":   body(strings.Replace(row, `,"errorCount":0`, ``, 1), "day"),
		"not a day":        body(strings.Replace(row, `T00:00:00Z`, `T05:00:00Z`, 1), "day"),
		"rows missing":     `{"result":{"data":{"effectiveGranularity":"day"}}}`,
	} {
		_, err := parseTable(resp([]byte(b)))
		wantDrift(t, name, err)
	}
	full := strings.TrimSuffix(strings.Repeat(row+",", TableLimit), ",")
	_, err = parseTable(resp([]byte(body(full, "day"))))
	wantDrift(t, "rows at the limit", err)
}
