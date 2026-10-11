package agentconfig

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/marmutapp/superbased-observer/internal/cfgfmt"
	"github.com/marmutapp/superbased-observer/internal/guidance"
	"github.com/marmutapp/superbased-observer/internal/textdiff"
)

// The hygiene pipeline (phase P5, backlog item 7; plan section 5.2): ONE
// pure function every save path runs before anything touches disk. It
// builds the bytes a save would write and judges them:
//
//	1 parse                         error with line and column
//	2 vendor schema (injected)      per-key; a pre-existing issue is a warning
//	3 catalogue type / enum / range per-key errors; uncatalogued keys warn
//	4 dangerous values              confirm (individual) or refuse
//	5 Observer integration          an owned span changed: refuse, name the verb
//	6 secret posture                warn / refuse / allow (D-104)
//	7 diff preview                  byte diff (secrets masked) + semantic changes
//
// Steps 8-10 (lock, etag re-check, backup, atomic write, read-back, record)
// are the boundary's (internal/agentconfigsvc), which re-runs this function
// on the bytes it read under the lock and again on the bytes on disk.

// EditOp is a form edit's operation.
type EditOp string

// The closed EditOp vocabulary.
const (
	OpSet    EditOp = "set"
	OpDelete EditOp = "delete"
	// OpOrgBlock writes Value (the org instruction text) as the file's
	// marked org block; OpRemoveOrgBlock removes it. Markdown only, and only
	// under Access.OrgWrite (the organization's apply, D-107 / T-426).
	OpOrgBlock       EditOp = "org_block"
	OpRemoveOrgBlock EditOp = "org_block_remove"
)

// Edit is one form edit: set or delete the value at a key path.
type Edit struct {
	Path  KeyPath `json:"path"`
	Op    EditOp  `json:"op"`
	Value any     `json:"value,omitempty"`
}

// Severity grades a finding.
type Severity string

// The closed Severity vocabulary. error and refuse block the save; confirm
// blocks until the caller confirms; warning never blocks.
const (
	SevError   Severity = "error"
	SevRefuse  Severity = "refuse"
	SevConfirm Severity = "confirm"
	SevWarning Severity = "warning"
)

// Finding is one hygiene result.
type Finding struct {
	Step      int      `json:"step"`
	Code      string   `json:"code"`
	Severity  Severity `json:"severity"`
	Path      string   `json:"path,omitempty"`
	SettingID string   `json:"setting_id,omitempty"`
	Message   string   `json:"message"`
	Line      int      `json:"line,omitempty"`
	Column    int      `json:"column,omitempty"`
}

// Change is one semantic change a save makes.
type Change struct {
	Path      string      `json:"path"`
	SettingID string      `json:"setting_id,omitempty"`
	Op        string      `json:"op"` // add | set | delete
	Before    any         `json:"before,omitempty"`
	After     any         `json:"after,omitempty"`
	Secret    bool        `json:"secret,omitempty"`
	Danger    DangerClass `json:"danger,omitempty"`
	// Uncatalogued: no catalogue row covers the key.
	Uncatalogued bool        `json:"uncatalogued,omitempty"`
	ShadowedBy   string      `json:"shadowed_by,omitempty"`
	Common       []CommonKey `json:"common,omitempty"`
	// rowPath is the covering catalogue row's dotted path ("" = none).
	rowPath string
}

// SchemaIssue is one vendor JSON-schema finding (the boundary's validator).
type SchemaIssue struct {
	Path     KeyPath
	Message  string
	Unknown  bool // an additionalProperties finding: an unknown key
	Location string
}

// SchemaCheck validates a decoded document against the shape's pinned
// vendor schema; nil when the shape has none.
type SchemaCheck func(shape string, tree map[string]any) []SchemaIssue

// SaveInput is one proposed save of one file.
type SaveInput struct {
	File SaveFile
	// Exactly one of Edits (the per-parameter form) or Raw (the raw
	// editor's whole text) is set.
	Edits []Edit
	Raw   *string
	// Others are the other present files of the same shape (shadowing).
	Others []FileSnapshot

	Detect   SecretDetector
	MaskText func(string) (string, bool)
	Spans    SpanEnv
	Access   Access
	// Integrated: Observer captures or hooks this tool on this node, so a
	// refuse_while_integrated setting may not be loosened.
	Integrated bool
	Schema     SchemaCheck
}

// SaveFile is the target: its row, resolved path and current bytes.
type SaveFile struct {
	Row     FileRow
	Path    string
	Present bool
	Bytes   []byte
}

