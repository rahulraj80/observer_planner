package cognition

import (
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/vendorapi"
	vs "github.com/marmutapp/superbased-observer/internal/vendorsurface"
)

// TestInventoryParsesCorpusFixtures (lane ENTAPI-L16B, T-708): each of the
// five enterprise inventory lists parses its corpus pages into resources of
// its one type, pages by end_cursor and validates.
func TestInventoryParsesCorpusFixtures(t *testing.T) {
	for _, c := range []struct {
		surface    vs.SurfaceID
		parse      func(vendorapi.Response) (vendorapi.Page, error)
		typ        vs.ResourceType
		first, all int
	}{
		{SurfaceOrganizations, parseOrganizations, OrganizationType, 2, 3},
		{SurfaceRoles, parseRoles, RoleType, 2, 3},
		{SurfaceServiceUsers, parseServiceUsers, ServiceUserType, 2, 3},
		{SurfaceIdPGroups, parseIdPGroups, IdPGroupType, 2, 3},
		{SurfaceACULimits, parseACULimits, ACULimitType, 2, 3},
	} {
		p1, err := c.parse(resp(fixture(t, c.surface, "normal_page.json")))
		if err != nil {
			t.Fatalf("%s: %v", c.surface, err)
		}
		if err := vendorapi.Validate(stamp(p1)); err != nil {
			t.Fatalf("%s: page does not validate: %v", c.surface, err)
		}
		if len(p1.Resources) != c.first || p1.NextCursor == "" {
			t.Fatalf("%s: %d resources next %q", c.surface, len(p1.Resources), p1.NextCursor)
		}
		r := resp(fixture(t, c.surface, "last_page.json"))
		r.Cursor = p1.NextCursor
		p2, err := c.parse(r)
		if err != nil || p2.NextCursor != "" || len(p1.Resources)+len(p2.Resources) != c.all {
			t.Fatalf("%s: last page %+v %v", c.surface, p2, err)
		}
		for _, res := range append(p1.Resources, p2.Resources...) {
			if res.Type != c.typ {
				t.Errorf("%s: resource type %s", c.surface, res.Type)
			}
		}
	}
}

// TestInventoryAttributes: limits and times are flat values; a null limit is
// absent (never zero); ACU limits are keyed by scope and owner.
func TestInventoryAttributes(t *testing.T) {
	p, err := parseOrganizations(resp(fixture(t, SurfaceOrganizations, "normal_page.json")))
	if err != nil {
		t.Fatal(err)
	}
	o := p.Resources[0]
	if o.ContainerKind != vs.ContainerWorkspace || o.Attrs["max_cycle_acu_limit"] != "2000" ||
		o.Attrs["created_at"] != "2026-01-01T00:00:00Z" || o.DisplayName != "Platform" {
		t.Errorf("organization %+v", o)
	}
	if _, ok := p.Resources[1].Attrs["max_cycle_acu_limit"]; ok {
		t.Error("a null limit landed")
	}
	p, err = parseServiceUsers(resp(fixture(t, SurfaceServiceUsers, "normal_page.json")))
	if err != nil {
		t.Fatal(err)
	}
	if su := p.Resources[0]; su.Attrs["enterprise_roles"] != "Enterprise Viewer" || su.Attrs["org_role_count"] != "0" ||
		su.Attrs["expires_at"] == "" || su.Kind() != vs.KindServiceAccount {
		t.Errorf("service user %+v", su)
	}
	p, err = parseACULimits(resp(fixture(t, SurfaceACULimits, "normal_page.json")))
	if err != nil {
		t.Fatal(err)
	}
	if p.Resources[0].ID.Value != "enterprise" || p.Resources[1].ID.Value != "org:org_synth01" ||
		p.Resources[1].Attrs["cycle_acu_limit"] != "1000" || p.Resources[1].Attrs["unit"] != UnitACU {
		t.Errorf("limits %+v", p.Resources)
	}
	user := `{"items":[{"cycle_acu_limit":10,"scope":"user","org_id":null,"user_id":"user_1"}]}`
	if p, err := parseACULimits(resp([]byte(user))); err != nil || p.Resources[0].ID.Value != "user::user_1" {
		t.Errorf("user limit %+v %v", p, err)
	}
}

