package vendorsanitize

import (
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/marmutapp/superbased-observer/internal/scrub"
	"github.com/marmutapp/superbased-observer/internal/vendorapi"
	vs "github.com/marmutapp/superbased-observer/internal/vendorsurface"
)

// ErrRefused wraps every page-level refusal.
var ErrRefused = errors.New("vendorsanitize: refused")

// Report counts what the boundary changed. It never names a field or a
// value.
//
// Quarantined counts whole rows (a resource, an edge, an audit event, a
// usage row, a fact, a snapshot or a retraction) dropped because a value
// that KEYS the row (an id, a join value, an identity, a dimension) carries
// secret or content material or exceeds the limit vendorapi.Validate
// enforces. Such a value cannot be redacted without changing what the row
// means, and before VCONN-5 (T-695) it refused the whole page, so one vendor
// value (a 300-character name, a key named like a secret) stalled the
// surface for every run. QuarantinedGraph counts the resources and edges
// among them: a page set with any is never closed authoritative for
// deletion, because the dropped resource would read as gone.
//
// ContentDropped counts content items dropped because the caller's policy
// allows no content (lane L15); ContentTruncated counts content texts cut at
// vendorapi.MaxContentBytes.
type Report struct {
	DroppedFields    int
	DroppedValues    int
	Redacted         int
	Quarantined      int
	QuarantinedGraph int
	ContentDropped   int
	ContentTruncated int
}

// Add sums two reports.
func (r Report) Add(o Report) Report {
	return Report{
		DroppedFields:    r.DroppedFields + o.DroppedFields,
		DroppedValues:    r.DroppedValues + o.DroppedValues,
		Redacted:         r.Redacted + o.Redacted,
		Quarantined:      r.Quarantined + o.Quarantined,
		QuarantinedGraph: r.QuarantinedGraph + o.QuarantinedGraph,
		ContentDropped:   r.ContentDropped + o.ContentDropped,
		ContentTruncated: r.ContentTruncated + o.ContentTruncated,
	}
}

// quarantine counts one dropped row.
func (r *Report) quarantine(graph bool) {
	r.Quarantined++
	if graph {
		r.QuarantinedGraph++
	}
}

// maxWords is the word count above which a non-content value is treated as
// prose and dropped.
const maxWords = 24