// SaveResult is the verdict and the preview.
type SaveResult struct {
	// OK: no error or refuse finding. A confirm finding still needs the
	// caller's confirmation (NeedsConfirm).
	OK           bool      `json:"ok"`
	NeedsConfirm bool      `json:"needs_confirm"`
	NoChange     bool      `json:"no_change"`
	Created      bool      `json:"created"`
	Findings     []Finding `json:"findings"`
	Changes      []Change  `json:"changes"`
	// Diff is the unified diff of the masked before / after text.
	Diff        string              `json:"diff"`
	DiffStats   textdiff.Stats      `json:"diff_stats"`
	Disclosures []cfgfmt.Disclosure `json:"disclosures,omitempty"`
	BeforeEtag  string              `json:"before_etag"`
	AfterEtag   string              `json:"after_etag"`
	// PreviewEtag binds a save to exactly this before / after pair.
	PreviewEtag string `json:"preview_etag"`
	// AddExclude: a new project_local file the boundary should add to
	// .git/info/exclude (local to the clone, never committed).
	AddExclude bool `json:"add_exclude,omitempty"`
	// After is the exact bytes to write (unmasked; never serialized).
	After []byte `json:"-"`
}

func (r *SaveResult) add(f Finding) { r.Findings = append(r.Findings, f) }

func (r *SaveResult) blocked() bool {
	for _, f := range r.Findings {
		if f.Severity == SevError || f.Severity == SevRefuse {
			return true
		}
	}
	return false
}

func (r *SaveResult) finish() SaveResult {
	r.OK = !r.blocked()
	r.NeedsConfirm = false
	for _, f := range r.Findings {
		if f.Severity == SevConfirm {
			r.NeedsConfirm = true
		}
	}
	sort.SliceStable(r.Findings, func(i, j int) bool { return r.Findings[i].Step < r.Findings[j].Step })
	if r.Findings == nil {
		r.Findings = []Finding{}
	}
	if r.Changes == nil {
		r.Changes = []Change{}
	}
	return *r
}

// PreviewEtag fingerprints a before / after pair.
func PreviewEtag(before, after string) string {
	sum := sha256.Sum256([]byte(before + "\n" + after))
	return hex.EncodeToString(sum[:16])
}

// isInstructionKind reports a Markdown instruction-family file.
func isInstructionKind(k FileKind) bool {
	switch k {
	case KindInstructions, KindSkill, KindAgent, KindCommand, KindRule:
		return true
	}
	return false
}

// PlanSave runs the hygiene pipeline (steps 1-7) for one proposed save.
func PlanSave(in SaveInput) SaveResult {
	res := SaveResult{BeforeEtag: Etag(in.File.Bytes, in.File.Present), Created: !in.File.Present}
	row := in.File.Row
	snap := FileSnapshot{Row: row, Resolved: ResolvedPath{Status: PathResolved, Path: in.File.Path}, State: FileMissing}
	if in.File.Present {
		snap.State = FilePresent
	}
	if ok, why := fileEditable(row, snap, in.Access); !ok {
		res.add(Finding{Step: 0, Code: "not_editable", Severity: SevRefuse, Message: why})
		return res.finish()
	}
	eng, _ := row.Format.Engine()
	if (in.Raw == nil) == (len(in.Edits) == 0) {
		res.add(Finding{Step: 0, Code: "bad_request", Severity: SevError, Message: "send either form edits or the raw text, not both or neither"})
		return res.finish()
	}

	// Step 1: build and parse the after document.
	var beforeDoc cfgfmt.Doc
	var beforeTree map[string]any
	if in.File.Present {
		d, err := cfgfmt.Parse(eng, in.File.Bytes)
		if err == nil {
			beforeDoc = d
			beforeTree, _ = d.Value()
		} else if in.Raw == nil {
			res.add(parseFinding(err, "the file on disk does not parse; fix it with the raw editor"))
			return res.finish()
		}
	}
	afterDoc, ok := buildAfter(&res, in, eng, beforeDoc)
	if !ok {
		return res.finish()
	}
	after := afterDoc.Bytes()
	afterTree, err := afterDoc.Value()
	if err != nil {
		res.add(parseFinding(err, ""))
		return res.finish()
	}
	// Re-parse the rendered bytes: the edit must survive its own encoding.
	if _, err := cfgfmt.Parse(eng, after); err != nil {
		res.add(parseFinding(err, "the edited document does not re-parse"))
		return res.finish()
	}
	res.After = after
	res.AfterEtag = Etag(after, true)
	res.PreviewEtag = PreviewEtag(res.BeforeEtag, res.AfterEtag)
	res.Disclosures = afterDoc.Disclosures()
	if in.File.Present && string(after) == string(in.File.Bytes) {
		res.NoChange = true
	}

	if isInstructionKind(row.Kind) {
		planInstruction(&res, in, beforeDoc, afterDoc)
	} else {
		planSettings(&res, in, beforeTree, afterTree)
	}

	// Step 5: Observer-owned spans.
	for _, s := range spansOfFile(row.ID) {
		b := readSpan(in.Spans, s, beforeTree, in.File.Bytes, in.File.Present)
		a := readSpan(in.Spans, s, afterTree, after, true)
		if !spanKept(b, a) {
			res.add(Finding{
				Step: 5, Code: "observer_span", Severity: SevRefuse, Path: s.Path.String(),
				Message: fmt.Sprintf("This save removes or changes what Observer's %s writer owns at %s. Use %s instead.", s.Owner, displayPath(s.Path), s.Verb),
			})
		}
	}

	// Repo placement (plan section 6.3).
	switch {
	case row.Scope == ScopeProjectShared:
		res.add(Finding{
			Step: 7, Code: "tracked_file", Severity: SevWarning,
			Message: "This is a shared project file: the change shows in git status and can be committed into the repository.",
		})
	case row.Scope == ScopeProjectLocal && !in.File.Present:
		res.AddExclude = true
	}

	// Step 7: the masked diff.
	beforeMasked := maskForDiff(row, in.File.Bytes, in.Detect, in.MaskText)
	afterMasked := maskForDiff(row, after, in.Detect, in.MaskText)
	name := in.File.Path
	if name == "" {
		name = row.ID
	}
	res.Diff, res.DiffStats = textdiff.Unified(name+" (current)", name+" (after save)", beforeMasked, afterMasked, 3)
	return res.finish()
}

