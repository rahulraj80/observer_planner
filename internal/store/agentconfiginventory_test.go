package store

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/orgcontract"
)

// TestComposeAgentConfigInventory pins the push seam of the AI tool
// configuration inventory (D-112, T-427): managed gate only, raw content
// only, the JSON-safe backstop re-scrubs a value the builder missed, and the
// report rides a push only until the org server accepted that digest.
func TestComposeAgentConfigInventory(t *testing.T) {
	t.Parallel()
	s, _ := newTestStore(t)
	ctx := context.Background()
	rep := orgcontract.AgentConfigInventoryReport{Digest: "d1", Rows: []orgcontract.AgentConfigStateRow{
		{Tool: "codex", Setting: "codex/model", FileID: "codex/user-config", Scope: "user", State: orgcontract.AgentConfigInventoryState, ObservedValue: `"o3"`},
		{Tool: "codex", Setting: "codex/@instructions", FileID: "codex/instructions:~/.codex/AGENTS.md", Scope: "user", State: orgcontract.AgentConfigInventoryState, ObservedValue: `"key AKIAIOSFODNN7EXAMPLE here"`},
		{Tool: "codex", Setting: "codex/model", FileID: "codex/user-config", State: "drifted"},
	}}
	if got := s.composeAgentConfigInventory(ctx, true); got != nil {
		t.Fatalf("before the first sweep: %+v", got)
	}
	s.SetAgentConfigInventory(rep)
	if got := s.composeAgentConfigInventory(ctx, true); got != nil {
		t.Fatalf("ungated compose = %+v", got)
	}
	s.SetAgentConfigStateGate(func(context.Context) bool { return false })
	if got := s.composeAgentConfigInventory(ctx, true); got != nil {
		t.Fatalf("individual node compose = %+v", got)
	}
	s.SetAgentConfigStateGate(func(context.Context) bool { return true })
	if got := s.composeAgentConfigInventory(ctx, false); got != nil {
		t.Fatalf("metadata-only compose = %+v", got)
	}
	got := s.composeAgentConfigInventory(ctx, true)
	if got == nil || len(got.Rows) != 2 || got.Rows[0].ObservedValue != `"o3"` {
		t.Fatalf("managed compose = %+v", got)
	}
	if strings.Contains(got.Rows[1].ObservedValue, "AKIAIOSFODNN7EXAMPLE") || got.Rows[1].ObservedValue == "" {
		t.Fatalf("the backstop must scrub, not drop: %q", got.Rows[1].ObservedValue)
	}
	// A failed push (no commit) re-attaches it; an accepted one stops it.
	if again := s.composeAgentConfigInventory(ctx, true); again == nil {
		t.Fatal("an undelivered inventory must ride the next push")
	}
	b := PushBatch{AgentConfigInventory: got}
	if !b.hasPostures() {
		t.Fatal("a changed inventory must force a push")
	}
	s.CommitPushedSnapshots()
	if again := s.composeAgentConfigInventory(ctx, true); again != nil {
		t.Fatalf("a delivered inventory rode again: %+v", again)
	}
	rep.Digest = "d2"
	s.SetAgentConfigInventory(rep)
	if again := s.composeAgentConfigInventory(ctx, true); again == nil || again.Digest != "d2" {
		t.Fatalf("a changed inventory must ride: %+v", again)
	}
}

// TestAgentConfigInventoryRefreshesAfterAcceptance is P3-13: an accepted
// push does not prove the org stored the inventory (its upsert is best
// effort and runs after the push commits, or on the apply worker), so an
// unchanged inventory rides again once agentConfigInventoryRefresh has
// passed since it was accepted, instead of never until a change or restart.
func TestAgentConfigInventoryRefreshesAfterAcceptance(t *testing.T) {
	t.Parallel()
	s, _ := newTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC)
	s.agentConfigInventory.now = func() time.Time { return now }
	s.SetAgentConfigStateGate(func(context.Context) bool { return true })
	s.SetAgentConfigInventory(orgcontract.AgentConfigInventoryReport{Digest: "d1", Rows: []orgcontract.AgentConfigStateRow{
		{Tool: "codex", Setting: "codex/model", FileID: "codex/user-config", Scope: "user", State: orgcontract.AgentConfigInventoryState, ObservedValue: `"o3"`},
	}})
	if got := s.composeAgentConfigInventory(ctx, true); got == nil {
		t.Fatal("first compose")
	}
	s.CommitPushedSnapshots()
	now = now.Add(agentConfigInventoryRefresh - time.Second)
	if got := s.composeAgentConfigInventory(ctx, true); got != nil {
		t.Fatalf("an accepted inventory rode again inside the refresh window: %+v", got)
	}
	now = now.Add(2 * time.Second)
	got := s.composeAgentConfigInventory(ctx, true)
	if got == nil || got.Digest != "d1" {
		t.Fatalf("an unchanged inventory must ride again after the refresh window: %+v", got)
	}
	s.CommitPushedSnapshots()
	if again := s.composeAgentConfigInventory(ctx, true); again != nil {
		t.Fatalf("the refresh restarted the window: %+v", again)
	}
}
