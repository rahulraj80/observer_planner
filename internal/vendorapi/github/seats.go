package github

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/marmutapp/superbased-observer/internal/vendorapi"
	vs "github.com/marmutapp/superbased-observer/internal/vendorsurface"
)

// Surface ids and graph vocabulary of the org seats driver.
const (
	SurfaceOrgSeats vs.SurfaceID = "github.copilot.org_seats"

	// SeatType is the namespaced resource type of one Copilot seat.
	SeatType vs.ResourceType = "Seat/github.copilot_seat/v1"
	// NamespaceSeat keys a seat by org login and assignee node id.
	NamespaceSeat = "github.copilot_seat"
	// NamespaceOrg keys an organization by lower-case login.
	NamespaceOrg = "github.org"
	// NamespaceUser, NamespaceTeam key an assignee by its GraphQL node id
	// (stable across renames; the numeric id is kept as an attribute).
	NamespaceUser = "github.user"
	NamespaceTeam = "github.team"

	// APIVersion is the REST API version the fixtures and this driver were
	// written against (rest-api-description ghec.2026-03-10).
	APIVersion = "2026-03-10"
	// SeatsPerPage is the documented maximum page size (per_page <= 100).
	SeatsPerPage = 100
	// SeatsContract is recorded on every manifest the driver opens.
	SeatsContract = "github.copilot.org_seats/v1 api=" + APIVersion
	// LegacySeatsKey is the ownership key of the legacy copilotanalytics
	// seats surface (decide.LegacyKey("copilotanalytics", "seats")):
	// ownership moves per legacy surface, so the poller's engagement and
	// billing surfaces keep running when seats move to the connection.
	LegacySeatsKey = "copilotanalytics.seats"
)

