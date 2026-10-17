package tooltax_test

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/guard"
	"github.com/marmutapp/superbased-observer/internal/tooltax"
)

// TestOperandDeclaredForEvaluableRows: every row whose canonical action type
// the guard evaluates (guard.ClassifyActionType) names the operand it is
// judged on, and every operand comes from the closed vocabulary. A gate
// receiver built on this table can therefore never meet an evaluable native
// tool without knowing which input to judge.
func TestOperandDeclaredForEvaluableRows(t *testing.T) {
	closed := map[string]bool{}
	for _, op := range tooltax.Operands() {
		closed[op] = true
	}
	evaluable := 0
	for _, e := range tooltax.Table() {
		if !closed[e.Operand] {
			t.Errorf("%s/%s: operand %q not in the closed vocabulary", e.Tool, e.Native, e.Operand)
		}
		if _, ok := guard.ClassifyActionType(e.ActionType); !ok {
			continue
		}
		evaluable++
		if e.Operand == tooltax.OperandNone {
			t.Errorf("%s/%s (%s): evaluable row declares no operand", e.Tool, e.Native, e.ActionType)
		}
	}
	if evaluable == 0 {
		t.Fatal("no evaluable rows: the pin would be vacuous")
	}
}