// buildAfter applies the raw text or the form edits.
func buildAfter(res *SaveResult, in SaveInput, eng cfgfmt.Format, beforeDoc cfgfmt.Doc) (cfgfmt.Doc, bool) {
	if in.Raw != nil {
		if (eng == cfgfmt.FormatMarkdown || eng == cfgfmt.FormatMDC) && in.MaskText != nil && in.File.Present {
			if _, found := in.MaskText(string(in.File.Bytes)); found {
				res.add(Finding{
					Step: 6, Code: "secret_in_text", Severity: SevRefuse,
					Message: "This file holds a credential Observer hides, so it cannot be saved from the raw view. Edit it in your own editor.",
				})
				return nil, false
			}
		}
		doc, err := cfgfmt.Parse(eng, []byte(*in.Raw))
		if err != nil {
			res.add(parseFinding(err, ""))
			return nil, false
		}
		if in.File.Present && eng != cfgfmt.FormatMarkdown && eng != cfgfmt.FormatMDC {
			orig, merr := maskDocument(eng, in.File.Row.Shape, in.File.Bytes, in.Detect, in.MaskText)
			if merr == nil {
				if err := restoreSecrets(orig, doc); err != nil {
					res.add(Finding{Step: 6, Code: "secret_placeholder", Severity: SevRefuse, Message: err.Error()})
					return nil, false
				}
			}
		}
		if containsPlaceholderText(doc.Bytes()) {
			res.add(Finding{Step: 6, Code: "secret_placeholder", Severity: SevRefuse, Message: ErrSecretPlaceholder.Error()})
			return nil, false
		}
		return doc, true
	}
	doc := beforeDoc
	if doc == nil {
		d, err := cfgfmt.Parse(eng, nil)
		if err != nil {
			res.add(parseFinding(err, ""))
			return nil, false
		}
		doc = d
	}
	for _, e := range in.Edits {
		if len(e.Path) == 0 {
			res.add(Finding{Step: 1, Code: "bad_edit", Severity: SevError, Message: "an edit needs a key path"})
			return nil, false
		}
		switch e.Op {
		case OpSet:
			if containsPlaceholder(e.Value) {
				res.add(Finding{Step: 6, Code: "secret_placeholder", Severity: SevRefuse, Path: e.Path.String(), Message: ErrSecretPlaceholder.Error()})
				return nil, false
			}
			if err := doc.Set(cfgfmt.Path(e.Path), e.Value); err != nil {
				res.add(Finding{Step: 1, Code: "edit_failed", Severity: SevError, Path: e.Path.String(), Message: err.Error()})
				return nil, false
			}
		case OpDelete:
			if _, err := doc.Delete(cfgfmt.Path(e.Path)); err != nil {
				res.add(Finding{Step: 1, Code: "edit_failed", Severity: SevError, Path: e.Path.String(), Message: err.Error()})
				return nil, false
			}
		case OpOrgBlock, OpRemoveOrgBlock:
			if f, ok := applyOrgBlockEdit(doc, e, in.Access); !ok {
				res.add(f)
				return nil, false
			}
		default:
			res.add(Finding{Step: 1, Code: "bad_edit", Severity: SevError, Path: e.Path.String(), Message: "op must be set or delete"})
			return nil, false
		}
	}
	return doc, true
}

// OrgBlockKind is the marked-block family of an org instruction.
const OrgBlockKind = "org"

// OrgBlockRev is the revision attribute an org block carries: a short hash
// of its text, so the marker says which text the org last wrote.
func OrgBlockRev(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:6])
}

// applyOrgBlockEdit applies one org block edit to a Markdown document.
func applyOrgBlockEdit(doc cfgfmt.Doc, e Edit, a Access) (Finding, bool) {
	if !a.OrgWrite {
		return Finding{Step: 4, Code: "org_locked", Severity: SevRefuse, Message: "Only your organization's AI tool policy writes an org instruction block."}, false
	}
	md, ok := doc.(*cfgfmt.MarkdownDoc)
	if !ok {
		return Finding{Step: 1, Code: "bad_edit", Severity: SevError, Message: "an org block lives in a Markdown instruction file"}, false
	}
	var err error
	if e.Op == OpOrgBlock {
		text, _ := e.Value.(string)
		err = md.UpsertBlock(cfgfmt.Block{Kind: OrgBlockKind, Rev: OrgBlockRev(text), Content: text})
	} else {
		_, err = md.RemoveBlock(OrgBlockKind, "")
	}
	if err != nil {
		return Finding{Step: 1, Code: "edit_failed", Severity: SevError, Message: err.Error()}, false
	}
	return Finding{}, true
}

