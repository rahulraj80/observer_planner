package agentconfigorg

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/marmutapp/superbased-observer/internal/agentconfig"
	"github.com/marmutapp/superbased-observer/internal/cfgfmt"
	"github.com/marmutapp/superbased-observer/internal/orgcontract"
)

// Source is one published document that addresses the member being
// composed for, with the org layer it applies through.
type Source struct {
	Name  string
	Layer agentconfig.Layer // LayerO1, LayerO2 or LayerO3
	// Team is the team an O2 document reaches the member through (the D-123
	// priority needs it); "" for O1 and O3.
	Team string
	Spec Spec
}

// ComposeInput is everything Compose needs for one member.
type ComposeInput struct {
	Sources []Source
	// TeamPriority is the org's team order for conflicting team values
	// (D-123), first wins.
	TeamPriority []string
	// TeamRank is the stored per-team priority (D-123): among a member's
	// teams that set different values for one setting, the team with the
	// highest priority wins when that priority is unique; equal priorities
	// (including two unset, zero ones) leave the conflict. Ordered-danger
	// common keys ignore it (strictest wins across every team). When set it
	// is applied before TeamPriority.
	TeamRank map[string]int
}

// rankFilter keeps, of the O2 candidates, only those whose team holds the
// highest priority among them (D-123); other layers pass through.
func rankFilter(cs []agentconfig.Candidate, rank map[string]int) []agentconfig.Candidate {
	best, seen := 0, false
	for _, c := range cs {
		if c.Layer != agentconfig.LayerO2 {
			continue
		}
		if r := rank[c.Team]; !seen || r > best {
			best, seen = r, true
		}
	}
	if !seen {
		return cs
	}
	out := make([]agentconfig.Candidate, 0, len(cs))
	for _, c := range cs {
		if c.Layer == agentconfig.LayerO2 && rank[c.Team] != best {
			continue
		}
		out = append(out, c)
	}
	return out
}

// ComposeSkip is a setting the composition left out, with the reason (a
// team conflict that appeared after publishing, a tool a common value
// cannot reach).
type ComposeSkip struct {
	Setting string `json:"setting"`
	Project string `json:"project,omitempty"`
	Mode    string `json:"mode"`
	Reason  string `json:"reason"`
}

// Composed is one member's composed body plus what it left out.
type Composed struct {
	Body    orgcontract.AgentConfigPolicyBody
	Skipped []ComposeSkip
}

// candidateRef ties a resolver candidate back to its document entry.
type candidateRef struct {
	src   Source
	entry Entry
}

// group is every candidate for one (target, project, mode).
type group struct {
	target, project, mode string
	common                bool
	cands                 []agentconfig.Candidate
}

