package controlcoverage

import (
	"sort"
	"strings"

	"github.com/marmutapp/superbased-observer/internal/integration"
)

// Hook capability matrix (hook-parity audit section 4, Lane G): the
// all-adapter "vendor surface vs Observer wiring" grid, GENERATED from the
// registry's hook bindings and capsFor, never hand-maintained. Every surface
// that shows hook coverage (`observer adapters --hooks`, the node Security
// page, the org Control coverage tab) renders these rows, so a lane that adds
// or wires a binding shows up everywhere without a per-vendor edit.

// CapGroup is one matrix column: a named set of capabilities shown together
// (the audit's section 4.2 columns, plus an Observe column for the
// fire-and-forget family).
type CapGroup struct {
	// ID is the stable column key.
	ID string
	// Label is the human column header.
	Label string
	// Caps are the capabilities folded into the column, in display order.
	Caps []integration.HookCap
}

// capGroups is the ordered column table. Every capability of the closed
// vocabulary appears in exactly one group (pinned by a test).
var capGroups = []CapGroup{
	{"tool_gate", "Tool gate", []integration.HookCap{integration.HookCapToolGate, integration.HookCapToolRewrite}},
	{"prompt_gate", "Prompt gate", []integration.HookCap{integration.HookCapPromptGate}},
	{"session", "Session", []integration.HookCap{integration.HookCapSessionLifecycle}},
	{"turn", "Turn end", []integration.HookCap{integration.HookCapTurnLifecycle, integration.HookCapStopGate}},
	{"subagent", "Subagent", []integration.HookCap{integration.HookCapSubagentLifecycle}},
	{"compact", "Compact", []integration.HookCap{integration.HookCapCompactLifecycle}},
	{"delivery", "Inject / message", []integration.HookCap{integration.HookCapContextInject, integration.HookCapUserMessage}},
	{"observe", "Observe", []integration.HookCap{
		integration.HookCapToolObserve, integration.HookCapToolFailureObserve, integration.HookCapModelCallObserve,
		integration.HookCapPermissionObserve, integration.HookCapPermissionGate, integration.HookCapEnvironmentObserve,
		integration.HookCapNotificationObserve, integration.HookCapTaskObserve,
	}},
}

// CapGroups returns the matrix columns, in order.
func CapGroups() []CapGroup {
	out := make([]CapGroup, len(capGroups))
	copy(out, capGroups)
	return out
}

// stateRank orders coverage states best-first: a column shows the best state
// among its capabilities.
var stateRank = map[CoverageState]int{
	CoverageWired:         4,
	CoverageRegistered:    3,
	CoverageProbeRequired: 2,
	CoverageVendorOnly:    1,
	CoverageNone:          0,
}

// BetterState reports whether a is a stronger coverage state than b.
func BetterState(a, b CoverageState) bool { return stateRank[a] > stateRank[b] }

// decisionLetters spells a binding's reply decisions the way the audit's
// section 4.1 legend does: D deny, A ask, M modify, C context, U user
// message (allow is implied and not spelled).
var decisionLetters = []struct {
	decision, letter string
}{
	{integration.HookDecisionDeny, "D"},
	{integration.HookDecisionAsk, "A"},
	{integration.HookDecisionModify, "M"},
	{integration.HookDecisionContext, "C"},
	{integration.HookDecisionMessage, "U"},
}

// DecisionLetters renders b's decisions as the legend letters; "O" (observe
// only) when it expresses none of them.
func DecisionLetters(b integration.HookBinding) string {
	var out []string
	for _, d := range decisionLetters {
		if b.HasDecision(d.decision) {
			out = append(out, d.letter)
		}
	}
	if len(out) == 0 {
		return "O"
	}
	return strings.Join(out, " ")
}

// BindingView is one binding as the coverage surfaces show it.
type BindingView struct {
	Cap         integration.HookCap
	Phase       string
	NativeEvent string
	// Decisions is the legend spelling (DecisionLetters).
	Decisions  string
	Registered bool
	Wired      bool
	// Grounding is the binding's grounding kind (doc / fixture / live /
	// inferred) and Source its evidence.
	Grounding string
	Source    string
	Note      string
}

// MatrixCell is one (tool, column) cell.
type MatrixCell struct {
	Group string
	// State is the best coverage state among the column's capabilities.
	State CoverageState
	// Events lists the column's native events with their decision letters
	// ("PreToolUse D A M"), registry order, de-duplicated.
	Events []string
	// AltSeam is the first non-hook seam for the column's primary (first)
	// capability, AltSeamNone when the ladder has none.
	AltSeam string
}