// orgBlocksOf renders a Markdown document's org blocks for comparison ("" for
// none, or for a document that is not Markdown).
func orgBlocksOf(doc cfgfmt.Doc) string {
	md, ok := doc.(*cfgfmt.MarkdownDoc)
	if !ok || md == nil {
		return ""
	}
	blocks, err := md.Blocks()
	if err != nil {
		return "\x00unparseable"
	}
	var b strings.Builder
	for _, bl := range blocks {
		if bl.Kind == OrgBlockKind {
			b.WriteString(bl.Policy + "\x00" + bl.Content + "\x00")
		}
	}
	return b.String()
}

func containsPlaceholderText(b []byte) bool { return strings.Contains(string(b), SecretPlaceholder) }

func parseFinding(err error, prefix string) Finding {
	f := Finding{Step: 1, Code: "parse_error", Severity: SevError, Message: err.Error()}
	var pe *cfgfmt.ParseError
	if errors.As(err, &pe) {
		f.Line, f.Column = pe.Line, pe.Column
	}
	if prefix != "" {
		f.Message = prefix + ": " + f.Message
	}
	return f
}

func displayPath(p KeyPath) string {
	if len(p) == 0 {
		return "the whole file"
	}
	return p.String()
}

// maskForDiff renders bytes with secrets hidden ("" for an absent file).
func maskForDiff(row FileRow, b []byte, detect SecretDetector, maskText func(string) (string, bool)) string {
	if len(b) == 0 {
		return ""
	}
	md, _ := MaskedFile(row, b, detect, maskText)
	return string(md.Bytes)
}

// planInstruction runs the Markdown checks: front matter lint for skills
// and agents (required name / description), unknown front-matter keys warn.
func planInstruction(res *SaveResult, in SaveInput, before, after cfgfmt.Doc) {
	if !res.NoChange {
		res.Changes = append(res.Changes, Change{Path: "(content)", Op: changeOp(in.File.Present, true)})
	}
	// Step 4, the organization lock for instruction blocks (D-107, T-426): a
	// developer save may not add, change or remove an org block.
	if in.Access.OrgBlockLocked != "" && !in.Access.OrgWrite {
		var beforeBlocks string
		if before != nil {
			beforeBlocks = orgBlocksOf(before)
		}
		if beforeBlocks != orgBlocksOf(after) {
			res.add(Finding{
				Step: 4, Code: "org_locked", Severity: SevRefuse,
				Message: "The block between the observer:org markers is set by your organization's policy " + in.Access.OrgBlockLocked + ". Edit the text around it, not inside it.",
			})
		}
	}
	md, ok := after.(*cfgfmt.MarkdownDoc)
	if !ok {
		return
	}
	fm, err := md.Value()
	if err != nil {
		return
	}
	switch in.File.Row.Kind {
	case KindSkill, KindAgent:
		for _, k := range []string{"name", "description"} {
			if v, ok := fm[k].(string); !ok || strings.TrimSpace(v) == "" {
				res.add(Finding{
					Step: 3, Code: "frontmatter_required", Severity: SevError, Path: k,
					Message: fmt.Sprintf("A %s file needs a %q in its front matter.", in.File.Row.Kind, k),
				})
			}
		}
	}
	known := map[string]bool{}
	for _, k := range guidance.KnownFrontmatterKeys() {
		known[k] = true
	}
	var unknown []string
	for k := range fm {
		if !known[k] {
			unknown = append(unknown, k)
		}
	}
	sort.Strings(unknown)
	if len(unknown) > 0 && len(known) > 0 {
		res.add(Finding{
			Step: 3, Code: "frontmatter_unknown", Severity: SevWarning,
			Message: "Front-matter keys Observer does not recognise (kept as written): " + strings.Join(unknown, ", "),
		})
	}
	_ = before
}

func changeOp(existedBefore, existsAfter bool) string {
	switch {
	case !existedBefore && existsAfter:
		return "add"
	case existedBefore && !existsAfter:
		return "delete"
	}
	return "set"
}

