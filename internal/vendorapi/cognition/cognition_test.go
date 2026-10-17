package cognition

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/vendorapi"
	vs "github.com/marmutapp/superbased-observer/internal/vendorsurface"
)

func fixture(t *testing.T, surface vs.SurfaceID, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "vendorapi", "cognition", string(surface), name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func resp(body []byte) vendorapi.Response {
	return vendorapi.Response{Status: 200, Origin: "https://api.devin.ai:443", Body: body, Params: vendorapi.Params{}}
}

func stamp(p vendorapi.Page) vendorapi.Page {
	if p.Snapshot == nil {
		p.Snapshot = &vendorapi.SnapshotManifest{}
	}
	p.Snapshot.ManifestID = "run-1"
	p.Snapshot.ObservedAt = time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	return p
}

func isDrift(err error) bool { return err != nil && errors.Is(err, vendorapi.ErrSchemaDrift) }

// TestConsumptionParsesCorpusFixtures: one total fact per billing day plus
// one fact per reported product; a null product (review, automation) is
// absent, never zero; the day runs from 08:00 UTC.
func TestConsumptionParsesCorpusFixtures(t *testing.T) {
	p, err := parseConsumption(resp(fixture(t, SurfaceConsumptionDaily, "normal_page.json")))
	if err != nil {
		t.Fatal(err)
	}
	if err := vendorapi.Validate(stamp(p)); err != nil {
		t.Fatalf("parsed page does not validate: %v", err)
	}
	// Day 1: total + devin, cascade, terminal, review (automation null) = 5.
	// Day 2: total + devin, cascade, terminal, automation (review null) = 5.
	if len(p.Facts) != 10 || p.SourceRecords != 2 || p.NextCursor != "" {
		t.Fatalf("facts %d records %d next %q", len(p.Facts), p.SourceRecords, p.NextCursor)
	}
	day1 := time.Date(2026, 10, 5, 8, 0, 0, 0, time.UTC)
	got := map[string]int64{}
	for _, f := range p.Facts {
		if f.Unit != UnitACU || f.CoverageDomain != ConsumptionDomain || f.Identity != "" {
			t.Fatalf("fact %+v", f)
		}
		if f.IntervalStart.Equal(day1) {
			if !f.IntervalEnd.Equal(day1.Add(24 * time.Hour)) {
				t.Fatalf("interval %v", f)
			}
			got[f.Metric+"/"+f.Feature] = f.ValueMicros
		}
	}
	want := map[string]int64{
		"acus_total/": 20_250_000, "acus/devin": 12_000_000, "acus/cascade": 5_250_000, "acus/terminal": 1_000_000,
		"acus/review": 2_000_000,
	}
	if len(got) != len(want) {
		t.Fatalf("day 1 facts %v", got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %d, want %d", k, got[k], v)
		}
	}
	empty, err := parseConsumption(resp(fixture(t, SurfaceConsumptionDaily, "empty_page.json")))
	if err != nil || len(empty.Facts) != 0 {
		t.Fatalf("empty page %+v %v", empty, err)
	}
}

func TestConsumptionBuildWindow(t *testing.T) {
	from := time.Date(2026, 10, 6, 3, 0, 0, 0, time.UTC) // before 08:00: billing day of 10-05
	req, err := buildConsumption(nil, vendorapi.Window{From: from, To: from.Add(6 * time.Hour)}, "")
	if err != nil {
		t.Fatal(err)
	}
	// One settle day back from the 10-05 billing day: 10-04 08:00 UTC.
	if req.Method != "GET" || req.Path != "/v3/enterprise/consumption/daily" ||
		req.Query["time_after"][0] != "1791100800" || req.Query["time_before"][0] != "1791277200" {
		t.Fatalf("request %+v", req)
	}
	for _, c := range []struct {
		w      vendorapi.Window
		cursor string
	}{
		{vendorapi.Window{}, ""},
		{vendorapi.Window{From: from, To: from.Add(32 * 24 * time.Hour)}, ""},
		{vendorapi.Window{From: from, To: from.Add(time.Hour)}, "x"},
	} {
		if _, err := buildConsumption(nil, c.w, c.cursor); err == nil {
			t.Errorf("build accepted window %v cursor %q", c.w, c.cursor)
		}
	}
	if d := ConsumptionDaily(); d.Tool != "devin" || d.Auth.Scheme != vendorapi.SchemeBearer || d.MaxWindow != ConsumptionMaxWindow {
		t.Errorf("driver %+v", d)
	}
}

func TestUsersParseAndPage(t *testing.T) {
	p, err := parseUsers(resp(fixture(t, SurfaceEnterpriseUsers, "normal_page.json")))
	if err != nil {
		t.Fatal(err)
	}
	if err := vendorapi.Validate(stamp(p)); err != nil {
		t.Fatalf("parsed page does not validate: %v", err)
	}
	if len(p.Resources) != 2 || p.NextCursor != "synthcursor0002" {
		t.Fatalf("resources %d next %q", len(p.Resources), p.NextCursor)
	}
	a := p.Resources[0]
	if a.Type != UserType || a.ID.Namespace != NamespaceUser || a.ID.Value != "user_synth0001" || a.DisplayName != "Ada Admin" ||
		a.Attrs["email"] != "admin@example.com" || a.Attrs["enterprise_roles"] != "Enterprise Admin" ||
		a.Attrs["org_role_count"] != "0" || a.Attrs["enterprise_joined_at"] != "2026-01-01T00:00:00Z" {
		t.Fatalf("user 0 = %+v", a)
	}
	if b := p.Resources[1]; b.Attrs["org_role_count"] != "1" || b.Attrs["enterprise_roles"] != "" || b.Attrs["enterprise_joined_at"] != "" {
		t.Fatalf("user 1 = %+v", b)
	}
	last, err := parseUsers(resp(fixture(t, SurfaceEnterpriseUsers, "last_page.json")))
	if err != nil || last.NextCursor != "" || len(last.Resources) != 1 {
		t.Fatalf("last page: %+v %v", last, err)
	}
	req, err := buildUsers(nil, vendorapi.Window{}, "synthcursor0002")
	if err != nil || req.Query["after"][0] != "synthcursor0002" || req.Query["first"][0] != "200" || req.Query["email"] != nil {
		t.Fatalf("page two request %+v %v", req, err)
	}
	if _, err := buildUsers(nil, vendorapi.Window{}, "bad cursor!"); err == nil {
		t.Error("a malformed cursor was sent back")
	}
	if d := EnterpriseUsers(); !d.AuthoritativeSnapshot || d.Tool != "" {
		t.Errorf("driver %+v", d)
	}
}

func TestAuditParseClosedDTO(t *testing.T) {
	p, err := parseAudit(resp(fixture(t, SurfaceAuditLogs, "normal_page.json")))
	if err != nil {
		t.Fatal(err)
	}
	if err := vendorapi.Validate(stamp(p)); err != nil {
		t.Fatalf("parsed page does not validate: %v", err)
	}
	if len(p.Audit) != 2 || p.NextCursor != "synthaudit0002" {
		t.Fatalf("events %d next %q", len(p.Audit), p.NextCursor)
	}
	e0, e1 := p.Audit[0], p.Audit[1]
	if e0.Action != "login" || e0.ActorKind != "email" || e0.ActorIdentity != "admin@example.com" || e0.ActorIsAgent ||
		!e0.At.Equal(time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)) {
		t.Fatalf("event 0 = %+v", e0)
	}
	if e1.Action != "create_service_api_key" || e1.ActorKind != ActorServiceUser || !e1.ActorIsAgent ||
		e1.Detail["service_user_name"] != "ci-bot" || e1.Detail["org_id"] != "org_synth01" {
		t.Fatalf("event 1 = %+v", e1)
	}
	if len(canaryAudit(p)) != 0 {
		t.Error("canary on documented actions")
	}
	odd, err := parseAudit(resp([]byte(`{"items":[{"audit_log_id":"a","action":"Brand New","created_at":1791190800,
		"org_id":null,"user_id":null,"user_email":null,"service_user_id":null,"service_user_name":null,
		"data":{"api_key_value":"never-read","nested":{"x":1}}}],"has_next_page":false,"end_cursor":null}`)))
	if err != nil {
		t.Fatal(err)
	}
	if odd.Audit[0].Action != "other" || len(canaryAudit(odd)) != 1 || len(odd.Audit[0].Detail) != 0 {
		t.Errorf("unknown action or data leak: %+v canary %v", odd.Audit[0], canaryAudit(odd))
	}
	from := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	req, err := buildAudit(nil, vendorapi.Window{From: from, To: from.Add(time.Hour)}, "")
	if err != nil || req.Query["order"][0] != "asc" || req.Query["time_after"][0] != "1791158400" || req.Query["after"] != nil {
		t.Fatalf("request %+v %v", req, err)
	}
	if _, err := buildAudit(nil, vendorapi.Window{From: from, To: from.Add(40 * 24 * time.Hour)}, ""); err == nil {
		t.Error("an over-long window was accepted")
	}
	if d := AuditLogs(); !d.ClosedDTO || d.Tool != "devin" {
		t.Errorf("driver %+v", d)
	}
}

