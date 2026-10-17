package jetbrains

import (
	"errors"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/vendorapi"
)

const usersBody = `[
 {"id":"u1","createdDate":"2026-01-15T10:42:31.123Z","createdBy":"system","email":"user1@toolbox.com","username":"user1","role":"ADMIN","isActive":true},
 {"id":"u2","createdDate":"2026-03-09T16:30:00+02:00","createdBy":"system","email":null,"username":"user3","role":"USER","isActive":false}
]`

func TestUsers(t *testing.T) {
	d := Users()
	if d.Auth.Scheme != vendorapi.SchemePrefixed || d.Auth.Prefix != "Automation" || !vendorapi.ValidAuthPrefix(d.Auth.Prefix) {
		t.Fatalf("auth contract %+v", d.Auth)
	}
	req, err := d.Build(nil, vendorapi.Window{}, "")
	if err != nil || req.Method != "GET" || req.Path != "/api/v1/users" {
		t.Fatalf("build %+v %v", req, err)
	}
	if _, err := d.Build(nil, vendorapi.Window{}, "x"); err == nil {
		t.Fatal("a cursor must be refused: the list is not paged")
	}
	p, err := d.ParseResponse(vendorapi.Response{Status: 200, Body: []byte(usersBody)})
	if err != nil || len(p.Resources) != 2 || p.Snapshot == nil {
		t.Fatalf("parse %+v %v", p, err)
	}
	a := p.Resources[0]
	if a.DisplayName != "user1" || a.Attrs["email"] != "user1@toolbox.com" || a.Attrs["role"] != "ADMIN" ||
		a.Attrs["is_active"] != "true" || a.Attrs["created_at"] != "2026-01-15T10:42:31Z" {
		t.Errorf("first user %+v", a)
	}
	b := p.Resources[1]
	if _, has := b.Attrs["email"]; has || b.Attrs["is_active"] != "false" || b.Attrs["created_at"] != "2026-03-09T14:30:00Z" {
		t.Errorf("second user %+v", b)
	}
	for _, a := range p.Resources {
		if _, has := a.Attrs["created_by"]; has {
			t.Error("createdBy must never be landed")
		}
	}
	for name, body := range map[string]string{
		"object not array":  `{"users":[]}`,
		"null":              `null`,
		"missing role":      `[{"id":"u1","username":"a","isActive":true}]`,
		"missing id":        `[{"username":"a","role":"USER","isActive":true}]`,
		"repeat":            `[{"id":"u1","username":"a","role":"USER","isActive":true},{"id":"u1","username":"b","role":"USER","isActive":true}]`,
		"bad created date":  `[{"id":"u1","username":"a","role":"USER","isActive":true,"createdDate":"2026-01-15"}]`,
		"isActive a string": `[{"id":"u1","username":"a","role":"USER","isActive":"true"}]`,
	} {
		if _, err := d.ParseResponse(vendorapi.Response{Status: 200, Body: []byte(body)}); !errors.Is(err, vendorapi.ErrSchemaDrift) {
			t.Errorf("%s: err %v, want schema drift", name, err)
		}
	}
	if _, err := d.ParseResponse(vendorapi.Response{Status: 500, Body: []byte(`[]`)}); !errors.Is(err, vendorapi.ErrSchemaDrift) {
		t.Errorf("a non-200 must be drift, got %v", err)
	}
}

func TestValidAuthPrefix(t *testing.T) {
	for p, want := range map[string]bool{
		"Automation": true, "Token": true, "X-Key": true,
		"": false, "Bearer x": false, "1abc": false, "Auto\r\nmation": false, "a:b": false,
	} {
		if got := vendorapi.ValidAuthPrefix(p); got != want {
			t.Errorf("ValidAuthPrefix(%q) = %v, want %v", p, got, want)
		}
	}
}
