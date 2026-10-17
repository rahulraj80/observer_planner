package store

import (
	"context"
	"sync"
	"time"

	"github.com/marmutapp/superbased-observer/internal/orgcontract"
)

// The managed node's AI tool configuration inventory (D-112 "the org sees
// everything on managed nodes", T-427). agentconfigsvc.Inventory builds it
// from the vendor files; the daemon's org applier hands it here; the push
// seam composes it onto PushEnvelope.AgentConfigInventory through
// composeAgentConfigInventory, so orgpush.go names nothing of it. It is held
// in memory (rebuilt from the files, never a table) and ships:
//
//   - only through the same managed gate as AgentConfigState (an individual
//     node never reports it);
//   - only when the node ships raw content (shipsRawContent), since every
//     row is value-bearing;
//   - only when its digest differs from the last one the org server
//     ACCEPTED (commit-on-success, like the state digest), so an unchanged
//     inventory costs nothing per push. A daemon restart sends it once more.
//   - and, unchanged, again once agentConfigInventoryRefresh has passed
//     since it was last accepted (P3-13). An accepted push does not prove
//     the org stored the inventory: the server's inventory upsert runs after
//     the push commits and is best effort (a lock, a database blip, or a
//     managed enrolment not yet visible there), and on the durable-log path
//     the collector answers before the apply worker runs at all. Without
//     the refresh a lost upsert left the org's view stale or empty until the
//     inventory changed or the daemon restarted, with no signal anywhere.
//     The server's unchanged-digest path only touches received_at, so the
//     refresh is cheap there, and the wire is unchanged (compat both ways).
//
// Every value is re-scrubbed at compose through the JSON-safe backstop the
// state report uses (wireObservedValue): the builder already scrubbed it.

// agentConfigInventoryRefresh bounds how long an unchanged inventory goes
// unsent after the org last accepted it (P3-13).
const agentConfigInventoryRefresh = time.Hour

type agentConfigInventoryHolder struct {
	mu          sync.Mutex
	rep         *orgcontract.AgentConfigInventoryReport
	delivered   string
	deliveredAt time.Time
	pending     string
	// now is the clock (nil: time.Now), a field so a test can move it.
	now func() time.Time
}

func (h *agentConfigInventoryHolder) clock() time.Time {
	if h.now != nil {
		return h.now()
	}
	return time.Now()
}

// SetAgentConfigInventory records the node's latest inventory (the org
// applier's sweep calls it).
func (s *Store) SetAgentConfigInventory(rep orgcontract.AgentConfigInventoryReport) {
	h := &s.agentConfigInventory
	h.mu.Lock()
	defer h.mu.Unlock()
	cp := rep
	cp.Rows = append([]orgcontract.AgentConfigStateRow(nil), rep.Rows...)
	h.rep = &cp
}

// composeAgentConfigInventory returns the inventory to attach to this push,
// or nil: off the managed gate, without raw content, before the first sweep,
// and when the org server accepted this digest less than
// agentConfigInventoryRefresh ago.
func (s *Store) composeAgentConfigInventory(ctx context.Context, includeValues bool) *orgcontract.AgentConfigInventoryReport {
	h := &s.agentConfigInventory
	h.mu.Lock()
	h.pending = ""
	rep := h.rep
	fresh := rep != nil && rep.Digest == h.delivered && h.clock().Sub(h.deliveredAt) < agentConfigInventoryRefresh
	h.mu.Unlock()
	if !includeValues || rep == nil || fresh {
		return nil
	}
	if s.agentConfigStateGate == nil || !s.agentConfigStateGate(ctx) {
		return nil
	}
	out := &orgcontract.AgentConfigInventoryReport{Digest: rep.Digest, Truncated: rep.Truncated, Rows: make([]orgcontract.AgentConfigStateRow, 0, len(rep.Rows))}
	for _, r := range rep.Rows {
		if r.State != orgcontract.AgentConfigInventoryState {
			continue
		}
		if len(r.ObservedValue) > orgcontract.AgentConfigObservedMaxBytes {
			r.ObservedValue = ""
		}
		r.ObservedValue = wireObservedValue(r.ObservedValue)
		r.Reason = orgcontract.NormalizeAgentConfigReason(r.Reason)
		out.Rows = append(out.Rows, r)
	}
	if len(out.Rows) > orgcontract.AgentConfigInventoryMaxRows {
		out.Rows, out.Truncated = out.Rows[:orgcontract.AgentConfigInventoryMaxRows], true
	}
	h.mu.Lock()
	h.pending = rep.Digest
	h.mu.Unlock()
	return out
}

// commitAgentConfigInventory promotes the composed digest to delivered
// (CommitPushedSnapshots, after an accepted push).
func (s *Store) commitAgentConfigInventory() {
	h := &s.agentConfigInventory
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.pending != "" {
		h.delivered, h.pending = h.pending, ""
		h.deliveredAt = h.clock()
	}
}
