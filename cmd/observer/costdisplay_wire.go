package main

import (
	"context"
	"sync"
	"time"

	"github.com/marmutapp/superbased-observer/internal/orgcontract"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// The developer cost-display boundary (operator ruling 2026-10-02: an org
// admin chooses whether developers see dollar costs or only token usage).
//
// The resolution is store.ResolveDeveloperCostDisplay - the same seam GET
// /api/cost-display reads - against the node's CURRENT enrolment. This
// resolver only caches its answer briefly for the daemon's in-process
// developer surfaces that render text outside the dashboard: the
// budget-warning service (its messages feed the prompt-hook notice, `observer
// guard status`, the dashboard banner and the desktop notification) and the
// guard's budget verdict text. The statusline asks the dashboard per tick.

// costDisplayCacheTTL bounds how stale a cached answer may be. A new verified
// document also invalidates it at once (orgclient.SetDisplayPolicySink).
const costDisplayCacheTTL = 5 * time.Second

type costDisplayResolver struct {
	st  *store.Store
	now func() time.Time

	mu   sync.Mutex
	at   time.Time
	mode string
}

func newCostDisplayResolver(st *store.Store) *costDisplayResolver {
	return &costDisplayResolver{st: st, now: time.Now}
}

// Mode returns "usd" or "tokens". An answer that is KNOWN to be dollars (a
// solo install, an unenrolled node, an org that never chose) is dollars; when
// this node cannot tell (the enrolment or the cached signed policy cannot be
// read) it is token usage, never the dollars default (Sol A4-03 class, D-013).
// A nil resolver is a host with no org wiring at all, so dollars.
func (r *costDisplayResolver) Mode() string {
	if r == nil || r.st == nil {
		return orgcontract.DisplayCostUSD
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.mode != "" && r.now().Sub(r.at) < costDisplayCacheTTL {
		return r.mode
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	r.mode = r.resolve(ctx)
	r.at = r.now()
	return r.mode
}

// resolve is one uncached resolution through the shared store seam.
func (r *costDisplayResolver) resolve(ctx context.Context) string {
	enr, err := r.st.LoadEnrolment(ctx)
	if err != nil {
		return orgcontract.DisplayCostTokens
	}
	var orgID string
	if enr != nil {
		orgID = enr.OrgID
	}
	d := r.st.ResolveDeveloperCostDisplay(ctx, orgID != "", orgID)
	if d.Unverified {
		return orgcontract.DisplayCostTokens
	}
	return d.Mode
}

// Invalidate drops the cached answer so the next Mode re-resolves.
func (r *costDisplayResolver) Invalidate() {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.mode = ""
	r.mu.Unlock()
}
