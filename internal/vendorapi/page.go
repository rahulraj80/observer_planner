package vendorapi

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/marmutapp/superbased-observer/internal/vendorsurface"
)

// Params are a connection's validated per-surface parameters (selectors,
// project ids, report names). Credentials are never here: the egress seam
// injects them per AuthSpec at request time (plan 6.2).
type Params map[string]string

// Window is a half-open fetch window [From, To).
type Window struct {
	From time.Time
	To   time.Time
}

// Request is what a driver's Build returns: a method, a path relative to the
// surface's declared host tuple and a query. It carries no header and no
// credential; the egress seam adds both.
//
// Body is set only for a reviewed read-only POST (lane ENTAPI-D3, T-692): it
// is one of the closed body types of post.go, never caller-supplied bytes,
// and the egress seam sends it only when its closed POST table lists the
// surface, the path and the body kind.
type Request struct {
	Method string
	Path   string
	Query  map[string][]string
	Body   PostBody
}

// DriftSignal is one canary finding (a required field missing, an unknown
// shape). Path is a schema property path with dynamic keys wildcarded; a raw
// unexpected field name is never carried (plan 12A.7).
type DriftSignal struct {
	Path string
	Kind string
}

// Page is one parsed response. Graph surfaces fill Snapshot, Resources and
// Edges; activity surfaces fill the activity landings (lane F11, activity.go:
// audit events, per-call usage, aggregate facts, snapshots, retractions), so
// a graph driver and an activity driver share one return type. A content
// surface fills Content (lane L15, content.go), which lands only under a
// recorded per-connection content consent.
type Page struct {
	NextCursor string
	Truncated  bool
	Snapshot   *SnapshotManifest
	Resources  []ResourceObservation
	Edges      []EdgeObservation

	Audit       []AuditEvent
	Usage       []UsageRow
	Facts       []FactRow
	Snapshots   []VendorSnapshot
	Retractions []Retraction

	// Content holds vendor content items (prompts, responses, transcripts):
	// the sanitizer drops them unless the connection's content policy is on.
	Content []ContentItem

	// SourceRecords is how many records of the vendor's record container
	// the page parsed, set by a driver whose landings are not one row per
	// record (a report bucket becomes several facts, lane ENTAPI-D1; a
	// report file line becomes several facts, lane L8). Zero means one
	// landing per record. Only the conformance suite reads it; nothing lands
	// it.
	SourceRecords int
}

// NodeRef names one resource of the graph by superkind and namespaced id.
type NodeRef struct {
	Kind vendorsurface.ResourceKind `json:"kind"`
	ID   vendorsurface.ExternalID   `json:"id"`
}

// ScopeKey is one (namespace, superkind) pair a snapshot covers.
type ScopeKey struct {
	Namespace string                     `json:"namespace"`
	Kind      vendorsurface.ResourceKind `json:"kind"`
}

// SnapshotManifest describes one graph run's page set (plan 4.5 R-E-05). Only
// a Complete and AuthoritativeForDeletion snapshot may tombstone resources,
// and only inside Scope (narrowed to children of ScopeParent when set) and
// only edges whose kind is in ScopeEdges.
type SnapshotManifest struct {
	ManifestID               string
	Scope                    []ScopeKey
	ScopeParent              *NodeRef
	ScopeEdges               []vendorsurface.EdgeKind
	SelectorDigest           string
	PageSet                  string
	ContractVersion          string
	Complete                 bool
	AuthoritativeForDeletion bool
	ObservedAt               time.Time
}

// ResourceObservation is one resource as one page saw it. Attrs is the
// sanitized allowlist for the kind: flat strings only, never a nested body.
// FieldAuthority marks, per attribute name, whether this surface is the
// authority for that field ("authoritative") or only reports it
// ("advisory"); an absent entry is advisory.
type ResourceObservation struct {
	Type           vendorsurface.ResourceType
	ID             vendorsurface.ExternalID
	ContainerKind  vendorsurface.ContainerKind
	Parent         *NodeRef
	DisplayName    string
	Attrs          map[string]string
	FieldAuthority map[string]string
}

// Kind returns the superkind of the observation's type ("" when the type does
// not parse; Validate rejects that).
func (r ResourceObservation) Kind() vendorsurface.ResourceKind {
	k, _, err := vendorsurface.ParseResourceType(r.Type)
	if err != nil {
		return ""
	}
	return k
}

// EdgeObservation is one typed, directional edge as one page saw it, with an
// optional validity interval.
type EdgeObservation struct {
	Edge       vendorsurface.EdgeKind
	From       NodeRef
	To         NodeRef
	Attrs      map[string]string
	ValidFrom  *time.Time
	ValidUntil *time.Time
}

// Limits on what one observation may carry.
const (
	MaxDisplayName = 256
	MaxAttrs       = 64
	MaxAttrValue   = 512
)

var attrKeyRE = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

// credentialAttrs is the closed attribute allowlist of a Credential resource
// (plan 4.0: id, name, redacted hint, owner, scopes, created, last used,
// expiry, state; NEVER a value). The id and name travel as ID and DisplayName.
var credentialAttrs = map[string]bool{
	"redacted_hint": true, "owner": true, "scopes": true, "created": true,
	"last_used": true, "expires": true, "state": true, "credential_type": true,
}

