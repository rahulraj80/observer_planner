package jetbrains

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/marmutapp/superbased-observer/internal/vendorapi"
	vs "github.com/marmutapp/superbased-observer/internal/vendorsurface"
)

// SurfaceHubSCIM is the catalog id of the JetBrains Hub SCIM 2.0 endpoint.
const SurfaceHubSCIM vs.SurfaceID = "jetbrains.hub.scim2"

// Hub SCIM vocabulary.
const (
	HubSCIMContract = "jetbrains.hub.scim2/v1 scim 2.0 ListResponse of /Users (RFC 7644 3.4.2, 2026-10-08)"

	// HubUserType is one user a Hub authentication module provisions.
	HubUserType vs.ResourceType = "User/jetbrains.hub_user/v1"
	// NamespaceHubUser keys a user by its SCIM id.
	NamespaceHubUser = "jetbrains.hub_user"
	// HubSCIMPageSize is the count the driver asks for per page.
	HubSCIMPageSize = 100

	listResponseSchema = "urn:ietf:params:scim:api:messages:2.0:ListResponse"
)

// hubAuth is the documented SCIM contract: the Hub permanent token as the
// identity provider's "Secret Token", sent as "Authorization: Bearer <token>"
// (RFC 7644 section 2 bearer authorization).
var hubAuth = vendorapi.AuthContract{Kind: vs.AuthBearer, Scheme: vendorapi.SchemeBearer, Header: "Authorization"}

// HubSCIM returns the driver of jetbrains.hub.scim2.
//
// Contract (Hub "User Import and Sync" and "Microsoft Entra ID auth module":
// the tenant URL is <hub-base-url>/api/rest/scim2 and the secret token is a
// Hub permanent token; the read itself is the SCIM 2.0 standard, RFC 7644
// section 3.4.2): GET /api/rest/scim2/Users on the customer's Hub (a
// customer_host surface: the connection declares the tuple), startIndex
// (1-based) and count. The answer is a ListResponse: schemas, totalResults,
// itemsPerPage, startIndex and Resources (absent when there are none). Hub
// itself documents only the endpoint and the token, not a list response:
// that Hub answers the standard ListResponse of RFC 7643 User resources is
// Inferred, so every rung is at most assumption_tested and any other shape is
// schema drift. A Hub installed under a path prefix (a /hub base URL) is not
// reachable: the declared tuple carries no path.
//
// Paging is the offset family with a 1-based index: the cursor keeps the
// 0-based offset and the request sends startIndex = offset + 1; an echoed
// startIndex other than the one asked, a total that does not cover the page,
// and a page repeating the previous page's last user are drift. Lands one
// resource per user: the primary e-mail (else the first), whether active,
// created and last modified times; the display name (else the formatted
// name, else userName) is the display name. Passwords are never returned by
// SCIM (RFC 7643 "returned: never"); groups, roles, entitlements, x509
// certificates, phone numbers, addresses, photos and any extension are never
// decoded. A complete page set is authoritative for the module's users. Read
// only: SCIM writes are the identity provider's, never Observer's.
func HubSCIM() vendorapi.Driver {
	return vendorapi.Driver{
		Surface:               SurfaceHubSCIM,
		Build:                 buildHubSCIM,
		ParseResponse:         parseHubSCIM,
		Auth:                  hubAuth,
		ContentTypes:          []string{"application/scim+json", "application/json"},
		Supplies:              []vs.DataClass{vs.ClassIdentity},
		AuthoritativeSnapshot: true,
		ClosedDTO:             true,
		ContractVersion:       HubSCIMContract,
		KnownPaths: []string{
			"Resources", "Resources[]", "Resources[].active", "Resources[].displayName", "Resources[].emails",
			"Resources[].emails[]", "Resources[].emails[].primary", "Resources[].emails[].type",
			"Resources[].emails[].value", "Resources[].id", "Resources[].meta", "Resources[].meta.created",
			"Resources[].meta.lastModified", "Resources[].meta.resourceType", "Resources[].name",
			"Resources[].name.formatted", "Resources[].schemas", "Resources[].userName", "itemsPerPage", "schemas",
			"startIndex", "totalResults",
		},
	}
}

func buildHubSCIM(_ vendorapi.Params, _ vendorapi.Window, cursor string) (vendorapi.Request, error) {
	off, ok := vendorapi.OffsetOf(cursor)
	if !ok {
		return vendorapi.Request{}, fmt.Errorf("%s: cursor is not an offset cursor", SurfaceHubSCIM)
	}
	return vendorapi.Request{Method: "GET", Path: "/api/rest/scim2/Users", Query: map[string][]string{
		"startIndex": {strconv.Itoa(off + 1)}, "count": {strconv.Itoa(HubSCIMPageSize)},
	}}, nil
}

// integer is a bare JSON integer; a quoted, fractional or exponent value is a
// type change and fails the page.
type integer int64

// UnmarshalJSON accepts a bare JSON integer only.
func (n *integer) UnmarshalJSON(b []byte) error {
	s := string(b)
	if s == "" || strings.ContainsAny(s, `".eE+`) {
		return fmt.Errorf("jetbrains: %.16q is not a JSON integer", b)
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return err
	}
	*n = integer(v)
	return nil
}

type scimUser struct {
	ID          *string `json:"id"`
	UserName    *string `json:"userName"`
	DisplayName *string `json:"displayName"`
	Name        *struct {
		Formatted *string `json:"formatted"`
	} `json:"name"`
	Active *bool `json:"active"`
	Emails *[]struct {
		Value   *string `json:"value"`
		Primary *bool   `json:"primary"`
	} `json:"emails"`
	Meta *struct {
		Created      *string `json:"created"`
		LastModified *string `json:"lastModified"`
	} `json:"meta"`
}

