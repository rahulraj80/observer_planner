//go:build !sbo_omit_capture_proxy

package main

import (
	"context"
	"database/sql"
	"errors"

	"github.com/marmutapp/superbased-observer/internal/models"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// proxyToolVersionSink adapts *store.Store to proxy.ToolVersionSink
// (T-784). The proxy hands over plain strings (session id, the tool its
// User-Agent row named, the build version); this adapter decides, in
// order, whether a stamp is due and writes it through the ONE tool-version
// seam, Store.SetSessionToolVersion (validated; tool_version first-wins,
// tool_version_last moves forward to the newest build, agent migration 162):
//
//  1. session row not ingested yet       -> not settled (proxy retries later)
//  2. session is owned by another tool   -> settled, no write (a client
//     that borrows another tool's UA, e.g. pi spoofing claude-cli, must
//     never stamp that tool's version onto its own session)
//  3. otherwise                          -> write, settled
//
// A session that already holds a version is NOT skipped: the proxy's
// User-Agent is the best live signal of an upgrade inside a session (the
// developer updates the CLI and resumes), and the store keeps the first
// build while recording the newer one as the latest. The proxy offers each
// (session, version) pair once, so a repeat of the same build costs nothing.
type proxyToolVersionSink struct {
	s *store.Store
}

// StampSessionToolVersion implements proxy.ToolVersionSink.
func (k proxyToolVersionSink) StampSessionToolVersion(ctx context.Context, sessionID, tool, version string) (bool, error) {
	if _, err := k.s.LoadSessionToolVersion(ctx, sessionID); errors.Is(err, sql.ErrNoRows) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	owned, err := k.s.CapturedSessionForTool(ctx, sessionID, tool)
	if err != nil {
		return false, err
	}
	if !owned {
		return true, nil
	}
	if _, err := k.s.SetSessionToolVersion(ctx, models.SessionToolVersion{SessionID: sessionID, Version: version}); err != nil {
		return false, err
	}
	return true, nil
}