// Compose builds the member's body: per setting and project the enforced
// value (most specific org layer, or strictest for an ordered-danger common
// key) and, where nothing is enforced, the default value.
func Compose(in ComposeInput) (Composed, error) {
	srcs := append([]Source(nil), in.Sources...)
	for _, s := range srcs {
		switch s.Layer {
		case agentconfig.LayerO1, agentconfig.LayerO2, agentconfig.LayerO3:
		default:
			return Composed{}, fmt.Errorf("agentconfigorg.Compose: %q is not an org layer", s.Layer)
		}
	}
	sort.SliceStable(srcs, func(i, j int) bool {
		if specificity(srcs[i].Layer) != specificity(srcs[j].Layer) {
			return specificity(srcs[i].Layer) < specificity(srcs[j].Layer)
		}
		if srcs[i].Team != srcs[j].Team {
			return srcs[i].Team < srcs[j].Team
		}
		return srcs[i].Name < srcs[j].Name
	})
	refs := map[string]candidateRef{}
	groups := map[string]*group{}
	var order []string
	for si, s := range srcs {
		for ei, e := range Normalize(s.Spec).Settings {
			key := strconv.Itoa(si) + ":" + strconv.Itoa(ei)
			refs[key] = candidateRef{src: s, entry: e}
			common, target := e.Common != "", e.Setting
			if common {
				target = e.Common
			}
			gk := fmt.Sprintf("%t\x00%s\x00%s\x00%s", common, target, e.Project, e.Mode)
			g, ok := groups[gk]
			if !ok {
				g = &group{target: target, project: e.Project, mode: e.Mode, common: common}
				groups[gk] = g
				order = append(order, gk)
			}
			g.cands = append(g.cands, agentconfig.Candidate{
				Layer: s.Layer, Mode: agentconfig.Mode(e.Mode), Value: e.Value, Team: s.Team, Source: key,
			})
		}
	}
	sort.Strings(order)

	type slot struct {
		d      orgcontract.AgentConfigDirective
		direct bool
	}
	slots := map[string]slot{} // setting \x00 project \x00 mode
	var out Composed
	put := func(d orgcontract.AgentConfigDirective, direct bool) {
		k := d.Setting + "\x00" + d.Project + "\x00" + d.Mode
		if prev, ok := slots[k]; ok && prev.direct && !direct {
			return // a tool-specific value beats a common-key expansion
		}
		slots[k] = slot{d: d, direct: direct}
	}
	for _, gk := range order {
		g := groups[gk]
		res, skip := resolveGroup(in, g)
		if skip != nil {
			out.Skipped = append(out.Skipped, *skip)
			continue
		}
		if !res.Set {
			continue
		}
		winner := res.Winner
		names, placement := attribution(res, refs)
		value := res.Value
		if g.common {
			natives, skips, err := ExpandCommon(agentconfig.CommonKey(g.target), value, g.project, placement)
			if err != nil {
				out.Skipped = append(out.Skipped, ComposeSkip{Setting: g.target, Project: g.project, Mode: g.mode, Reason: err.Error()})
				continue
			}
			for _, sk := range skips {
				out.Skipped = append(out.Skipped, ComposeSkip{Setting: sk.Tool + " (" + g.target + ")", Project: g.project, Mode: g.mode, Reason: sk.Reason})
			}
			for _, n := range natives {
				raw, err := json.Marshal(n.Value)
				if err != nil {
					return Composed{}, fmt.Errorf("agentconfigorg.Compose: %s: %w", n.Setting, err)
				}
				put(orgcontract.AgentConfigDirective{
					Setting: n.Setting, Value: raw, Mode: g.mode, Layer: string(winner.Layer), Policy: names,
					Common: g.target, Project: g.project, Placement: placement,
				}, false)
			}
			continue
		}
		raw, err := json.Marshal(value)
		if err != nil {
			return Composed{}, fmt.Errorf("agentconfigorg.Compose: %s: %w", g.target, err)
		}
		put(orgcontract.AgentConfigDirective{
			Setting: g.target, Value: raw, Mode: g.mode, Layer: string(winner.Layer), Policy: names,
			Project: g.project, Placement: placement,
		}, true)
	}
	// Enforced always wins (D-122): a default for a setting the org also
	// enforces is dropped.
	for k, s := range slots {
		if s.d.Mode != orgcontract.AgentConfigModeDefault {
			continue
		}
		if _, enforced := slots[s.d.Setting+"\x00"+s.d.Project+"\x00"+orgcontract.AgentConfigModeEnforced]; enforced {
			delete(slots, k)
		}
	}
	for _, s := range slots {
		out.Body.Directives = append(out.Body.Directives, s.d)
	}
	sort.Slice(out.Body.Directives, func(i, j int) bool {
		a, b := out.Body.Directives[i], out.Body.Directives[j]
		if a.Setting != b.Setting {
			return a.Setting < b.Setting
		}
		if a.Project != b.Project {
			return a.Project < b.Project
		}
		return a.Mode < b.Mode
	})
	composeKnobs(&out.Body, srcs)
	out.Body.Instructions = composeInstructions(srcs, in.TeamRank)
	return out, nil
}

// composeInstructions folds every document's org instructions into one block
// per scope (the user level, or one project): the texts in layer order (the
// org's first, then the member's teams by D-123 priority, then the member's
// own), joined by a blank line, so every layer's instruction reaches the
// developer (an instruction adds to, never replaces, another). The most
// specific contributing document decides a project block's placement.
func composeInstructions(srcs []Source, rank map[string]int) []orgcontract.AgentConfigInstruction {
	ordered := append([]Source(nil), srcs...)
	sort.SliceStable(ordered, func(i, j int) bool {
		a, b := ordered[i], ordered[j]
		if specificity(a.Layer) != specificity(b.Layer) {
			return specificity(a.Layer) < specificity(b.Layer)
		}
		if a.Layer == agentconfig.LayerO2 && rank[a.Team] != rank[b.Team] {
			return rank[a.Team] > rank[b.Team]
		}
		return false
	})
	type acc struct {
		texts, names     []string
		layer, placement string
		best             int
	}
	groups := map[string]*acc{}
	var order []string
	for _, s := range ordered {
		for _, in := range Normalize(s.Spec).Instructions {
			if strings.TrimSpace(in.Text) == "" {
				continue
			}
			g, ok := groups[in.Project]
			if !ok {
				g = &acc{best: -1}
				groups[in.Project] = g
				order = append(order, in.Project)
			}
			g.texts = append(g.texts, in.Text)
			if !containsString(g.names, s.Name) {
				g.names = append(g.names, s.Name)
			}
			if sp := specificity(s.Layer); sp >= g.best {
				g.best, g.layer, g.placement = sp, string(s.Layer), in.Placement
			}
		}
	}
	sort.Strings(order)
	var out []orgcontract.AgentConfigInstruction
	for _, p := range order {
		g := groups[p]
		out = append(out, orgcontract.AgentConfigInstruction{
			Project: p, Placement: g.placement, Text: strings.Join(g.texts, "\n\n"),
			Policy: strings.Join(g.names, ", "), Layer: g.layer,
		})
	}
	return out
}

