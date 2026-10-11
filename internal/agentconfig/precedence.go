package agentconfig

import (
	"errors"
	"fmt"
	"sort"

	"github.com/marmutapp/superbased-observer/internal/cfgfmt"
)

// Layers and precedence (plan section 3, operator ruling 7 plus D-122 and
// D-123). Observer authors values in six layers above the tools' own
// files; Resolve computes the one effective value of one setting from every
// candidate, walking the precedence TABLE (never a nested conditional).

// Layer is one authored or vendor layer.
type Layer string

const (
	// LayerVendorDefault: the tool's built-in default (catalogue Default).
	LayerVendorDefault Layer = "vendor_default"
	// LayerL1: the developer's all-adapters global value (common keys).
	LayerL1 Layer = "l1_all_adapters"
	// LayerL2: the developer's per-adapter global value.
	LayerL2 Layer = "l2_adapter"
	// LayerL3: the developer's per-project value.
	LayerL3 Layer = "l3_project"
	// LayerO1 / O2 / O3: org-wide, team and member org layers.
	LayerO1 Layer = "o1_org"
	LayerO2 Layer = "o2_team"
	LayerO3 Layer = "o3_member"
	// LayerVendorManaged: the vendor's own managed layer (lever A, MDM).
	LayerVendorManaged Layer = "vendor_managed"
)

// Mode is an org value's mode. Developer and vendor layers carry none.
type Mode string

const (
	ModeNone     Mode = ""
	ModeDefault  Mode = "default"
	ModeEnforced Mode = "enforced"
)

// PrecedenceRow is one row of the section 3.2 table.
type PrecedenceRow struct {
	// Rank is the row's rank, lowest first, when the developer holds no edit
	// grant for the setting.
	Rank int
	// GrantRank is the rank when the developer DOES hold an edit grant
	// (D-122): the developer's explicit L1-L3 value then beats an org
	// default, which only fills a gap. Enforced and vendor-managed rows
	// keep their rank.
	GrantRank int
	Layer     Layer
	Mode      Mode
	// ManagedOnly rows apply on managed nodes only; an individual node has
	// ranks 1 to 4 (plus the vendor's own managed layer, which the tool
	// applies whether or not Observer manages the node).
	ManagedOnly bool
	Note        string
}

// precedenceTable is section 3.2, lowest to highest.
var precedenceTable = []PrecedenceRow{
	{Rank: 1, GrantRank: 1, Layer: LayerVendorDefault, Note: "the catalogue Default"},
	{Rank: 2, GrantRank: 5, Layer: LayerL1, Note: "common keys only"},
	{Rank: 3, GrantRank: 6, Layer: LayerL2, Note: "beats L1 for that tool"},
	{Rank: 4, GrantRank: 7, Layer: LayerL3, Note: "beats L2 inside the project"},
	{Rank: 5, GrantRank: 2, Layer: LayerO1, Mode: ModeDefault, ManagedOnly: true, Note: "D-122: above L1-L3 unless the developer holds an edit grant"},
	{Rank: 6, GrantRank: 3, Layer: LayerO2, Mode: ModeDefault, ManagedOnly: true},
	{Rank: 7, GrantRank: 4, Layer: LayerO3, Mode: ModeDefault, ManagedOnly: true},
	{Rank: 8, GrantRank: 8, Layer: LayerO1, Mode: ModeEnforced, ManagedOnly: true, Note: "org-enforced always wins over every local layer"},
	{Rank: 9, GrantRank: 9, Layer: LayerO2, Mode: ModeEnforced, ManagedOnly: true},
	{Rank: 10, GrantRank: 10, Layer: LayerO3, Mode: ModeEnforced, ManagedOnly: true, Note: "most specific enforced wins, except ordered-danger keys (strictest)"},
	{Rank: 11, GrantRank: 11, Layer: LayerVendorManaged, Note: "physically the top of the tool's own chain"},
}

// PrecedenceTable returns the section 3.2 table (a copy).
func PrecedenceTable() []PrecedenceRow { return append([]PrecedenceRow(nil), precedenceTable...) }

func precedenceRow(l Layer, m Mode) (PrecedenceRow, bool) {
	for _, r := range precedenceTable {
		if r.Layer == l && r.Mode == m {
			return r, true
		}
	}
	return PrecedenceRow{}, false
}

func isOrgLayer(l Layer) bool { return l == LayerO1 || l == LayerO2 || l == LayerO3 }

