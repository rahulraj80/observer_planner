package jetbrains

import (
	"errors"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/vendorapi"
)

const scimPage = `{
 "schemas": ["urn:ietf:params:scim:api:messages:2.0:ListResponse"],
 "totalResults": 3, "itemsPerPage": 2, "startIndex": 1,
 "Resources": [
  {"schemas": ["urn:ietf:params:scim:schemas:core:2.0:User"], "id": "2819c223", "userName": "bjensen@corp.example",
   "name": {"formatted": "Ms. Barbara J Jensen III"}, "displayName": "Babs Jensen", "active": true,
   "emails": [{"value": "bjensen@other.example", "type": "home"}, {"value": "BJensen@Corp.Example", "type": "work", "primary": true}],
   "meta": {"resourceType": "User", "created": "2010-01-23T04:56:22Z", "lastModified": "2011-05-13T04:42:34Z"},
   "password": "never-returned-but-must-not-land", "groups": [{"value": "g1", "display": "Tour Guides"}]},
  {"id": "u2", "userName": "jsmith", "active": false}
 ]
}`

func TestHubSCIMBuild(t *testing.T) {
	d := HubSCIM()
	if d.Auth.Scheme != vendorapi.SchemeBearer || !d.AuthoritativeSnapshot || !d.ClosedDTO {
		t.Fatalf("driver %+v", d)
	}
	req, err := d.Build(nil, vendorapi.Window{}, "")
	if err != nil || req.Path != "/api/rest/scim2/Users" || req.Query["startIndex"][0] != "1" || req.Query["count"][0] != "100" {
		t.Fatalf("build %+v %v", req, err)
	}
	if _, has := req.Query["filter"]; has {
		t.Error("a filter must never be sent")
	}
	if _, err := d.Build(nil, vendorapi.Window{}, "bogus"); err == nil {
		t.Error("a non-offset cursor must be refused")
	}
}

func TestHubSCIMParse(t *testing.T) {
	d := HubSCIM()
	p, err := d.ParseResponse(vendorapi.Response{Status: 200, Body: []byte(scimPage)})
	if err != nil || len(p.Resources) != 2 || p.Snapshot == nil || p.NextCursor == "" {
		t.Fatalf("parse %+v %v", p, err)
	}
	a := p.Resources[0]
	if a.DisplayName != "Babs Jensen" || a.Attrs["email"] != "bjensen@corp.example" || a.Attrs["active"] != "true" ||
		a.Attrs["created_at"] != "2010-01-23T04:56:22Z" || a.Attrs["last_modified_at"] != "2011-05-13T04:42:34Z" {
		t.Errorf("first user %+v", a)
	}
	for _, r := range p.Resources {
		for k, v := range r.Attrs {
			if k == "password" || k == "groups" || strings.Contains(v, "never-returned") {
				t.Errorf("%s must never land", k)
			}
		}
	}
	b := p.Resources[1]
	if b.DisplayName != "jsmith" || b.Attrs["active"] != "false" {
		t.Errorf("second user %+v", b)
	}
	if _, has := b.Attrs["email"]; has {
		t.Error("a user with no e-mail lands none")
	}
	// Page two asks startIndex 3 and completes the set.
	req, err := d.Build(nil, vendorapi.Window{}, p.NextCursor)
	if err != nil || req.Query["startIndex"][0] != "3" {
		t.Fatalf("page two %+v %v", req, err)
	}
	last := `{"schemas":["urn:ietf:params:scim:api:messages:2.0:ListResponse"],"totalResults":3,"itemsPerPage":1,"startIndex":3,
	 "Resources":[{"id":"u3","userName":"c"}]}`
	p2, err := d.ParseResponse(vendorapi.Response{Status: 200, Body: []byte(last), Cursor: p.NextCursor})
	if err != nil || p2.NextCursor != "" || len(p2.Resources) != 1 {
		t.Fatalf("last page %+v %v", p2, err)
	}
	// The same page served again at the second index is a loop.
	if _, err := d.ParseResponse(vendorapi.Response{Status: 200, Body: []byte(strings.Replace(scimPage, `"startIndex": 1`, `"startIndex": 3`, 1)), Cursor: p.NextCursor}); !errors.Is(err, vendorapi.ErrSchemaDrift) {
		t.Errorf("loop: %v", err)
	}
	empty, err := d.ParseResponse(vendorapi.Response{Status: 200, Body: []byte(`{"schemas":["urn:ietf:params:scim:api:messages:2.0:ListResponse"],"totalResults":0}`)})
	if err != nil || len(empty.Resources) != 0 || empty.NextCursor != "" {
		t.Errorf("empty module %+v %v", empty, err)
	}
	for name, body := range map[string]string{
		"not a list":       strings.Replace(scimPage, "ListResponse", "Error", 1),
		"no totalResults":  strings.Replace(scimPage, `"totalResults": 3,`, ``, 1),
		"quoted total":     strings.Replace(scimPage, `"totalResults": 3`, `"totalResults": "3"`, 1),
		"wrong startIndex": strings.Replace(scimPage, `"startIndex": 1`, `"startIndex": 2`, 1),
		"short total":      strings.Replace(scimPage, `"totalResults": 3`, `"totalResults": 1`, 1),
		"missing userName": strings.Replace(scimPage, `"userName": "jsmith", `, ``, 1),
		"repeat":           strings.Replace(scimPage, `"id": "u2"`, `"id": "2819c223"`, 1),
		"bad created":      strings.Replace(scimPage, `"2010-01-23T04:56:22Z"`, `"2010-01-23"`, 1),
		"active a string":  strings.Replace(scimPage, `"active": false`, `"active": "false"`, 1),
		"no Resources":     `{"schemas":["urn:ietf:params:scim:api:messages:2.0:ListResponse"],"totalResults":2}`,
	} {
		if _, err := d.ParseResponse(vendorapi.Response{Status: 200, Body: []byte(body)}); !errors.Is(err, vendorapi.ErrSchemaDrift) {
			t.Errorf("%s: err %v, want schema drift", name, err)
		}
	}
	if _, err := d.ParseResponse(vendorapi.Response{Status: 401, Body: []byte(`{}`)}); !errors.Is(err, vendorapi.ErrSchemaDrift) {
		t.Errorf("a non-200 must be drift, got %v", err)
	}
}
