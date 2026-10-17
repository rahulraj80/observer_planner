package cursor

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
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "vendorapi", "cursor", string(surface), name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func resp(body []byte) vendorapi.Response {
	return vendorapi.Response{Status: 200, Origin: "https://api.cursor.com:443", Body: body, Params: vendorapi.Params{}}
}

func stamp(p vendorapi.Page) vendorapi.Page {
	if p.Snapshot == nil {
		p.Snapshot = &vendorapi.SnapshotManifest{}
	}
	p.Snapshot.ManifestID = "run-1"
	p.Snapshot.ObservedAt = time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	return p
}

func TestMembersParsesCorpusFixtures(t *testing.T) {
	p, err := parseMembers(resp(fixture(t, SurfaceMembers, "normal_page.json")))
	if err != nil {
		t.Fatal(err)
	}
	if err := vendorapi.Validate(stamp(p)); err != nil {
		t.Fatalf("parsed page does not validate: %v", err)
	}
	if len(p.Resources) != 2 || p.NextCursor != "" {
		t.Fatalf("resources %d next %q", len(p.Resources), p.NextCursor)
	}
	r := p.Resources[0]
	if r.Type != MemberType || r.ID.Namespace != NamespaceUser || r.ID.Value != "user_PDSPmvukpYgZEDXsoNirw3CFhy" ||
		r.DisplayName != "Alex" || r.Attrs["role"] != "member" || r.Attrs["is_removed"] != "false" || r.Attrs["email"] != "developer@company.com" {
		t.Fatalf("member 0 = %+v", r)
	}
	empty, err := parseMembers(resp(fixture(t, SurfaceMembers, "empty_page.json")))
	if err != nil || len(empty.Resources) != 0 || empty.Snapshot == nil {
		t.Fatalf("empty page: %+v %v", empty, err)
	}
	if _, err := buildMembers(nil, vendorapi.Window{}, "2"); err == nil {
		t.Error("the members surface is not paged")
	}
}

func TestAuditParsesCorpusFixtures(t *testing.T) {
	p, err := parseAudit(resp(fixture(t, SurfaceAuditLogs, "normal_page.json")))
	if err != nil {
		t.Fatal(err)
	}
	if err := vendorapi.Validate(stamp(p)); err != nil {
		t.Fatalf("parsed page does not validate: %v", err)
	}
	if len(p.Audit) != 2 || p.NextCursor != "2" {
		t.Fatalf("events %d next %q", len(p.Audit), p.NextCursor)
	}
	e := p.Audit[1]
	if e.Namespace != AuditNamespace || e.EventID != "8a1f0f0e-0d1b-4c7e-9b3a-2f6e1c9d4a55" || e.Action != "add_user" ||
		e.ActorKind != "email" || e.ActorIdentity != "admin@company.com" || e.Target != "developer@company.com" ||
		e.Detail["invite_id"] != "3f9a1c2b" || e.Detail["ip_address"] != "203.0.113.42" {
		t.Fatalf("event 1 = %+v", e)
	}
	if !e.At.Equal(time.Date(2024, 1, 15, 12, 30, 0, 0, time.UTC)) {
		t.Errorf("time %s", e.At)
	}
	last, err := parseAudit(resp(fixture(t, SurfaceAuditLogs, "last_page.json")))
	if err != nil || last.NextCursor != "" {
		t.Fatalf("last page: next %q err %v", last.NextCursor, err)
	}
}