// TestInventoryFieldPolicy (plan 12A.5): every row is schema drift.
func TestInventoryFieldPolicy(t *testing.T) {
	page := func(item string) []byte { return []byte(`{"items":[` + item + `]}`) }
	org := `"org_id":"o","name":"n","created_at":1,"updated_at":1,"max_session_acu_limit":null,"max_cycle_acu_limit":null`
	for name, c := range map[string]struct {
		parse func(vendorapi.Response) (vendorapi.Page, error)
		body  []byte
	}{
		"items missing":          {parseRoles, []byte(`{"has_next_page":false}`)},
		"org id missing":         {parseOrganizations, page(`{"name":"n","created_at":1,"updated_at":1}`)},
		"org quoted limit":       {parseOrganizations, page(`{` + org[:len(org)-len(`null`)] + `"5"}`)},
		"org negative limit":     {parseOrganizations, page(`{` + org[:len(org)-len(`null`)] + `-5}`)},
		"org fractional time":    {parseOrganizations, page(`{"org_id":"o","name":"n","created_at":1.5,"updated_at":1}`)},
		"org repeated":           {parseOrganizations, page(`{` + org + `},{` + org + `}`)},
		"role type undocumented": {parseRoles, page(`{"role_id":"r","role_name":"n","role_type":"global"}`)},
		"role name missing":      {parseRoles, page(`{"role_id":"r","role_type":"org"}`)},
		"service user roles":     {parseServiceUsers, page(`{"service_user_id":"s","name":"n","expires_at":null,"deleted_at":null}`)},
		"service user role type": {parseServiceUsers, page(`{"service_user_id":"s","name":"n","role_assignments":[{"role":{"role_id":"r","role_name":"n","role_type":"x"}}]}`)},
		"group name missing":     {parseIdPGroups, page(`{}`)},
		"limit scope":            {parseACULimits, page(`{"cycle_acu_limit":1,"scope":"team"}`)},
		"limit org scope no org": {parseACULimits, page(`{"cycle_acu_limit":1,"scope":"org","org_id":null}`)},
		"limit quoted":           {parseACULimits, page(`{"cycle_acu_limit":"1","scope":"enterprise"}`)},
		"has next no cursor":     {parseIdPGroups, []byte(`{"items":[],"has_next_page":true,"end_cursor":null}`)},
		"has next quoted":        {parseIdPGroups, []byte(`{"items":[],"has_next_page":"true","end_cursor":"c"}`)},
	} {
		if _, err := c.parse(resp(c.body)); !isDrift(err) {
			t.Errorf("%s: err %v, want schema drift", name, err)
		}
	}
	r := resp([]byte(`{"items":[]}`))
	r.Status = 204
	if _, err := parseRoles(r); err == nil {
		t.Error("a 204 was accepted")
	}
	if _, err := parseRoles(resp(page(`{"role_id":"r","role_name":"n","role_type":"org","permissions":["x"]}`))); err != nil {
		t.Errorf("unknown members must be ignored: %v", err)
	}
}

// TestInventoryBuild: the documented page size and a validated cursor.
func TestInventoryBuild(t *testing.T) {
	for _, d := range []vendorapi.Driver{Organizations(), Roles(), ServiceUsers(), IdPGroups(), ACULimits()} {
		req, err := d.Build(nil, vendorapi.Window{}, "")
		if err != nil || req.Method != "GET" || req.Query["first"][0] != "200" || req.Query["after"] != nil {
			t.Errorf("%s: %+v %v", d.Surface, req, err)
		}
		if req, err := d.Build(nil, vendorapi.Window{}, "abc123"); err != nil || req.Query["after"][0] != "abc123" {
			t.Errorf("%s: cursor %+v %v", d.Surface, req, err)
		}
		if _, err := d.Build(nil, vendorapi.Window{}, "a b"); err == nil {
			t.Errorf("%s: a cursor with a space was accepted", d.Surface)
		}
		if !d.AuthoritativeSnapshot || d.Auth.Scheme != vendorapi.SchemeBearer || d.Tool != "" {
			t.Errorf("%s: driver %+v", d.Surface, d)
		}
	}
	if req, _ := ServiceUsers().Build(nil, vendorapi.Window{}, ""); req.Query["include_inactive"] != nil {
		t.Error("include_inactive must stay at its documented default")
	}
}

// TestMetricsActive: one distinct-user fact per UTC day, never summed; the
// request re-reads the day before the window.
func TestMetricsActive(t *testing.T) {
	p, err := parseMetricsActive(resp(fixture(t, SurfaceMetricsActive, "normal_page.json")))
	if err != nil {
		t.Fatal(err)
	}
	if err := vendorapi.Validate(stamp(p)); err != nil {
		t.Fatal(err)
	}
	if len(p.Facts) != 2 || p.Facts[0].Metric != MetricDailyActiveUsers || p.Facts[0].Unit != UnitUsers ||
		p.Facts[0].ValueMicros != 14_000_000 || !p.Facts[1].IntervalStart.Equal(time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("facts %+v", p.Facts)
	}
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	req, err := buildMetricsActive(nil, vendorapi.Window{From: now.Add(-time.Hour), To: now}, "")
	if err != nil || req.Path != "/v3/enterprise/metrics/dau" || req.Query["time_after"][0] != "1791331200" {
		t.Fatalf("request %+v %v", req, err)
	}
	for _, w := range []vendorapi.Window{{}, {From: now.Add(-40 * 24 * time.Hour), To: now}} {
		if _, err := buildMetricsActive(nil, w, ""); err == nil {
			t.Errorf("window %v accepted", w)
		}
	}
	for name, body := range map[string]string{
		"object":        `{"items":[]}`,
		"null":          `null`,
		"over a day":    `[{"start_time":0,"end_time":90000,"active_users":1}]`,
		"empty period":  `[{"start_time":5,"end_time":5,"active_users":1}]`,
		"overlap":       `[{"start_time":0,"end_time":86400,"active_users":1},{"start_time":3600,"end_time":90000,"active_users":1}]`,
		"count missing": `[{"start_time":0,"end_time":86400}]`,
		"quoted count":  `[{"start_time":0,"end_time":86400,"active_users":"3"}]`,
		"negative":      `[{"start_time":0,"end_time":86400,"active_users":-3}]`,
	} {
		if _, err := parseMetricsActive(resp([]byte(body))); !isDrift(err) {
			t.Errorf("%s: err %v, want schema drift", name, err)
		}
	}
	if p, err := parseMetricsActive(resp([]byte(`[]`))); err != nil || len(p.Facts) != 0 {
		t.Errorf("empty list %+v %v", p, err)
	}
}
