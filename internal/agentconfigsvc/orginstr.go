package agentconfigsvc

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/marmutapp/superbased-observer/internal/agentconfig"
	"github.com/marmutapp/superbased-observer/internal/agentconfigorg"
	"github.com/marmutapp/superbased-observer/internal/cfgfmt"
	"github.com/marmutapp/superbased-observer/internal/gitview"
	"github.com/marmutapp/superbased-observer/internal/orgcontract"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// Org instructions on a managed node (D-107, T-426): each composed
// instruction becomes a marked org block
//
//	<!-- observer:org-begin rev=<hash> -->
//	...the organization's text...
//	<!-- observer:org-end -->
//
// ADDED to each installed AI tool's own instruction file, at the user level
// or in every local clone of a project, through the same sanity-checked
// write path as every org write. The developer's text around the block is
// never touched. The block is decided with the same agentconfigorg.Decide
// table as a setting (first apply, in place, drift re-asserted or reported,
// back-off), reported as one state row per tool and file with the pseudo
// setting "<tool>/@org-instructions", and REMOVED from every file it was
// written to once the policy no longer places it there (a withdrawal, a
// narrower scope, a tool uninstalled).
//
// Which file: the tool's fixed-name instruction rows of the scope (the
// catalogue's Files table, never a tool name): for a project, the personal
// (local-only or gitignored) rows under local_only placement and the shared
// tracked rows under shared placement. An existing file is preferred (the
// block is added to whatever exists); otherwise the shortest-path row is
// created. Several tools that read the same file (AGENTS.md) share one block.
// A tool whose catalogue has instruction files but none of this scope gets a
// no_file row, so the org sees where the instruction cannot land.

// instrTarget is one instruction file the block lands in, with every
// installed tool that reads it.
type instrTarget struct {
	path    string
	project string // local root ("" = user level)
	hash    string
	rows    []agentconfig.FileRow // one per tool, same path
}

// applyInstructions decides and writes every instruction's targets and
// removes stale blocks. It returns the state rows.
func (s *Service) applyInstructions(ctx context.Context, in OrgApplyInput, prior map[string]store.AgentConfigOrgStateRow, now time.Time, out *OrgApplyResult) []store.AgentConfigOrgStateRow {
	var rows []store.AgentConfigOrgStateRow
	keep := map[string]bool{}
	for _, ins := range in.Body.Instructions {
		text := strings.TrimRight(strings.ReplaceAll(ins.Text, "\r\n", "\n"), " \t\n")
		if text == "" {
			continue
		}
		ins.Text = text
		targets, missing := s.instructionTargets(ins, in.ProjectRoots, in, now)
		rows = append(rows, missing...)
		for _, t := range targets {
			keep[t.path] = true
			rows = append(rows, s.applyInstructionTarget(ctx, in, ins, text, t, prior, now, out)...)
		}
	}
	s.removeStaleOrgBlocks(ctx, in.Prior, keep, out)
	return rows
}

// instructionTools are the catalogued tools installed here, sorted.
func (s *Service) instructionTools() []string {
	var out []string
	for _, c := range agentconfig.Coverage() {
		if s.Installed(c.Tool) {
			out = append(out, c.Tool)
		}
	}
	sort.Strings(out)
	return out
}

// instructionRowsAt lists a tool's fixed-name instruction rows of one scope,
// shortest path first. hasAny reports whether the tool has any
// instruction-family file at all (a tool without one gets no state row).
func instructionRowsAt(tool string, project, localOnly bool) (rows []agentconfig.FileRow, hasAny bool) {
	for _, f := range agentconfig.Files() {
		if f.Tool != tool || !agentconfig.IsInstructionKind(f.Kind) {
			continue
		}
		hasAny = true
		if f.Kind != agentconfig.KindInstructions || f.WriteNeeds != agentconfig.PrivUser || strings.ContainsAny(f.Paths.Linux, "*?[") {
			continue
		}
		if f.Format != agentconfig.FormatMarkdown {
			continue
		}
		personal := f.Scope == agentconfig.ScopeProjectLocal || f.Gitignored
		switch {
		case !project && f.Scope == agentconfig.ScopeUser:
		case project && localOnly && f.Scope != agentconfig.ScopeUser && personal:
		case project && !localOnly && f.Scope == agentconfig.ScopeProjectShared && !personal:
		default:
			continue
		}
		rows = append(rows, f)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if len(rows[i].Paths.Linux) != len(rows[j].Paths.Linux) {
			return len(rows[i].Paths.Linux) < len(rows[j].Paths.Linux)
		}
		return rows[i].ID < rows[j].ID
	})
	return rows, hasAny
}