// secretShaped are attribute-name fragments no resource of any kind may carry:
// a value under such a name is a secret or a secret's material.
var secretShaped = []string{"secret", "password", "passwd", "token", "private_key", "api_key", "apikey", "credential_value"}

var edgeKinds = map[vendorsurface.EdgeKind]bool{
	vendorsurface.EdgeContains: true, vendorsurface.EdgeMemberOf: true, vendorsurface.EdgeAssignedRole: true,
	vendorsurface.EdgeLicensedTo: true, vendorsurface.EdgeInvitedTo: true, vendorsurface.EdgeOwnsCredential: true,
	vendorsurface.EdgeTargets: true, vendorsurface.EdgeInheritsFrom: true, vendorsurface.EdgeSyncedFrom: true,
	vendorsurface.EdgeBilledThrough: true, vendorsurface.EdgeGovernedBy: true,
}

var containerKinds = map[vendorsurface.ContainerKind]bool{
	vendorsurface.ContainerWorkspace: true, vendorsurface.ContainerProject: true, vendorsurface.ContainerTeam: true,
	vendorsurface.ContainerBillingGroup: true, vendorsurface.ContainerCostCenter: true,
	vendorsurface.ContainerNamespace: true, vendorsurface.ContainerRepository: true,
}

var superkinds = map[vendorsurface.ResourceKind]bool{
	vendorsurface.KindOrg: true, vendorsurface.KindContainer: true, vendorsurface.KindGroup: true,
	vendorsurface.KindUser: true, vendorsurface.KindInvite: true, vendorsurface.KindSeat: true,
	vendorsurface.KindServiceAccount: true, vendorsurface.KindCredential: true, vendorsurface.KindRole: true,
	vendorsurface.KindPolicy: true, vendorsurface.KindLimit: true,
}

// ErrInvalidPage wraps every Validate refusal.
var ErrInvalidPage = errors.New("vendorapi: invalid page")

func validNode(n NodeRef) bool { return superkinds[n.Kind] && n.ID.Valid() }

// resourceRule is one row of the per-observation rule table, walked top down;
// the first failing row names the refusal.
type resourceRule struct {
	name string
	ok   func(ResourceObservation) bool
}

var resourceRules = []resourceRule{
	{"resource type is Kind/vendor.subtype/vN", func(r ResourceObservation) bool { return r.Kind() != "" }},
	{"external id is namespaced", func(r ResourceObservation) bool { return r.ID.Valid() }},
	{"container kind set exactly on containers", func(r ResourceObservation) bool {
		if r.Kind() == vendorsurface.KindContainer {
			return containerKinds[r.ContainerKind]
		}
		return r.ContainerKind == ""
	}},
	{"parent is a typed namespaced reference", func(r ResourceObservation) bool { return r.Parent == nil || validNode(*r.Parent) }},
	{"display name within limit", func(r ResourceObservation) bool { return len(r.DisplayName) <= MaxDisplayName }},
	{"attributes within limits", func(r ResourceObservation) bool { return attrsWithinLimits(r.Attrs) }},
	{"no secret-shaped attribute", func(r ResourceObservation) bool { return !hasSecretShapedAttr(r.Attrs) }},
	{"credential carries metadata only", func(r ResourceObservation) bool {
		if r.Kind() != vendorsurface.KindCredential {
			return true
		}
		for k := range r.Attrs {
			if !credentialAttrs[k] {
				return false
			}
		}
		return true
	}},
	{"field authority names known attributes", func(r ResourceObservation) bool {
		for k, v := range r.FieldAuthority {
			if v != "authoritative" && v != "advisory" {
				return false
			}
			if _, ok := r.Attrs[k]; !ok && k != "display_name" && k != "parent" {
				return false
			}
		}
		return true
	}},
}

func attrsWithinLimits(attrs map[string]string) bool {
	if len(attrs) > MaxAttrs {
		return false
	}
	for k, v := range attrs {
		if !attrKeyRE.MatchString(k) || len(v) > MaxAttrValue {
			return false
		}
	}
	return true
}

func hasSecretShapedAttr(attrs map[string]string) bool {
	for k := range attrs {
		for _, frag := range secretShaped {
			if strings.Contains(k, frag) {
				return true
			}
		}
	}
	return false
}