func containsString(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// resolveGroup runs the precedence resolver over one group's candidates
// (after the D-123 team-rank filter for a key that is not ordered-danger).
// A refusal comes back as the skip to record.
func resolveGroup(in ComposeInput, g *group) (agentconfig.Resolution, *ComposeSkip) {
	ord, enum := agentconfig.OrderNone, []string(nil)
	if g.common {
		spec, _ := agentconfig.CommonSpecFor(agentconfig.CommonKey(g.target))
		ord, enum = spec.Order, spec.Enum
	}
	cands := g.cands
	if in.TeamRank != nil && ord == agentconfig.OrderNone {
		cands = rankFilter(cands, in.TeamRank)
	}
	res, err := agentconfig.Resolve(agentconfig.ResolveInput{
		Managed: true, Order: ord, Enum: enum, TeamPriority: in.TeamPriority, Candidates: cands,
	})
	if err != nil {
		reason := err.Error()
		if errors.Is(err, agentconfig.ErrTeamConflict) {
			reason = "team_conflict: the member's teams set different values and the team priority does not order them"
		}
		return agentconfig.Resolution{}, &ComposeSkip{Setting: g.target, Project: g.project, Mode: g.mode, Reason: reason}
	}
	return res, nil
}

// attribution names the document(s) a resolution came from and the
// placement of the most specific contributing entry.
func attribution(res agentconfig.Resolution, refs map[string]candidateRef) (string, string) {
	contrib := res.Contributors
	if !res.Merged {
		contrib = []agentconfig.Candidate{res.Winner}
	}
	var names []string
	seen := map[string]bool{}
	placement := ""
	best := -1
	for _, c := range contrib {
		ref, ok := refs[c.Source]
		if !ok {
			continue
		}
		if !seen[ref.src.Name] {
			seen[ref.src.Name] = true
			names = append(names, ref.src.Name)
		}
		if sp := specificity(ref.src.Layer); sp > best {
			best, placement = sp, ref.entry.Placement
		}
	}
	return strings.Join(names, ", "), placement
}

func specificity(l agentconfig.Layer) int {
	switch l {
	case agentconfig.LayerO1:
		return 1
	case agentconfig.LayerO2:
		return 2
	case agentconfig.LayerO3:
		return 3
	}
	return 0
}

// secretsStrictness orders the posture strictest last.
var secretsStrictness = map[string]int{
	orgcontract.AgentConfigSecretsAllow: 1, orgcontract.AgentConfigSecretsWarn: 2, orgcontract.AgentConfigSecretsRefuse: 3,
}

// composeKnobs folds the document-level fields: the secrets posture is
// strictest-wins (it is a safety knob); the drift response and the
// developer's default access come from the most specific document that sets
// them; grants are the union (a grant is how the admin opens a setting to a
// team or a member); the policy names list every contributing document.
func composeKnobs(b *orgcontract.AgentConfigPolicyBody, srcs []Source) {
	b.SecretsPosture = orgcontract.AgentConfigSecretsWarn
	strict := 0
	grants := map[string]bool{}
	names := map[string]bool{}
	accessAt, driftAt := -1, -1
	b.OnDrift = orgcontract.AgentConfigOnDriftReassert
	b.DeveloperAccess = orgcontract.AgentConfigAccessReadOnly
	for _, s := range srcs {
		names[s.Name] = true
		sp := specificity(s.Layer)
		if v := s.Spec.SecretsPosture; v != "" && secretsStrictness[v] > strict {
			strict, b.SecretsPosture = secretsStrictness[v], v
		}
		if v := s.Spec.OnDrift; v != "" && sp >= driftAt {
			driftAt, b.OnDrift = sp, v
		}
		if v := s.Spec.DeveloperAccess; v != "" && sp >= accessAt {
			accessAt, b.DeveloperAccess = sp, v
		}
		for _, g := range s.Spec.Grants {
			grants[strings.TrimSpace(g)] = true
		}
	}
	for g := range grants {
		if g != "" {
			b.Grants = append(b.Grants, orgcontract.AgentConfigGrant{Setting: g})
		}
	}
	sort.Slice(b.Grants, func(i, j int) bool { return b.Grants[i].Setting < b.Grants[j].Setting })
	for n := range names {
		b.Policies = append(b.Policies, n)
	}
	sort.Strings(b.Policies)
}

// TeamDoc is one published team-targeted document, for the publish lint.
type TeamDoc struct {
	Name  string
	Teams []string
	Spec  Spec
}

// LintTeamConflictsRanked is LintTeamConflicts with the stored integer team
// priority (ComposeInput.TeamRank): for each member and target, only the
// teams holding the highest priority among those that set it count, and the
// member is in conflict when those teams still disagree. Ordered-danger
// common keys never conflict (strictest wins). Publishing is blocked while
// the list is non-empty (D-123).
func LintTeamConflictsRanked(docs []TeamDoc, rank map[string]int, memberships map[string][]string) []TeamConflict {
	type tk struct{ target, project, mode string }
	type tv struct {
		team  string
		value any
	}
	values := map[tk][]tv{}
	for _, d := range docs {
		for _, e := range Normalize(d.Spec).Settings {
			target := "setting:" + e.Setting
			if e.Common != "" {
				target = "common:" + e.Common
				if spec, ok := agentconfig.CommonSpecFor(agentconfig.CommonKey(e.Common)); ok && spec.Order != agentconfig.OrderNone {
					continue
				}
			}
			k := tk{target, e.Project, e.Mode}
			for _, t := range d.Teams {
				values[k] = append(values[k], tv{team: t, value: e.Value})
			}
		}
	}
	keys := make([]tk, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].target != keys[j].target {
			return keys[i].target < keys[j].target
		}
		if keys[i].project != keys[j].project {
			return keys[i].project < keys[j].project
		}
		return keys[i].mode < keys[j].mode
	})
	members := make([]string, 0, len(memberships))
	for m := range memberships {
		members = append(members, m)
	}
	sort.Strings(members)
	var out []TeamConflict
	for _, k := range keys {
		for _, m := range members {
			on := map[string]bool{}
			for _, t := range memberships[m] {
				on[t] = true
			}
			var mine []tv
			best, seen := 0, false
			for _, v := range values[k] {
				if !on[v.team] {
					continue
				}
				mine = append(mine, v)
				if r := rank[v.team]; !seen || r > best {
					best, seen = r, true
				}
			}
			var top []tv
			for _, v := range mine {
				if rank[v.team] == best {
					top = append(top, v)
				}
			}
			if len(top) < 2 {
				continue
			}
			differ := false
			for _, v := range top[1:] {
				if !cfgfmt.Equal(normalize(v.value), normalize(top[0].value)) {
					differ = true
					break
				}
			}
			if !differ {
				continue
			}
			teams := make([]string, 0, len(top))
			for _, v := range top {
				teams = append(teams, v.team)
			}
			sort.Strings(teams)
			out = append(out, TeamConflict{Member: m, Target: k.target, Project: k.project, Mode: k.mode, Teams: teams})
		}
	}
	return out
}

