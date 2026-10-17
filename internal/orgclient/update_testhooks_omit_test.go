//go:build sbo_omit_org_update

package orgclient

import (
	"context"
	"testing"
)

// update_testhooks_omit_test.go is update_testhooks_test.go for a build
// without the org.update component: there is no rail to pin or fetch.

func pinUpdateKeyForTest(*Client, string) {}

func updateKeyPinnedForTest(*Client) (pinned, ok bool) { return false, false }

func updateRailPageCases(context.Context, *testing.T, *Client) []struct {
	rail  string
	fetch func() error
} {
	return nil
}

// TestUpdateRailIsInertWithoutOrgUpdate pins the omitted rail: a push
// acknowledgment's nudge and a 426 arm nothing, and the pending fetch makes
// no request (the client has no server to reach here, so any request would
// fail the test through a panic on its nil store).
func TestUpdateRailIsInertWithoutOrgUpdate(t *testing.T) {
	c := &Client{}
	c.noteUpdateNudge([]byte(`{"update_versions":{"stable":9}}`))
	c.markUpdateChannelPendingAfterRefusal(context.Background())
	c.FetchPendingUpdateManifests(context.Background())
	c.resetUpdateCache()
	if c.AgentTooOld() != nil {
		t.Fatal("no refusal was recorded")
	}
	c.noteAgentTooOld([]byte(`{"reason":"agent_too_old","min_version":"1.40.0","your_version":"1.36.0"}`), 426)
	if r := c.AgentTooOld(); r == nil || r.MinVersion != "1.40.0" {
		t.Fatalf("the 426 memory is core and must work without the update rail: %+v", r)
	}
}
