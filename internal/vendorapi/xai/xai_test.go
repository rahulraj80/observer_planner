package xai

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/vendorapi"
	vs "github.com/marmutapp/superbased-observer/internal/vendorsurface"
)

const teamID = "65c1e471-205f-4566-9c5a-07198bcdf4ce"

func fixture(t *testing.T, s vs.SurfaceID, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "vendorapi", "xai", string(s), name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func resp(body []byte) vendorapi.Response {
	return vendorapi.Response{
		Status: 200, Origin: "https://management-api.x.ai:443", Body: body,
		Params: vendorapi.Params{ParamTeam: teamID},
	}
}

func drifted(err error) bool { return err != nil && errors.Is(err, vendorapi.ErrSchemaDrift) }

// TestAuditEvents (lane ENTAPI-L16B, T-708): the documented example lands
// one event under the fixed team_event action with the email as actor and the
// description as a detail; an empty email keeps the user id; the token pages.
func TestAuditEvents(t *testing.T) {
	p, err := parseAudit(resp(fixture(t, SurfaceAuditEvents, "normal_page.json")))
	if err != nil {
		t.Fatal(err)
	}
	if err := vendorapi.Validate(p); err != nil {
		t.Fatal(err)
	}
	e := p.Audit[0]
	if len(p.Audit) != 1 || p.NextCursor != "550e8400-e29b-41d4-a716-446655440000" || e.Action != AuditAction ||
		e.ActorKind != "email" || e.ActorIdentity != "admin@example.com" ||
		e.Detail["description"] != "API key 'Production Key' was created" || e.Detail["user_id"] != "user-123" ||
		!e.At.Equal(time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC)) {
		t.Fatalf("page %+v", p)
	}
	for k := range e.Detail {
		if k != "description" && k != "user_id" {
			t.Errorf("detail %q landed (names and profile images are never read)", k)
		}
	}
	r := resp(fixture(t, SurfaceAuditEvents, "last_page.json"))
	r.Cursor = p.NextCursor
	p2, err := parseAudit(r)
	if err != nil || p2.NextCursor != "" || p2.Audit[0].ActorKind != ActorUser || p2.Audit[0].ActorIdentity != "user-456" {
		t.Fatalf("last page %+v %v", p2, err)
	}
	for name, body := range map[string]string{
		"events missing": `{"nextPageToken":"x"}`,
		"id missing":     `{"events":[{"eventTime":"2025-01-15T10:30:00Z"}]}`,
		"bad time":       `{"events":[{"eventId":"a","eventTime":"15/01/2025"}]}`,
		"repeated":       `{"events":[{"eventId":"a","eventTime":"2025-01-15T10:30:00Z"},{"eventId":"a","eventTime":"2025-01-15T10:30:00Z"}]}`,
		"token type":     `{"events":[],"nextPageToken":7}`,
		"token space":    `{"events":[],"nextPageToken":"a b"}`,
	} {
		if _, err := parseAudit(resp([]byte(body))); !drifted(err) {
			t.Errorf("%s: err %v, want schema drift", name, err)
		}
	}
	r = resp([]byte(`{"events":[],"nextPageToken":"t1"}`))
	r.Cursor = "t1"
	if _, err := parseAudit(r); !drifted(err) {
		t.Error("a repeated token was accepted")
	}
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	req, err := buildAudit(vendorapi.Params{ParamTeam: teamID}, vendorapi.Window{From: now.Add(-time.Hour), To: now}, "tok")
	if err != nil || req.Path != "/audit/teams/"+teamID+"/events" || req.Query["pageToken"][0] != "tok" ||
		req.Query["orderBy"][0] != "TIME_ASCENDING" || req.Query["eventTimeFrom"][0] != "2026-10-08T11:00:00Z" {
		t.Fatalf("request %+v %v", req, err)
	}
	for _, p := range []vendorapi.Params{{}, {ParamTeam: "../x"}, {ParamTeam: "a/b"}} {
		if _, err := buildAudit(p, vendorapi.Window{From: now.Add(-time.Hour), To: now}, ""); err == nil {
			t.Errorf("team %q accepted", p[ParamTeam])
		}
	}
}

// TestSpendingLimits: the documented example lands one team limit in USD
// cents; an absent member is absent, never zero.
func TestSpendingLimits(t *testing.T) {
	p, err := parseLimits(resp(fixture(t, SurfaceSpendingLimits, "normal_page.json")))
	if err != nil {
		t.Fatal(err)
	}
	p.Snapshot.ManifestID, p.Snapshot.ObservedAt = "run-1", time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	if err := vendorapi.Validate(p); err != nil {
		t.Fatal(err)
	}
	r := p.Resources[0]
	if r.ID.Value != teamID || r.Attrs["soft_cents"] != "20000" || r.Attrs["effective_hard_cents"] != "22500" ||
		r.Attrs["unit"] != UnitUSDCents {
		t.Fatalf("limit %+v", r)
	}
	if _, ok := r.Attrs["hard_override_cents"]; ok {
		t.Error("an absent override landed")
	}
	for name, body := range map[string]string{
		"missing":  `{}`,
		"not int":  `{"spendingLimits":{"softSl":{"val":"20.5"}}}`,
		"negative": `{"spendingLimits":{"softSl":{"val":"-1"}}}`,
		"number":   `{"spendingLimits":{"softSl":{"val":20000}}}`,
		"no val":   `{"spendingLimits":{"softSl":{}}}`,
	} {
		if _, err := parseLimits(resp([]byte(body))); !drifted(err) {
			t.Errorf("%s: err %v, want schema drift", name, err)
		}
	}
	req, err := buildLimits(vendorapi.Params{ParamTeam: teamID}, vendorapi.Window{}, "")
	if err != nil || req.Path != "/v1/billing/teams/"+teamID+"/postpaid/spending-limits" {
		t.Fatalf("request %+v %v", req, err)
	}
	if _, err := buildLimits(vendorapi.Params{ParamTeam: teamID}, vendorapi.Window{}, "2"); err == nil {
		t.Error("the surface is not paged")
	}
	if d := SpendingLimits(); d.Auth.Scheme != vendorapi.SchemeBearer || d.Auth.Kind != vs.AuthBearer {
		t.Errorf("driver %+v", d)
	}
}