// Candidate is one layer's value for the setting being resolved.
type Candidate struct {
	Layer Layer
	Mode  Mode
	Value any
	// Team is the O2 team the value came from (the conflict rule needs it).
	Team string
	// Source names the file or policy document, for the UI.
	Source string
}

// ResolveInput is everything Resolve needs for one setting.
type ResolveInput struct {
	// Managed: the node is org-managed (org layers apply).
	Managed bool
	// Grant: the developer holds an edit grant for this setting (D-122).
	Grant bool
	// Order and Enum: the setting's ordered-danger rule (CommonSpec.Order /
	// Enum for a common key); OrderNone resolves by specificity.
	Order OrderKind
	Enum  []string
	// TeamPriority is the policy's team priority order (D-123), first wins.
	TeamPriority []string
	Candidates   []Candidate
}

// Ignored is a candidate that does not take part, with the reason.
type Ignored struct {
	Candidate Candidate
	Reason    string
}

// Resolution is the effective value and how it was reached.
type Resolution struct {
	// Set is false when no candidate supplies a value.
	Set    bool
	Value  any
	Winner Candidate
	// Rank is the winner's rank in the table as applied (GrantRank when
	// the developer holds a grant).
	Rank int
	// Merged: the winner was composed from several org candidates
	// (strictest / union / intersect); Contributors lists them.
	Merged       bool
	Contributors []Candidate
	// Shadowed lists the losing candidates, highest rank first: what the UI
	// names as "set here, but overridden by ...".
	Shadowed []Candidate
	Ignored  []Ignored
}

// Errors Resolve returns.
var (
	// ErrTeamConflict: a member's teams set different values for a key
	// that is not ordered-danger, and the policy's team priority does not
	// order them (D-123). Publishing is blocked by the same rule
	// (LintTeamConflicts), so a node only sees this on a malformed policy.
	ErrTeamConflict = errors.New("agentconfig: conflicting team values with no team priority")
	// ErrLayerConflict: two different values in one non-team layer.
	ErrLayerConflict = errors.New("agentconfig: two values in one layer")
	// ErrBadCandidate: an unknown layer, or a mode on the wrong layer.
	ErrBadCandidate = errors.New("agentconfig: malformed candidate")
)

// Resolve computes the effective value of one setting.
func Resolve(in ResolveInput) (Resolution, error) {
	var res Resolution
	type ranked struct {
		c       Candidate
		rank    int
		merged  bool
		contrib []Candidate
	}
	groups := map[PrecedenceRow][]Candidate{}
	orgTier := map[Mode][]Candidate{}
	for _, c := range in.Candidates {
		row, ok := precedenceRow(c.Layer, c.Mode)
		if !ok {
			return res, fmt.Errorf("%w: layer %q mode %q", ErrBadCandidate, c.Layer, c.Mode)
		}
		if row.ManagedOnly && !in.Managed {
			res.Ignored = append(res.Ignored, Ignored{c, "an individual node applies ranks 1 to 4 (and the vendor's own managed layer) only"})
			continue
		}
		if in.Order != OrderNone && isOrgLayer(c.Layer) {
			orgTier[c.Mode] = append(orgTier[c.Mode], c)
			continue
		}
		groups[row] = append(groups[row], c)
	}
	var all []ranked
	rankOf := func(row PrecedenceRow) int {
		if in.Grant {
			return row.GrantRank
		}
		return row.Rank
	}
	for row, cs := range groups {
		c, err := collapseLayer(row, cs, in.TeamPriority)
		if err != nil {
			return res, err
		}
		all = append(all, ranked{c: c, rank: rankOf(row)})
	}
	// Ordered-danger keys: strictest-wins across every org layer of a mode
	// tier; the merged value sits at the most specific contributing layer.
	for _, mode := range []Mode{ModeDefault, ModeEnforced} {
		cs := orgTier[mode]
		if len(cs) == 0 {
			continue
		}
		merged, err := mergeOrdered(in.Order, in.Enum, cs)
		if err != nil {
			return res, err
		}
		row, _ := precedenceRow(merged.Layer, mode)
		all = append(all, ranked{c: merged, rank: rankOf(row), merged: len(cs) > 1, contrib: cs})
	}
	if len(all) == 0 {
		return res, nil
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].rank > all[j].rank })
	w := all[0]
	res.Set, res.Value, res.Winner, res.Rank = true, w.c.Value, w.c, w.rank
	res.Merged = w.merged
	if w.merged {
		res.Contributors = w.contrib
	}
	for _, r := range all[1:] {
		res.Shadowed = append(res.Shadowed, r.c)
	}
	return res, nil
}

