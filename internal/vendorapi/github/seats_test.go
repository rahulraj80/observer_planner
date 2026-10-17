package github

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

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "vendorapi", "github", string(SurfaceOrgSeats), name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func resp(body []byte, link string) vendorapi.Response {
	h := map[string]string{}
	if link != "" {
		h["link"] = link
	}
	return vendorapi.Response{
		Status: 200, Origin: "https://api.github.com:443", Path: "/orgs/acme/copilot/billing/seats",
		Header: h, Body: body, Params: vendorapi.Params{"org": "Acme"},
	}
}

// stamp fills the two manifest fields the runner owns, so a parsed page can
// pass vendorapi.Validate the way the runner presents it.
func stamp(p vendorapi.Page) vendorapi.Page {
	if p.Snapshot != nil {
		p.Snapshot.ManifestID = "run-1:" + string(SurfaceOrgSeats)
		p.Snapshot.ObservedAt = time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	}
	return p
}

func TestOrgSeatsParsesCorpusFixtures(t *testing.T) {
	p, err := parseSeats(resp(fixture(t, "normal_page.json"), `<https://api.github.com/organizations/1/copilot/billing/seats?page=2>; rel="next"`))
	if err != nil {
		t.Fatal(err)
	}
	if err := vendorapi.Validate(stamp(p)); err != nil {
		t.Fatalf("parsed page does not validate: %v", err)
	}
	if len(p.Resources) != 2 || len(p.Edges) != 2 || p.NextCursor != "2" {
		t.Fatalf("resources %d edges %d next %q, want 2, 2, \"2\"", len(p.Resources), len(p.Edges), p.NextCursor)
	}
	r := p.Resources[0]
	if r.Type != SeatType || r.ID.Value != "acme:MDQ6VXNlcjE=" || r.DisplayName != "octocat" || r.Parent.ID.Value != "acme" {
		t.Fatalf("seat 0 = %+v", r)
	}
	for k, want := range map[string]string{
		"plan_type": "business", "assignee_type": "user", "assignee_id": "1",
		"assigning_team_slug": "justice-league", "last_activity_editor": "vscode/1.77.3/copilot/1.86.82",
	} {
		if r.Attrs[k] != want {
			t.Errorf("attr %s = %q, want %q", k, r.Attrs[k], want)
		}
	}
	if _, ok := r.Attrs["pending_cancellation_date"]; ok {
		t.Errorf("a null optional field must be absent, not empty")
	}
	if p.Resources[1].Attrs["pending_cancellation_date"] != "2021-11-01" {
		t.Errorf("seat 1 pending cancellation lost")
	}
	if p.Edges[0].Edge != vs.EdgeLicensedTo || p.Edges[0].To.Kind != vs.KindUser || p.Edges[0].To.ID.Namespace != NamespaceUser {
		t.Errorf("edge 0 = %+v", p.Edges[0])
	}
	empty, err := parseSeats(resp(fixture(t, "empty_page.json"), ""))
	if err != nil || len(empty.Resources) != 0 || empty.NextCursor != "" || empty.Snapshot == nil {
		t.Fatalf("empty page: %+v %v (an empty snapshot still opens its manifest, so a complete run can tombstone)", empty, err)
	}
}