// TeamConflict is one member whose teams disagree on a value (D-123).
type TeamConflict struct {
	Member  string   `json:"member"`
	Target  string   `json:"target"`
	Project string   `json:"project,omitempty"`
	Mode    string   `json:"mode"`
	Teams   []string `json:"teams"`
}

// LintTeamConflicts lists every member whose teams set different values for
// one target (setting or common key) in one mode and project, where the key
// is not ordered-danger and the team priority does not order every
// conflicting team. Publishing is blocked while the list is non-empty
// (D-123). memberships maps a member to their teams.
func LintTeamConflicts(docs []TeamDoc, priority []string, memberships map[string][]string) []TeamConflict {
	type tk struct{ target, project, mode string }
	values := map[tk][]agentconfig.TeamValue{}
	orders := map[tk]agentconfig.OrderKind{}
	for _, d := range docs {
		for _, e := range Normalize(d.Spec).Settings {
			target := "setting:" + e.Setting
			order := agentconfig.OrderNone
			if e.Common != "" {
				target = "common:" + e.Common
				if spec, ok := agentconfig.CommonSpecFor(agentconfig.CommonKey(e.Common)); ok {
					order = spec.Order
				}
			}
			k := tk{target, e.Project, e.Mode}
			orders[k] = order
			for _, t := range d.Teams {
				values[k] = append(values[k], agentconfig.TeamValue{Team: t, Mode: agentconfig.Mode(e.Mode), Value: e.Value})
			}
		}
	}
	keys := make([]tk, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].target != keys[j].target {
			return keys[i].target < keys[j].target
		}
		if keys[i].project != keys[j].project {
			return keys[i].project < keys[j].project
		}
		return keys[i].mode < keys[j].mode
	})
	var out []TeamConflict
	for _, k := range keys {
		for _, c := range agentconfig.LintTeamConflicts(orders[k], priority, values[k], memberships) {
			out = append(out, TeamConflict{Member: c.Member, Target: k.target, Project: k.project, Mode: k.mode, Teams: c.Teams})
		}
	}
	return out
}
