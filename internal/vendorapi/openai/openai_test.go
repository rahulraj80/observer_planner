package openai

import (
	"encoding/json"
	"errors"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/testsupport/vendormock"
	"github.com/marmutapp/superbased-observer/internal/vendorapi"
	vs "github.com/marmutapp/superbased-observer/internal/vendorsurface"
)

func fixture(t *testing.T, surface vs.SurfaceID, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "vendorapi", "openai", string(surface), name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func resp(body []byte) vendorapi.Response {
	return vendorapi.Response{Status: 200, Origin: "https://api.openai.com:443", Body: body, Params: vendorapi.Params{}}
}

func stamp(p vendorapi.Page) vendorapi.Page {
	if p.Snapshot == nil {
		p.Snapshot = &vendorapi.SnapshotManifest{}
	}
	p.Snapshot.ManifestID = "run-1"
	p.Snapshot.ObservedAt = time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	return p
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

func byMetric(p vendorapi.Page) map[string]vendorapi.FactRow {
	out := map[string]vendorapi.FactRow{}
	for _, f := range p.Facts {
		out[f.ModelRaw+"|"+f.Feature+"|"+f.Metric] = f
	}
	return out
}

func TestUsageParsesCorpusFixtures(t *testing.T) {
	p := mustParse(t, parseUsage, fixture(t, SurfaceUsageCompletions, "normal_page.json"))
	if p.NextCursor != "page_AAAAAGdGxdEiJdKOAAAAAGcqsYA=" || p.SourceRecords != 1 {
		t.Fatalf("next %q records %d", p.NextCursor, p.SourceRecords)
	}
	f := byMetric(p)
	day := time.Unix(1730419200, 0).UTC()
	for metric, want := range map[string]int64{
		"input_tokens": 1000, "output_tokens": 500, "model_requests": 5, "input_tokens_cached": 400,
		"input_tokens_uncached": 600, "input_tokens_cache_write_30m": 128, "input_audio_tokens": 0, "output_audio_tokens": 0,
	} {
		r, ok := f["gpt-5.5||"+metric]
		if !ok || r.ValueMicros != want*1_000_000 || !r.IntervalStart.Equal(day) || r.IntervalEnd.Sub(r.IntervalStart) != 24*time.Hour {
			t.Errorf("%s = %+v", metric, r)
		}
	}
	if len(p.Facts) != 8 {
		t.Errorf("facts %d, want 8 (absent optional counts land nothing)", len(p.Facts))
	}
	if f["gpt-5.5||model_requests"].Unit != "requests" {
		t.Error("request count unit")
	}
	last := mustParse(t, parseUsage, fixture(t, SurfaceUsageCompletions, "last_page.json"))
	if last.NextCursor != "" || last.SourceRecords != 2 || len(last.Facts) != 6 {
		t.Fatalf("last page next %q records %d facts %d", last.NextCursor, last.SourceRecords, len(last.Facts))
	}
}

func TestCostsParsesCorpusFixtures(t *testing.T) {
	p := mustParse(t, parseCosts, fixture(t, SurfaceCosts, "normal_page.json"))
	f := byMetric(p)
	if len(p.Facts) != 2 || f["|gpt-5.5, input|cost"].ValueMicros != 60_000 || f["|gpt-5.5, output|cost"].ValueMicros != 1_250_000 ||
		f["|gpt-5.5, output|cost"].Unit != "usd" || p.SourceRecords != 1 {
		t.Fatalf("cost facts %v", f)
	}
	empty := mustParse(t, parseCosts, fixture(t, SurfaceCosts, "empty_page.json"))
	if len(empty.Facts) != 0 || empty.NextCursor != "" || empty.SourceRecords != 1 {
		t.Fatalf("empty page %+v", empty)
	}
}

func TestAuditParsesCorpusFixtures(t *testing.T) {
	p := mustParse(t, parseAudit, fixture(t, SurfaceAuditLogs, "normal_page.json"))
	if len(p.Audit) != 3 || p.NextCursor != "audit_log-SYNTH0003" {
		t.Fatalf("events %d next %q", len(p.Audit), p.NextCursor)
	}
	e := p.Audit[0]
	if e.Namespace != AuditNamespace || e.EventID != "audit_log-SYNTH0001" || e.Action != "api_key.created" ||
		e.ActorKind != "email" || e.ActorIdentity != "admin@example.com" || e.Target != "key_SYNTH01" ||
		e.Detail["ip_address"] != "192.0.2.10" || e.Detail["project_id"] != "proj_SYNTHabc123" || e.Detail["actor_type"] != "session" ||
		!e.At.Equal(time.Unix(1730419200, 0)) {
		t.Fatalf("event 0 %+v", e)
	}
	for k := range e.Detail {
		if strings.Contains(k, "scope") || strings.Contains(k, "data") {
			t.Errorf("detail data member read: %q", k)
		}
	}
	sa := p.Audit[1]
	if sa.ActorKind != "service_account_id" || sa.ActorIdentity != "svc_acct_SYNTH01" || sa.Detail["key_type"] != "service_account" ||
		sa.Target != "proj_SYNTHdef456" {
		t.Fatalf("event 1 %+v", sa)
	}
	if p.Audit[2].Target != "" || p.Audit[2].Action != "login.succeeded" {
		t.Fatalf("event 2 %+v", p.Audit[2])
	}
	empty := mustParse(t, parseAudit, fixture(t, SurfaceAuditLogs, "empty_page.json"))
	if len(empty.Audit) != 0 || empty.NextCursor != "" {
		t.Fatalf("empty %+v", empty)
	}
}

func TestUsersParsesCorpusFixtures(t *testing.T) {
	p := mustParse(t, parseUsers, fixture(t, SurfaceUsers, "normal_page.json"))
	if len(p.Resources) != 2 || p.NextCursor != "user-SYNTH02" {
		t.Fatalf("users %d next %q", len(p.Resources), p.NextCursor)
	}
	r := p.Resources[0]
	if r.Type != UserType || r.ID.Value != "user-SYNTH01" || r.Attrs["role"] != "owner" || r.Attrs["added_at"] != "2024-11-01T00:00:01Z" ||
		r.Attrs["is_service_account"] != "false" || r.Attrs["is_scim_managed"] != "false" || r.DisplayName != "Member 1" {
		t.Fatalf("user 0 %+v", r)
	}
	if _, ok := p.Resources[1].Attrs["key_last_used_at"]; ok {
		t.Error("a null last-used time landed")
	}
	last := mustParse(t, parseUsers, fixture(t, SurfaceUsers, "last_page.json"))
	if len(last.Resources) != 1 || last.NextCursor != "" {
		t.Fatalf("last page %+v", last)
	}
}

// TestFieldPolicyMatrix (OpenAI drivers, plan 12A.5).
func TestFieldPolicyMatrix(t *testing.T) {
	bucket := func(results string) []byte {
		return []byte(`{"object":"page","data":[{"object":"bucket","start_time":1730419200,"end_time":1730505600,"results":[` +
			results + `]}],"has_more":false,"next_page":null}`)
	}
	usage := `{"input_tokens":1,"output_tokens":2,"num_model_requests":3,"model":"m"}`
	event := func(fields string) []byte {
		return []byte(`{"object":"list","data":[{` + fields + `}],"has_more":false}`)
	}
	drift := []struct {
		name  string
		parse func(vendorapi.Response) (vendorapi.Page, error)
		body  []byte
	}{
		{"usage container", parseUsage, []byte(`{"object":"page","buckets":[],"has_more":false}`)},
		{"usage flag type", parseUsage, []byte(`{"data":[],"has_more":"false"}`)},
		{"usage more without cursor", parseUsage, []byte(`{"data":[],"has_more":true}`)},
		{"usage required missing", parseUsage, bucket(`{"input_tokens":1,"output_tokens":2}`)},
		{"usage negative", parseUsage, bucket(`{"input_tokens":-1,"output_tokens":2,"num_model_requests":3}`)},
		{"usage repeated model", parseUsage, bucket(usage + "," + usage)},
		{"usage not daily", parseUsage, []byte(`{"data":[{"start_time":1730419300,"end_time":1730505700,"results":[]}],"has_more":false}`)},
		{"usage bounds type", parseUsage, []byte(`{"data":[{"start_time":"x","end_time":1730505600,"results":[]}],"has_more":false}`)},
		{"costs currency", parseCosts, bucket(`{"amount":{"value":1,"currency":"eur"},"line_item":"a"}`)},
		{"costs amount missing", parseCosts, bucket(`{"line_item":"a"}`)},
		{"costs repeated line", parseCosts, bucket(`{"amount":{"value":1,"currency":"usd"},"line_item":"a"},{"amount":{"value":2,"currency":"usd"},"line_item":"a"}`)},
		{"costs exponent", parseCosts, bucket(`{"amount":{"value":1e2,"currency":"usd"},"line_item":"a"}`)},
		{"audit id missing", parseAudit, event(`"type":"login.succeeded","effective_at":1`)},
		{"audit time type", parseAudit, event(`"id":"a","type":"login.succeeded","effective_at":"soon"`)},
		{"audit repeated", parseAudit, []byte(`{"data":[{"id":"a","type":"x.y","effective_at":1},{"id":"a","type":"x.y","effective_at":2}],"has_more":false}`)},
		{"audit more without last id", parseAudit, []byte(`{"data":[],"has_more":true}`)},
		{"users id missing", parseUsers, []byte(`{"data":[{"email":"a@b.c"}],"has_more":false}`)},
		{"users added type", parseUsers, []byte(`{"data":[{"id":"u","added_at":"yesterday"}],"has_more":false}`)},
		{"users flag type", parseUsers, []byte(`{"data":[{"id":"u","is_service_account":"no"}],"has_more":false}`)},
	}
	for _, c := range drift {
		if _, err := c.parse(resp(c.body)); err == nil || !errors.Is(err, vendorapi.ErrSchemaDrift) {
			t.Errorf("%s: err %v, want schema drift", c.name, err)
		}
	}
	st := resp([]byte(`{"data":[],"has_more":false}`))
	st.Status = 204
	for _, parse := range []func(vendorapi.Response) (vendorapi.Page, error){parseUsage, parseCosts, parseAudit, parseUsers} {
		if _, err := parse(st); !errors.Is(err, vendorapi.ErrSchemaDrift) {
			t.Errorf("status 204 accepted: %v", err)
		}
	}
	// Unknown event types and an undecodable type land as other with a canary;
	// a detail object's data and unknown top-level members are never read.
	p, err := parseAudit(resp(event(`"id":"a","type":"Brand New Event!","effective_at":5,"Brand New Event!":{"id":"r","data":{"secret_value":"never"}},"extra":{"k":1}`)))
	if err != nil {
		t.Fatal(err)
	}
	if p.Audit[0].Action != "other" || len(canaryAudit(p)) != 1 || p.Audit[0].Target != "r" || len(p.Audit[0].Detail) != 0 {
		t.Fatalf("unknown type %+v", p.Audit[0])
	}
	if len(canaryAudit(vendorapi.Page{})) != 0 {
		t.Error("no canary on a clean page")
	}
}

func TestBuildWindowsAndPaging(t *testing.T) {
	from := time.Date(2026, 10, 7, 1, 0, 0, 0, time.UTC)
	w := vendorapi.Window{From: from, To: from.Add(5 * time.Hour)}
	req, err := buildUsage(nil, w, "")
	if err != nil {
		t.Fatal(err)
	}
	if req.Path != "/v1/organization/usage/completions" || req.Query["start_time"][0] != "1791244800" ||
		req.Query["end_time"][0] != "1791352800" || req.Query["bucket_width"][0] != "1d" || req.Query["limit"][0] != "31" ||
		req.Query["group_by[]"][0] != "model" || req.Query["page"] != nil {
		t.Fatalf("usage request %+v", req)
	}
	req, err = buildCosts(nil, w, "page_x")
	if err != nil || req.Path != "/v1/organization/costs" || req.Query["group_by[]"][0] != "line_item" || req.Query["page"][0] != "page_x" {
		t.Fatalf("costs request %+v %v", req, err)
	}
	req, err = buildAudit(nil, w, "audit_log-1")
	if err != nil || req.Path != "/v1/organization/audit_logs" || req.Query["effective_at[gte]"][0] != "1791334800" ||
		req.Query["effective_at[lt]"][0] != "1791352800" || req.Query["after"][0] != "audit_log-1" || req.Query["limit"][0] != "100" {
		t.Fatalf("audit request %+v %v", req, err)
	}
	req, err = buildUsers(nil, vendorapi.Window{}, "")
	if err != nil || req.Path != "/v1/organization/users" || req.Query["after"] != nil {
		t.Fatalf("users request %+v %v", req, err)
	}
	for _, c := range []struct {
		name  string
		build func(vendorapi.Params, vendorapi.Window, string) (vendorapi.Request, error)
		w     vendorapi.Window
		cur   string
	}{
		{"usage empty window", buildUsage, vendorapi.Window{}, ""},
		{"costs empty window", buildCosts, vendorapi.Window{}, ""},
		{"audit empty window", buildAudit, vendorapi.Window{}, ""},
		{"usage bad cursor", buildUsage, w, "a b"},
		{"users bad cursor", buildUsers, w, "x&y"},
	} {
		if _, err := c.build(nil, c.w, c.cur); err == nil {
			t.Errorf("%s accepted", c.name)
		}
	}
	for _, d := range []vendorapi.Driver{UsageCompletions(), Costs(), AuditLogs(), Users()} {
		if d.Auth.Kind != vs.AuthBearer || d.Auth.Scheme != vendorapi.SchemeBearer || !d.ClosedDTO || len(d.Headers) != 0 {
			t.Errorf("%s driver %+v", d.Surface, d)
		}
	}
	for _, d := range []vendorapi.Driver{UsageCompletions(), Costs()} {
		if d.LegacyPoller != LegacyOrgKey || d.Tool != Tool || d.MaxWindow != ReportMaxWindow {
			t.Errorf("%s legacy %q tool %q", d.Surface, d.LegacyPoller, d.Tool)
		}
	}
}

// alignBuckets puts every generated bucket on a UTC day (the schema types the
// bounds as plain integers, so random bounds almost never form a daily
// bucket); every other member stays random.
func alignBuckets(v any) any {
	m, ok := v.(map[string]any)
	if !ok {
		return v
	}
	data, ok := m["data"].([]any)
	if !ok {
		return v
	}
	for i, b := range data {
		if bm, ok := b.(map[string]any); ok {
			start := int64(1730419200 + i*86400)
			bm["start_time"], bm["end_time"] = start, start+86400
		}
	}
	return m
}

// TestSchemaRandom drives the usage and costs parsers with random instances
// of the official response schemas (openai-python admin types, extracted to
// testdata/vendorschema): every instance either parses to one record per
// bucket with a valid page, or fails as schema drift. Nothing panics.
func TestSchemaRandom(t *testing.T) {
	for _, c := range []struct {
		id    vs.SurfaceID
		parse func(vendorapi.Response) (vendorapi.Page, error)
	}{{SurfaceUsageCompletions, parseUsage}, {SurfaceCosts, parseCosts}} {
		s, err := vendormock.LoadSchema(string(c.id))
		if err != nil {
			t.Fatal(err)
		}
		rng := rand.New(rand.NewSource(610))
		parsed := 0
		for i := 0; i < 300; i++ {
			raw, err := json.Marshal(alignBuckets(vendormock.Generate(s.Schema, rng, vendormock.GenOptions{})))
			if err != nil {
				t.Fatal(err)
			}
			p, err := c.parse(resp(raw))
			if err != nil {
				if !errors.Is(err, vendorapi.ErrSchemaDrift) {
					t.Fatalf("%s instance %d: non-drift error %v", c.id, i, err)
				}
				continue
			}
			var doc struct {
				Data []json.RawMessage `json:"data"`
			}
			_ = json.Unmarshal(raw, &doc)
			if p.SourceRecords != len(doc.Data) {
				t.Fatalf("%s instance %d: %d records parsed from %d buckets", c.id, i, p.SourceRecords, len(doc.Data))
			}
			if err := vendorapi.Validate(stamp(p)); err != nil {
				t.Fatalf("%s instance %d: %v", c.id, i, err)
			}
			parsed++
		}
		if parsed == 0 {
			t.Fatalf("%s: no random instance parsed: the property test would be vacuous", c.id)
		}
	}
}
