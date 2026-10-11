//go:build sbo_omit_agentaccess_relay

package store

import (
	"context"

	"github.com/marmutapp/superbased-observer/internal/mcpintel/correlate"
	"github.com/marmutapp/superbased-observer/internal/orgcontract"
)

// mcprelay_omit.go is the off side of mcprelaysummary.go and
// mcpcorrelate.go: a `-tags sbo_omit_agentaccess_relay` build (component
// agentaccess.relay compiled out, lane MP-L1I, T-715) links no relay record
// chain (internal/mcprelay/record), so the push composes no relay activity
// or event rows, the snapshot gate sees a constant relay probe, and the
// session MCP-call panel lists no calls. The shared correlation and
// discovery vocabularies (internal/mcpintel/correlate, discover) are core,
// so the MCP inventory wire and the panel's empty answer are unchanged in
// shape.

// mcpRelayEventsPerBatch caps the per-record rows one SelectUnpushedSince
// reads (unused without the relay chain; kept so orgpush.go compiles).
const mcpRelayEventsPerBatch = 2000

// SelectMCPRelayActivity composes no rows: there is no relay chain.
func (s *Store) SelectMCPRelayActivity(context.Context) ([]orgcontract.MCPRelayActivityRow, error) {
	return nil, nil
}

// SelectMCPRelayEvents composes no rows: there is no relay chain.
func (s *Store) SelectMCPRelayEvents(context.Context, int64, int) ([]orgcontract.MCPRelayEventRow, error) {
	return nil, nil
}

// mcpRelayHeadSeq is 0: there is no relay chain.
func (s *Store) mcpRelayHeadSeq(context.Context) (int64, error) { return 0, nil }

// probeMCPRelayRecords is a constant token: nothing to change.
func (s *Store) probeMCPRelayRecords(context.Context) (string, error) {
	return "agentaccess.relay:not_compiled", nil
}

// LoadSessionMCPCalls lists no calls: no relay record exists in this build.
// The answer still carries the session id and an empty, non-nil call list,
// the same shape the full build returns for a session with no relay records
// (T-726: it used to return an empty session_id and a null calls array).
func (s *Store) LoadSessionMCPCalls(_ context.Context, sessionID string) (correlate.Result, error) {
	return correlate.Result{SessionID: sessionID, Calls: []correlate.Call{}}, nil
}