// planSettings runs steps 2, 3, 4 and 6 for a structured file.
func planSettings(res *SaveResult, in SaveInput, beforeTree, afterTree map[string]any) {
	row := in.File.Row
	pc := planCtx{
		in: in, res: res, before: beforeTree, after: afterTree,
		rows: map[string]SettingRow{}, common: map[string][]CommonKey{},
		judge: newSecretJudge(row.Shape, in.Detect), higher: higherTrees(in),
	}
	for _, r := range shapeRows(row.Shape) {
		pc.rows[r.Path.String()] = r
	}
	for _, m := range MapRows() {
		if m.Tool != row.Tool {
			continue
		}
		for _, id := range m.Settings {
			pc.common[id] = append(pc.common[id], m.Key)
		}
	}
	changes := diffTrees(pc.rows, beforeTree, afterTree)
	changed := map[string]bool{}
	for i := range changes {
		changed[changes[i].Path] = true
		pc.change(&changes[i])
	}
	res.Changes = changes
	lockedPathFindings(res, in, pc.rows, beforeTree, afterTree)
	existingSecrets(res, in, pc.judge, afterTree, changed)
	schemaFindings(res, in, afterTree, changed)
}

// lockedPathFindings evaluates every org-locked setting of this file's shape
// against the before and after trees directly, independent of how diffTrees
// attributed the change (review M3-03). Replacing a locked setting's ancestor
// object with a scalar or an array is one uncatalogued change at the ancestor,
// so checkRow's org_locked refusal never ran for it; this pass refuses any
// save under which a locked path's value (or its presence) differs, unless
// checkRow already refused that setting. Rows are walked in path order so the
// findings are deterministic.
func lockedPathFindings(res *SaveResult, in SaveInput, rows map[string]SettingRow, before, after map[string]any) {
	if len(in.Access.Locked) == 0 {
		return
	}
	refused := map[string]bool{}
	for _, f := range res.Findings {
		if f.Code == "org_locked" && f.SettingID != "" {
			refused[f.SettingID] = true
		}
	}
	paths := make([]string, 0, len(rows))
	for p := range rows {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		r := rows[p]
		pol := in.Access.Locked[r.ID]
		if pol == "" || refused[r.ID] {
			continue
		}
		bv, bok := lookupTree(before, r.Path)
		av, aok := lookupTree(after, r.Path)
		if bok == aok && (!bok || cfgfmt.Equal(bv, av)) {
			continue
		}
		refused[r.ID] = true
		res.add(Finding{
			Step: 3, Code: "org_locked", Severity: SevRefuse, Path: r.Path.String(), SettingID: r.ID,
			Message: "Set by your organization's policy " + pol + ".",
		})
	}
}

// planCtx carries one structured save's inputs through its per-change checks.
type planCtx struct {
	in            SaveInput
	res           *SaveResult
	before, after map[string]any
	rows          map[string]SettingRow
	common        map[string][]CommonKey
	judge         secretJudge
	higher        []higherTree
}

// higherTree is a decoded higher-precedence file of the same shape.
type higherTree struct {
	path string
	id   string
	tree map[string]any
}

func higherTrees(in SaveInput) []higherTree {
	var out []higherTree
	for _, o := range in.Others {
		if o.Row.Precedence <= in.File.Row.Precedence || o.State != FilePresent {
			continue
		}
		if t := DecodeTree(o.Row, o.Bytes); t != nil {
			out = append(out, higherTree{path: o.Resolved.Path, id: o.Row.ID, tree: t})
		}
	}
	return out
}

// isSecretAt reports a secret value (whole or inside) at a path.
func (pc *planCtx) isSecretAt(kp KeyPath, v any, found bool) bool {
	return found && v != nil && (pc.judge.wholeSecret(kp, v) || pc.judge.containsSecret(v))
}

// change runs the per-change checks (steps 3, 4 and 6, plus shadowing).
func (pc *planCtx) change(c *Change) {
	r, catalogued := pc.rows[c.rowPath]
	c.Uncatalogued = !catalogued
	kp := splitDotted(c.Path, pc.before, pc.after)
	afterV, afterFound := lookupTree(pc.after, kp)
	beforeV, beforeFound := lookupTree(pc.before, kp)
	c.Secret = pc.isSecretAt(kp, afterV, afterFound) || pc.isSecretAt(kp, beforeV, beforeFound)
	if c.Secret {
		c.Before, c.After = maskedOrNil(c.Before), maskedOrNil(c.After)
	}
	switch {
	case catalogued:
		c.SettingID, c.Danger, c.Common = r.ID, r.Danger, pc.common[r.ID]
		rowV, rowFound := lookupTree(pc.after, r.Path)
		rowBefore, _ := lookupTree(pc.before, r.Path)
		checkRow(pc.res, pc.in, r, c, rowV, rowFound, rowBefore)
	case !pc.in.Access.ToolGranted(pc.in.File.Row.Tool):
		pc.res.add(Finding{
			Step: 3, Code: "not_granted", Severity: SevRefuse, Path: c.Path,
			Message: "Your organization manages this machine; a key Observer's catalogue does not know can be changed only with a grant for the whole tool.",
		})
	default:
		pc.res.add(Finding{
			Step: 3, Code: "uncatalogued_key", Severity: SevWarning, Path: c.Path,
			Message: "Observer's catalogue does not know this key; it is written as you typed it.",
		})
	}
	// Step 6: secret posture for a value this save introduces or changes.
	if c.Op != "delete" && pc.isSecretAt(kp, afterV, afterFound) {
		secretFinding(pc.res, pc.in.Access.SecretsPosture, c.Path)
	}
	// Shadowing: a higher-precedence file of the same shape sets it too.
	for _, h := range pc.higher {
		if _, found := lookupTree(h.tree, kp); found {
			c.ShadowedBy = h.id
			pc.res.add(Finding{
				Step: 7, Code: "shadowed", Severity: SevWarning, Path: c.Path,
				Message: fmt.Sprintf("%s also sets this and wins over this file, so the change will not take effect.", h.path),
			})
			return
		}
	}
}