// TestFieldPolicyMatrix (cursor drivers, plan 12A.5): missing required
// fields, a wrong JSON type and a non-200 status fail the page as schema
// drift; unknown members are ignored; an unknown event type lands as other
// with a canary signal; unknown event_data members are never decoded.
func TestFieldPolicyMatrix(t *testing.T) {
	drift := []struct {
		name string
		f    func() error
	}{
		{"members container missing", func() error { _, err := parseMembers(resp([]byte(`{"members":[]}`))); return err }},
		{"member id missing", func() error {
			_, err := parseMembers(resp([]byte(`{"teamMembers":[{"name":"x"}]}`)))
			return err
		}},
		{"member repeated", func() error {
			_, err := parseMembers(resp([]byte(`{"teamMembers":[{"id":"u1"},{"id":"u1"}]}`)))
			return err
		}},
		{"members wrong type", func() error {
			_, err := parseMembers(resp([]byte(`{"teamMembers":[{"id":"u1","isRemoved":"no"}]}`)))
			return err
		}},
		{"members status", func() error {
			r := resp([]byte(`{"teamMembers":[]}`))
			r.Status = 202
			_, err := parseMembers(r)
			return err
		}},
		{"audit pagination missing", func() error { _, err := parseAudit(resp([]byte(`{"events":[]}`))); return err }},
		{"audit flag type", func() error {
			_, err := parseAudit(resp([]byte(`{"events":[],"pagination":{"page":1,"hasNextPage":"yes"}}`)))
			return err
		}},
		{"audit event id missing", func() error {
			_, err := parseAudit(resp([]byte(`{"events":[{"timestamp":"2024-01-15T10:15:00Z","event_type":"login"}],"pagination":{"page":1,"hasNextPage":false}}`)))
			return err
		}},
		{"audit timestamp", func() error {
			_, err := parseAudit(resp([]byte(`{"events":[{"event_id":"e","timestamp":"yesterday","event_type":"login"}],"pagination":{"page":1,"hasNextPage":false}}`)))
			return err
		}},
		{"audit page zero", func() error {
			_, err := parseAudit(resp([]byte(`{"events":[],"pagination":{"page":0,"hasNextPage":false}}`)))
			return err
		}},
	}
	for _, c := range drift {
		if err := c.f(); err == nil || !errors.Is(err, vendorapi.ErrSchemaDrift) {
			t.Errorf("%s: err %v, want schema drift", c.name, err)
		}
	}
	p, err := parseAudit(resp([]byte(`{"events":[{"event_id":"e","timestamp":"2024-01-15T10:15:00Z","event_type":"Weird Thing!",
		"extra":"ignored","event_data":{"role":"admin","api_key_value":"never-read","nested":{"a":1}}}],
		"pagination":{"page":1,"hasNextPage":false},"newTopLevel":true}`)))
	if err != nil {
		t.Fatal(err)
	}
	if p.Audit[0].Action != "other" || len(canaryAudit(p)) != 1 {
		t.Errorf("unknown event type: action %q canary %v", p.Audit[0].Action, canaryAudit(p))
	}
	if p.Audit[0].Detail["role"] != "admin" || len(p.Audit[0].Detail) != 1 {
		t.Errorf("closed DTO read %v", p.Audit[0].Detail)
	}
	if len(canaryAudit(vendorapi.Page{})) != 0 {
		t.Error("no canary on a clean page")
	}
}

func TestAuditBuildWindowAndPaging(t *testing.T) {
	from := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	req, err := buildAudit(nil, vendorapi.Window{From: from, To: from.Add(24 * time.Hour)}, "")
	if err != nil {
		t.Fatal(err)
	}
	if req.Method != "GET" || req.Path != "/teams/audit-logs" || req.Query["startTime"][0] != "1790812800000" ||
		req.Query["pageSize"][0] != "500" || req.Query["page"] != nil {
		t.Fatalf("request %+v", req)
	}
	req, err = buildAudit(nil, vendorapi.Window{From: from, To: from.Add(time.Hour)}, "3")
	if err != nil || req.Query["page"][0] != "3" {
		t.Fatalf("page 3: %+v %v", req, err)
	}
	for _, c := range []struct {
		w      vendorapi.Window
		cursor string
	}{
		{vendorapi.Window{}, ""},
		{vendorapi.Window{From: from, To: from.Add(31 * 24 * time.Hour)}, ""},
		{vendorapi.Window{From: from, To: from.Add(time.Hour)}, "1"},
		{vendorapi.Window{From: from, To: from.Add(time.Hour)}, "x"},
	} {
		if _, err := buildAudit(nil, c.w, c.cursor); err == nil {
			t.Errorf("build accepted window %v cursor %q", c.w, c.cursor)
		}
	}
	if d := AuditLogs(); d.MaxWindow != AuditMaxWindow || !d.ClosedDTO || d.Auth.Scheme != vendorapi.SchemeBasicUser {
		t.Errorf("driver %+v", d)
	}
	if !strings.HasPrefix(string(Members().Surface), "cursor.admin.") {
		t.Error("surface id")
	}
}
