package poolside

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/vendorapi"
)

const dailyBody = `{
 "results": [
  {"date": "2025-03-27",
   "metrics": {"spend": 0.0177072, "prompt_tokens": 111, "completion_tokens": 1711, "total_tokens": 1822, "api_requests": 11},
   "breakdown": {
    "models": {
     "laguna": {"metrics": {"spend": 1.82e-05, "prompt_tokens": 37, "completion_tokens": 9, "total_tokens": 46, "api_requests": 1,
                "cache_read_input_tokens": 0, "cache_creation_input_tokens": 0, "successful_requests": 1, "failed_requests": 0},
                "metadata": {}, "api_key_breakdown": {"3126b6eaf1": {"metrics": {"spend": 1.82e-05}, "metadata": {"user_email": "a@corp.example"}}}},
     "laguna-xs": {"metrics": {"spend": 0.0176890, "prompt_tokens": 74, "completion_tokens": 1702, "total_tokens": 1776, "api_requests": 10}}
    },
    "providers": {"hosted_vllm": {"metrics": {"spend": 0.0177072}}},
    "api_keys": {"3126b6eaf1": {"metrics": {"spend": 0.0177072}, "metadata": {"key_alias": "ci", "user_email": "a@corp.example"}}}
   }}
 ],
 "metadata": {"total_spend": 0.0177072, "total_prompt_tokens": 111, "total_completion_tokens": 1711, "total_api_requests": 11,
              "page": 1, "total_pages": 1, "has_more": false}
}`

func TestLiteLLMBuild(t *testing.T) {
	d := LiteLLM()
	if d.Auth.Scheme != vendorapi.SchemeBearer || d.Tool != "gateway:litellm" || !vendorapi.IsGatewayTool(d.Tool) {
		t.Fatalf("driver %+v", d)
	}
	from := time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)
	req, err := d.Build(nil, vendorapi.Window{From: from, To: from.Add(24 * time.Hour)}, "")
	if err != nil || req.Method != "GET" || req.Path != "/user/daily/activity" {
		t.Fatalf("build %+v %v", req, err)
	}
	q := req.Query
	if q["start_date"][0] != "2026-10-07" || q["end_date"][0] != "2026-10-07" || q["page"][0] != "1" || q["page_size"][0] != "1000" {
		t.Errorf("query %v", q)
	}
	for _, k := range []string{"user_id", "api_key", "model", "timezone", "include_current_utc_day"} {
		if _, has := q[k]; has {
			t.Errorf("%s must never be sent", k)
		}
	}
	// A window crossing midnight asks for both dates.
	req, _ = d.Build(nil, vendorapi.Window{From: from.Add(12 * time.Hour), To: from.Add(36 * time.Hour)}, "")
	if req.Query["start_date"][0] != "2026-10-07" || req.Query["end_date"][0] != "2026-10-08" {
		t.Errorf("crossing window %v", req.Query)
	}
	for name, c := range map[string]struct {
		w      vendorapi.Window
		cursor string
	}{
		"cursor":       {vendorapi.Window{From: from, To: from.Add(time.Hour)}, "2"},
		"empty window": {vendorapi.Window{From: from, To: from}, ""},
		"too long":     {vendorapi.Window{From: from, To: from.Add(25 * time.Hour)}, ""},
	} {
		if _, err := d.Build(nil, c.w, c.cursor); err == nil {
			t.Errorf("%s: want a refusal", name)
		}
	}
}

