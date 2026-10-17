//go:build sbo_omit_agentaccess_relay

package store

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// TestLoadSessionMCPCallsOmitCarriesTheSessionID pins T-726: a build without
// agentaccess.relay answers the session MCP-call panel with the session id
// and an empty call list, the same JSON shape the full build gives a session
// with no relay records (it used to answer session_id "" and calls null).
func TestLoadSessionMCPCallsOmitCarriesTheSessionID(t *testing.T) {
	res, err := New(nil).LoadSessionMCPCalls(context.Background(), "sess-t726")
	if err != nil {
		t.Fatal(err)
	}
	if res.SessionID != "sess-t726" {
		t.Errorf("session_id = %q, want sess-t726", res.SessionID)
	}
	b, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"calls":[]`) {
		t.Errorf("body = %s, want an empty calls array, not null", b)
	}
}