// Validate is the structural gate a page passes before landing. It returns
// nil for an empty page and an ErrInvalidPage-wrapped error naming the first
// failing rule otherwise.
func Validate(p Page) error {
	if err := ValidateActivity(p); err != nil {
		return err
	}
	if err := ValidateContent(p); err != nil {
		return err
	}
	if len(p.Resources) == 0 && len(p.Edges) == 0 {
		return nil
	}
	m := p.Snapshot
	if m == nil {
		return fmt.Errorf("%w: graph observations need a snapshot manifest", ErrInvalidPage)
	}
	if err := validateManifest(*m); err != nil {
		return err
	}
	inScope := map[ScopeKey]bool{}
	for _, s := range m.Scope {
		inScope[s] = true
	}
	edgeScope := map[vendorsurface.EdgeKind]bool{}
	for _, e := range m.ScopeEdges {
		edgeScope[e] = true
	}
	seen := map[NodeRef]bool{}
	for i, r := range p.Resources {
		for _, rule := range resourceRules {
			if !rule.ok(r) {
				return fmt.Errorf("%w: resource %d: %s", ErrInvalidPage, i, rule.name)
			}
		}
		key := ScopeKey{Namespace: r.ID.Namespace, Kind: r.Kind()}
		if !inScope[key] {
			return fmt.Errorf("%w: resource %d (%s %s) is outside the manifest scope", ErrInvalidPage, i, key.Kind, key.Namespace)
		}
		if m.ScopeParent != nil && (r.Parent == nil || *r.Parent != *m.ScopeParent) {
			return fmt.Errorf("%w: resource %d is not a child of the manifest's scope parent", ErrInvalidPage, i)
		}
		n := NodeRef{Kind: r.Kind(), ID: r.ID}
		if seen[n] {
			return fmt.Errorf("%w: resource %d repeats %s %s/%s", ErrInvalidPage, i, n.Kind, n.ID.Namespace, n.ID.Value)
		}
		seen[n] = true
	}
	type edgeKey struct {
		e        vendorsurface.EdgeKind
		from, to NodeRef
	}
	seenEdge := map[edgeKey]bool{}
	for i, e := range p.Edges {
		switch {
		case !edgeKinds[e.Edge]:
			return fmt.Errorf("%w: edge %d: unknown edge kind %q", ErrInvalidPage, i, e.Edge)
		case !edgeScope[e.Edge]:
			return fmt.Errorf("%w: edge %d: kind %q is not declared in the manifest", ErrInvalidPage, i, e.Edge)
		case !validNode(e.From) || !validNode(e.To):
			return fmt.Errorf("%w: edge %d: endpoints must be typed namespaced references", ErrInvalidPage, i)
		case !inScope[ScopeKey{Namespace: e.From.ID.Namespace, Kind: e.From.Kind}]:
			return fmt.Errorf("%w: edge %d: source is outside the manifest scope", ErrInvalidPage, i)
		case !attrsWithinLimits(e.Attrs) || hasSecretShapedAttr(e.Attrs):
			return fmt.Errorf("%w: edge %d: attributes", ErrInvalidPage, i)
		case e.ValidFrom != nil && e.ValidUntil != nil && !e.ValidUntil.After(*e.ValidFrom):
			return fmt.Errorf("%w: edge %d: empty validity interval", ErrInvalidPage, i)
		}
		k := edgeKey{e.Edge, e.From, e.To}
		if seenEdge[k] {
			return fmt.Errorf("%w: edge %d repeats", ErrInvalidPage, i)
		}
		seenEdge[k] = true
	}
	return nil
}

func validateManifest(m SnapshotManifest) error {
	switch {
	case strings.TrimSpace(m.ManifestID) == "":
		return fmt.Errorf("%w: manifest id is required", ErrInvalidPage)
	case len(m.Scope) == 0:
		return fmt.Errorf("%w: manifest scope is empty", ErrInvalidPage)
	case m.AuthoritativeForDeletion && !m.Complete:
		return fmt.Errorf("%w: only a complete snapshot may be authoritative for deletion", ErrInvalidPage)
	case m.ObservedAt.IsZero():
		return fmt.Errorf("%w: manifest observed_at is required", ErrInvalidPage)
	case m.ScopeParent != nil && !validNode(*m.ScopeParent):
		return fmt.Errorf("%w: manifest scope parent must be a typed namespaced reference", ErrInvalidPage)
	}
	for _, s := range m.Scope {
		if !superkinds[s.Kind] || !(vendorsurface.ExternalID{Namespace: s.Namespace, Value: "x"}).Valid() {
			return fmt.Errorf("%w: manifest scope entry %q/%q", ErrInvalidPage, s.Kind, s.Namespace)
		}
	}
	for _, e := range m.ScopeEdges {
		if !edgeKinds[e] {
			return fmt.Errorf("%w: manifest scope edge %q", ErrInvalidPage, e)
		}
	}
	return nil
}

// ScopeString renders a manifest scope canonically (sorted kind@namespace,
// comma separated), the form the store records.
func ScopeString(m SnapshotManifest) string {
	parts := make([]string, 0, len(m.Scope))
	for _, s := range m.Scope {
		parts = append(parts, string(s.Kind)+"@"+s.Namespace)
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}

// EdgeScopeString renders the declared edge kinds canonically.
func EdgeScopeString(m SnapshotManifest) string {
	parts := make([]string, 0, len(m.ScopeEdges))
	for _, e := range m.ScopeEdges {
		parts = append(parts, string(e))
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}

// MayTombstone reports whether a snapshot may mark resources absent from it as
// deleted (plan 4.5 R-E-05): only a complete snapshot that is authoritative
// for deletion. Every other snapshot only adds and refreshes.
func MayTombstone(m SnapshotManifest) bool { return m.Complete && m.AuthoritativeForDeletion }
