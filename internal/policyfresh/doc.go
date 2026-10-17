// Package policyfresh is the PURE decision core of the node's policy-freshness
// checker (2026-10-02 budget-latency arc, P2).
//
// The operator's ask: any next activity on the node (a prompt, a proxied
// request) should act as a trigger to check, with a tiny request, whether the
// organization changed any policy - budgets, guardrails, pricing, routing,
// governance - and only then pull and apply the changed one. Before this a new
// budget reached a node only on its push cycle (15 min on the demo estate) and
// the guard bundle on an hourly poll.
//
// The rules, an ordered table walked top-down (CLAUDE.md #5), one test row
// each:
//
//  1. the org server does not offer the probe (404 within the last hour)
//     -> do nothing (the periodic rails keep converging as before);
//  2. a check is already in flight -> coalesce;
//  3. a probe just finished -> re-arm a HELD probe (at most one start per
//     HoldGap) while the node is active, the server holds and the probe did
//     not fail; otherwise do nothing;
//  4. a scheduled or backstop wake-up -> check now;
//  5. a probe is already scheduled -> activity joins it;
//  6. the first activity after an idle period -> check NOW;
//  7. less than MinInterval since the last check -> schedule one for
//     lastCheck + MinInterval + jitter (bursts collapse into one);
//  8. otherwise -> check now.
//
// The backstop (a check every Backstop interval with no activity at all) is
// the caller's timer feeding Decide with TriggerBackstop, which skips rule 4/5
// semantics and simply checks unless 1 or 2 hold.
//
// THE HELD PROBE (T-474, "within seconds" on an active node). Every probe an
// active node sends (rules 3, 4 for a scheduled wake, 6, 7, 8) asks the org
// server to HOLD it for up to DefaultHold while nothing changes; the server
// answers the moment a version moves. While the node stays active the
// finished probe is re-armed at once (rule 3), so one probe is always open
// and a change arrives in about a second, at about one request per hold:
// the same volume the 30 s activity floor allowed. The backstop of an idle
// node never holds, so idle cost is unchanged. An older server never says it
// held (no orgcontract.HeaderPolicyVersionsHold), so rule 3 never re-arms and
// the node keeps exactly the pre-hold cadence.
//
// Pure: no clock (the caller passes now), no I/O, no randomness source (the
// caller passes the jitter draw). Pinned by imports_test.go.
package policyfresh
