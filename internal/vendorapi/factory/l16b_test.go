package factory

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/vendorapi"
	vs "github.com/marmutapp/superbased-observer/internal/vendorsurface"
)

func fixtureOf(t *testing.T, s vs.SurfaceID, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "vendorapi", "factory", string(s), name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func drifted(err error) bool { return err != nil && errors.Is(err, vendorapi.ErrSchemaDrift) }

// stampGraph stamps a graph page's own manifest (stamp replaces it).
func stampGraph(p vendorapi.Page) vendorapi.Page {
	m := *p.Snapshot
	m.ManifestID, m.ObservedAt = "run-1", time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	p.Snapshot = &m
	return p
}

// TestUsersParsesCorpusFixtures (lane ENTAPI-L16B, T-708): the documented
// /users example lands per-user facts for its UTC day under the user's
// email; a null email keeps the vendor user id; the cursor is the
// documented meta.next_cursor.
func TestUsersParsesCorpusFixtures(t *testing.T) {
	p, err := parseUsers(resp(fixtureOf(t, SurfaceUsers, "normal_page.json")))
	if err != nil {
		t.Fatal(err)
	}
	if err := vendorapi.Validate(stamp(p)); err != nil {
		t.Fatal(err)
	}
	if p.NextCursor != "user_01HPMQ8ABCDE7Y7PR3TTZY4KLM" || p.SourceRecords != 1 || len(p.Facts) != len(userMetrics) {
		t.Fatalf("page next %q records %d facts %d", p.NextCursor, p.SourceRecords, len(p.Facts))
	}
	got := map[string]vendorapi.FactRow{}
	for _, f := range p.Facts {
		if f.IdentityKind != "email" || f.Identity != "developer@company.com" || f.ModelRaw != "" ||
			!f.IntervalStart.Equal(time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)) {
			t.Fatalf("fact %+v", f)
		}
		got[f.Metric] = f
	}
	if f := got[MetricStandardCredits]; f.Unit != UnitCredits || f.ValueMicros != 450000*1_000_000 {
		t.Errorf("credits %+v", f)
	}
	if f := got["git_commits"]; f.Unit != UnitCount || f.ValueMicros != 8*1_000_000 {
		t.Errorf("commits %+v", f)
	}
	r := resp(fixtureOf(t, SurfaceUsers, "last_page.json"))
	r.Cursor = p.NextCursor
	p2, err := parseUsers(r)
	if err != nil || p2.NextCursor != "" || p2.Facts[0].IdentityKind != "user_id" || p2.Facts[0].Identity != "user_synth0002" {
		t.Fatalf("last page %+v %v", p2, err)
	}
	req, err := buildUsers(nil, vendorapi.Window{
		From: time.Date(2026, 10, 8, 11, 0, 0, 0, time.UTC),
		To:   time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC),
	}, "user_x")
	if err != nil || req.Query["endDate"][0] != "2026-10-07" || req.Query["limit"][0] != "100" || req.Query["cursor"][0] != "user_x" {
		t.Fatalf("request %+v %v", req, err)
	}
}

// TestUsersFieldPolicy (plan 12A.5).
func TestUsersFieldPolicy(t *testing.T) {
	row := `{"user_id":"u","date":"2026-01-15","billable_tokens":1`
	for name, body := range map[string]string{
		"data missing":       `{"meta":{}}`,
		"meta missing":       `{"data":[]}`,
		"credits missing":    `{"data":[{"user_id":"u","date":"2026-01-15"}],"meta":{}}`,
		"user missing":       `{"data":[{"date":"2026-01-15","billable_tokens":1}],"meta":{}}`,
		"date format":        `{"data":[{"user_id":"u","date":"15/01/2026","billable_tokens":1}],"meta":{}}`,
		"quoted count":       `{"data":[` + row + `,"tool_calls":"3"}],"meta":{}}`,
		"negative count":     `{"data":[` + row + `,"sessions":-1}],"meta":{}}`,
		"repeated user day":  `{"data":[` + row + `},` + row + `}],"meta":{}}`,
		"has more no cursor": `{"data":[],"meta":{"has_more":true,"next_cursor":null}}`,
		"has more quoted":    `{"data":[],"meta":{"has_more":"true","next_cursor":"c"}}`,
	} {
		if _, err := parseUsers(resp([]byte(body))); !drifted(err) {
			t.Errorf("%s: err %v, want schema drift", name, err)
		}
	}
	r := resp([]byte(`{"data":[],"meta":{"has_more":true,"next_cursor":"c1"}}`))
	r.Cursor = "c1"
	if _, err := parseUsers(r); !drifted(err) {
		t.Error("a repeated cursor was accepted")
	}
}

