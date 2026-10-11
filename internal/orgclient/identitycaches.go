package orgclient

import (
	"context"
	"fmt"
	"os"

	"github.com/marmutapp/superbased-observer/internal/store"
)

// Identity-bound caches at enrolment (T-393, the review M3-01 class).
//
// Every document the org signs for one enrolment identity (one org, usually
// one member) is cached on the node, durably in the store or in this client's
// memory or as a file. When `observer enroll` replaces the enrolment with
// a DIFFERENT org or member, each of those caches must go before the new
// enrolment row is written: otherwise the previous org's policy keeps applying
// under the new enrolment, and a cache that pins the previous org's key or
// highest version refuses the new org's documents.
//
// identityBoundCaches is the ONE table Enroll walks. The durable documents are
// one row that defers to store.ClearIdentityBoundDocs, the same table
// `observer unenroll` walks, so the two paths cannot drift. The test
// TestEveryOrgclientStoreWriterHasAnIdentityDecision fails when a new store
// writer appears in this package without a recorded decision.

// identityScope says when Enroll clears a cache.
type identityScope int

const (
	// clearOnEveryEnrolment: the document is signed for one enrolment epoch,
	// so even a re-enrolment as the same org and member drops it.
	clearOnEveryEnrolment identityScope = iota
	// clearOnIdentityChange: the document is signed for one org (and usually
	// one member); a re-enrolment that changes either drops it.
	clearOnIdentityChange
)

// identityBoundCache is one row of the clearing table.
type identityBoundCache struct {
	name  string
	scope identityScope
	clear func(ctx context.Context, c *Client, oldEnr *store.Enrolment) error
}

var identityBoundCaches = []identityBoundCache{
	{"org budget", clearOnEveryEnrolment, func(ctx context.Context, c *Client, _ *store.Enrolment) error {
		c.budget.clear()
		return c.store.ClearOrgBudget(ctx)
	}},
	// Announcements, display / routing / AI tool policies, access grants and
	// the request outbox, intel (cache, terms, outbox, consent), prices,
	// budget, the guard bundle ETag and the update manifest.
	{"identity-bound org documents", clearOnIdentityChange, func(ctx context.Context, c *Client, oldEnr *store.Enrolment) error {
		return c.store.ClearIdentityBoundDocs(ctx, oldEnr)
	}},
	{"org pricing hot cache", clearOnIdentityChange, func(_ context.Context, c *Client, _ *store.Enrolment) error {
		c.pricing.clearFor("")
		return nil
	}},
	{"update manifest hot cache", clearOnIdentityChange, func(_ context.Context, c *Client, _ *store.Enrolment) error {
		c.resetUpdateCache()
		c.clearAgentTooOld()
		return nil
	}},
	{"policy version memory", clearOnIdentityChange, func(_ context.Context, c *Client, _ *store.Enrolment) error {
		c.policyVersMu.Lock()
		c.lastPolicyVersions = nil
		c.policyVersMu.Unlock()
		return nil
	}},
	// Break-glass leases carry the previous org's provider credential for one
	// member: drop the memory and the bearer-store slot.
	{"break-glass leases", clearOnIdentityChange, func(_ context.Context, c *Client, _ *store.Enrolment) error {
		c.clearBreakGlass()
		if ls, ok := c.bearers.(BreakGlassLeaseStore); ok {
			if err := ls.SaveBreakGlassLeases(nil); err != nil {
				return err
			}
		}
		return nil
	}},
	{"governance sidecar", clearOnIdentityChange, func(_ context.Context, c *Client, _ *store.Enrolment) error {
		c.removeGovernanceSidecar()
		return nil
	}},
	{"node.features sidecar", clearOnIdentityChange, func(_ context.Context, c *Client, _ *store.Enrolment) error {
		c.removeFeaturesSidecar()
		return nil
	}},
	{"org guard bundle file", clearOnIdentityChange, func(_ context.Context, c *Client, _ *store.Enrolment) error {
		if c.guardBundleCachePath == "" {
			return nil
		}
		if err := os.Remove(c.guardBundleCachePath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove %s: %w", c.guardBundleCachePath, err)
		}
		return nil
	}},
}