// collapseLayer reduces one layer's candidates to one. Equal values
// collapse; differing team values follow the team priority (D-123).
func collapseLayer(row PrecedenceRow, cs []Candidate, priority []string) (Candidate, error) {
	distinct := []Candidate{cs[0]}
	for _, c := range cs[1:] {
		dup := false
		for _, d := range distinct {
			if equalValue(c.Value, d.Value) {
				dup = true
				break
			}
		}
		if !dup {
			distinct = append(distinct, c)
		}
	}
	if len(distinct) == 1 {
		return distinct[0], nil
	}
	if row.Layer != LayerO2 {
		return Candidate{}, fmt.Errorf("%w: %s %s", ErrLayerConflict, row.Layer, row.Mode)
	}
	if c, ok := byTeamPriority(cs, priority); ok {
		return c, nil
	}
	teams := make([]string, 0, len(cs))
	for _, c := range cs {
		teams = append(teams, c.Team)
	}
	return Candidate{}, fmt.Errorf("%w: teams %v (%s)", ErrTeamConflict, teams, row.Mode)
}

// byTeamPriority picks the candidate of the first-listed team, provided
// every conflicting team is listed (an unlisted team means the admin has
// not ordered it, so the conflict stands).
func byTeamPriority(cs []Candidate, priority []string) (Candidate, bool) {
	pos := map[string]int{}
	for i, t := range priority {
		if _, seen := pos[t]; !seen {
			pos[t] = i
		}
	}
	best, bestPos := Candidate{}, -1
	for _, c := range cs {
		p, ok := pos[c.Team]
		if !ok || c.Team == "" {
			return Candidate{}, false
		}
		if bestPos < 0 || p < bestPos {
			best, bestPos = c, p
		}
	}
	return best, bestPos >= 0
}

// mergeOrdered composes an ordered-danger value from several org
// candidates of one mode tier.
func mergeOrdered(order OrderKind, enum []string, cs []Candidate) (Candidate, error) {
	top := cs[0]
	for _, c := range cs[1:] {
		if layerSpecificity(c.Layer) > layerSpecificity(top.Layer) {
			top = c
		}
	}
	out := Candidate{Layer: top.Layer, Mode: top.Mode, Team: top.Team, Source: "merged (" + string(order) + ")"}
	switch order {
	case OrderEnum:
		best, bestIdx := Candidate{}, -1
		for _, c := range cs {
			idx := indexOf(enum, c.Value)
			if idx < 0 {
				return Candidate{}, fmt.Errorf("%w: %v is not one of %v", ErrBadValue, c.Value, enum)
			}
			if bestIdx < 0 || idx < bestIdx || (idx == bestIdx && layerSpecificity(c.Layer) > layerSpecificity(best.Layer)) {
				best, bestIdx = c, idx
			}
		}
		out.Value, out.Layer, out.Team, out.Source = best.Value, best.Layer, best.Team, best.Source
	case OrderTrueStrict:
		v := false
		for _, c := range cs {
			b, ok := normalized(c.Value).(bool)
			if !ok {
				return Candidate{}, fmt.Errorf("%w: %v is not a bool", ErrBadValue, c.Value)
			}
			v = v || b
		}
		out.Value = v
	case OrderUnion, OrderIntersect:
		lists := make([][]any, 0, len(cs))
		for _, c := range layerOrdered(cs) {
			l, ok := normalized(c.Value).([]any)
			if !ok {
				return Candidate{}, fmt.Errorf("%w: %v is not a list", ErrBadValue, c.Value)
			}
			lists = append(lists, l)
		}
		if order == OrderUnion {
			out.Value = unionLists(lists)
		} else {
			out.Value = intersectLists(lists)
		}
	default:
		return Candidate{}, fmt.Errorf("%w: order %q", ErrBadCandidate, order)
	}
	return out, nil
}

func layerSpecificity(l Layer) int {
	switch l {
	case LayerO1:
		return 1
	case LayerO2:
		return 2
	case LayerO3:
		return 3
	}
	return 0
}

