package perplexity

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

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	return fixtureOf(t, SurfaceMemberUsage, name)
}

func fixtureOf(t *testing.T, s vs.SurfaceID, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "vendorapi", "perplexity", string(s), name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestMemberQueryVolume (lane ENTAPI-L16D, T-713): the query_volume dataset
// is its own row and driver. Each member-day lands a queries total and one
// per-feature fact (Feature, never ModelRaw); Project and Comet never land;
// the request names query_volume; the field policy of the credit driver
// applies unchanged.
func TestMemberQueryVolume(t *testing.T) {
	d := MemberQueryVolume()
	if d.Surface != SurfaceMemberQueryVolume || d.Surface == MemberUsage().Surface {
		t.Fatalf("surface %s", d.Surface)
	}
	p, err := d.ParseResponse(resp(fixtureOf(t, SurfaceMemberQueryVolume, "normal_page.json")))
	if err != nil {
		t.Fatal(err)
	}
	if err := vendorapi.Validate(p); err != nil {
		t.Fatal(err)
	}
	features := 0
	for _, f := range p.Facts {
		if f.Metric != MetricQueries || f.Unit != UnitCount || f.ModelRaw != "" || f.IdentityKind != "email" {
			t.Fatalf("fact %+v", f)
		}
		if f.Feature != "" {
			features++
		}
	}
	if features == 0 || features == len(p.Facts) || p.NextCursor == "" {
		t.Fatalf("page %+v", p)
	}
	for _, f := range p.Facts {
		if strings.Contains(strings.ToLower(f.Feature), "space") {
			t.Fatalf("a Project breakdown landed: %+v", f)
		}
	}
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	req, err := d.Build(nil, vendorapi.Window{From: now.Add(-time.Hour), To: now}, "")
	if err != nil || req.Query["dataset"][0] != "query_volume" || req.Query["group_by"][0] != "user_email" {
		t.Fatalf("request %+v %v", req, err)
	}
	bad := `{"categories":[],"has_more":false,"data":[{"start_time":0,"end_time":86400,"results":[` +
		`{"user_email":"a@x.test","count":2,"by_categories":{"Feature":[{"category":"f","count":"1"}]}}]}]}`
	if _, err := d.ParseResponse(resp([]byte(bad))); !errors.Is(err, vendorapi.ErrSchemaDrift) {
		t.Errorf("quoted feature count: %v", err)
	}
}

func resp(body []byte) vendorapi.Response {
	return vendorapi.Response{Status: 200, Origin: "https://api.perplexity.ai:443", Body: body}
}

// TestMemberUsage (lane ENTAPI-L16B, T-708): the documented example lands a
// credits total and one per-model fact per member-day under the member's
// email; next_page pages; a fractional credit count is kept exactly.
func TestMemberUsage(t *testing.T) {
	p, err := parseMemberUsage(resp(fixture(t, "normal_page.json")))
	if err != nil {
		t.Fatal(err)
	}
	if err := vendorapi.Validate(p); err != nil {
		t.Fatal(err)
	}
	if len(p.Facts) != 4 || p.SourceRecords != 1 || p.NextCursor != "eyJwayI6ICJPUkcjLi4uIn0=" {
		t.Fatalf("page %+v", p)
	}
	day := time.Unix(1746057600, 0).UTC()
	for _, f := range p.Facts {
		if f.IdentityKind != "email" || f.Unit != UnitCredits || f.Metric != MetricCredits || !f.IntervalStart.Equal(day) {
			t.Fatalf("fact %+v", f)
		}
	}
	if f := p.Facts[1]; f.ModelRaw != "claude-opus-4-8" || f.ValueMicros != 900_000_000 || f.Identity != "alice@example.com" {
		t.Errorf("model fact %+v", f)
	}
	r := resp(fixture(t, "last_page.json"))
	r.Cursor = p.NextCursor
	p2, err := parseMemberUsage(r)
	if err != nil || p2.NextCursor != "" || len(p2.Facts) != 3 || p2.Facts[0].ValueMicros != 120_500_000 {
		t.Fatalf("last page %+v %v", p2, err)
	}
	bucket := func(row string) string {
		return `{"categories":[],"has_more":false,"data":[{"start_time":0,"end_time":86400,"results":[` + row + `]}]}`
	}
	for name, body := range map[string]string{
		"categories missing": `{"data":[],"has_more":false}`,
		"has_more missing":   `{"categories":[],"data":[]}`,
		"has_more quoted":    `{"categories":[],"data":[],"has_more":"false"}`,
		"more no page":       `{"categories":[],"data":[],"has_more":true,"next_page":null}`,
		"long period":        `{"categories":[],"has_more":false,"data":[{"start_time":0,"end_time":90000,"results":[]}]}`,
		"results missing":    `{"categories":[],"has_more":false,"data":[{"start_time":0,"end_time":86400}]}`,
		"email missing":      bucket(`{"count":1}`),
		"quoted count":       bucket(`{"user_email":"a@x.test","count":"1"}`),
		"negative":           bucket(`{"user_email":"a@x.test","count":-1}`),
		"repeated member":    bucket(`{"user_email":"a@x.test","count":1},{"user_email":"a@x.test","count":2}`),
		"model no category":  bucket(`{"user_email":"a@x.test","count":1,"by_categories":{"Model":[{"count":1}]}}`),
		"model repeated":     bucket(`{"user_email":"a@x.test","count":2,"by_categories":{"Model":[{"category":"m","count":1},{"category":"m","count":1}]}}`),
	} {
		if _, err := parseMemberUsage(resp([]byte(body))); err == nil || !errors.Is(err, vendorapi.ErrSchemaDrift) {
			t.Errorf("%s: err %v, want schema drift", name, err)
		}
	}
	r = resp([]byte(`{"categories":[],"data":[],"has_more":true,"next_page":"p1"}`))
	r.Cursor = "p1"
	if _, err := parseMemberUsage(r); err == nil {
		t.Error("a repeated page token was accepted")
	}
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	req, err := buildMemberUsage(nil, vendorapi.Window{From: now.Add(-time.Hour), To: now}, "tok")
	if err != nil || req.Query["dataset"][0] != "credit_usage" || req.Query["group_by"][0] != "user_email" ||
		req.Query["start_time"][0] != "1791331200" || req.Query["page"][0] != "tok" || req.Query["limit"][0] != "100" {
		t.Fatalf("request %+v %v", req, err)
	}
	if _, err := buildMemberUsage(nil, vendorapi.Window{From: now.Add(-40 * 24 * time.Hour), To: now}, ""); err == nil {
		t.Error("a 40-day window was accepted")
	}
}
