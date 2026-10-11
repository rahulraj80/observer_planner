//go:build !sbo_omit_terminals

package main

import (
	"context"

	"github.com/marmutapp/superbased-observer/internal/dashapi"
	"github.com/marmutapp/superbased-observer/internal/integration"
	"github.com/marmutapp/superbased-observer/internal/store"
	"github.com/marmutapp/superbased-observer/internal/termsession"
)

// terminal_policystop.go wires dashapi.PolicyStopProvider onto the ONE
// shared terminal stack's *termsession.Manager plus the store's node-
// intervention audit trail (internal/store.LookupProcessControlStop). It is
// the seam that lets a dashboard terminal exit ("New Terminal", not a GUI
// launch) explain itself when an org-managed node's OWN node-intervention
// loop stopped the vendor process — resolving the SAME guard_events audit
// row cmd/observer/nodeintervention_audit.go already wrote, never a guess.
//
// Two facts compose here that neither seam has alone:
//   - the PTY child's OS pid + launch time (termsession.Manager, which keeps
//     the pid resolvable past exit — see ProcessAttributionForHandle);
//   - the audit row correlated by that pid within the run's own lifetime
//     window, or, for a wrapped launch whose vendor process is the
//     launcher's child, by the run's tool within a tight window around
//     its exit (store.LookupProcessControlStopForRun, T-584).
// Composing them in cmd keeps both internal/intelligence/dashboard and
// internal/termsession free of any internal/store import (CLAUDE.md module-
// boundary rule #2 — no type leakage past a seam).

// policyStopProvider implements dashapi.PolicyStopProvider over one
// terminal stack's manager + the daemon's store.
type policyStopProvider struct {
	mgr *termsession.Manager
	st  *store.Store
}

// PolicyStopForHandle satisfies dashapi.PolicyStopProvider. It returns
// ok=false whenever the handle carries no recorded pid (every non-dashboard
// launch kind, and any handle the manager has already garbage-collected) or
// no matching audit row exists in the run's own window — never a guess.
func (p *policyStopProvider) PolicyStopForHandle(ctx context.Context, handle string) (dashapi.PolicyStop, bool) {
	if p == nil || p.mgr == nil || p.st == nil {
		return dashapi.PolicyStop{}, false
	}
	pid, launchedAt, ok := p.mgr.ProcessAttributionForHandle(handle)
	if !ok {
		return dashapi.PolicyStop{}, false
	}
	run := store.ProcessControlRun{PID: pid, LaunchedAt: launchedAt}
	// A wrapped launch's PTY child is the `observer <tool>` launcher, which
	// starts the vendor process as its child, so the audit row names a pid
	// this handle never saw (T-584). Once the run has exited, hand the store
	// its canonical tool and exit time so it can fall back to the tool's
	// stop inside a tight window around that exit.
	if sub, exitedAt, exited := p.mgr.ExitAttributionForHandle(handle); exited {
		if tool, known := integration.ToolForLaunchSubcommand(sub); known {
			run.Tool = tool
			run.ExitedAt = exitedAt
		}
	}
	stop, found, err := p.st.LookupProcessControlStopForRun(ctx, run)
	if err != nil || !found {
		return dashapi.PolicyStop{}, false
	}
	return dashapi.PolicyStop{
		RuleID:   stop.RuleID,
		Decision: stop.Decision,
		Reason:   stop.Reason,
		At:       stop.At,
	}, true
}
