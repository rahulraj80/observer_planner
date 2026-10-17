package vendorapi

import (
	"errors"
	"strings"
	"testing"
	"time"

	vs "github.com/marmutapp/superbased-observer/internal/vendorsurface"
)

var t0 = time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)

func ghID(v string) vs.ExternalID { return vs.ExternalID{Namespace: "github.login", Value: v} }

func basePage() Page {
	team := NodeRef{Kind: vs.KindContainer, ID: vs.ExternalID{Namespace: "github.team", Value: "core"}}
	return Page{
		Snapshot: &SnapshotManifest{
			ManifestID: "m-1",
			Scope: []ScopeKey{
				{Namespace: "github.login", Kind: vs.KindUser},
				{Namespace: "github.team", Kind: vs.KindContainer},
				{Namespace: "github.pat", Kind: vs.KindCredential},
			},
			ScopeEdges: []vs.EdgeKind{vs.EdgeMemberOf},
			Complete:   true, AuthoritativeForDeletion: true, ObservedAt: t0,
		},
		Resources: []ResourceObservation{
			{Type: "User/github.member/v1", ID: ghID("octo"), DisplayName: "octo", Attrs: map[string]string{"role": "member"}},
			{Type: "Container/github.team/v1", ID: team.ID, ContainerKind: vs.ContainerTeam},
			{
				Type: "Credential/github.fine_grained_pat/v1", ID: vs.ExternalID{Namespace: "github.pat", Value: "1"},
				Attrs: map[string]string{"redacted_hint": "...abcd", "owner": "octo", "expires": "2027-01-01"},
			},
		},
		Edges: []EdgeObservation{{Edge: vs.EdgeMemberOf, From: NodeRef{Kind: vs.KindUser, ID: ghID("octo")}, To: team}},
	}
}

// TestValidatePageTable walks one case per refusal rule: a page that breaks a
// rule is refused with ErrInvalidPage naming it, and the base page is valid.
func TestValidatePageTable(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Page)
		want   string // "" = valid
	}{
		{"base page is valid", func(*Page) {}, ""},
		{"empty page needs no manifest", func(p *Page) { *p = Page{} }, ""},
		{"observations without a manifest", func(p *Page) { p.Snapshot = nil }, "snapshot manifest"},
		{"manifest without id", func(p *Page) { p.Snapshot.ManifestID = " " }, "manifest id"},
		{"manifest without scope", func(p *Page) { p.Snapshot.Scope = nil }, "scope is empty"},
		{"authoritative but incomplete", func(p *Page) { p.Snapshot.Complete = false }, "only a complete snapshot"},
		{"manifest without time", func(p *Page) { p.Snapshot.ObservedAt = time.Time{} }, "observed_at"},
		{"unparseable resource type", func(p *Page) { p.Resources[0].Type = "user" }, "Kind/vendor.subtype/vN"},
		{"un-namespaced external id", func(p *Page) { p.Resources[0].ID.Namespace = "login" }, "namespaced"},
		{"container without container kind", func(p *Page) { p.Resources[1].ContainerKind = "" }, "container kind"},
		{"non-container with container kind", func(p *Page) { p.Resources[0].ContainerKind = vs.ContainerTeam }, "container kind"},
		{"untyped parent", func(p *Page) { p.Resources[0].Parent = &NodeRef{Kind: "Thing", ID: ghID("x")} }, "parent"},
		{"long display name", func(p *Page) { p.Resources[0].DisplayName = strings.Repeat("a", MaxDisplayName+1) }, "display name"},
		{"bad attribute key", func(p *Page) { p.Resources[0].Attrs["Bad Key"] = "x" }, "attributes within limits"},
		{"secret-shaped attribute on a user", func(p *Page) { p.Resources[0].Attrs["access_token"] = "x" }, "secret-shaped"},
		{"credential value attribute", func(p *Page) { p.Resources[2].Attrs["key_material"] = "x" }, "metadata only"},
		{"credential secret attribute", func(p *Page) { p.Resources[2].Attrs["secret"] = "x" }, "secret-shaped"},
		{"field authority on unknown attribute", func(p *Page) {
			p.Resources[0].FieldAuthority = map[string]string{"nope": "authoritative"}
		}, "field authority"},
		{"field authority bad value", func(p *Page) {
			p.Resources[0].FieldAuthority = map[string]string{"role": "sometimes"}
		}, "field authority"},
		{"resource outside scope", func(p *Page) { p.Resources[0].ID.Namespace = "github.email" }, "outside the manifest scope"},
		{"duplicate resource", func(p *Page) { p.Resources = append(p.Resources, p.Resources[0]) }, "repeats"},
		{"scope parent not matched", func(p *Page) {
			p.Snapshot.ScopeParent = &NodeRef{Kind: vs.KindContainer, ID: vs.ExternalID{Namespace: "github.team", Value: "core"}}
		}, "scope parent"},
		{"unknown edge kind", func(p *Page) { p.Edges[0].Edge = "likes" }, "unknown edge kind"},
		{"undeclared edge kind", func(p *Page) { p.Edges[0].Edge = vs.EdgeAssignedRole }, "not declared"},
		{"untyped edge endpoint", func(p *Page) { p.Edges[0].To.Kind = "" }, "endpoints"},
		{"edge source outside scope", func(p *Page) {
			p.Edges[0].From = NodeRef{Kind: vs.KindGroup, ID: vs.ExternalID{Namespace: "github.team", Value: "g"}}
		}, "source is outside"},
		{"secret-shaped edge attribute", func(p *Page) { p.Edges[0].Attrs = map[string]string{"token": "x"} }, "attributes"},
		{"empty validity interval", func(p *Page) {
			from, until := t0, t0
			p.Edges[0].ValidFrom, p.Edges[0].ValidUntil = &from, &until
		}, "validity interval"},
		{"duplicate edge", func(p *Page) { p.Edges = append(p.Edges, p.Edges[0]) }, "edge 1 repeats"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := basePage()
			tc.mutate(&p)
			err := Validate(p)
			if tc.want == "" {
				if err != nil {
					t.Fatalf("want valid, got %v", err)
				}
				return
			}
			if !errors.Is(err, ErrInvalidPage) || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want ErrInvalidPage containing %q, got %v", tc.want, err)
			}
		})
	}
}

// TestMayTombstone pins plan 4.5 R-E-05: only a complete, authoritative
// snapshot may mark resources deleted.
func TestMayTombstone(t *testing.T) {
	for _, tc := range []struct {
		complete, auth, want bool
	}{{true, true, true}, {true, false, false}, {false, false, false}, {false, true, false}} {
		if got := MayTombstone(SnapshotManifest{Complete: tc.complete, AuthoritativeForDeletion: tc.auth}); got != tc.want {
			t.Errorf("complete=%v auth=%v: got %v", tc.complete, tc.auth, got)
		}
	}
}

func TestScopeStringIsCanonical(t *testing.T) {
	m := basePage().Snapshot
	if got, want := ScopeString(*m), "Container@github.team,Credential@github.pat,User@github.login"; got != want {
		t.Fatalf("ScopeString = %q, want %q", got, want)
	}
	if got := EdgeScopeString(*m); got != "member_of" {
		t.Fatalf("EdgeScopeString = %q", got)
	}
}
