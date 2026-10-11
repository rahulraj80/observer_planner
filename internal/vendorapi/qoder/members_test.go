package qoder

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
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "vendorapi", "qoder", string(SurfaceMembers), name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func resp(body []byte) vendorapi.Response {
	return vendorapi.Response{Status: 200, Origin: "https://api.qoder.com:443", Body: body}
}

// TestMembers (lane ENTAPI-L16B, T-708): the documented example lands two
// members, the absent email absent; nextToken pages and is omitted on the
// last page; every field-policy row is schema drift.
func TestMembers(t *testing.T) {
	p, err := parseMembers(resp(fixture(t, "normal_page.json")))
	if err != nil {
		t.Fatal(err)
	}
	p.Snapshot.ManifestID, p.Snapshot.ObservedAt = "run-1", time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	if err := vendorapi.Validate(p); err != nil {
		t.Fatal(err)
	}
	if len(p.Resources) != 2 || p.NextCursor != "eyJwYWdlIjogMn0=" {
		t.Fatalf("page %+v", p)
	}
	a, c := p.Resources[0], p.Resources[1]
	if a.ID.Value != "member_abc123" || a.DisplayName != "Alice" || a.Attrs["email"] != "alice@example.com" ||
		a.Attrs["role"] != "org_admin" || a.Attrs["status"] != "ENABLED" || a.Attrs["joined_at"] != "2025-06-01T08:00:00Z" ||
		a.Attrs["user_id"] != "550e8400-e29b-41d4-a716-446655440000" {
		t.Errorf("alice %+v", a)
	}
	if _, ok := c.Attrs["email"]; ok {
		t.Error("an absent email landed")
	}
	r := resp(fixture(t, "last_page.json"))
	r.Cursor = p.NextCursor
	if p2, err := parseMembers(r); err != nil || p2.NextCursor != "" || len(p2.Resources) != 1 {
		t.Fatalf("last page %+v %v", p2, err)
	}
	for name, body := range map[string]string{
		"members missing": `{"maxResults":20}`,
		"id missing":      `{"members":[{"status":"ENABLED","joinedAt":"2025-06-01T08:00:00Z"}]}`,
		"status":          `{"members":[{"id":"m","status":"ACTIVE","joinedAt":"2025-06-01T08:00:00Z"}]}`,
		"joined":          `{"members":[{"id":"m","status":"ENABLED","joinedAt":"June"}]}`,
		"deleted":         `{"members":[{"id":"m","status":"DELETED","joinedAt":"2025-06-01T08:00:00Z","deletedAt":"x"}]}`,
		"repeated":        `{"members":[{"id":"m","status":"ENABLED","joinedAt":"2025-06-01T08:00:00Z"},{"id":"m","status":"ENABLED","joinedAt":"2025-06-01T08:00:00Z"}]}`,
		"token type":      `{"members":[],"nextToken":5}`,
	} {
		if _, err := parseMembers(resp([]byte(body))); err == nil || !errors.Is(err, vendorapi.ErrSchemaDrift) {
			t.Errorf("%s: err %v, want schema drift", name, err)
		}
	}
	r = resp([]byte(`{"members":[],"nextToken":"t"}`))
	r.Cursor = "t"
	if _, err := parseMembers(r); err == nil {
		t.Error("a repeated token was accepted")
	}
	req, err := buildMembers(vendorapi.Params{ParamOrganization: "org_xxx"}, vendorapi.Window{}, "eyJwYWdlIjogMn0=")
	if err != nil || req.Path != "/v1/organizations/org_xxx/members" || req.Query["maxResults"][0] != "100" ||
		req.Query["nextToken"][0] != "eyJwYWdlIjogMn0=" || req.Query["includeDeleted"] != nil {
		t.Fatalf("request %+v %v", req, err)
	}
	for _, o := range []string{"", "../x", "a/b", "org xxx"} {
		if _, err := buildMembers(vendorapi.Params{ParamOrganization: o}, vendorapi.Window{}, ""); err == nil {
			t.Errorf("organization %q accepted", o)
		}
	}
}
