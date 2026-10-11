package modelvalue

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/routingcore"
)

// TestFilterSimTurnsByKind pins the R1.5 turn-kind filter: it runs the
// real §R8.2 classifier (sidechain → subagent; an empty window degrades
// to unknown), and an empty kind is a no-filter passthrough.
func TestFilterSimTurnsByKind(t *testing.T) {
	t.Parallel()
	turns := []routingcore.SimTurn{
		{SessionID: "sub", In: routingcore.DecisionInput{
			Session: routingcore.SessionState{IsSidechain: true},
		}},
		{SessionID: "unk", In: routingcore.DecisionInput{}},
	}

	sub := FilterSimTurnsByKind(turns, routingcore.TurnSubagent)
	if len(sub) != 1 || sub[0].SessionID != "sub" {
		t.Errorf("subagent filter = %+v, want the sidechain turn only", sub)
	}
	unk := FilterSimTurnsByKind(turns, routingcore.TurnUnknown)
	if len(unk) != 1 || unk[0].SessionID != "unk" {
		t.Errorf("unknown filter = %+v, want the empty-window turn only", unk)
	}
	if got := FilterSimTurnsByKind(turns, ""); len(got) != 2 {
		t.Errorf("empty kind must pass through all turns, got %d", len(got))
	}
	if got := FilterSimTurnsByKind(turns, routingcore.TurnPlan); len(got) != 0 {
		t.Errorf("non-matching kind must yield empty, got %d", len(got))
	}
}