// Mark is the audit's Observer-wiring letter for the cell: W wired, R
// registered, "-" otherwise.
func (c MatrixCell) Mark() string {
	switch c.State {
	case CoverageWired:
		return "W"
	case CoverageRegistered:
		return "R"
	case CoverageProbeRequired:
		return "probe"
	default:
		return "-"
	}
}

// MatrixRow is one tool's row of the matrix.
type MatrixRow struct {
	Tool string
	// Mechanism is the registry's hook mechanism ("" when none).
	Mechanism string
	// AutoWired mirrors the registry: false means the mechanism is
	// registered by hand / `observer init` only, never auto-registered.
	AutoWired bool
	// Note is the honest-zero reason for a row with no hook surface.
	Note     string
	Cells    []MatrixCell
	Caps     map[integration.HookCap]CapCoverage
	Bindings []BindingView
}

// HookMatrix returns the matrix for every registry row, tool-sorted.
func HookMatrix() []MatrixRow {
	caps := integration.Capabilities()
	sort.Slice(caps, func(i, j int) bool { return caps[i].Tool < caps[j].Tool })
	out := make([]MatrixRow, 0, len(caps))
	for _, c := range caps {
		out = append(out, matrixRowFor(c))
	}
	return out
}

// HookMatrixFor returns one tool's matrix row (ok=false for an unknown tool).
func HookMatrixFor(tool string) (MatrixRow, bool) {
	c, ok := integration.For(tool)
	if !ok {
		return MatrixRow{}, false
	}
	return matrixRowFor(c), true
}

func matrixRowFor(c integration.Capability) MatrixRow {
	row := MatrixRow{Tool: c.Tool, AutoWired: c.Hook.AutoWired, Caps: capsFor(c)}
	if c.Hook.Mechanism != integration.HookNone {
		row.Mechanism = string(c.Hook.Mechanism)
	}
	if len(c.Hook.Bindings) == 0 {
		row.Note = c.Hook.Note
	}
	for _, b := range c.Hook.Bindings {
		row.Bindings = append(row.Bindings, BindingView{
			Cap: b.Cap, Phase: b.Phase, NativeEvent: b.NativeEvent, Decisions: DecisionLetters(b),
			Registered: b.Registered, Wired: b.Wired,
			Grounding: string(b.Grounding.Kind), Source: b.Grounding.Source, Note: b.Note,
		})
	}
	for _, g := range capGroups {
		cell := MatrixCell{Group: g.ID, State: CoverageNone}
		seen := map[string]bool{}
		for i, cp := range g.Caps {
			cov := row.Caps[cp]
			if BetterState(cov.State, cell.State) {
				cell.State = cov.State
			}
			if i == 0 {
				cell.AltSeam = cov.AltSeam
			}
			for _, b := range c.Hook.Bindings {
				if b.Cap != cp {
					continue
				}
				ev := b.NativeEvent + " " + DecisionLetters(b)
				if !seen[ev] {
					seen[ev] = true
					cell.Events = append(cell.Events, ev)
				}
			}
		}
		row.Cells = append(row.Cells, cell)
	}
	return row
}

// CapSummary is one capability's fleet-of-adapters count (audit section
// 4.3): how many registry rows the vendor surface provides it on, how many
// Observer wires, registers, or can only reach through a probe.
type CapSummary struct {
	Cap           integration.HookCap
	Provides      int
	Wired         int
	Registered    int
	ProbeRequired int
}

// Gap is how many providing rows Observer does not wire.
func (s CapSummary) Gap() int { return s.Provides - s.Wired }

// CapSummaries returns the per-capability counts over every registry row,
// in vocabulary order.
func CapSummaries() []CapSummary { return summariesOver(HookMatrix()) }

// summariesOver counts the per-capability states over rows.
func summariesOver(rows []MatrixRow) []CapSummary {
	out := make([]CapSummary, 0, len(integration.HookCaps()))
	for _, cp := range integration.HookCaps() {
		s := CapSummary{Cap: cp}
		for _, r := range rows {
			switch r.Caps[cp].State {
			case CoverageWired:
				s.Wired++
				s.Provides++
			case CoverageRegistered:
				s.Registered++
				s.Provides++
			case CoverageProbeRequired:
				s.ProbeRequired++
				s.Provides++
			case CoverageVendorOnly:
				s.Provides++
			}
		}
		out = append(out, s)
	}
	return out
}