// resourceAttrs is the per-landing-type attribute allowlist. A row is added
// with the driver that lands the type, reviewed against the vendor's
// documented fields.
var resourceAttrs = map[vs.ResourceType][]string{
	// github.copilot.org_seats (lane F8): seat metadata and the assignee's
	// identity, never a token or an email.
	"Seat/github.copilot_seat/v1": {
		"assignee_type", "assignee_node_id", "assignee_id", "assignee_login", "plan_type", "created_at",
		"updated_at", "pending_cancellation_date", "last_activity_at", "last_activity_editor",
		"last_authenticated_at", "assigning_team_slug", "assigning_team_node_id",
	},
	// cursor.admin.members (lane F11, T-608): the documented member fields
	// (email, role, removed flag); the name travels as the display name.
	"User/cursor.member/v1": {"email", "role", "is_removed"},
	// anthropic.console.users (lane ENTAPI-D1, T-609): the documented "List
	// Users" fields (email, role, account type, added time); the name travels
	// as the display name. Never a key.
	"User/anthropic.console_user/v1": {"email", "role", "type", "added_at"},
	// openai.platform.users (lane ENTAPI-D1, T-610): the documented
	// organization_user fields; timestamps rendered RFC 3339. Never a key: the
	// key last-used time (api_key_last_used_at, renamed key_last_used_at: an
	// attribute name containing api_key is refused as secret-shaped) is a time.
	"User/openai.platform_user/v1": {
		"email", "role", "added_at", "is_service_account", "is_scim_managed", "key_last_used_at",
	},
	// cognition.devin_v3.ent_users (lane ENTAPI-L16, T-619): the documented
	// EnterpriseUser fields reduced to flat values (email, the enterprise-level
	// role names, the count of org-level role assignments, the join time
	// RFC 3339); the name travels as the display name. Never a key.
	"User/cognition.enterprise_user/v1": {"email", "enterprise_roles", "org_role_count", "enterprise_joined_at"},
	// The Devin v3 enterprise inventory (lane ENTAPI-L16B, T-708), each the
	// documented members of its schema reduced to flat values. Organizations:
	// times RFC 3339 and the two ACU limits (absent when null); the name
	// travels as the display name.
	"Container/cognition.organization/v1": {"created_at", "updated_at", "max_session_acu_limit", "max_cycle_acu_limit"},
	// Roles: the documented role_type (enterprise or org); the name is the
	// display name.
	"Role/cognition.role/v1": {"role_type"},
	// Service users: an agent identity, never a key (the key list is another,
	// unbuilt surface): role names, the org role count, expiry and deletion
	// times.
	"ServiceAccount/cognition.service_user/v1": {"enterprise_roles", "org_role_count", "expires_at", "deleted_at"},
	// IdP groups: the name is the key and the display name; nothing else is
	// documented.
	"Group/cognition.idp_group/v1": {},
	// Devin ACU limits: the scope, its owner ids and the per-cycle limit with
	// its unit (acu, never money).
	"Limit/cognition.acu_limit/v1": {"scope", "org_id", "user_id", "cycle_acu_limit", "unit"},
	// factory.api.org_users (lane ENTAPI-L16B, T-708): the documented member
	// fields (email, creation and last sign-in times RFC 3339); first and last
	// name travel as the display name. The open metadata object is never
	// decoded, and the schema has no role.
	"User/factory.org_user/v1": {"email", "created_at", "last_sign_in_at"},
	// factory.api.limits_users: the global per-user credit limit and each
	// user override (scope, the user's email, the limit in credits, its state:
	// set, unset or inherits_global).
	"Limit/factory.credit_limit/v1": {"scope", "email", "limit", "unit", "state"},
	// qoder.oapi.members_list (lane ENTAPI-L16B, T-708): the documented member
	// fields (user id, email when present, role name, status, join and deletion
	// times RFC 3339); the name travels as the display name.
	"User/qoder.member/v1": {"user_id", "email", "role", "status", "joined_at", "deleted_at"},
	// qoder.oapi.groups (lane ENTAPI-L16C, T-710): the documented group fields
	// (source, external id and sync type of a synchronized group, whether it is
	// managed externally, member count, times RFC 3339); the name travels as
	// the display name. The description and the creator id are never read.
	// jetbrains.ideservices.users_list (lane ENTAPI-L16C): the documented user
	// object reduced to email, role, whether active and the creation time; the
	// username travels as the display name, createdBy is never read.
	"User/jetbrains.ideservices_user/v1": {"email", "role", "is_active", "created_at"},
	// jetbrains.hub.scim2 (lane ENTAPI-L16E, T-717): an RFC 7643 User reduced
	// to the primary e-mail, whether active and the meta times; the display
	// name travels as the display name. Groups, roles, entitlements, phone
	// numbers, addresses, photos, certificates and extensions are never read.
	"User/jetbrains.hub_user/v1": {"email", "active", "created_at", "last_modified_at"},
	"Group/qoder.group/v1":       {"source", "external_id", "sync_type", "is_managed", "member_count", "created_at", "updated_at"},
	// qoder.oapi.billing_groups: whether it is the built-in Unassigned group,
	// member count, the limit state (unlimited, frozen, set) and amount, the
	// selected period's usage, whether it is blocked, its unit (credits, never
	// money), the period and times. The description and creator are never read.
	"Group/qoder.billing_group/v1": {
		"is_unassigned", "member_count", "limit_state", "limit_amount", "current_used", "is_blocked", "unit",
		"period_start", "period_end", "created_at", "updated_at",
	},
	// xai.mgmt.spending_limits: the team's postpaid monthly limit, every
	// documented member in USD cents (absent when the vendor omits it).
	"Limit/xai.spending_limit/v1": {
		"unit", "period", "billing", "hard_override_cents", "hard_auto_cents", "effective_hard_cents", "soft_cents",
		"effective_cents",
	},
	// Lane ENTAPI-L16C second half (T-710): organization members of
	// OpenRouter (email, the org:admin / org:member role; first and last name
	// as the display name), Prime team members (email when present, ADMIN /
	// MEMBER, join time RFC 3339; userName as the display name) and Kilo active
	// members (email, role, status; the image URL is never read).
	"User/openrouter.org_member/v1": {"email", "role"},
	"User/prime.team_member/v1":     {"email", "role", "joined_at"},
	"User/kilo.org_member/v1":       {"email", "role", "status"},
}

// edgeAttrs is the per-edge-kind attribute allowlist; an edge kind with no
// row keeps no attribute.
var edgeAttrs = map[vs.EdgeKind][]string{}

// HasAllowlist reports whether a resource type has a reviewed row.
func HasAllowlist(t vs.ResourceType) bool {
	_, ok := resourceAttrs[t]
	return ok
}

func listed(list []string, k string) bool {
	for _, v := range list {
		if v == k {
			return true
		}
	}
	return false
}

// contentLike reports whether a value looks like content rather than
// metadata.
func contentLike(v string) bool {
	if len(v) > vendorapi.MaxAttrValue {
		return true
	}
	for _, r := range v {
		if unicode.IsControl(r) {
			return true
		}
	}
	return len(strings.Fields(v)) > maxWords
}

