package prime

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
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "vendorapi", "prime", string(SurfaceTeamMembers), name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func resp(body []byte, cursor string) vendorapi.Response {
	return vendorapi.Response{
		Status: 200, Origin: "https://api.primeintellect.ai:443", Body: body, Cursor: cursor,
		Params: vendorapi.Params{ParamTeam: "team_1"},
	}
}

// TestTeamMembers (lane ENTAPI-L16C, T-710): offset paging to the terminal
// page, the loop and echoed-offset rules, the null email absent, and every
// field-policy row is schema drift.
func TestTeamMembers(t *testing.T) {
	p, err := parseTeamMembers(resp(fixture(t, "normal_page.json"), ""))
	if err != nil {
		t.Fatal(err)
	}
	p.Snapshot.ManifestID, p.Snapshot.ObservedAt = "run-1", time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	if err := vendorapi.Validate(p); err != nil {
		t.Fatal(err)
	}
	if len(p.Resources) != 2 || p.NextCursor == "" {
		t.Fatalf("page %+v", p)
	}
	if a := p.Resources[0]; a.Attrs["role"] != "ADMIN" || a.Attrs["email"] != "ada@example.com" || a.Attrs["joined_at"] != "2026-03-01T09:00:00Z" {
		t.Errorf("ada %+v", a)
	}
	if _, ok := p.Resources[1].Attrs["email"]; ok {
		t.Error("a null email landed")
	}
	last, err := parseTeamMembers(resp(fixture(t, "last_page.json"), p.NextCursor))
	if err != nil || last.NextCursor != "" || len(last.Resources) != 1 {
		t.Fatalf("last page %+v %v", last, err)
	}
	for name, r := range map[string]vendorapi.Response{
		"loop":           resp(fixture(t, "normal_page.json"), p.NextCursor),
		"data missing":   resp([]byte(`{"total_count":0}`), ""),
		"total missing":  resp([]byte(`{"data":[]}`), ""),
		"offset echo":    resp([]byte(`{"total_count":1,"offset":5,"data":[]}`), ""),
		"role":           resp([]byte(`{"total_count":1,"data":[{"userId":"u","userName":null,"userEmail":null,"role":"OWNER","joinedAt":"2026-01-01T00:00:00Z"}]}`), ""),
		"joined":         resp([]byte(`{"total_count":1,"data":[{"userId":"u","userName":null,"userEmail":null,"role":"MEMBER","joinedAt":"yesterday"}]}`), ""),
		"repeated":       resp([]byte(`{"total_count":2,"data":[{"userId":"u","role":"MEMBER","joinedAt":"2026-01-01T00:00:00Z"},{"userId":"u","role":"MEMBER","joinedAt":"2026-01-01T00:00:00Z"}]}`), ""),
		"total quoted":   resp([]byte(`{"total_count":"1","data":[]}`), ""),
		"status not 200": {Status: 204, Body: []byte(`{}`)},
	} {
		if _, err := parseTeamMembers(r); err == nil || !errors.Is(err, vendorapi.ErrSchemaDrift) {
			t.Errorf("%s: err %v, want schema drift", name, err)
		}
	}
	req, err := buildTeamMembers(vendorapi.Params{ParamTeam: "team_1"}, vendorapi.Window{}, p.NextCursor)
	if err != nil || req.Path != "/api/v1/teams/team_1/members" || req.Query["offset"][0] != "2" {
		t.Fatalf("request %+v %v", req, err)
	}
	for _, team := range []string{"", "../x", "a/b", "t x"} {
		if _, err := buildTeamMembers(vendorapi.Params{ParamTeam: team}, vendorapi.Window{}, ""); err == nil {
			t.Errorf("team %q accepted", team)
		}
	}
}
