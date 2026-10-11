package github

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/marmutapp/superbased-observer/internal/vendorapi"
	vs "github.com/marmutapp/superbased-observer/internal/vendorsurface"
)

// Audit log surfaces (lane L8, T-611): GET /orgs/{org}/audit-log and
// GET /enterprises/{enterprise}/audit-log (rest-api-description
// ghec.2026-03-10, operations orgs/get-audit-log and
// enterprise-admin/get-audit-log). The window is a created: search phrase,
// paging follows the Link header (page numbers or the after cursor), and
// each event lands once as a vendor audit event.
const (
	SurfaceAuditOrg vs.SurfaceID = "github.audit.org_pull"
	SurfaceAuditEnt vs.SurfaceID = "github.audit.enterprise_pull"

	// AuditNamespace is the vendor audit namespace of both audit logs (the
	// _document_id is unique across them).
	AuditNamespace = "github.audit"
	// AuditPerPage is the documented maximum page size.
	AuditPerPage = 100
	// AuditContract is recorded on every run of an audit log surface.
	AuditContract = "github.audit/v1 api=" + APIVersion
)

// auditSpec is one row of the audit surface table.
type auditSpec struct {
	surface  vs.SurfaceID
	param    string
	prefix   string
	re       *regexp.Regexp
	supplies []vs.DataClass
	// pages reports that the endpoint also takes a page number (the
	// enterprise audit log does; the org audit log pages by cursor only).
	pages bool
}

var auditSurfaces = []auditSpec{
	{SurfaceAuditOrg, "org", "/orgs/", orgLoginRE, []vs.DataClass{vs.ClassAudit}, false},
	{SurfaceAuditEnt, "enterprise", "/enterprises/", enterpriseRE, []vs.DataClass{vs.ClassAudit}, true},
}

// AuditLookback is the audit log's documented retention (180 days).
const AuditLookback = 180 * 24 * time.Hour

// AuditLogs returns the driver constructors of the audit log surfaces.
//
// An audit entry carries action-specific members, some of them
// credential-adjacent (hashed tokens, token scopes), so the surface is
// classified may_return_secret and the driver reads a closed set of members
// only (auditDetailKeys); every other member is never decoded (plan 4.5
// R-E-06).
func AuditLogs() map[vs.SurfaceID]func() vendorapi.Driver {
	out := map[vs.SurfaceID]func() vendorapi.Driver{}
	for _, a := range auditSurfaces {
		a := a
		out[a.surface] = func() vendorapi.Driver {
			return vendorapi.Driver{
				Surface: a.surface,
				Build: func(p vendorapi.Params, w vendorapi.Window, c string) (vendorapi.Request, error) {
					return buildAudit(a, p, w, c)
				},
				ParseResponse: func(r vendorapi.Response) (vendorapi.Page, error) { return parseAudit(a, r) },
				Canary:        canaryAudit,
				Auth:          vendorapi.AuthContract{Kind: vs.AuthBearer, Scheme: vendorapi.SchemeBearer, Header: "Authorization"},
				Headers: map[string]string{
					"Accept":               "application/vnd.github+json",
					"X-GitHub-Api-Version": APIVersion,
				},
				ResponseHeaders: []string{"link"},
				ContentTypes:    []string{"application/json"},
				Supplies:        a.supplies,
				ClosedDTO:       true,
				MaxLookback:     AuditLookback,
				ContractVersion: AuditContract,
				Tool:            Tool,
				KnownPaths:      knownPaths[a.surface],
			}
		}
	}
	return out
}

// afterRE is the shape of GitHub's opaque audit cursor (base64 text).
var afterRE = regexp.MustCompile(`^[A-Za-z0-9+/=_-]{1,512}$`)

func buildAudit(a auditSpec, p vendorapi.Params, w vendorapi.Window, cursor string) (vendorapi.Request, error) {
	owner := p[a.param]
	if !a.re.MatchString(owner) {
		return vendorapi.Request{}, fmt.Errorf("%s: param %s must be a GitHub %s name", a.surface, a.param, a.param)
	}
	if w.From.IsZero() || !w.To.After(w.From) {
		return vendorapi.Request{}, fmt.Errorf("%s: a non-empty window is required", a.surface)
	}
	q := map[string][]string{
		"phrase":   {"created:" + w.From.UTC().Format(time.RFC3339) + ".." + w.To.UTC().Format(time.RFC3339)},
		"per_page": {strconv.Itoa(AuditPerPage)},
		"order":    {"asc"},
	}
	switch kind, v, _ := strings.Cut(cursor, ":"); {
	case cursor == "":
	case kind == "page" && a.pages:
		if n, err := strconv.Atoi(v); err != nil || n < 2 || strconv.Itoa(n) != v {
			return vendorapi.Request{}, fmt.Errorf("%s: page cursor must be a page number", a.surface)
		}
		q["page"] = []string{v}
	case kind == "after" && afterRE.MatchString(v):
		q["after"] = []string{v}
	default:
		return vendorapi.Request{}, fmt.Errorf("%s: unknown cursor", a.surface)
	}
	return vendorapi.Request{Method: "GET", Path: a.prefix + owner + "/audit-log", Query: q}, nil
}