func hasSecret(v string) bool { return len(scrub.DetectSecrets(v)) > 0 }

// cleanValue applies the value rules; ok false means drop the value.
func cleanValue(v string, rep *Report) (string, bool) {
	if contentLike(v) {
		rep.DroppedValues++
		return "", false
	}
	if hasSecret(v) {
		rep.Redacted++
		return scrub.Redacted, true
	}
	return v, true
}

func cleanAttrs(in map[string]string, allow []string, rep *Report) map[string]string {
	if len(in) == 0 {
		return in
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		if !listed(allow, k) {
			rep.DroppedFields++
			continue
		}
		if c, ok := cleanValue(v, rep); ok {
			out[k] = c
		}
	}
	return out
}

func identityOK(id vs.ExternalID) bool {
	return !hasSecret(id.Value) && !hasSecret(id.Namespace) && !contentLike(id.Value)
}

func nodeOK(n *vendorapi.NodeRef) bool { return n == nil || identityOK(n.ID) }

// Page sanitizes one parsed page. It returns a new page (the input is not
// modified), the counts, and an ErrRefused-wrapped error when the page must
// not land at all. A page is refused only for what no row of it can be
// trusted without: a landing type, namespace or class with no reviewed
// allowlist row, or secret material in the snapshot's scope parent. A row
// whose own key carries secret or content material, or exceeds a
// vendorapi.Validate limit, is quarantined (dropped and counted) and the
// rest of the page lands (VCONN-5, T-695).
//
// Page allows no content: every content item is dropped and counted
// (PageWith takes a content policy, lane L15).
func Page(p vendorapi.Page) (vendorapi.Page, Report, error) {
	return PageWith(p, Policy{})
}

// sanitizePage sanitizes everything but content (PageWith owns content).
func sanitizePage(p vendorapi.Page) (vendorapi.Page, Report, error) {
	var rep Report
	out := p
	if m := p.Snapshot; m != nil && !nodeOK(m.ScopeParent) {
		return vendorapi.Page{}, Report{}, fmt.Errorf("%w: manifest scope parent carries secret material", ErrRefused)
	}
	out.Resources = make([]vendorapi.ResourceObservation, 0, len(p.Resources))
	// dropped holds the nodes quarantined on this page, so an edge that
	// names one is quarantined too instead of pointing at nothing.
	dropped := map[vendorapi.NodeRef]bool{}
	for i, r := range p.Resources {
		allow, ok := resourceAttrs[r.Type]
		if !ok {
			return vendorapi.Page{}, Report{}, fmt.Errorf("%w: resource %d has a type with no allowlist row", ErrRefused, i)
		}
		if !identityOK(r.ID) || !nodeOK(r.Parent) {
			dropped[vendorapi.NodeRef{Kind: r.Kind(), ID: r.ID}] = true
			rep.quarantine(true)
			continue
		}
		r.Attrs = cleanAttrs(r.Attrs, allow, &rep)
		if r.DisplayName != "" {
			// A display name longer than vendorapi.MaxDisplayName is dropped
			// here rather than left for Validate to refuse the page.
			if c, ok := cleanValue(r.DisplayName, &rep); ok && len(c) <= vendorapi.MaxDisplayName {
				r.DisplayName = c
			} else {
				if ok {
					rep.DroppedValues++
				}
				r.DisplayName = ""
			}
		}
		if len(r.FieldAuthority) > 0 {
			fa := make(map[string]string, len(r.FieldAuthority))
			for k, v := range r.FieldAuthority {
				if _, kept := r.Attrs[k]; kept || k == "display_name" || k == "parent" {
					fa[k] = v
				}
			}
			r.FieldAuthority = fa
		}
		out.Resources = append(out.Resources, r)
	}
	out.Edges = make([]vendorapi.EdgeObservation, 0, len(p.Edges))
	for _, e := range p.Edges {
		if !identityOK(e.From.ID) || !identityOK(e.To.ID) || dropped[e.From] || dropped[e.To] {
			rep.quarantine(true)
			continue
		}
		e.Attrs = cleanAttrs(e.Attrs, edgeAttrs[e.Edge], &rep)
		out.Edges = append(out.Edges, e)
	}
	if m := p.Snapshot; m != nil {
		cp := *m
		if rep.QuarantinedGraph > 0 {
			// A quarantined resource must never read as deleted.
			cp.AuthoritativeForDeletion = false
		}
		out.Snapshot = &cp
	}
	if err := activity(p, &out, &rep); err != nil {
		return vendorapi.Page{}, Report{}, err
	}
	return out, rep, nil
}
