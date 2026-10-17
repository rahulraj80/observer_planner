package agentconfigsvc

import (
	"context"
	"encoding/json"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/marmutapp/superbased-observer/internal/agentconfig"
	"github.com/marmutapp/superbased-observer/internal/agentconfigorg"
	"github.com/marmutapp/superbased-observer/internal/orgcontract"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// The org AI tool policy apply (adapter config phases P6 and P7, plan
// sections 3.2, 6.2 lever B, 6.3, 6.5 and 6.6). ApplyOrg projects one
// verified policy body onto this managed node's vendor files through the
// SAME write path a developer's save takes (hygiene pipeline, backup ring,
// atomic write, read-back, layer + history records), and decides per target
// with the pure agentconfigorg.Decide table: first apply, in place, drift
// re-asserted, drift reported, back-off after three drifts in an hour
// (D-101), refused by the sanity check, a granted developer's own value
// (D-122). Lever B is tamper-EVIDENT, not tamper-proof.

// OrgFileNone is the file id a state row carries when no file takes the
// setting here (unknown setting, tool not installed, no writable file).
const OrgFileNone = "none"

// OrgApplyInput is one pass.
type OrgApplyInput struct {
	Body    orgcontract.AgentConfigPolicyBody
	Version int64
	// Prior is the stored state of the previous pass.
	Prior []store.AgentConfigOrgStateRow
	// ProjectRoots maps a project identity (git remote hash) onto the local
	// clones of that project.
	ProjectRoots map[string][]string
	// EditGrants are the honoured AI tool setting edit grants (T-436).
	EditGrants []string
	Now        time.Time
}

// OrgApplyResult is the new complete state and what the pass did.
type OrgApplyResult struct {
	Rows []store.AgentConfigOrgStateRow
	// Writes counts files written; DriftEvents counts new drift events.
	Writes      int
	DriftEvents int
}

// orgTarget is one file a directive lands in.
type orgTarget struct {
	row     agentconfig.FileRow
	project string // local root ("" = user file)
	hash    string // the project identity the directive named
	// tracked: a tracked shared project file that would shadow an enforced
	// user value and that no local-only file can shadow in turn
	// (shadowTarget); the org value is written into it (D-103, T-431).
	tracked bool
}

// ApplyOrg runs one pass. It never fails as a whole: a file that cannot be
// read or written becomes a refused state row and the pass continues.
func (s *Service) ApplyOrg(ctx context.Context, in OrgApplyInput) OrgApplyResult {
	now := in.Now
	if now.IsZero() {
		now = s.o.Now()
	}
	prior := map[string]store.AgentConfigOrgStateRow{}
	for _, p := range in.Prior {
		prior[p.Key()] = p
	}
	access := agentconfigorg.WithEditGrants(agentconfigorg.AccessFor(in.Body), in.EditGrants)
	var out OrgApplyResult
	for _, d := range in.Body.Directives {
		out.Rows = append(out.Rows, s.applyDirective(ctx, in, d, access, prior, now, &out)...)
	}
	out.Rows = append(out.Rows, s.applyInstructions(ctx, in, prior, now, &out)...)
	sort.SliceStable(out.Rows, func(i, j int) bool { return out.Rows[i].Key() < out.Rows[j].Key() })
	return out
}

// applyDirective resolves one directive's targets and decides each.
func (s *Service) applyDirective(ctx context.Context, in OrgApplyInput, d orgcontract.AgentConfigDirective, access agentconfig.Access,
	prior map[string]store.AgentConfigOrgStateRow, now time.Time, out *OrgApplyResult,
) []store.AgentConfigOrgStateRow {
	var expected any
	if err := json.Unmarshal(d.Value, &expected); err != nil {
		expected = nil
	}
	base := store.AgentConfigOrgStateRow{
		SettingID: d.Setting, FileID: OrgFileNone, ProjectHash: d.Project, Mode: d.Mode, Layer: d.Layer,
		PolicyName: d.Policy, ExpectedHash: agentconfigorg.ValueHash(expected), PolicyVersion: in.Version, ObservedAt: now,
	}
	setting, known := agentconfig.Setting(d.Setting)
	if !known {
		base.Tool, base.State = toolOf(d.Setting), string(agentconfigorg.StateUnknownSetting)
		return []store.AgentConfigOrgStateRow{base}
	}
	base.Tool = setting.Tool
	if !s.Installed(setting.Tool) {
		base.State = string(agentconfigorg.StateNotInstalled)
		if f := agentconfig.UserFileFor(d.Setting); f != "" && d.Project == "" {
			base.FileID = f
		}
		return []store.AgentConfigOrgStateRow{base}
	}
	targets := s.orgTargets(d, setting, in.ProjectRoots)
	if len(targets) == 0 {
		if d.Project != "" {
			return nil // this machine has no clone of the project: no target, no state
		}
		base.State = string(agentconfigorg.StateNoFile)
		return []store.AgentConfigOrgStateRow{base}
	}
	var rows []store.AgentConfigOrgStateRow
	for _, t := range targets {
		rows = append(rows, s.applyTarget(ctx, in, d, setting, t, expected, access, base, prior, now, out))
	}
	return rows
}

// orgTargets lists the files one directive lands in: the user file (plus,
// for an enforced value, every project file of a known clone that sets the
// key and would shadow it, D-124), or the project file of every local clone
// of the named project (local-only or shared per the placement, D-103).
func (s *Service) orgTargets(d orgcontract.AgentConfigDirective, setting agentconfig.SettingRow, roots map[string][]string) []orgTarget {
	var out []orgTarget
	if d.Project != "" {
		id := agentconfig.ProjectFileFor(d.Setting, d.Placement != orgcontract.AgentConfigPlacementShared)
		row, ok := fileRow(id)
		if !ok {
			return nil
		}
		for _, root := range roots[d.Project] {
			if fi, err := os.Stat(root); err == nil && fi.IsDir() {
				out = append(out, orgTarget{row: row, project: root, hash: d.Project})
			}
		}
		return out
	}
	row, ok := fileRow(agentconfig.UserFileFor(d.Setting))
	if !ok {
		return nil
	}
	out = append(out, orgTarget{row: row})
	if d.Mode != orgcontract.AgentConfigModeEnforced {
		return out
	}
	local, hasLocal := fileRow(agentconfig.ProjectFileFor(d.Setting, true))
	for hash, list := range roots {
		for _, root := range list {
			seen := map[string]bool{}
			for _, id := range agentconfig.ProjectFiles(d.Setting) {
				prow, ok := fileRow(id)
				if !ok {
					continue
				}
				rp := agentconfig.ResolvePath(prow, s.env(root))
				if rp.Status != agentconfig.PathResolved {
					continue
				}
				b, present, err := s.readFile(rp.Path)
				if err != nil || !present {
					continue
				}
				if _, found := agentconfig.LookupDotted(agentconfig.DecodeTree(prow, b), setting.Path.String()); !found {
					continue
				}
				row, tracked := shadowTarget(prow, local, hasLocal)
				if seen[row.ID] {
					continue
				}
				seen[row.ID] = true
				out = append(out, orgTarget{row: row, project: root, hash: hash, tracked: tracked})
			}
		}
	}
	return out
}

// isTrackedFile reports a project file a commit carries (shared scope and
// not gitignored by the vendor).
func isTrackedFile(r agentconfig.FileRow) bool {
	return r.Scope == agentconfig.ScopeProjectShared && !r.Gitignored
}

// shadowTarget decides where an ENFORCED user-level value goes when a project
// file of a local clone sets the same key and would shadow it (D-124). D-103
// ("Org policy CAN ABSOLUTELY WRITE files inside repo") with the D-103
// design's preference for the vendor's local-only file:
//
//   - a personal (local-only or gitignored) file that sets the key is
//     rewritten in place;
//   - a TRACKED shared file is shadowed through the tool's local-only
//     project file when that file wins over it (created when absent and
//     git-excluded by the save path), so the repository is not touched;
//   - with no such local-only file the tracked file itself is rewritten
//     (tracked=true): the org value takes effect, the row carries the
//     project_shared scope so the node and the org show that the change can
//     be committed with the repository, and the publish lint warns the
//     admin (tracked_shadow). Before T-431 this case was reported and left
//     alone, so the enforced value silently did not take effect there.
//
// A shared-placement PROJECT directive (an explicit admin choice, linted
// tracked_file at publish) writes the shared file through orgTargets.
func shadowTarget(sets, local agentconfig.FileRow, hasLocal bool) (agentconfig.FileRow, bool) {
	if !isTrackedFile(sets) {
		return sets, false
	}
	if hasLocal && !isTrackedFile(local) && local.Precedence > sets.Precedence {
		return local, false
	}
	return sets, true
}

// applyTarget decides and, when the table says so, writes one target.
func (s *Service) applyTarget(ctx context.Context, in OrgApplyInput, d orgcontract.AgentConfigDirective, setting agentconfig.SettingRow,
	t orgTarget, expected any, access agentconfig.Access, base store.AgentConfigOrgStateRow,
	prior map[string]store.AgentConfigOrgStateRow, now time.Time, out *OrgApplyResult,
) store.AgentConfigOrgStateRow {
	st := base
	st.FileID, st.ProjectRoot, st.ProjectHash, st.Scope = t.row.ID, t.project, t.hash, string(t.row.Scope)
	st.Personal = t.row.Scope == agentconfig.ScopeProjectLocal || t.row.Gitignored
	p, hasPrior := prior[st.Key()]
	rp := agentconfig.ResolvePath(t.row, s.env(t.project))
	if rp.Status != agentconfig.PathResolved {
		st.State, st.Reason = string(agentconfigorg.StateNoFile), "unresolved_path"
		return st
	}
	b, present, err := s.readFile(rp.Path)
	if err != nil {
		st.State, st.Reason = string(agentconfigorg.StateRefused), "unreadable_file"
		return st
	}
	var current any
	found := false
	if present {
		current, found = agentconfig.LookupDotted(agentconfig.DecodeTree(t.row, b), setting.Path.String())
	}
	in2 := agentconfigorg.DecideInput{
		KnownSetting: true, Installed: true, HasFile: true, Mode: d.Mode, Granted: !access.ReadOnly && access.Granted(d.Setting),
		Expected: expected, Current: current, CurrentFound: found, OnDrift: in.Body.OnDrift, Now: now,
		LockLifted: d.Mode == orgcontract.AgentConfigModeEnforced && agentconfigorg.EditGranted(in.EditGrants, d.Setting),
	}
	if hasPrior {
		in2.Prior = agentconfigorg.Prior{Known: true, ExpectedHash: p.ExpectedHash, State: agentconfigorg.State(p.State), DriftTimes: p.DriftTimes}
		st.LastAppliedAt, st.LastDriftAt = p.LastAppliedAt, p.LastDriftAt
	}
	dec := agentconfigorg.Decide(in2)
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
	if !dec.Write {
		return st
	}
	if reason, ok := s.writeOrgValue(ctx, d, setting, t, rp.Path, expected); !ok {
		st.State, st.Reason = string(agentconfigorg.StateRefused), reason
		return st
	}
	out.Writes++
	st.LastAppliedAt, st.ObservedHash, st.ObservedValue, st.ObservedSecret = now, st.ExpectedHash, "", false
	return st
}

// observe records the value found (for the org view, D-112). A secret-shaped
// value is kept in its scrubbed form only (ObservedSecret marks it): the raw
// secret is never stored in the org state and never sent (T-428, scrub
// instead of withhold).
func (s *Service) observe(st *store.AgentConfigOrgStateRow, v any) {
	scrubbed, changed := s.ScrubOrgValue(v)
	secret := changed || (s.o.Detect != nil && secretInValue(v, s.o.Detect))
	b, err := json.Marshal(scrubbed)
	if err != nil || len(b) > orgcontract.AgentConfigObservedMaxBytes {
		st.ObservedSecret = secret
		return
	}
	if secret && !changed {
		// The detector saw a credential the scrubber could not place: keep
		// the fact, never the text.
		st.ObservedSecret = true
		return
	}
	st.ObservedValue, st.ObservedSecret = string(b), secret
}

func secretInValue(v any, detect agentconfig.SecretDetector) bool {
	switch x := v.(type) {
	case string:
		return detect(x)
	case []any:
		for _, e := range x {
			if secretInValue(e, detect) {
				return true
			}
		}
	case map[string]any:
		for k, e := range x {
			if s, ok := e.(string); ok && agentconfig.SecretKeyName(k) && s != "" {
				return true
			}
			if secretInValue(e, detect) {
				return true
			}
		}
	}
	return false
}

// orgWriteAccess is the posture an ORG write is judged under: the org's
// value passed the publish lint (no secrets, no Observer span, in the
// catalogue's type and range), so it is never refused for the developer's
// grants or locks; the sanity check still refuses what would break this
// node (an Observer-owned span, capture switched off while integrated, a
// file that does not parse).
var orgWriteAccess = agentconfig.Access{SecretsPosture: agentconfig.SecretsWarn, OrgWrite: true}

// writeOrgValue writes one value through the shared pipeline. It returns a
// short closed reason code when the sanity check or the write refused.
func (s *Service) writeOrgValue(ctx context.Context, d orgcontract.AgentConfigDirective, setting agentconfig.SettingRow, t orgTarget, path string, value any) (string, bool) {
	return s.writeOrgEdits(ctx, t.row, t.project, path, []agentconfig.Edit{{Path: setting.Path, Op: agentconfig.OpSet, Value: value}}, d.Layer, d.Common)
}

// writeOrgEdits writes org edits (a setting value, or an org instruction
// block) to one file through the shared pipeline.
func (s *Service) writeOrgEdits(ctx context.Context, row agentconfig.FileRow, project, path string, edits []agentconfig.Edit, layer, common string) (string, bool) {
	unlock, err := s.lock(path)
	if err != nil {
		return "lock_failed", false
	}
	defer unlock()
	pin := pinTarget(path)
	req := SaveRequest{
		Target: Target{Tool: row.Tool, FileID: row.ID, Project: project},
		Edits:  edits,
	}
	p, err := s.planAccess(ctx, req, orgWriteAccess)
	if err != nil {
		return "plan_failed", false
	}
	if !p.OK {
		for _, f := range p.Findings {
			if f.Severity == agentconfig.SevError || f.Severity == agentconfig.SevRefuse {
				return orgcontract.NormalizeAgentConfigReason(f.Code), false
			}
		}
		return "refused", false
	}
	if p.NoChange {
		return "", true
	}
	w, err := s.commitFile(pin, p)
	if err != nil {
		return "write_failed", false
	}
	if p.AddExclude && project != "" {
		s.addGitExclude(project, path)
	}
	s.record(ctx, row, path, project, p, w, layer, common, "org")
	return "", true
}

func toolOf(settingID string) string {
	tool, _, _ := strings.Cut(settingID, "/")
	return tool
}