func TestLiteLLMParse(t *testing.T) {
	d := LiteLLM()
	p, err := d.ParseResponse(vendorapi.Response{Status: 200, Body: []byte(dailyBody)})
	if err != nil {
		t.Fatal(err)
	}
	// laguna: 8 metrics (the optional four present); laguna-xs: the 4 required.
	if len(p.Facts) != 12 || p.SourceRecords != 1 {
		t.Fatalf("facts %d records %d", len(p.Facts), p.SourceRecords)
	}
	got := map[string]int64{}
	for _, f := range p.Facts {
		if f.ModelRaw == "" || f.Identity != "" || f.IdentityKind != "" || f.CoverageDomain != Domain || f.SourceTZ != "UTC" ||
			!f.IntervalEnd.Equal(f.IntervalStart.Add(24*time.Hour)) {
			t.Errorf("fact %+v", f)
		}
		got[f.ModelRaw+"|"+f.Metric] = f.ValueMicros
	}
	for k, want := range map[string]int64{
		"laguna|cost": 18, "laguna|input_tokens": 37_000_000, "laguna|requests": 1_000_000,
		"laguna|cache_read_tokens": 0, "laguna|failed_requests": 0, "laguna-xs|cost": 17_689,
		"laguna-xs|output_tokens": 1_702_000_000,
	} {
		if got[k] != want {
			t.Errorf("%s = %d, want %d", k, got[k], want)
		}
	}
	if _, has := got["laguna-xs|cache_read_tokens"]; has {
		t.Error("an absent optional metric must not land (never zero)")
	}
	for _, f := range p.Facts {
		if strings.Contains(f.Feature+f.ModelRaw+f.Metric, "@") || strings.Contains(f.ModelRaw, "3126") {
			t.Errorf("a key, alias or e-mail leaked into a fact: %+v", f)
		}
	}

	drifts := map[string]string{
		"has_more":         strings.Replace(dailyBody, `"has_more": false`, `"has_more": true`, 1),
		"two pages":        strings.Replace(dailyBody, `"total_pages": 1`, `"total_pages": 2`, 1),
		"page two":         strings.Replace(dailyBody, `"page": 1,`, `"page": 2,`, 1),
		"no metadata":      `{"results": []}`,
		"no has_more":      `{"results": [], "metadata": {"page": 1, "total_pages": 0}}`,
		"quoted spend":     strings.Replace(dailyBody, `"spend": 1.82e-05, "prompt_tokens": 37`, `"spend": "1.82e-05", "prompt_tokens": 37`, 1),
		"negative spend":   strings.Replace(dailyBody, `"spend": 1.82e-05, "prompt_tokens": 37`, `"spend": -1.82e-05, "prompt_tokens": 37`, 1),
		"fractional count": strings.Replace(dailyBody, `"prompt_tokens": 37`, `"prompt_tokens": 37.5`, 1),
		"missing required": strings.Replace(dailyBody, `"prompt_tokens": 74, `, ``, 1),
		"bad date":         strings.Replace(dailyBody, `"2025-03-27"`, `"27/03/2025"`, 1),
		"repeated date":    `{"results": [{"date": "2025-03-27", "breakdown": {"models": {}}}, {"date": "2025-03-27", "breakdown": {"models": {}}}], "metadata": {"page": 1, "total_pages": 1, "has_more": false}}`,
		"flat model entry": `{"results": [{"date": "2025-03-27", "breakdown": {"models": {"m": {"spend": 1}}}}], "metadata": {"page": 1, "total_pages": 1, "has_more": false}}`,
		"no breakdown":     `{"results": [{"date": "2025-03-27"}], "metadata": {"page": 1, "total_pages": 1, "has_more": false}}`,
		"results object":   `{"results": {}, "metadata": {"page": 1, "total_pages": 1, "has_more": false}}`,
	}
	for name, body := range drifts {
		if _, err := d.ParseResponse(vendorapi.Response{Status: 200, Body: []byte(body)}); !errors.Is(err, vendorapi.ErrSchemaDrift) {
			t.Errorf("%s: err %v, want schema drift", name, err)
		}
	}
	if _, err := d.ParseResponse(vendorapi.Response{Status: 401, Body: []byte(`{}`)}); !errors.Is(err, vendorapi.ErrSchemaDrift) {
		t.Errorf("a non-200 must be drift, got %v", err)
	}
	empty, err := d.ParseResponse(vendorapi.Response{Status: 200, Body: []byte(`{"results": [], "metadata": {"page": 1, "total_pages": 0, "has_more": false}}`)})
	if err != nil || len(empty.Facts) != 0 {
		t.Errorf("an empty window: %+v %v", empty, err)
	}
}