// existingSecrets is D-126: credentials already in the file, outside this
// save's changes, are flagged (never blocking).
func existingSecrets(res *SaveResult, in SaveInput, judge secretJudge, afterTree map[string]any, changed map[string]bool) {
	existing := 0
	for _, m := range judge.findSecrets(afterTree) {
		if !changed[m.Path.String()] {
			existing++
		}
	}
	if existing > 0 && in.Access.SecretsPosture != SecretsAllow {
		res.add(Finding{
			Step: 6, Code: "existing_secret", Severity: SevWarning,
			Message: fmt.Sprintf("This file already holds %s. Consider an environment-variable reference instead.", CountNoun(existing, "plaintext credential value", "")),
		})
	}
}

// schemaFindings is step 2: the vendor schema. An issue this save
// introduces is an error; one already in the file, or an unknown key, is a
// warning (a save is never blocked by a problem it did not introduce).
func schemaFindings(res *SaveResult, in SaveInput, afterTree map[string]any, changed map[string]bool) {
	if in.Schema == nil {
		return
	}
	for _, is := range in.Schema(in.File.Row.Shape, afterTree) {
		p := is.Path.String()
		sev := SevWarning
		if !is.Unknown && touches(changed, p) {
			sev = SevError
		}
		msg := is.Message
		if sev == SevWarning && !is.Unknown {
			msg = "Already in the file (not introduced by this save): " + msg
		}
		res.add(Finding{Step: 2, Code: "schema", Severity: sev, Path: p, Message: msg})
	}
}

// touches reports whether a schema-issue path is inside or above a changed path.
func touches(changed map[string]bool, p string) bool {
	for c := range changed {
		if c == p || strings.HasPrefix(p, c+".") || strings.HasPrefix(c, p+".") || p == "" {
			return true
		}
	}
	return false
}

func maskedOrNil(v any) any {
	if v == nil {
		return nil
	}
	return SecretPlaceholder
}

func secretFinding(res *SaveResult, posture SecretsPosture, path string) {
	msg := "This value looks like a plaintext credential. Prefer an environment-variable reference (for example ${API_KEY}) so the secret does not live in a settings file."
	switch posture {
	case SecretsAllow:
		return
	case SecretsRefuse:
		res.add(Finding{
			Step: 6, Code: "plaintext_secret", Severity: SevRefuse, Path: path,
			Message: "Your organization does not allow plaintext credentials in settings files. " + msg,
		})
	default:
		res.add(Finding{Step: 6, Code: "plaintext_secret", Severity: SevWarning, Path: path, Message: msg})
	}
}

// checkRow runs steps 3 and 4 for a catalogued change.
func checkRow(res *SaveResult, in SaveInput, r SettingRow, c *Change, afterV any, afterFound bool, beforeV any) {
	if pol := in.Access.Locked[r.ID]; pol != "" {
		res.add(Finding{
			Step: 3, Code: "org_locked", Severity: SevRefuse, Path: c.Path, SettingID: r.ID,
			Message: "Set by your organization's policy " + pol + ".",
		})
		return
	}
	if !in.Access.Granted(r.ID) {
		res.add(Finding{
			Step: 3, Code: "not_granted", Severity: SevRefuse, Path: c.Path, SettingID: r.ID,
			Message: "Your organization manages this machine and has not granted editing of this setting.",
		})
		return
	}
	if !r.Writable {
		res.add(Finding{
			Step: 3, Code: "not_writable", Severity: SevRefuse, Path: c.Path, SettingID: r.ID,
			Message: readOnlyReason(r, Access{}),
		})
		return
	}
	if !scopeAllowed(r.Scopes, in.File.Row.Scope) {
		res.add(Finding{
			Step: 3, Code: "wrong_scope", Severity: SevError, Path: c.Path, SettingID: r.ID,
			Message: fmt.Sprintf("The tool does not read this setting from a %s file.", in.File.Row.Scope),
		})
	}
	if r.Deprecated {
		res.add(Finding{Step: 3, Code: "deprecated", Severity: SevWarning, Path: c.Path, SettingID: r.ID, Message: "The vendor marks this setting deprecated."})
	}
	if r.Verified == VerifiedUnverified {
		res.add(Finding{Step: 3, Code: "unverified", Severity: SevWarning, Path: c.Path, SettingID: r.ID, Message: "Not verified for your version of the tool."})
	}
	if afterFound {
		if msg := checkValue(r, afterV); msg != "" {
			res.add(Finding{Step: 3, Code: "invalid_value", Severity: SevError, Path: c.Path, SettingID: r.ID, Message: msg})
			return
		}
	}
	// Step 4: dangerous values.
	switch r.Danger {
	case DangerConfirm:
		if afterFound {
			res.add(Finding{
				Step: 4, Code: "dangerous_value", Severity: SevConfirm, Path: c.Path, SettingID: r.ID,
				Message: dangerMessage(r),
			})
		}
	case DangerRefuseWhileIntegrated:
		if in.Integrated && loosens(r, beforeV, afterV, afterFound) {
			res.add(Finding{
				Step: 4, Code: "refused_while_integrated", Severity: SevRefuse, Path: c.Path, SettingID: r.ID,
				Message: "This would switch off Observer's capture, hooks or MCP server for this tool while Observer integrates it. " +
					"Remove Observer's integration first (observer uninstall / route off), then change it.",
			})
		}
	}
}

