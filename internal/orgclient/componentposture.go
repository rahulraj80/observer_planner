package orgclient

import "github.com/marmutapp/superbased-observer/internal/store"

// SetComponentPostureProvider forwards the node's component-posture seam
// (modular packaging plan 6.4, T-682) onto the store this client pushes
// from, for the same reason SetBudgetPostureProvider is a forwarder: the push
// loop's store handle is owned by the client bundle. Nil-safe; idempotent.
func (c *Client) SetComponentPostureProvider(p store.ComponentPostureProvider) {
	if c == nil || c.store == nil {
		return
	}
	c.store.SetComponentPostureProvider(p)
}