// TestFieldPolicyMatrix (plan 12A.5): a missing required field, a wrong JSON
// type, a repeated key, a bad timestamp, a negative amount, a non-200 status
// and a paging contract break fail the page as schema drift.
func TestFieldPolicyMatrix(t *testing.T) {
	cons := func(b string) error { _, err := parseConsumption(resp([]byte(b))); return err }
	users := func(b string) error { _, err := parseUsers(resp([]byte(b))); return err }
	audit := func(b string) error { _, err := parseAudit(resp([]byte(b))); return err }
	day := `{"date":1791187200,"acus":1.5,"acus_by_product":{"devin":1.5,"cascade":0,"terminal":0}}`
	cases := []struct {
		name string
		err  error
	}{
		{"consumption list missing", cons(`{"total_acus":1}`)},
		{"consumption total missing", cons(`{"consumption_by_date":[]}`)},
		{"consumption date string", cons(`{"total_acus":1,"consumption_by_date":[{"date":"2026-10-05","acus":1,"acus_by_product":{"devin":1,"cascade":0,"terminal":0}}]}`)},
		{"consumption negative", cons(`{"total_acus":1,"consumption_by_date":[{"date":1791187200,"acus":-1,"acus_by_product":{"devin":1,"cascade":0,"terminal":0}}]}`)},
		{"consumption product missing", cons(`{"total_acus":1,"consumption_by_date":[{"date":1791187200,"acus":1,"acus_by_product":{"devin":1,"cascade":0}}]}`)},
		{"consumption breakdown missing", cons(`{"total_acus":1,"consumption_by_date":[{"date":1791187200,"acus":1}]}`)},
		{"consumption repeated day", cons(`{"total_acus":3,"consumption_by_date":[` + day + `,` + day + `]}`)},
		{"consumption quoted amount", cons(`{"total_acus":"1","consumption_by_date":[]}`)},
		{"consumption exponent", cons(`{"total_acus":1e3,"consumption_by_date":[]}`)},
		{"consumption status", func() error {
			r := resp([]byte(`{"total_acus":0,"consumption_by_date":[]}`))
			r.Status = 202
			_, err := parseConsumption(r)
			return err
		}()},
		{"users items missing", users(`{"has_next_page":false}`)},
		{"users id missing", users(`{"items":[{"email":"a@b","name":"x","role_assignments":[]}]}`)},
		{"users repeated", users(`{"items":[{"user_id":"u","role_assignments":[]},{"user_id":"u","role_assignments":[]}]}`)},
		{"users roles missing", users(`{"items":[{"user_id":"u"}]}`)},
		{"users role type", users(`{"items":[{"user_id":"u","role_assignments":[{"role":{"role_id":"r","role_name":"n","role_type":"team"}}]}]}`)},
		{"users flag type", users(`{"items":[],"has_next_page":"yes","end_cursor":"c"}`)},
		{"users next without cursor", users(`{"items":[],"has_next_page":true,"end_cursor":null}`)},
		{"users joined at string", users(`{"items":[{"user_id":"u","role_assignments":[],"enterprise_joined_at":"2026"}]}`)},
		{"audit id missing", audit(`{"items":[{"action":"login","created_at":1}]}`)},
		{"audit created_at missing", audit(`{"items":[{"audit_log_id":"a","action":"login"}]}`)},
		{"audit action missing", audit(`{"items":[{"audit_log_id":"a","created_at":1}]}`)},
		{"audit repeated", audit(`{"items":[{"audit_log_id":"a","action":"login","created_at":1},{"audit_log_id":"a","action":"login","created_at":2}]}`)},
	}
	for _, c := range cases {
		if !isDrift(c.err) {
			t.Errorf("%s: err %v, want schema drift", c.name, c.err)
		}
	}
	loop := resp([]byte(`{"items":[],"has_next_page":true,"end_cursor":"c1"}`))
	loop.Cursor = "c1"
	if _, err := parseUsers(loop); !isDrift(err) {
		t.Errorf("a page answering its own cursor as end_cursor: %v", err)
	}
	if _, err := parseUsers(resp([]byte(`{"items":[],"unknown":1}`))); err != nil {
		t.Errorf("unknown members must be ignored: %v", err)
	}
}