func dangerMessage(r SettingRow) string {
	msg := "This setting can loosen the tool's safety (approvals, sandbox, or a command it runs). Confirm to save."
	if r.Note != "" {
		msg += " " + firstSentence(r.Note)
	}
	return msg
}

// loosens is the refuse_while_integrated transition table, walked in order
// (CLAUDE.md #5): the first matching rule decides.
func loosens(r SettingRow, before, after any, afterFound bool) bool {
	for _, rule := range integratedSafety {
		if rule.applies(r, afterFound) {
			return rule.unsafe(r, before, after)
		}
	}
	return true
}

type safetyRule struct {
	applies func(r SettingRow, afterFound bool) bool
	unsafe  func(r SettingRow, before, after any) bool
}

var integratedSafety = []safetyRule{
	// Deleting the key returns the tool to its default.
	{func(_ SettingRow, found bool) bool { return !found }, func(SettingRow, any, any) bool { return false }},
	// A bool with a documented default: only the non-default value loosens.
	{
		func(r SettingRow, _ bool) bool { return r.Type == TypeBool && r.Default != "" },
		func(r SettingRow, _, after any) bool { return fmt.Sprint(after) != strings.TrimSpace(r.Default) },
	},
	// A list or object loosens when it starts naming Observer (a deny list
	// that drops Observer's MCP server, an exclusion of its hooks).
	{func(r SettingRow, _ bool) bool {
		switch r.Type {
		case TypeStringList, TypeRuleList, TypeObject, TypeMCPMap, TypeStringMap:
			return true
		}
		return false
	}, func(_ SettingRow, before, after any) bool {
		names := func(v any) bool {
			return deepAny(v, func(s string) bool { return strings.Contains(strings.ToLower(s), "observer") })
		}
		return names(after) && !names(before)
	}},
	// Anything else: any change loosens (conservative).
	{func(SettingRow, bool) bool { return true }, func(_ SettingRow, before, after any) bool { return !cfgfmt.Equal(before, after) }},
}

func scopeAllowed(scopes []Scope, s Scope) bool {
	if len(scopes) == 0 {
		return true
	}
	for _, x := range scopes {
		if x == s {
			return true
		}
	}
	return false
}

// CheckValue validates a value against a catalogue row's type, enum and
// bounds ("" when it fits). The org policy lint runs the same check a
// developer's save runs, so the org can never publish a value the node's
// sanity check would refuse.
func CheckValue(r SettingRow, v any) string {
	n, err := cfgfmt.Normalize(v)
	if err != nil {
		return err.Error()
	}
	return checkValue(r, n)
}

// checkValue validates a value against the row's type, enum and bounds.
func checkValue(r SettingRow, v any) string {
	if v == nil {
		return ""
	}
	if l, ok := v.([]any); ok && len(r.Enum) > 0 && (r.Type == TypeStringList || r.Type == TypeRuleList) {
		for _, e := range l {
			if !inEnum(r.Enum, fmt.Sprint(e)) {
				return fmt.Sprintf("%q is not one of the documented values: %s.", fmt.Sprint(e), strings.Join(r.Enum, ", "))
			}
		}
		return ""
	}
	if len(r.Enum) > 0 && isScalar(v) {
		if s := fmt.Sprint(v); !inEnum(r.Enum, s) {
			return fmt.Sprintf("%q is not one of the documented values: %s.", s, strings.Join(r.Enum, ", "))
		}
		return boundsMsg(r, v)
	}
	if check, ok := typeChecks[r.Type]; ok {
		if msg := check(v); msg != "" {
			return msg
		}
	}
	return boundsMsg(r, v)
}