// TestFieldPolicyMatrix (github.copilot.org_seats rows): a missing required
// field, a wrong JSON type, an identity-field change and a pagination change
// fail the page with ErrSchemaDrift; an unknown property is ignored and an
// unknown assignee shape lands as "other" with a canary signal.
func TestFieldPolicyMatrix(t *testing.T) {
	cases := []struct {
		name  string
		body  string
		link  string
		drift bool
	}{
		{"seats renamed", `{"total_seats":1,"seats_renamed":[]}`, "", true},
		{"total_seats missing", `{"seats":[]}`, "", true},
		{"total_seats wrong type", `{"total_seats":"1","seats":[]}`, "", true},
		{"seats wrong type", `{"total_seats":1,"seats":{}}`, "", true},
		{"assignee missing", `{"total_seats":1,"seats":[{"plan_type":"business"}]}`, "", true},
		{"assignee node id missing", `{"total_seats":1,"seats":[{"assignee":{"login":"a","id":1,"type":"User"}}]}`, "", true},
		{"duplicate assignee", `{"total_seats":2,"seats":[{"assignee":{"node_id":"N1","type":"User"}},{"assignee":{"node_id":"N1","type":"User"}}]}`, "", true},
		{"not json", `<html>`, "", true},
		{"next link to another host", `{"total_seats":0,"seats":[]}`, `<https://evil.example.com/orgs/acme/copilot/billing/seats?page=2>; rel="next"`, true},
		{"next link over plain http", `{"total_seats":0,"seats":[]}`, `<http://api.github.com/x?page=2>; rel="next"`, true},
		{"next link without a page", `{"total_seats":0,"seats":[]}`, `<https://api.github.com/x?cursor=abc>; rel="next"`, true},
		{"next link with userinfo", `{"total_seats":0,"seats":[]}`, `<https://u@api.github.com/x?page=2>; rel="next"`, true},
		{"unknown property ignored", `{"total_seats":0,"seats":[],"brand_new":{"x":1}}`, "", false},
		{"prev link only", `{"total_seats":0,"seats":[]}`, `<https://api.github.com/x?page=1>; rel="prev"`, false},
		{"unknown assignee shape", `{"total_seats":1,"seats":[{"assignee":{"node_id":"N9","type":"Mannequin"}}]}`, "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, err := parseSeats(resp([]byte(c.body), c.link))
			if c.drift {
				if !errors.Is(err, vendorapi.ErrSchemaDrift) {
					t.Fatalf("want schema drift, got %v", err)
				}
				if strings.Contains(err.Error(), "evil.example.com") || strings.Contains(err.Error(), "Mannequin") {
					t.Fatalf("a drift error must not carry raw values: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if c.name == "unknown assignee shape" {
				if p.Resources[0].Attrs["assignee_type"] != "other" || len(p.Edges) != 0 {
					t.Fatalf("unknown shape must land as other without an edge: %+v", p)
				}
				if sig := canarySeats(p); len(sig) != 1 || sig[0].Kind != "unknown_enum" {
					t.Fatalf("canary = %+v", sig)
				}
			}
		})
	}
}

func TestBuildSeats(t *testing.T) {
	r, err := buildSeats(vendorapi.Params{"org": "acme-inc"}, vendorapi.Window{}, "")
	if err != nil || r.Method != "GET" || r.Path != "/orgs/acme-inc/copilot/billing/seats" ||
		r.Query["per_page"][0] != "100" || r.Query["page"] != nil {
		t.Fatalf("first page request = %+v, %v", r, err)
	}
	r, err = buildSeats(vendorapi.Params{"org": "acme"}, vendorapi.Window{}, "3")
	if err != nil || r.Query["page"][0] != "3" {
		t.Fatalf("page 3 request = %+v, %v", r, err)
	}
	for _, org := range []string{"", "../admin", "a/b", "a?x=1", "-acme", "acme-", "a--b", strings.Repeat("a", 40)} {
		if _, err := buildSeats(vendorapi.Params{"org": org}, vendorapi.Window{}, ""); err == nil {
			t.Errorf("org %q accepted", org)
		}
	}
	for _, cur := range []string{"1", "0", "-2", "02", "x", "2&per_page=1"} {
		if _, err := buildSeats(vendorapi.Params{"org": "acme"}, vendorapi.Window{}, cur); err == nil {
			t.Errorf("cursor %q accepted", cur)
		}
	}
}

// TestAuthSchemePerEndpointNoGenericFallback: the driver carries one exact
// auth contract (the catalog's kind, the documented scheme) and nothing that
// would let the runner try a second scheme.
func TestAuthSchemePerEndpointNoGenericFallback(t *testing.T) {
	d := OrgSeats()
	if d.Auth != (vendorapi.AuthContract{Kind: vs.AuthBearer, Scheme: vendorapi.SchemeBearer, Header: "Authorization"}) {
		t.Fatalf("auth contract = %+v", d.Auth)
	}
	for k := range d.Headers {
		if strings.EqualFold(k, "authorization") || strings.Contains(strings.ToLower(k), "token") {
			t.Fatalf("static header %q would carry a credential", k)
		}
	}
}

// TestOrgSeatsSchemaRandom drives the parser with random instances of the
// official response schema (rest-api-description ghec.2026-03-10): every
// instance either parses to one seat per record with the page still valid,
// or fails as schema drift. Nothing panics and nothing is silently dropped.
func TestOrgSeatsSchemaRandom(t *testing.T) {
	s, err := vendormock.LoadSchema(string(SurfaceOrgSeats))
	if err != nil {
		t.Fatal(err)
	}
	rng := rand.New(rand.NewSource(606))
	parsed := 0
	for i := 0; i < 300; i++ {
		raw, err := json.Marshal(vendormock.Generate(s.Schema, rng, vendormock.GenOptions{}))
		if err != nil {
			t.Fatal(err)
		}
		p, err := parseSeats(resp(raw, ""))
		if err != nil {
			if !errors.Is(err, vendorapi.ErrSchemaDrift) {
				t.Fatalf("instance %d: non-drift error %v", i, err)
			}
			continue
		}
		var doc struct {
			Seats []json.RawMessage `json:"seats"`
		}
		_ = json.Unmarshal(raw, &doc)
		if len(p.Resources) != len(doc.Seats) {
			t.Fatalf("instance %d: %d seats parsed from %d records", i, len(p.Resources), len(doc.Seats))
		}
		if err := vendorapi.Validate(stamp(p)); err != nil {
			t.Fatalf("instance %d: %v", i, err)
		}
		parsed++
	}
	if parsed == 0 {
		t.Fatal("no random instance parsed: the property test would be vacuous")
	}
}
