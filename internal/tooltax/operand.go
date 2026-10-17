package tooltax

// Operand column (hook-parity audit item 0.6, docs/plans/hook-parity-lane0-
// brief.md step 3): for every native tool the guard can evaluate, WHICH
// input the guard judges. A gate hook receiver needs both halves of a
// native tool name before it can evaluate a pre-tool payload: the canonical
// action type (already this table) and the operand selector (this column).
// Today only internal/hook's claudeToolShape and the Cursor builder carry
// the second half, each as its own per-vendor table; this column makes the
// data exist once, for every adapter, so later lanes read one table.
//
// The values are abstract selectors, not literal JSON keys: a vendor whose
// shell tool names its input `cmd` still has Operand "command"; the reply
// dialect of each vendor maps the selector onto its own field name.

// Operand selectors. Closed set.
const (
	// OperandNone: the row is not evaluable by the guard (no operand).
	OperandNone = ""
	// OperandCommand: the shell command text.
	OperandCommand = "command"
	// OperandFilePath: the path of the one file read / written / edited.
	OperandFilePath = "file_path"
	// OperandNotebookPath: a notebook path (Claude Code NotebookEdit).
	OperandNotebookPath = "notebook_path"
	// OperandPath: a directory or search-root path.
	OperandPath = "path"
	// OperandURL: the URL fetched or browsed.
	OperandURL = "url"
	// OperandQuery: a web search query.
	OperandQuery = "query"
	// OperandMCPName: the MCP tool name itself (server + tool).
	OperandMCPName = "mcp_name"
)

// Operands returns the closed operand vocabulary, OperandNone first.
func Operands() []string {
	return []string{
		OperandNone, OperandCommand, OperandFilePath, OperandNotebookPath,
		OperandPath, OperandURL, OperandQuery, OperandMCPName,
	}
}

// operandByActionType is the default operand per canonical action type: the
// action types the guard classifies as evaluable (guard.ClassifyActionType)
// and the input each one is judged on. An action type missing here has no
// operand.
var operandByActionType = map[string]string{
	ActionRunCommand:    OperandCommand,
	ActionReadFile:      OperandFilePath,
	ActionWriteFile:     OperandFilePath,
	ActionEditFile:      OperandFilePath,
	ActionConfigChange:  OperandFilePath,
	ActionMCPCall:       OperandMCPName,
	ActionWebFetch:      OperandURL,
	ActionWebSearch:     OperandQuery,
	ActionBrowserAction: OperandURL,
}

// operandOverride pins a row whose operand differs from its action type's
// default, keyed by (tool, native). Rows here are grounded in the receiver
// that already evaluates them (internal/hook/guarded.go::claudeToolShape),
// which TestClaudeToolShapeMatchesTooltax keeps in agreement.
var operandOverride = map[[2]string]string{
	// NotebookEdit names its operand notebook_path.
	{toolClaudeCode, "NotebookEdit"}: OperandNotebookPath,
	// Grep returns file content, so the guard judges its search path
	// (claudeToolShape classifies it as a read of that path).
	{toolClaudeCode, "Grep"}: OperandPath,
	// Monitor runs its command as a background shell script; the guard
	// judges the command (claudeToolShape classifies it as run_command).
	{toolClaudeCode, "Monitor"}: OperandCommand,
}

// operandFor resolves one row's operand: an explicit override, else the
// action type's default.
func operandFor(tool, native, actionType string) string {
	if op, ok := operandOverride[[2]string{tool, native}]; ok {
		return op
	}
	return operandByActionType[actionType]
}

// withOperands stamps the Operand column on every row of t in place and
// returns it.
func withOperands(t []Entry) []Entry {
	for i := range t {
		t[i].Operand = operandFor(t[i].Tool, t[i].Native, t[i].ActionType)
	}
	return t
}