// orgLoginRE is a GitHub organization login: alphanumerics and single
// hyphens, at most 39 characters. Anything else is refused before it can
// reach a request path.
var orgLoginRE = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9]|-[A-Za-z0-9]){0,38}$`)

// OrgSeats returns the driver of github.copilot.org_seats.
//
// Contract (rest-api-description ghec.2026-03-10, operation
// copilot/list-copilot-seats): GET /orgs/{org}/copilot/billing/seats with
// page and per_page (<= 100), Authorization: Bearer with a token holding
// manage_billing:copilot or read:org, Link-header pagination. The legacy
// copilotanalytics seats poller reads the same endpoint, so the runner runs
// this surface only after that poller's ownership moved to the connection.
func OrgSeats() vendorapi.Driver {
	return vendorapi.Driver{
		Surface:       SurfaceOrgSeats,
		Build:         buildSeats,
		ParseResponse: parseSeats,
		Canary:        canarySeats,
		Auth:          vendorapi.AuthContract{Kind: vs.AuthBearer, Scheme: vendorapi.SchemeBearer, Header: "Authorization"},
		Headers: map[string]string{
			"Accept":               "application/vnd.github+json",
			"X-GitHub-Api-Version": APIVersion,
		},
		ResponseHeaders:       []string{"link"},
		ContentTypes:          []string{"application/json"},
		Supplies:              []vs.DataClass{vs.ClassSeats, vs.ClassAdoption},
		AuthoritativeSnapshot: true,
		LegacyPoller:          LegacySeatsKey,
		ClosedDTO:             false,
		ContractVersion:       SeatsContract,
		KnownPaths:            seatsKnownPaths,
	}
}

func buildSeats(p vendorapi.Params, _ vendorapi.Window, cursor string) (vendorapi.Request, error) {
	org := p["org"]
	if !orgLoginRE.MatchString(org) {
		return vendorapi.Request{}, fmt.Errorf("github.org_seats: param org must be an organization login")
	}
	q := map[string][]string{"per_page": {strconv.Itoa(SeatsPerPage)}}
	if cursor != "" {
		if n, err := strconv.Atoi(cursor); err != nil || n < 2 || strconv.Itoa(n) != cursor {
			return vendorapi.Request{}, fmt.Errorf("github.org_seats: cursor must be a page number")
		}
		q["page"] = []string{cursor}
	}
	return vendorapi.Request{Method: "GET", Path: "/orgs/" + org + "/copilot/billing/seats", Query: q}, nil
}

// seatsPage and the types below decode only the declared fields; unknown
// members are ignored (the field-policy matrix parses unknown properties
// leniently, plan 12A.5). Pointers distinguish absent from empty so a
// missing required field is drift, never a zero.
type seatsPage struct {
	TotalSeats *int64  `json:"total_seats"`
	Seats      *[]seat `json:"seats"`
}

type seat struct {
	CreatedAt               *string   `json:"created_at"`
	UpdatedAt               *string   `json:"updated_at"`
	PendingCancellationDate *string   `json:"pending_cancellation_date"`
	LastActivityAt          *string   `json:"last_activity_at"`
	LastActivityEditor      *string   `json:"last_activity_editor"`
	LastAuthenticatedAt     *string   `json:"last_authenticated_at"`
	PlanType                *string   `json:"plan_type"`
	Assignee                *assignee `json:"assignee"`
	AssigningTeam           *team     `json:"assigning_team"`
}

// assignee is the oneOf of a simple user, a team or an organization.
type assignee struct {
	Login  string      `json:"login"`
	Slug   string      `json:"slug"`
	ID     json.Number `json:"id"`
	NodeID string      `json:"node_id"`
	Type   string      `json:"type"`
}

type team struct {
	Slug   string `json:"slug"`
	NodeID string `json:"node_id"`
}

// assigneeKind is the closed table from the assignee shape to the graph
// node it is licensed to. Rows are walked top down; an unknown shape maps to
// "other" (no edge), never a guess.
var assigneeKinds = []struct {
	match func(a assignee) bool
	label string
	kind  vs.ResourceKind
	ns    string
}{
	{func(a assignee) bool { return a.Type == "User" || a.Type == "Bot" }, "user", vs.KindUser, NamespaceUser},
	{func(a assignee) bool { return a.Type == "Organization" }, "organization", vs.KindOrg, NamespaceOrg},
	{func(a assignee) bool { return a.Type == "" && a.Slug != "" }, "team", vs.KindGroup, NamespaceTeam},
}

func classify(a assignee) (label string, kind vs.ResourceKind, ns string) {
	for _, r := range assigneeKinds {
		if r.match(a) {
			return r.label, r.kind, r.ns
		}
	}
	return "other", "", ""
}

func drift(format string, a ...any) error {
	return fmt.Errorf("%w: github.org_seats: "+format, append([]any{vendorapi.ErrSchemaDrift}, a...)...)
}

func setAttr(m map[string]string, k string, v *string) {
	if v != nil && *v != "" {
		m[k] = *v
	}
}

// SelectorDigest is the manifest selector of one org's seat snapshot.
func SelectorDigest(org string) string {
	sum := sha256.Sum256([]byte("org=" + strings.ToLower(org)))
	return hex.EncodeToString(sum[:8])
}

func parseSeats(r vendorapi.Response) (vendorapi.Page, error) {
	org := strings.ToLower(r.Params["org"])
	if !orgLoginRE.MatchString(org) {
		return vendorapi.Page{}, fmt.Errorf("github.org_seats: response without its org param")
	}
	if r.Status != 200 {
		return vendorapi.Page{}, drift("status %d is not the documented 200", r.Status)
	}
	var doc seatsPage
	dec := json.NewDecoder(bytes.NewReader(r.Body))
	dec.UseNumber()
	if err := dec.Decode(&doc); err != nil {
		return vendorapi.Page{}, drift("body does not decode as the documented object")
	}
	if doc.Seats == nil || doc.TotalSeats == nil {
		return vendorapi.Page{}, drift("required field seats or total_seats is missing")
	}
	next, err := nextPage(r)
	if err != nil {
		return vendorapi.Page{}, err
	}
	orgRef := vendorapi.NodeRef{Kind: vs.KindOrg, ID: vs.ExternalID{Namespace: NamespaceOrg, Value: org}}
	page := vendorapi.Page{
		NextCursor: next,
		Snapshot: &vendorapi.SnapshotManifest{
			Scope:           []vendorapi.ScopeKey{{Namespace: NamespaceSeat, Kind: vs.KindSeat}},
			ScopeParent:     &orgRef,
			ScopeEdges:      []vs.EdgeKind{vs.EdgeLicensedTo},
			SelectorDigest:  SelectorDigest(org),
			PageSet:         "copilot_billing_seats",
			ContractVersion: SeatsContract,
		},
	}
	seen := map[string]bool{}
	for i, s := range *doc.Seats {
		if s.Assignee == nil || s.Assignee.NodeID == "" {
			return vendorapi.Page{}, drift("seats[%d].assignee.node_id is missing", i)
		}
		a := *s.Assignee
		if seen[a.NodeID] {
			return vendorapi.Page{}, drift("seats[%d] repeats an assignee", i)
		}
		seen[a.NodeID] = true
		label, kind, ns := classify(a)
		attrs := map[string]string{"assignee_type": label, "assignee_node_id": a.NodeID}
		if a.ID != "" {
			attrs["assignee_id"] = a.ID.String()
		}
		name := a.Login
		if name == "" {
			name = a.Slug
		}
		if name != "" {
			attrs["assignee_login"] = name
		}
		setAttr(attrs, "plan_type", s.PlanType)
		setAttr(attrs, "created_at", s.CreatedAt)
		setAttr(attrs, "updated_at", s.UpdatedAt)
		setAttr(attrs, "pending_cancellation_date", s.PendingCancellationDate)
		setAttr(attrs, "last_activity_at", s.LastActivityAt)
		setAttr(attrs, "last_activity_editor", s.LastActivityEditor)
		setAttr(attrs, "last_authenticated_at", s.LastAuthenticatedAt)
		if t := s.AssigningTeam; t != nil {
			if t.Slug != "" {
				attrs["assigning_team_slug"] = t.Slug
			}
			if t.NodeID != "" {
				attrs["assigning_team_node_id"] = t.NodeID
			}
		}
		parent := orgRef
		seatRef := vendorapi.NodeRef{Kind: vs.KindSeat, ID: vs.ExternalID{Namespace: NamespaceSeat, Value: org + ":" + a.NodeID}}
		page.Resources = append(page.Resources, vendorapi.ResourceObservation{
			Type: SeatType, ID: seatRef.ID, Parent: &parent, DisplayName: name, Attrs: attrs,
		})
		if kind != "" {
			page.Edges = append(page.Edges, vendorapi.EdgeObservation{
				Edge: vs.EdgeLicensedTo, From: seatRef,
				To: vendorapi.NodeRef{Kind: kind, ID: vs.ExternalID{Namespace: ns, Value: a.NodeID}},
			})
		}
	}
	return page, nil
}

// canarySeats reports drift that does not fail the page: an assignee shape
// outside the closed table (landed as "other", plan 12A.5). Paths are schema
// paths, never values.
func canarySeats(p vendorapi.Page) []vendorapi.DriftSignal {
	var out []vendorapi.DriftSignal
	other := false
	for _, r := range p.Resources {
		if r.Attrs["assignee_type"] == "other" {
			other = true
		}
	}
	if other {
		out = append(out, vendorapi.DriftSignal{Path: "seats[].assignee", Kind: "unknown_enum"})
	}
	return out
}

// seatsKnownPaths are the documented response paths (the corpus field
// ledger of ghec.2026-03-10 copilot/list-copilot-seats).
var seatsKnownPaths = []string{
	"seats", "seats[]", "seats[].assignee", "seats[].assignee.avatar_url", "seats[].assignee.events_url",
	"seats[].assignee.followers_url", "seats[].assignee.following_url", "seats[].assignee.gists_url",
	"seats[].assignee.gravatar_id", "seats[].assignee.html_url", "seats[].assignee.id", "seats[].assignee.login",
	"seats[].assignee.node_id", "seats[].assignee.organizations_url", "seats[].assignee.received_events_url",
	"seats[].assignee.repos_url", "seats[].assignee.site_admin", "seats[].assignee.starred_url",
	"seats[].assignee.subscriptions_url", "seats[].assignee.type", "seats[].assignee.url", "seats[].assigning_team",
	"seats[].assigning_team.description", "seats[].assigning_team.html_url", "seats[].assigning_team.id",
	"seats[].assigning_team.members_url", "seats[].assigning_team.name", "seats[].assigning_team.node_id",
	"seats[].assigning_team.notification_setting", "seats[].assigning_team.parent", "seats[].assigning_team.permission",
	"seats[].assigning_team.privacy", "seats[].assigning_team.repositories_url", "seats[].assigning_team.slug",
	"seats[].assigning_team.url", "seats[].created_at", "seats[].last_activity_at", "seats[].last_activity_editor",
	"seats[].last_authenticated_at", "seats[].pending_cancellation_date", "seats[].plan_type", "seats[].updated_at",
	"total_seats",
}
