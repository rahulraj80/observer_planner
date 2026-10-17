package main

import (
	"context"
	"database/sql"
	"time"

	"github.com/marmutapp/superbased-observer/internal/dashapi"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// remote_core.go is the always-linked half of the remote wiring (lane
// MP-L1I, T-715): the remote_audit sink the dashboard writes through in
// every build. remote.go, remote_enable.go and remotenotify.go link
// internal/remoteauth, remotecfg, tailnet and remotenotify and are left out
// by -tags sbo_omit_remote (remote_omit.go stands in).

// remoteAuditSink adapts the dashboard's RemoteAuditRecord onto the node-local
// remote_audit store seam (plan §4.8). Best-effort: an audit-write failure must
// never break a request, so errors are swallowed. Metadata only.
func remoteAuditSink(database *sql.DB) func(dashapi.RemoteAuditRecord) {
	if database == nil {
		return nil
	}
	st := store.New(database)
	return func(rec dashapi.RemoteAuditRecord) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = st.InsertRemoteAudit(ctx, store.RemoteAuditEvent{
			Kind: rec.Kind, SessionID: rec.SessionID, Principal: rec.Principal,
			RemoteAddr: rec.RemoteAddr, Route: rec.Route, Decision: rec.Decision, Detail: rec.Detail,
		})
	}
}