// TestOrgUsersAndLimits: the member list never decodes metadata and pages
// by pagination.nextCursor; credit limits land global plus each override, a
// null limit absent (never zero).
func TestOrgUsersAndLimits(t *testing.T) {
	p, err := parseOrgUsers(resp(fixtureOf(t, SurfaceOrgUsers, "normal_page.json")))
	if err != nil {
		t.Fatal(err)
	}
	if err := vendorapi.Validate(stampGraph(p)); err != nil {
		t.Fatal(err)
	}
	if len(p.Resources) != 2 || p.NextCursor != "synthcursor0002" || p.Resources[0].DisplayName != "Ada Admin" ||
		p.Resources[0].Attrs["last_sign_in_at"] != "2026-10-01T09:30:00Z" || p.Resources[1].Attrs["email"] != "dev@example.com" {
		t.Fatalf("users %+v", p)
	}
	secret := `{"users":[{"id":"u","email":"e@x.test","createdAt":"2026-01-01T00:00:00Z","metadata":{"token":"sk-ant-api03-zzz"}}],` +
		`"pagination":{"hasMore":false,"nextCursor":null}}`
	p, err = parseOrgUsers(resp([]byte(secret)))
	if err != nil || len(p.Resources[0].Attrs) != 2 {
		t.Fatalf("metadata must never be decoded: %+v %v", p, err)
	}
	for name, body := range map[string]string{
		"users missing":   `{"pagination":{"hasMore":false,"nextCursor":null}}`,
		"hasMore missing": `{"users":[],"pagination":{"nextCursor":null}}`,
		"hasMore quoted":  `{"users":[],"pagination":{"hasMore":"false","nextCursor":null}}`,
		"more no cursor":  `{"users":[],"pagination":{"hasMore":true,"nextCursor":null}}`,
		"email missing":   `{"users":[{"id":"u","createdAt":"2026-01-01T00:00:00Z"}],"pagination":{"hasMore":false,"nextCursor":null}}`,
		"bad time":        `{"users":[{"id":"u","email":"e","createdAt":"yesterday"}],"pagination":{"hasMore":false,"nextCursor":null}}`,
	} {
		if _, err := parseOrgUsers(resp([]byte(body))); !drifted(err) {
			t.Errorf("org users %s: err %v, want schema drift", name, err)
		}
	}

	p, err = parseLimitsUsers(resp(fixtureOf(t, SurfaceLimitsUsers, "normal_page.json")))
	if err != nil {
		t.Fatal(err)
	}
	if err := vendorapi.Validate(stampGraph(p)); err != nil {
		t.Fatal(err)
	}
	if len(p.Resources) != 3 || p.SourceRecords != 2 || p.Resources[0].Attrs["limit"] != "5000000" ||
		p.Resources[1].ID.Value != "user:ada@example.com" || p.Resources[1].Attrs["limit"] != "20000000" {
		t.Fatalf("limits %+v", p.Resources)
	}
	if a := p.Resources[2].Attrs; a["state"] != "inherits_global" || a["limit"] != "" {
		t.Errorf("a null override must be absent, never zero: %+v", a)
	}
	for name, body := range map[string]string{
		"global missing": `{"userLimits":[]}`,
		"users missing":  `{"globalLimit":null}`,
		"quoted":         `{"globalLimit":"5","userLimits":[]}`,
		"negative":       `{"globalLimit":null,"userLimits":[{"email":"e","limit":-1}]}`,
		"email missing":  `{"globalLimit":null,"userLimits":[{"limit":1}]}`,
		"repeated":       `{"globalLimit":null,"userLimits":[{"email":"e","limit":1},{"email":"e","limit":2}]}`,
	} {
		if _, err := parseLimitsUsers(resp([]byte(body))); !drifted(err) {
			t.Errorf("limits %s: err %v, want schema drift", name, err)
		}
	}
	if p, err := parseLimitsUsers(resp([]byte(`{"globalLimit":null,"userLimits":[]}`))); err != nil ||
		p.Resources[0].Attrs["state"] != "unset" {
		t.Errorf("unset global %+v %v", p, err)
	}
}
