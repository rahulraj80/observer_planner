//go:build sbo_omit_agentaccess_relay

package main

import (
	"context"
	"database/sql"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/spf13/cobra"

	"github.com/marmutapp/superbased-observer/internal/components"
	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/guard"
	"github.com/marmutapp/superbased-observer/internal/orgclient"
	"github.com/marmutapp/superbased-observer/internal/policystate"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// mcprelay_omit.go stands in for mcprelay*.go, mcp_cmd.go, mcp_doctor.go and
// mcp_access_dashboard.go in a build without agentaccess.relay (-tags
// sbo_omit_agentaccess_relay, lane MP-L1I, T-715): internal/mcprelay (and its
// localpdp / coverage / project / record subpackages), internal/mcpaccess,
// internal/policyfam/mcpaccess, internal/agentid, internal/dpop, mcpegress
// and netpolicy are not linked. `observer mcp` and `observer mcp-relay`
// answer "not in this build" (exit 3); no relay seam is bound into the
// proxy, the guard or the hook; the policy-state row is the honest
// none / no_policy "this node runs no MCP relay"; and the dashboard's
// /api/mcp-access/status reports the seam absent.
//
// Operator rule D-514: an [mcp_relay] block this build cannot honour never
// stops `observer start`; the relay stays off and one log line names the
// build and the fix. Relay-wrapped MCP client entries a previous build
// wrote are restored with `observer mcp unwrap` in a build that has the
// component (the compiled-out teardown rule).
func init() { components.MarkOmitted("agentaccess.relay") }

// newMCPCmd registers `observer mcp` as a compiled-out verb.
func newMCPCmd() *cobra.Command {
	return compiledOutVerb("mcp", "Agent Access MCP relay (not in this build)", "agentaccess.relay")
}

// newMCPRelayCmd registers `observer mcp-relay` as a compiled-out verb.
func newMCPRelayCmd() *cobra.Command {
	return compiledOutVerb("mcp-relay", "Agent Access MCP relay (not in this build)", "agentaccess.relay")
}

// mcpRelayHandle is empty: no relay runs in this build.
type mcpRelayHandle struct{}

// SetGovernance has nothing to bind.
func (h *mcpRelayHandle) SetGovernance(*nodeGovernanceHandle) {}

// mcpRelayRuntime is empty: no relay runtime starts in this build.
type mcpRelayRuntime struct{}

// processMCPRelay stays nil for the process lifetime.
var processMCPRelay atomic.Pointer[mcpRelayHandle]

// wireMCPRelay binds nothing. When [mcp_relay] asks for the relay, it logs
// the one reason the setting is not honoured.
func wireMCPRelay(cfg config.Config, _ *store.Store, _ *nodeGovernanceHandle, _ *guard.Guard, logger *slog.Logger) *mcpRelayHandle {
	if line, off := settingOffLine("mcp_relay.enabled", cfg.MCPRelay.Enabled, nil); off && logger != nil {
		logger.Warn("mcp-relay: " + line)
	}
	return nil
}

// startMCPRelayRuntime is never reached (the handle is nil); it starts
// nothing.
func startMCPRelayRuntime(context.Context, config.Config, *sql.DB, *store.Store, *mcpRelayHandle, *slog.Logger) (*mcpRelayRuntime, error) {
	return nil, nil
}

// hookMCPAccessLookup binds no lookup: the hook stays fail-open, exactly the
// pre-relay hook.
func hookMCPAccessLookup(config.Config) guard.MCPAccessLookup { return nil }

// mcpRelayPublish has no relay to apply a tools.mcp_access body to (the
// family's compiler is not linked either, so no such body is accepted).
func mcpRelayPublish(orgclient.PolicyResourceResult) {}

// mcpRelayClear has no relay to clear.
func mcpRelayClear() {}

// newMCPRelayPointReader reports the node-mcp-relay point as none: this
// node runs no MCP relay (the same row a relay-disabled node reports).
func newMCPRelayPointReader(_ *mcpRelayHandle, lastFetch func() orgclient.PolicyResourceFetchOutcome, now func() time.Time) policystate.PointReader {
	return func(context.Context) (policystate.PointFacts, error) {
		f := policystate.PointFacts{EnforceMode: "off", LastSeen: now()}
		if lastFetch != nil {
			o := lastFetch()
			reject := wirePolicyResourceRejectReason(o.RejectCode)
			f.LatestFetchRejected = reject != ""
			f.RejectCode = reject
			f.RejectedVersion = o.Version
			f.Unreachable = o.Unreachable
		}
		return f, nil
	}
}

// mcpAccessStatusSeam is nil: the dashboard reports the status seam absent.
func mcpAccessStatusSeam(string, *sql.DB) func(ctx context.Context, verifyChain bool) (any, error) {
	return nil
}