type scimList struct {
	Schemas      *[]string   `json:"schemas"`
	TotalResults *integer    `json:"totalResults"`
	StartIndex   *integer    `json:"startIndex"`
	ItemsPerPage *integer    `json:"itemsPerPage"`
	Resources    *[]scimUser `json:"Resources"`
}

func scimDrift(format string, a ...any) error {
	return fmt.Errorf("%w: %s: "+format, append([]any{vendorapi.ErrSchemaDrift, SurfaceHubSCIM}, a...)...)
}

func nonEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return strings.TrimSpace(*s)
}

func scimTime(s *string) (string, bool) {
	if s == nil {
		return "", true
	}
	t, err := time.Parse(time.RFC3339Nano, *s)
	if err != nil {
		return "", false
	}
	return t.UTC().Format(time.RFC3339), true
}

// primaryEmail is the e-mail marked primary, else the first with a value.
func primaryEmail(u scimUser) string {
	if u.Emails == nil {
		return ""
	}
	first := ""
	for _, e := range *u.Emails {
		v := nonEmpty(e.Value)
		if v == "" {
			continue
		}
		if e.Primary != nil && *e.Primary {
			return v
		}
		if first == "" {
			first = v
		}
	}
	return first
}

func parseHubSCIM(r vendorapi.Response) (vendorapi.Page, error) {
	if r.Status != 200 {
		return vendorapi.Page{}, scimDrift("status %d is not 200", r.Status)
	}
	var doc scimList
	if err := json.NewDecoder(bytes.NewReader(r.Body)).Decode(&doc); err != nil {
		return vendorapi.Page{}, scimDrift("body does not decode as a SCIM ListResponse")
	}
	if doc.Schemas == nil || doc.TotalResults == nil {
		return vendorapi.Page{}, scimDrift("required schemas or totalResults is missing")
	}
	isList := false
	for _, s := range *doc.Schemas {
		isList = isList || s == listResponseSchema
	}
	if !isList {
		return vendorapi.Page{}, scimDrift("schemas does not name the ListResponse message")
	}
	off, ok := vendorapi.OffsetOf(r.Cursor)
	if !ok {
		return vendorapi.Page{}, scimDrift("the page was asked with a cursor that is not an offset cursor")
	}
	if doc.StartIndex != nil && int(*doc.StartIndex) != off+1 {
		return vendorapi.Page{}, scimDrift("startIndex %d is not the %d asked", *doc.StartIndex, off+1)
	}
	var users []scimUser
	if doc.Resources != nil {
		users = *doc.Resources
	} else if *doc.TotalResults > 0 && off < int(*doc.TotalResults) {
		return vendorapi.Page{}, scimDrift("Resources is missing while totalResults promises users")
	}
	page := vendorapi.Page{SourceRecords: len(users), Snapshot: &vendorapi.SnapshotManifest{
		Scope:          []vendorapi.ScopeKey{{Namespace: NamespaceHubUser, Kind: vs.KindUser}},
		SelectorDigest: "scim_module_users", PageSet: "hub_scim_users", ContractVersion: HubSCIMContract,
	}}
	ids := make([]string, 0, len(users))
	seen := map[string]bool{}
	for i, u := range users {
		id := nonEmpty(u.ID)
		if id == "" || nonEmpty(u.UserName) == "" {
			return vendorapi.Page{}, scimDrift("Resources[%d] misses id or userName (required by RFC 7643)", i)
		}
		if seen[id] {
			return vendorapi.Page{}, scimDrift("Resources[%d] repeats a user", i)
		}
		seen[id] = true
		ids = append(ids, id)
		attrs := map[string]string{}
		if e := primaryEmail(u); e != "" {
			attrs["email"] = vendorapi.FoldEmail(e)
		}
		if u.Active != nil {
			attrs["active"] = strconv.FormatBool(*u.Active)
		}
		if u.Meta != nil {
			c, okc := scimTime(u.Meta.Created)
			m, okm := scimTime(u.Meta.LastModified)
			if !okc || !okm {
				return vendorapi.Page{}, scimDrift("Resources[%d].meta has a time that is not an RFC 3339 date-time", i)
			}
			if c != "" {
				attrs["created_at"] = c
			}
			if m != "" {
				attrs["last_modified_at"] = m
			}
		}
		name := nonEmpty(u.DisplayName)
		if name == "" && u.Name != nil {
			name = nonEmpty(u.Name.Formatted)
		}
		if name == "" {
			name = nonEmpty(u.UserName)
		}
		page.Resources = append(page.Resources, vendorapi.ResourceObservation{
			Type: HubUserType, ID: vs.ExternalID{Namespace: NamespaceHubUser, Value: id}, DisplayName: name, Attrs: attrs,
		})
	}
	if vendorapi.OffsetLoop(r.Cursor, ids) {
		return vendorapi.Page{}, scimDrift("the page repeats the previous page's last user (offset paging loop)")
	}
	next, ok := vendorapi.NextOffsetCursor(off, int(*doc.TotalResults), ids)
	if !ok {
		return vendorapi.Page{}, scimDrift("totalResults %d does not cover startIndex %d plus %d users", *doc.TotalResults, off+1, len(ids))
	}
	page.NextCursor = next
	return page, nil
}