// layerOrdered sorts org candidates org-wide first (stable), so a merged
// list keeps the broadest layer's order.
func layerOrdered(cs []Candidate) []Candidate {
	out := append([]Candidate(nil), cs...)
	sort.SliceStable(out, func(i, j int) bool { return layerSpecificity(out[i].Layer) < layerSpecificity(out[j].Layer) })
	return out
}

func indexOf(enum []string, v any) int {
	s, ok := v.(string)
	if !ok {
		return -1
	}
	for i, e := range enum {
		if e == s {
			return i
		}
	}
	return -1
}

func unionLists(lists [][]any) []any {
	out := []any{}
	for _, l := range lists {
		for _, v := range l {
			if !containsValue(out, v) {
				out = append(out, v)
			}
		}
	}
	return out
}

func intersectLists(lists [][]any) []any {
	out := []any{}
	for _, v := range lists[0] {
		in := !containsValue(out, v)
		for _, l := range lists[1:] {
			in = in && containsValue(l, v)
		}
		if in {
			out = append(out, v)
		}
	}
	return out
}

func containsValue(list []any, v any) bool {
	for _, e := range list {
		if cfgfmt.Equal(e, v) {
			return true
		}
	}
	return false
}

func equalValue(a, b any) bool { return cfgfmt.Equal(normalized(a), normalized(b)) }

// --- D-123: the publish-time team conflict lint ------------------------------

// TeamValue is one team document's value for one setting.
type TeamValue struct {
	Team  string
	Mode  Mode
	Value any
}

// TeamConflict is one member whose teams disagree with no resolution.
type TeamConflict struct {
	Member string
	Mode   Mode
	Teams  []string
	Values []any
}

// LintTeamConflicts lists every member whose teams set different values of
// one setting in the same mode, where the key is not ordered-danger and the
// policy's team priority does not order every conflicting team (D-123:
// publishing is blocked while the list is non-empty). memberships maps a
// member to its teams. The output is sorted by member, then mode.
func LintTeamConflicts(order OrderKind, priority []string, values []TeamValue, memberships map[string][]string) []TeamConflict {
	if order != OrderNone {
		return nil // ordered-danger keys resolve strictest-wins
	}
	byTeam := map[string][]TeamValue{}
	for _, v := range values {
		byTeam[v.Team] = append(byTeam[v.Team], v)
	}
	members := make([]string, 0, len(memberships))
	for m := range memberships {
		members = append(members, m)
	}
	sort.Strings(members)
	var out []TeamConflict
	for _, m := range members {
		for _, mode := range []Mode{ModeDefault, ModeEnforced} {
			var cs []Candidate
			for _, t := range memberships[m] {
				for _, v := range byTeam[t] {
					if v.Mode == mode {
						cs = append(cs, Candidate{Layer: LayerO2, Mode: mode, Team: t, Value: v.Value})
					}
				}
			}
			if len(cs) < 2 {
				continue
			}
			row, _ := precedenceRow(LayerO2, mode)
			if _, err := collapseLayer(row, cs, priority); err == nil {
				continue
			}
			tc := TeamConflict{Member: m, Mode: mode}
			for _, c := range cs {
				tc.Teams = append(tc.Teams, c.Team)
				tc.Values = append(tc.Values, c.Value)
			}
			out = append(out, tc)
		}
	}
	return out
}

// --- the tool's own file chain ---------------------------------------------

// FileValue is a native value as one file holds it.
type FileValue struct {
	FileID string
	Value  any
}

// ResolveFiles applies the TOOL's own file precedence (FileRow.Precedence,
// higher wins) to the values several files hold for one setting: what the
// tool will actually use when Observer's projection meets the tool's chain
// (project over user, managed over all). Shadowed is highest first.
func ResolveFiles(values []FileValue) (winner FileValue, shadowed []FileValue, err error) {
	if len(values) == 0 {
		return FileValue{}, nil, nil
	}
	prec := map[string]int{}
	for _, f := range filesTable() {
		prec[f.ID] = f.Precedence
	}
	vs := append([]FileValue(nil), values...)
	for _, v := range vs {
		if _, ok := prec[v.FileID]; !ok {
			return FileValue{}, nil, fmt.Errorf("%w: unknown file %q", ErrBadCandidate, v.FileID)
		}
	}
	sort.SliceStable(vs, func(i, j int) bool { return prec[vs[i].FileID] > prec[vs[j].FileID] })
	return vs[0], vs[1:], nil
}
