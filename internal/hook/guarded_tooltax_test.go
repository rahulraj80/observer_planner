package hook

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/models"
	"github.com/marmutapp/superbased-observer/internal/tooltax"
)

// claudeGuardActionOverrides are the Claude Code tools whose GUARD action
// type deliberately differs from the canonical taxonomy row (the watcher's
// classification). Both are SR27-D1 guard-bypass fixes (security review
// 2026-09-27): the guard must judge them as what they can do, not as what
// they are for. tooltax keeps the canonical class; the operand column
// records the input the guard judges, which the two agree on.
var claudeGuardActionOverrides = map[string]struct{ tooltax, guard string }{
	// Grep with output_mode "content" returns file bytes: a read of its path.
	"Grep": {tooltax: models.ActionSearchText, guard: models.ActionReadFile},
	// Monitor runs its command as a background shell script.
	"Monitor": {tooltax: models.ActionAgentControl, guard: models.ActionRunCommand},
}

// TestClaudeToolShapeMatchesTooltax: every claudeToolShape entry agrees
// with the claude-code tooltax row on the OPERAND (which input the guard
// judges) and, outside the two named overrides, on the action type. This
// is the agreement Lane A relies on before guarded.go reads tooltax
// instead of its own table (brief step 3: the data exists and agrees; the
// switch is not made here).
func TestClaudeToolShapeMatchesTooltax(t *testing.T) {
	markers := map[string]string{
		"C": tooltax.OperandCommand, "F": tooltax.OperandFilePath, "N": tooltax.OperandNotebookPath,
		"P": tooltax.OperandPath, "U": tooltax.OperandURL, "Q": tooltax.OperandQuery,
	}
	var p guardedPreToolPayload
	p.ToolInput.Command, p.ToolInput.FilePath, p.ToolInput.NotebookPath = "C", "F", "N"
	p.ToolInput.Path, p.ToolInput.URL, p.ToolInput.Query = "P", "U", "Q"
	usedOverrides := map[string]bool{}
	for name, shape := range claudeToolShape {
		row, ok := tooltax.Resolve(models.ToolClaudeCode, name)
		if !ok || row.Tool != models.ToolClaudeCode {
			t.Errorf("%s: no claude-code tooltax row (resolved %+v, %v)", name, row, ok)
			continue
		}
		operand, ok := markers[shape.target(&p)]
		if !ok {
			t.Errorf("%s: claudeToolShape target reads no known operand field", name)
			continue
		}
		if row.Operand != operand {
			t.Errorf("%s: tooltax Operand %q, claudeToolShape judges %q", name, row.Operand, operand)
		}
		if row.ActionType == shape.actionType {
			continue
		}
		ov, ok := claudeGuardActionOverrides[name]
		if !ok || ov.tooltax != row.ActionType || ov.guard != shape.actionType {
			t.Errorf("%s: tooltax action %q, claudeToolShape %q, and no matching override", name, row.ActionType, shape.actionType)
			continue
		}
		usedOverrides[name] = true
	}
	for name := range claudeGuardActionOverrides {
		if !usedOverrides[name] {
			t.Errorf("override for %s no longer needed: remove it", name)
		}
	}
}