// typeChecks is the value-shape table, one row per ValueType. TypeObject
// is the catalogue's catch-all for structured or mixed values (an array of
// objects, a union): the vendor schema (step 2) judges it, so it has no row.
var typeChecks = map[ValueType]func(v any) string{
	TypeBool: func(v any) string {
		if _, ok := v.(bool); !ok {
			return "Expected true or false."
		}
		return ""
	},
	TypeInt: func(v any) string {
		if f, ok := asFloat(v); !ok || f != math.Trunc(f) {
			return "Expected a whole number."
		}
		return ""
	},
	TypeFloat: func(v any) string {
		if _, ok := asFloat(v); !ok {
			return "Expected a number."
		}
		return ""
	},
	TypeString:     expectString,
	TypeEnum:       expectString,
	TypeStringList: expectStringList,
	TypeRuleList:   expectStringList,
	TypeStringMap: func(v any) string {
		m, ok := v.(map[string]any)
		if !ok {
			return "Expected a key / value map."
		}
		for _, e := range m {
			if _, ok := e.(string); !ok {
				return "Expected text values in the map."
			}
		}
		return ""
	},
	TypeMCPMap: func(v any) string {
		if _, ok := v.(map[string]any); !ok {
			return "Expected an object."
		}
		return ""
	},
}

func expectString(v any) string {
	if _, ok := v.(string); !ok {
		return "Expected text."
	}
	return ""
}

func expectStringList(v any) string {
	l, ok := v.([]any)
	if !ok {
		return "Expected a list."
	}
	for _, e := range l {
		if _, ok := e.(string); !ok {
			return "Expected a list of text values."
		}
	}
	return ""
}

func inEnum(enum []string, s string) bool {
	for _, e := range enum {
		if e == s {
			return true
		}
	}
	return false
}

func boundsMsg(r SettingRow, v any) string {
	f, ok := asFloat(v)
	if !ok {
		return ""
	}
	if r.Min != nil && (f < *r.Min || (r.ExclusiveMin && f == *r.Min)) {
		return fmt.Sprintf("Below the documented minimum %v.", *r.Min)
	}
	if r.Max != nil && (f > *r.Max || (r.ExclusiveMax && f == *r.Max)) {
		return fmt.Sprintf("Above the documented maximum %v.", *r.Max)
	}
	return ""
}

func isScalar(v any) bool {
	switch v.(type) {
	case string, bool, int64, float64:
		return true
	}
	return false
}

// diffTrees lists the changes between two decoded documents. Each change
// is attributed to the deepest catalogue row covering it (rowPath; "" for
// an uncatalogued key): a path that is an intermediate object of some row
// recurses, so a changed child is reported on its own and a child of a
// map-typed row (env.FOO under env) is attributed to that row.
func diffTrees(rows map[string]SettingRow, before, after map[string]any) []Change {
	prefix := map[string]bool{}
	for _, r := range rows {
		for i := 1; i < len(r.Path); i++ {
			prefix[r.Path[:i].String()] = true
		}
	}
	var out []Change
	var walk func(p KeyPath, b, a map[string]any, cover string)
	walk = func(p KeyPath, b, a map[string]any, cover string) {
		keys := map[string]bool{}
		for k := range b {
			keys[k] = true
		}
		for k := range a {
			keys[k] = true
		}
		sorted := make([]string, 0, len(keys))
		for k := range keys {
			sorted = append(sorted, k)
		}
		sort.Strings(sorted)
		for _, k := range sorted {
			kp := append(append(KeyPath(nil), p...), k)
			s := kp.String()
			bv, bok := b[k]
			av, aok := a[k]
			cov := cover
			if _, isRow := rows[s]; isRow {
				cov = s
			}
			if prefix[s] && objectOrAbsent(bv, bok) && objectOrAbsent(av, aok) {
				bm, _ := bv.(map[string]any)
				am, _ := av.(map[string]any)
				walk(kp, bm, am, cov)
				continue
			}
			if bok && aok && cfgfmt.Equal(bv, av) {
				continue
			}
			c := Change{Path: s, Op: changeOp(bok, aok), rowPath: cov}
			if bok {
				c.Before = bv
			}
			if aok {
				c.After = av
			}
			out = append(out, c)
		}
	}
	walk(nil, before, after, "")
	return out
}

// objectOrAbsent reports a value a walk can descend into.
func objectOrAbsent(v any, ok bool) bool {
	if !ok || v == nil {
		return true
	}
	_, isMap := v.(map[string]any)
	return isMap
}

// splitDotted recovers the KeyPath of a dotted change path by walking the
// trees (a key may itself hold a dot; KeyPath.String quotes it).
func splitDotted(dotted string, trees ...map[string]any) KeyPath {
	for _, t := range trees {
		if p, ok := findPath(t, nil, dotted); ok {
			return p
		}
	}
	return KeyPath(strings.Split(dotted, "."))
}

func findPath(m map[string]any, prefix KeyPath, want string) (KeyPath, bool) {
	for k, v := range m {
		kp := append(append(KeyPath(nil), prefix...), k)
		s := kp.String()
		if s == want {
			return kp, true
		}
		if sub, ok := v.(map[string]any); ok && strings.HasPrefix(want, s+".") {
			if p, ok := findPath(sub, kp, want); ok {
				return p, true
			}
		}
	}
	return nil, false
}

// CountNoun renders "1 file" / "3 files": a count with the noun in the right
// number. many is the plural form; "" means one plus "s". Every count this
// package and its CLI print goes through it, so no message reads "value(s)".
func CountNoun(n int, one, many string) string {
	if many == "" {
		many = one + "s"
	}
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}
