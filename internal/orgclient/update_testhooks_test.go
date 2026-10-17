//go:build !sbo_omit_org_update

package orgclient

import (
	"context"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/update"
)

// update_testhooks_test.go gives the core tests a handle on the update rail
// that compiles in both builds (lane MP-GOV4, T-711): this file carries the
// real rail; update_testhooks_omit_test.go the build without it.

// pinUpdateKeyForTest pins an org distribution key on the update rail.
func pinUpdateKeyForTest(c *Client, keyID string) {
	c.updates.mu.Lock()
	c.updates.keyID = keyID
	c.updates.mu.Unlock()
}

// updateKeyPinnedForTest reports whether the update rail still pins a key;
// ok is false in a build without the rail.
func updateKeyPinnedForTest(c *Client) (pinned, ok bool) {
	c.updates.mu.Lock()
	defer c.updates.mu.Unlock()
	return c.updates.keyID != "", true
}

// updateRailPageCases is the update rail's row of
// TestRailsTreatAnOlderServersPageAsNotSupported.
func updateRailPageCases(ctx context.Context, t *testing.T, c *Client) []struct {
	rail  string
	fetch func() error
} {
	return []struct {
		rail  string
		fetch func() error
	}{
		{updateRail, func() error {
			changed, err := c.FetchOrgUpdateManifest(ctx, update.Channel("stable"))
			if changed {
				t.Error("update manifest changed on a page")
			}
			return err
		}},
	}
}

// TestRefusalReasonMatchesUpdate pins the core copy of the 426 reason
// (agenttooold.go) to internal/update's.
func TestRefusalReasonMatchesUpdate(t *testing.T) {
	if refusalReasonAgentTooOld != update.RefusalReasonAgentTooOld {
		t.Fatalf("refusalReasonAgentTooOld = %q, update says %q", refusalReasonAgentTooOld, update.RefusalReasonAgentTooOld)
	}
}