// instructionTargets resolves where one instruction lands on this machine.
func (s *Service) instructionTargets(ins orgcontract.AgentConfigInstruction, roots map[string][]string, in OrgApplyInput, now time.Time) ([]instrTarget, []store.AgentConfigOrgStateRow) {
	type root struct{ dir, hash string }
	var places []root
	if ins.Project == "" {
		places = []root{{}}
	} else {
		for _, r := range roots[ins.Project] {
			if fi, err := os.Stat(r); err == nil && fi.IsDir() {
				places = append(places, root{dir: r, hash: ins.Project})
			}
		}
	}
	localOnly := ins.Placement != orgcontract.AgentConfigPlacementShared
	byPath := map[string]*instrTarget{}
	var order []string
	var missing []store.AgentConfigOrgStateRow
	for _, pl := range places {
		for _, tool := range s.instructionTools() {
			cands, hasAny := instructionRowsAt(tool, ins.Project != "", localOnly)
			if !hasAny {
				continue
			}
			row, path, ok := s.pickInstructionFile(cands, pl.dir)
			if !ok {
				missing = append(missing, store.AgentConfigOrgStateRow{
					SettingID: orgcontract.AgentConfigInstructionSetting(tool), FileID: OrgFileNone, ProjectRoot: pl.dir, ProjectHash: pl.hash,
					Tool: tool, Mode: orgcontract.AgentConfigModeEnforced, Layer: ins.Layer, PolicyName: ins.Policy,
					State: string(agentconfigorg.StateNoFile), Reason: "no_instruction_file",
					ExpectedHash: agentconfigorg.ValueHash(ins.Text), PolicyVersion: in.Version, ObservedAt: now,
				})
				continue
			}
			t, seen := byPath[path]
			if !seen {
				t = &instrTarget{path: path, project: pl.dir, hash: pl.hash}
				byPath[path] = t
				order = append(order, path)
			}
			t.rows = append(t.rows, row)
		}
	}
	out := make([]instrTarget, 0, len(order))
	for _, p := range order {
		out = append(out, *byPath[p])
	}
	return out, missing
}

// pickInstructionFile chooses the existing file (the block is added to
// whatever exists), else the first candidate, which is created.
func (s *Service) pickInstructionFile(cands []agentconfig.FileRow, project string) (agentconfig.FileRow, string, bool) {
	var first agentconfig.FileRow
	firstPath, found := "", false
	for _, c := range cands {
		rp := agentconfig.ResolvePath(c, s.env(project))
		if rp.Status != agentconfig.PathResolved {
			continue
		}
		if fi, err := os.Stat(rp.Path); err == nil && fi.Mode().IsRegular() {
			return c, rp.Path, true
		}
		if !found {
			first, firstPath, found = c, rp.Path, true
		}
	}
	return first, firstPath, found
}

// readOrgBlock returns the org block's text in a file (found=false when the
// file or the block is absent); a file whose markers do not parse is an
// error.
func (s *Service) readOrgBlock(path string) (text string, found, present bool, err error) {
	b, present, err := s.readFile(path)
	if err != nil || !present {
		return "", false, present, err
	}
	doc, err := cfgfmt.ParseMarkdown(b)
	if err != nil {
		return "", false, true, err
	}
	blocks, err := doc.Blocks()
	if err != nil {
		return "", false, true, err
	}
	for _, bl := range blocks {
		if bl.Kind == agentconfig.OrgBlockKind && bl.Policy == "" {
			return strings.TrimRight(bl.Content, "\n"), true, true, nil
		}
	}
	return "", false, true, nil
}

