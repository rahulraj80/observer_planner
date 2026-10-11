//go:build sbo_omit_org_update

package orgclient

import "context"

// update_omitted.go replaces update.go in a build without the org.update
// component (-tags sbo_omit_org_update, lane MP-GOV4, T-711): the manifest
// rail does nothing and internal/update is not linked. The push loop's calls
// stay in place and are no-ops, so the push rail itself is unchanged; the
// 426 version-floor memory is core (agenttooold.go) and keeps working.

// updateCache is empty: there is no manifest rail in this build.
type updateCache struct{}

// resetUpdateCache has nothing to drop beyond the empty cache itself.
func (c *Client) resetUpdateCache() { c.updates = updateCache{} }

// markUpdateChannelPendingAfterRefusal arms nothing: there is no update to
// fetch after a 426 in this build.
func (c *Client) markUpdateChannelPendingAfterRefusal(context.Context) {}

// noteUpdateNudge ignores the push acknowledgment's update_versions map.
func (c *Client) noteUpdateNudge([]byte) {}

// FetchPendingUpdateManifests fetches nothing.
func (c *Client) FetchPendingUpdateManifests(context.Context) {}
