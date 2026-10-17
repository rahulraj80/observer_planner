package controlcoverage

// The wire view of the hook-capability matrix: one JSON shape every surface
// serves (`observer adapters --hooks --json`, the node dashboard's
// /api/guard/hook-coverage, the org /api/org/guard/coverage tool_caps), so
// the node Security page and the org Control coverage tab render the same
// generated data. Pure value types with JSON tags; no I/O.

// MatrixView is the matrix payload.
type MatrixView struct {
	Groups    []GroupView   `json:"groups"`
	Rows      []RowView     `json:"rows"`
	Summaries []SummaryView `json:"summaries"`
}

// GroupView is one matrix column.
type GroupView struct {
	ID    string   `json:"id"`
	Label string   `json:"label"`
	Caps  []string `json:"caps"`
}

// CellView is one (tool, column) cell.
type CellView struct {
	Group   string   `json:"group"`
	State   string   `json:"state"`
	Mark    string   `json:"mark"`
	Events  []string `json:"events"`
	AltSeam string   `json:"alt_seam,omitempty"`
}

// CapView is one capability's coverage on one tool.
type CapView struct {
	State   string `json:"state"`
	AltSeam string `json:"alt_seam,omitempty"`
}

// BindingJSON is one binding.
type BindingJSON struct {
	Cap         string `json:"cap"`
	Phase       string `json:"phase,omitempty"`
	NativeEvent string `json:"native_event"`
	Decisions   string `json:"decisions"`
	Registered  bool   `json:"registered"`
	Wired       bool   `json:"wired"`
	Grounding   string `json:"grounding"`
	Source      string `json:"source,omitempty"`
	Note        string `json:"note,omitempty"`
}

// RowView is one tool's row.
type RowView struct {
	Tool      string             `json:"tool"`
	Mechanism string             `json:"mechanism,omitempty"`
	AutoWired bool               `json:"auto_wired"`
	Note      string             `json:"note,omitempty"`
	Cells     []CellView         `json:"cells"`
	Caps      map[string]CapView `json:"caps"`
	Bindings  []BindingJSON      `json:"bindings"`
}

// SummaryView is one capability's count over the rows in the view.
type SummaryView struct {
	Cap           string `json:"cap"`
	Provides      int    `json:"provides"`
	Wired         int    `json:"wired"`
	Registered    int    `json:"registered"`
	ProbeRequired int    `json:"probe_required"`
	Gap           int    `json:"gap"`
}

// GroupsView returns the columns as wire values.
func GroupsView() []GroupView {
	out := make([]GroupView, 0, len(capGroups))
	for _, g := range capGroups {
		gv := GroupView{ID: g.ID, Label: g.Label, Caps: []string{}}
		for _, c := range g.Caps {
			gv.Caps = append(gv.Caps, string(c))
		}
		out = append(out, gv)
	}
	return out
}

// RowViewOf converts one matrix row.
func RowViewOf(r MatrixRow) RowView {
	rv := RowView{
		Tool: r.Tool, Mechanism: r.Mechanism, AutoWired: r.AutoWired, Note: r.Note,
		Cells: []CellView{}, Caps: map[string]CapView{}, Bindings: []BindingJSON{},
	}
	for _, c := range r.Cells {
		rv.Cells = append(rv.Cells, CellView{
			Group: c.Group, State: string(c.State), Mark: c.Mark(),
			Events: append([]string{}, c.Events...), AltSeam: c.AltSeam,
		})
	}
	for cp, cov := range r.Caps {
		rv.Caps[string(cp)] = CapView{State: string(cov.State), AltSeam: cov.AltSeam}
	}
	for _, b := range r.Bindings {
		rv.Bindings = append(rv.Bindings, BindingJSON{
			Cap: string(b.Cap), Phase: b.Phase, NativeEvent: b.NativeEvent,
			Decisions: b.Decisions, Registered: b.Registered, Wired: b.Wired, Grounding: b.Grounding, Source: b.Source, Note: b.Note,
		})
	}
	return rv
}

// HookMatrixView returns the matrix view. With tools, only those rows (in
// the given order, unknown tools skipped) and the summaries count only them;
// without, every registry row.
func HookMatrixView(tools ...string) MatrixView {
	var rows []MatrixRow
	if len(tools) == 0 {
		rows = HookMatrix()
	} else {
		seen := map[string]bool{}
		for _, t := range tools {
			if seen[t] {
				continue
			}
			seen[t] = true
			if r, ok := HookMatrixFor(t); ok {
				rows = append(rows, r)
			}
		}
	}
	out := MatrixView{Groups: GroupsView(), Rows: []RowView{}, Summaries: []SummaryView{}}
	for _, r := range rows {
		out.Rows = append(out.Rows, RowViewOf(r))
	}
	for _, s := range summariesOver(rows) {
		out.Summaries = append(out.Summaries, SummaryView{
			Cap: string(s.Cap), Provides: s.Provides, Wired: s.Wired,
			Registered: s.Registered, ProbeRequired: s.ProbeRequired, Gap: s.Gap(),
		})
	}
	return out
}
