package orgclient

import (
	"context"

	"github.com/marmutapp/superbased-observer/internal/store"
)

// ActiveEnrolmentIdentity returns the node's ACTIVE enrolment identity
// (org_key, generation), or ok=false when the node is not enrolled, the
// generation row is missing or tombstoned, or either read failed. Every
// ok=false branch fails CLOSED: callers treat it as "no identity", never as
// a match on a transient read error.
//
// It is the ONE owner of the policy-resource identity read (plan
// docs/plans/plane-a-p0-5-unified-policy-resource-v1-plan.md section 6.9 and
// 6.10). The org-policy install gate (Client.PolicyIdentityMatches) and the
// Plane A admission service's Org-layer fence (cmd/observer obs_wire.go)
// both read through it, so a build without Plane A observability keeps the
// same gate (AA-67, T-673).
func ActiveEnrolmentIdentity(ctx context.Context, st *store.Store) (orgKey string, generation int64, ok bool) {
	if st == nil {
		return "", 0, false
	}
	enr, err := st.LoadEnrolment(ctx)
	if err != nil || enr == nil {
		return "", 0, false
	}
	key := OrgKey(enr.OrgServerURL, enr.OrgID)
	gen, found, err := st.LoadEnrolmentGeneration(ctx, key)
	if err != nil || !found || gen.Tombstoned {
		return "", 0, false
	}
	return key, gen.Generation, true
}

// PolicyIdentityMatches reports whether the live enrolment identity equals
// (orgKey, generation), with a fresh read every call (no cache). The policy
// resource dispatcher calls it immediately before installing a verified
// family so a fetch that committed under generation N cannot install after
// an intervening unenrol, re-enrol or tombstone (plan section 6.10, GWF-SF1).
// A nil client, a client without a store, or no active enrolment returns
// false (fail closed).
func (c *Client) PolicyIdentityMatches(ctx context.Context, orgKey string, generation int64) bool {
	if c == nil {
		return false
	}
	key, gen, ok := ActiveEnrolmentIdentity(ctx, c.store)
	return ok && key == orgKey && gen == generation
}