// applyInstructionTarget decides one file and returns one row per tool.
func (s *Service) applyInstructionTarget(ctx context.Context, in OrgApplyInput, ins orgcontract.AgentConfigInstruction, text string, t instrTarget,
	prior map[string]store.AgentConfigOrgStateRow, now time.Time, out *OrgApplyResult,
) []store.AgentConfigOrgStateRow {
	lead := t.rows[0]
	st := store.AgentConfigOrgStateRow{
		SettingID: orgcontract.AgentConfigInstructionSetting(lead.Tool), FileID: lead.ID, ProjectRoot: t.project, ProjectHash: t.hash,
		Tool: lead.Tool, Scope: string(lead.Scope), Mode: orgcontract.AgentConfigModeEnforced, Layer: ins.Layer, PolicyName: ins.Policy,
		ExpectedHash: agentconfigorg.ValueHash(text), PolicyVersion: in.Version, ObservedAt: now,
		Personal: lead.Scope == agentconfig.ScopeProjectLocal || lead.Gitignored,
	}
	p, hasPrior := prior[st.Key()]
	current, found, _, err := s.readOrgBlock(t.path)
	switch {
	case err != nil && errors.Is(err, cfgfmt.ErrMarker):
		st.State, st.Reason = string(agentconfigorg.StateRefused), "bad_marker"
		return s.instructionRows(st, t)
	case err != nil:
		st.State, st.Reason = string(agentconfigorg.StateRefused), "unreadable_file"
		return s.instructionRows(st, t)
	}
	din := agentconfigorg.DecideInput{
		KnownSetting: true, Installed: true, HasFile: true, Mode: orgcontract.AgentConfigModeEnforced,
		Expected: text, Current: current, CurrentFound: found, OnDrift: in.Body.OnDrift, Now: now,
	}
	if hasPrior {
		din.Prior = agentconfigorg.Prior{Known: true, ExpectedHash: p.ExpectedHash, State: agentconfigorg.State(p.State), DriftTimes: p.DriftTimes}
		st.LastAppliedAt, st.LastDriftAt = p.LastAppliedAt, p.LastDriftAt
	}
	dec := agentconfigorg.Decide(din)
	st.State, st.DriftTimes = string(dec.State), dec.DriftTimes
	if found {
		st.ObservedHash = agentconfigorg.ValueHash(current)
		if st.ObservedHash != st.ExpectedHash {
			s.observe(&st, current)
		}
	}
	if dec.DriftEvent {
		st.LastDriftAt = now
		out.DriftEvents++
	}
	if dec.Write {
		edit := agentconfig.Edit{Path: agentconfig.KeyPath{"(org block)"}, Op: agentconfig.OpOrgBlock, Value: text}
		if reason, ok := s.writeOrgEdits(ctx, lead, t.project, t.path, []agentconfig.Edit{edit}, ins.Layer, ""); !ok {
			st.State, st.Reason = string(agentconfigorg.StateRefused), reason
			return s.instructionRows(st, t)
		}
		out.Writes++
		st.LastAppliedAt, st.ObservedHash, st.ObservedValue, st.ObservedSecret = now, st.ExpectedHash, "", false
	}
	// A block in a tracked file that a commit already carries (plan section
	// 6.3): reported, so the admin sees an org block in the repository.
	if t.project != "" && !st.Personal && s.o.Committed != nil {
		if rel, err := filepath.Rel(t.project, t.path); err == nil && s.o.Committed(t.project, filepath.ToSlash(rel)) {
			st.Reason = "org_block_committed"
		}
	}
	return s.instructionRows(st, t)
}

// instructionRows fans one decided file out to a row per tool reading it.
func (s *Service) instructionRows(st store.AgentConfigOrgStateRow, t instrTarget) []store.AgentConfigOrgStateRow {
	out := make([]store.AgentConfigOrgStateRow, 0, len(t.rows))
	for _, r := range t.rows {
		row := st
		row.Tool, row.FileID, row.SettingID = r.Tool, r.ID, orgcontract.AgentConfigInstructionSetting(r.Tool)
		row.Scope = string(r.Scope)
		out = append(out, row)
	}
	return out
}

// removeStaleOrgBlocks removes the org block from every file a previous pass
// wrote it to that is no longer a target (T-426 acceptance: the block
// disappears on withdrawal). A file that no longer exists or holds no block
// is left alone.
func (s *Service) removeStaleOrgBlocks(ctx context.Context, prior []store.AgentConfigOrgStateRow, keep map[string]bool, out *OrgApplyResult) {
	done := map[string]bool{}
	for _, p := range prior {
		if !orgcontract.IsAgentConfigInstructionSetting(p.SettingID) || p.FileID == OrgFileNone {
			continue
		}
		row, ok := fileRow(p.FileID)
		if !ok {
			continue
		}
		rp := agentconfig.ResolvePath(row, s.env(p.ProjectRoot))
		if rp.Status != agentconfig.PathResolved || keep[rp.Path] || done[rp.Path] {
			continue
		}
		done[rp.Path] = true
		if _, found, _, err := s.readOrgBlock(rp.Path); err != nil || !found {
			continue
		}
		edit := agentconfig.Edit{Path: agentconfig.KeyPath{"(org block)"}, Op: agentconfig.OpRemoveOrgBlock}
		if _, ok := s.writeOrgEdits(ctx, row, p.ProjectRoot, rp.Path, []agentconfig.Edit{edit}, p.Layer, ""); ok {
			out.Writes++
		}
	}
}

// GitCommitted is the default Options.Committed: a read-only `git grep` of
// HEAD for the org block's begin marker in one file of the clone (the
// gitview envelope: no hooks, no pager, no lazy fetch beyond the read-only
// defaults, bounded output and time). Any failure (not a repository, no
// HEAD, the file not in HEAD) reads as not committed.
func GitCommitted(projectRoot, rel string) bool {
	if projectRoot == "" || rel == "" || strings.HasPrefix(rel, "..") {
		return false
	}
	if _, err := os.Stat(filepath.Join(projectRoot, ".git")); err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, _, err := gitview.RunReadOnly(ctx, projectRoot, 4096, "grep", "-l", "-F", "-e", "observer:"+agentconfig.OrgBlockKind+"-begin", "HEAD", "--", rel)
	return err == nil && len(strings.TrimSpace(string(out))) > 0
}
