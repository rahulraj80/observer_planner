package main

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/marmutapp/superbased-observer/internal/accessgrant"
	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/db"
	"github.com/marmutapp/superbased-observer/internal/guard"
	"github.com/marmutapp/superbased-observer/internal/orgclient"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// orggrant_wire.go - the ONE adapter from the guard's org-grant question
// (guard.OrgGrantClaim) to the node's verified grant cache
// (orgclient.ClaimAccessGrant / PeekAccessGrant). The hook process and the
// daemon both compose it, so both read the same re-verified cache and count
// uses through the same conditional UPDATE (request-grants G3).

// orgGrantStoreOpener yields a store for one grant question and a release
// func. The daemon hands its long-lived handle (release is a no-op); a hook
// process opens the database lazily, only on the rare blocking path, the
// SetApprovalLookup precedent.
type orgGrantStoreOpener func(ctx context.Context) (*store.Store, func(), error)

// newOrgGrantClaim builds the guard's org-grant claim over open. Any failure
// (no database, a timeout) answers "no grant", so enforcement stands.
func newOrgGrantClaim(open orgGrantStoreOpener, timeout time.Duration) guard.OrgGrantClaim {
	return func(q guard.OrgGrantQuery) guard.OrgGrantResult {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		st, release, err := open(ctx)
		if err != nil || st == nil {
			return guard.OrgGrantResult{}
		}
		defer release()
		aq := accessgrant.Query{
			RuleID: q.RuleID, Lane: q.Lane, CommandClass: q.CommandClass, OperandHash: q.OperandHash,
			ProjectRootHash: q.ProjectRootHash, Decision: q.Decision, RuleLocked: q.RuleLocked,
		}
		var c orgclient.GrantClaim
		if q.Consume {
			c = orgclient.ClaimAccessGrant(ctx, st, aq, time.Now())
		} else {
			c = orgclient.PeekAccessGrant(ctx, st, aq, time.Now())
		}
		return guard.OrgGrantResult{
			OK: c.OK, GrantID: c.Hit.GrantID, Effect: c.Hit.Effect, ExpiresAtNs: c.Hit.ExpiresAtNs,
			UsesLeft: c.Hit.UsesLeft, LockRefused: c.LockRefused, Unverified: c.Unverified,
		}
	}
}

// hookOrgGrantOpener opens the daemon's database for one question (a hook
// process is short-lived and holds no handle).
func hookOrgGrantOpener(dbPath string) orgGrantStoreOpener {
	return func(ctx context.Context) (*store.Store, func(), error) {
		database, err := db.Open(ctx, db.Options{Path: dbPath})
		if err != nil {
			return nil, nil, err
		}
		return store.New(database), func() { _ = database.Close() }, nil
	}
}

// daemonOrgGrantOpener hands out the daemon's own store handle.
func daemonOrgGrantOpener(st *store.Store) orgGrantStoreOpener {
	return func(context.Context) (*store.Store, func(), error) { return st, func() {}, nil }
}

// requestAccessBase is the node dashboard origin the guard's org-locked deny
// line links its request form under (request-grants G4): the configured
// dashboard address, a wildcard bind ("", 0.0.0.0, ::) mapped to loopback
// because the link is opened by a browser on THIS machine. "" when the
// address cannot be parsed: the line then offers the CLI command only.
// Never a network call; the hook process composes it from config alone.
func requestAccessBase(cfg config.Config) string {
	if !dashboardInBuild {
		// No request form to link to in a build without the dashboard
		// (ui.dashboard, MP-L1M): the line offers the CLI command only.
		return ""
	}
	addr := resolveDashboardAddr("", cfg.Dashboard.Addr, "127.0.0.1:8081")
	host, port, err := net.SplitHostPort(addr)
	if err != nil || port == "" {
		return ""
	}
	if ip := net.ParseIP(host); host == "" || (ip != nil && ip.IsUnspecified()) {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port)
}

// claimToolGrant answers a launch gate's tool access question (request-grants
// G8) over open and counts one use: a verified grant for exactly this tool.
// Any failure is "no grant", so the org's disallow stands.
func claimToolGrant(open orgGrantStoreOpener, timeout time.Duration, tool string) orgclient.TargetClaim {
	if tool == "" {
		return orgclient.TargetClaim{}
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	st, release, err := open(ctx)
	if err != nil || st == nil {
		return orgclient.TargetClaim{}
	}
	defer release()
	return orgclient.ClaimTargetGrant(ctx, st, accessgrant.TargetQuery{Type: accessgrant.TypeToolAllow, Target: tool}, time.Now())
}

// peekToolGrant is claimToolGrant without counting a use: the same verified
// read and the same match, for a gate that decides but does not start the
// tool itself.
func peekToolGrant(open orgGrantStoreOpener, timeout time.Duration, tool string) orgclient.TargetClaim {
	if tool == "" {
		return orgclient.TargetClaim{}
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	st, release, err := open(ctx)
	if err != nil || st == nil {
		return orgclient.TargetClaim{}
	}
	defer release()
	return orgclient.PeekTargetGrant(ctx, st, accessgrant.TargetQuery{Type: accessgrant.TypeToolAllow, Target: tool}, time.Now())
}

// daemonToolGrant is the dashboard launch gate's grant reader over the
// daemon's own store (nodeFeaturesHandle.SetToolGrant). It PEEKS (review
// M3-10): a dashboard launch runs the inner `observer <tool>` launcher, and
// that launcher is the one place a launch's use is counted
// (refuseIfToolDisallowed), so a one-use grant is spent once per launch,
// and never by a launch the dashboard then refuses for another reason.
func daemonToolGrant(st *store.Store) func(tool string) (int64, bool) {
	return func(tool string) (int64, bool) {
		c := peekToolGrant(daemonOrgGrantOpener(st), 2*time.Second, tool)
		return c.Hit.GrantID, c.OK
	}
}

// grantWindowWords renders a grant's remaining window for a launch note:
// "until 2026-10-04 18:00 UTC, 2 uses left" (either part may be absent).
func grantWindowWords(expiresAtNs, usesLeft int64) string {
	var parts []string
	if expiresAtNs > 0 {
		parts = append(parts, "until "+time.Unix(0, expiresAtNs).UTC().Format("2006-01-02 15:04 UTC"))
	}
	if usesLeft >= 0 {
		parts = append(parts, fmt.Sprintf("%d uses left", usesLeft))
	}
	if len(parts) == 0 {
		return ""
	}
	return " (" + strings.Join(parts, ", ") + ")"
}

// daemonFeatureGrant is the node.features gate's feature access reader over
// the daemon's own store (nodeFeaturesHandle.SetFeatureGrant). A feature
// grant is time-bound only, so this peeks and counts nothing.
func daemonFeatureGrant(st *store.Store) func(feature string) (int64, bool) {
	return func(feature string) (int64, bool) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		c := orgclient.PeekTargetGrant(ctx, st, accessgrant.TargetQuery{Type: accessgrant.TypeFeatureEnable, Target: feature}, time.Now())
		return c.Hit.GrantID, c.OK
	}
}