// auditNext reads the rel="next" link: same origin only, and either a page
// number or the after cursor (GitHub documents cursor paging for the audit
// log); anything else fails the page.
func auditNext(a auditSpec, r vendorapi.Response) (string, error) {
	link := nextLink(r.Header["link"])
	if link == "" {
		return "", nil
	}
	u, err := url.Parse(link)
	if err != nil || u.User != nil || u.Host == "" {
		return "", fmt.Errorf("%w: next link is not an absolute URL", vendorapi.ErrSchemaDrift)
	}
	want, err := url.Parse(r.Origin)
	if err != nil || origin(u) != origin(want) {
		return "", fmt.Errorf("%w: next link points at another origin", vendorapi.ErrSchemaDrift)
	}
	if after := u.Query().Get("after"); after != "" {
		if !afterRE.MatchString(after) {
			return "", fmt.Errorf("%w: next link carries an unreadable cursor", vendorapi.ErrSchemaDrift)
		}
		return "after:" + after, nil
	}
	page := u.Query().Get("page")
	if n, err := strconv.Atoi(page); err != nil || n < 2 || strconv.Itoa(n) != page || !a.pages {
		return "", fmt.Errorf("%w: next link carries no cursor this endpoint pages by", vendorapi.ErrSchemaDrift)
	}
	return "page:" + page, nil
}

// auditEntry is the closed DTO of one audit entry: only these members are
// decoded.
type auditEntry struct {
	Timestamp  *json.Number
	DocumentID *string
	Action     *string
	Actor      *string
}

// auditDetailKeys is the closed set of entry members landed as detail (the
// documented members of the reference examples). Every other member is
// never decoded.
var auditDetailKeys = []string{"org", "repo", "team", "user", "visibility", "business", "operation_type"}

var auditActionRE = regexp.MustCompile(`^[a-z][a-z0-9_.:-]{0,127}$`)

func auditDrift(format string, a ...any) error {
	return fmt.Errorf("%w: github.audit: "+format, append([]any{vendorapi.ErrSchemaDrift}, a...)...)
}

func parseAudit(a auditSpec, r vendorapi.Response) (vendorapi.Page, error) {
	if r.Status != 200 {
		return vendorapi.Page{}, auditDrift("status %d is not the documented 200", r.Status)
	}
	var raw []map[string]json.RawMessage
	if err := json.Unmarshal(r.Body, &raw); err != nil || raw == nil {
		return vendorapi.Page{}, auditDrift("body is not the documented array of entries")
	}
	next, err := auditNext(a, r)
	if err != nil {
		return vendorapi.Page{}, err
	}
	page := vendorapi.Page{NextCursor: next}
	seen := map[string]bool{}
	for i, m := range raw {
		var e auditEntry
		for k, dst := range map[string]any{"@timestamp": &e.Timestamp, "_document_id": &e.DocumentID, "action": &e.Action, "actor": &e.Actor} {
			if v, ok := m[k]; ok {
				dec := json.NewDecoder(bytes.NewReader(v))
				dec.UseNumber()
				if err := dec.Decode(dst); err != nil {
					return vendorapi.Page{}, auditDrift("entry %d member %s has the wrong type", i, k)
				}
			}
		}
		if e.DocumentID == nil || *e.DocumentID == "" || e.Timestamp == nil || e.Action == nil {
			return vendorapi.Page{}, auditDrift("entry %d misses _document_id, @timestamp or action", i)
		}
		ms, err := e.Timestamp.Int64()
		if err != nil || ms <= 0 {
			return vendorapi.Page{}, auditDrift("entry %d @timestamp is not epoch milliseconds", i)
		}
		if seen[*e.DocumentID] {
			return vendorapi.Page{}, auditDrift("entry %d repeats a document id", i)
		}
		seen[*e.DocumentID] = true
		action := strings.ToLower(strings.TrimSpace(*e.Action))
		if !auditActionRE.MatchString(action) {
			action = "other"
		}
		ev := vendorapi.AuditEvent{
			Namespace: AuditNamespace, EventID: *e.DocumentID, At: time.UnixMilli(ms).UTC(), Action: action,
			Detail: map[string]string{},
		}
		if e.Actor != nil && *e.Actor != "" {
			ev.ActorKind, ev.ActorIdentity = "login", *e.Actor
		}
		for _, k := range auditDetailKeys {
			v, ok := m[k]
			if !ok {
				continue
			}
			var s string
			if err := json.Unmarshal(v, &s); err == nil && s != "" {
				ev.Detail[k] = s
			}
		}
		switch {
		case ev.Detail["user"] != "":
			ev.Target = ev.Detail["user"]
		case ev.Detail["repo"] != "":
			ev.Target = ev.Detail["repo"]
		case ev.Detail["team"] != "":
			ev.Target = ev.Detail["team"]
		}
		page.Audit = append(page.Audit, ev)
	}
	return page, nil
}

// canaryAudit reports an action outside the action vocabulary (landed as
// "other").
func canaryAudit(p vendorapi.Page) []vendorapi.DriftSignal {
	for _, e := range p.Audit {
		if e.Action == "other" {
			return []vendorapi.DriftSignal{{Path: "[].action", Kind: "unknown_enum"}}
		}
	}
	return nil
}
