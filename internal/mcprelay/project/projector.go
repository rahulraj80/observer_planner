package project

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Typed refusals.
var (
	ErrNoWriter    = errors.New("project: no Writer wired")
	ErrNoJournal   = errors.New("project: no Journal wired (a write without a journaled original cannot be reversed)")
	ErrCredential  = errors.New("project: an authorization header is never projected (tokens are memory-only)")
	ErrDuplicate   = errors.New("project: duplicate entry name")
	ErrBadEndpoint = errors.New("project: entry URL must be https:// or a loopback http://")
	ErrNoWrapCmd   = errors.New("project: WrapSpec.Command (the observer binary) is required")
	// ErrConfigChanged is the CAS refusal every Writer.Commit / Writer.Revert
	// raises when the config no longer holds the bytes the caller compared
	// against in this process (Sol P3+P4 finding 7). The journal rows are
	// retained; the message names the file.
	ErrConfigChanged = errors.New("project: client config changed underneath the relay; refusing to write (journal retained)")
)

// skipRules is the ordered table deciding whether a client is projected.
var skipRules = []struct {
	reason string
	match  func(c Client) bool
}{
	{"unverified", func(c Client) bool { return !c.Verified }},
	{"no_config_path", func(c Client) bool { return strings.TrimSpace(c.ConfigPath) == "" }},
	{"no_format", func(c Client) bool { return strings.TrimSpace(c.Format) == "" }},
}

// Plan validates the desired state and decides per client. It is pure.
func Plan(clients []Client, desired Desired) ([]Op, error) {
	if desired.Wrap != nil && strings.TrimSpace(desired.Wrap.Command) == "" {
		return nil, ErrNoWrapCmd
	}
	if err := validateEntries(desired.Remote); err != nil {
		return nil, err
	}
	ops := make([]Op, 0, len(clients))
	for _, c := range clients {
		op := Op{Client: c}
		for _, r := range skipRules {
			if r.match(c) {
				op.Skip = r.reason
				break
			}
		}
		ops = append(ops, op)
	}
	return ops, nil
}

func validateEntries(entries []RemoteEntry) error {
	seen := map[string]bool{}
	for _, e := range entries {
		if strings.TrimSpace(e.Name) == "" {
			return fmt.Errorf("project.Plan: entry with empty name")
		}
		if seen[e.Name] {
			return fmt.Errorf("project.Plan: %w: %q", ErrDuplicate, e.Name)
		}
		seen[e.Name] = true
		if !validEndpoint(e.URL) {
			return fmt.Errorf("project.Plan: %w: %q (%s)", ErrBadEndpoint, e.URL, e.Name)
		}
		if e.Transport != "" && e.Transport != "http" && e.Transport != "sse" {
			return fmt.Errorf("project.Plan: entry %q transport %q is not http/sse", e.Name, e.Transport)
		}
		for k := range e.Headers {
			if strings.EqualFold(strings.TrimSpace(k), "authorization") {
				return fmt.Errorf("project.Plan: %w (%s)", ErrCredential, e.Name)
			}
		}
	}
	return nil
}

func validEndpoint(u string) bool {
	switch {
	case strings.HasPrefix(u, "https://"):
		return len(u) > len("https://")
	case strings.HasPrefix(u, "http://127.0.0.1"), strings.HasPrefix(u, "http://localhost"), strings.HasPrefix(u, "http://[::1]"):
		return true
	}
	return false
}

// Fingerprint is the idempotence marker: sha256 over the canonical JSON of
// the desired state (remote entries sorted by name + the wrap spec + the
// approved binding set, sorted). Two applies of the same desired state
// carry the same fingerprint.
func Fingerprint(desired Desired) string {
	sorted := append([]RemoteEntry(nil), desired.Remote...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	approved := append([]ApprovedServer(nil), desired.Approved...)
	sort.Slice(approved, func(i, j int) bool {
		a, b := approved[i], approved[j]
		if a.VServer != b.VServer {
			return a.VServer < b.VServer
		}
		return a.ServerID < b.ServerID
	})
	raw, _ := json.Marshal(struct {
		Remote   []RemoteEntry    `json:"remote"`
		Wrap     *WrapSpec        `json:"wrap,omitempty"`
		Approved []ApprovedServer `json:"approved,omitempty"`
	}{sorted, desired.Wrap, approved})
	return hashHex(raw)
}

// bound returns desired with the wrap's Bind installed from
// desired.Approved (the projector's binding authority - a caller-supplied
// Bind is replaced) and a func returning the refusals the writer's
// consultations produced, sorted and de-duplicated by key.
func bound(desired Desired) (Desired, func() []EntryRefusal) {
	if desired.Wrap == nil {
		return desired, func() []EntryRefusal { return nil }
	}
	refused := map[string]string{}
	w := *desired.Wrap
	approved := desired.Approved
	w.Bind = func(key string) (Binding, string) {
		b, reason := Bind(approved, key)
		if reason != "" {
			refused[key] = reason
		}
		return b, reason
	}
	desired.Wrap = &w
	return desired, func() []EntryRefusal {
		out := make([]EntryRefusal, 0, len(refused))
		for k, r := range refused {
			out = append(out, EntryRefusal{Key: k, Reason: r})
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
		if len(out) == 0 {
			return nil
		}
		return out
	}
}

// verifyBindings refuses a stage whose writer wrapped an entry without its
// approved binding (a writer that ignored WrapSpec.BindingFor): the wrapper
// could not authorize a single call for it, so nothing is written.
func verifyBindings(st Staged, approved []ApprovedServer) error {
	for _, w := range st.Wrapped {
		b, reason := Bind(approved, w.Key)
		if reason != "" {
			return fmt.Errorf("writer wrapped entry %q that binds to no approved server (%s)", w.Key, reason)
		}
		if w.VServer != b.VServer || w.ServerID != b.ServerID {
			return fmt.Errorf("writer staged entry %q under binding %s/%s, approved binding is %s/%s", w.Key, w.VServer, w.ServerID, b.VServer, b.ServerID)
		}
	}
	return nil
}

func hashHex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// ownedDigestTag domain-separates the owned-part digests (T-397) from the
// whole-file digests an older build stored in the same column, so a legacy
// row can never verify by accident.
const ownedDigestTag = "sbo-mcp-relay-owned-v1"

// OwnedStdioDigest is the applied digest of one wrapped stdio entry: the
// SHA-256 of the entry key and its launch (command + args) ONLY. The rest
// of the entry (env values, headers, timeouts) and every other byte of the
// client config are deliberately outside it: the config can hold
// credentials (D-268 / D-270), and only the part the relay owns may leave a
// derivative in the journal (T-397).
func OwnedStdioDigest(key, command string, args []string) string {
	if args == nil {
		args = []string{}
	}
	b, _ := json.Marshal(struct {
		K string   `json:"k"`
		C string   `json:"c"`
		A []string `json:"a"`
	}{key, command, args})
	return hashHex(append([]byte(ownedDigestTag+"\x00stdio\x00"), b...))
}

// OwnedRemoteDigest is the applied digest of one relay-owned remote entry:
// the SHA-256 of the entry key and the whole entry value as the client
// config holds it (canonical JSON, keys sorted). The relay writes the whole
// entry and it carries no secret by construction (Plan refuses an
// authorization header).
func OwnedRemoteDigest(key string, entry any) string {
	b, err := json.Marshal(struct {
		K string `json:"k"`
		E any    `json:"e"`
	}{key, entry})
	if err != nil {
		return ""
	}
	return hashHex(append([]byte(ownedDigestTag+"\x00remote\x00"), b...))
}

// Projector applies a plan through the two seams. It is the ONE owner of
// the launch-journal sequence (parking decision B5): the writers are pure
// write primitives and journal nothing on their own.
type Projector struct {
	Writer  Writer
	Journal Journal
	Now     func() time.Time
}

func (p *Projector) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now().UTC()
}

// rowKey is the journal primary key.
func rowKey(tool, path, key string) string { return tool + "\x00" + path + "\x00" + key }

// Apply projects the desired state into every plannable client, per client:
// stage -> (no change: refresh the owned digests only) -> one journal row
// per replaced / added entry carrying the original launch and the OWNED
// digest of the entry as staged (CAS on the row's generation) -> atomic
// rewrite (CAS on the staged Before, compared in this process only). No
// copy of the client config is taken and no whole-file digest is stored:
// the config can hold credentials (D-268 / D-270, T-397), so the journal
// keeps only what the relay itself wrote, and disable reverses exactly
// those entries. A journal failure aborts THAT client's write (never a
// half-journaled write); a writer failure is reported per client; the loop
// continues so one broken config does not hold the rest hostage. Both seams
// must be wired before any write. A crash between any two steps is
// recoverable by re-running Apply: a row whose config was never rewritten
// stages Changed again and is re-applied.
func (p *Projector) Apply(ctx context.Context, clients []Client, desired Desired) (Report, error) {
	if p.Writer == nil {
		return Report{}, ErrNoWriter
	}
	if p.Journal == nil {
		return Report{}, ErrNoJournal
	}
	ops, err := Plan(clients, desired)
	if err != nil {
		return Report{}, err
	}
	existingRows, err := p.Journal.List(ctx)
	if err != nil {
		return Report{}, fmt.Errorf("project.Apply: journal list: %w", err)
	}
	existing := make(map[string]JournalRow, len(existingRows))
	for _, r := range existingRows {
		existing[rowKey(r.Tool, r.ConfigPath, r.EntryKey)] = r
	}
	rep := Report{Fingerprint: Fingerprint(desired)}
	for _, op := range ops {
		r := Receipt{Client: op.Client, Skipped: op.Skip}
		if op.Skip == "" {
			r = p.applyOne(ctx, op.Client, desired, existing)
		}
		rep.Receipts = append(rep.Receipts, r)
	}
	return rep, nil
}

func (p *Projector) applyOne(ctx context.Context, c Client, desired Desired, existing map[string]JournalRow) Receipt {
	r := Receipt{Client: c}
	bd, refusals := bound(desired)
	st, err := p.Writer.Stage(ctx, c, bd)
	r.Refused = refusals()
	if err != nil {
		r.Err = fmt.Sprintf("stage: %v", err)
		return r
	}
	if err := verifyBindings(st, desired.Approved); err != nil {
		r.Err = fmt.Sprintf("stage: %v; write refused", err)
		return r
	}
	now := p.now()
	if !st.Changed {
		r.Note = p.refreshOwned(ctx, c, st.Owned, now, existing, nil)
		return r
	}
	rows := make([]JournalRow, 0, len(st.Wrapped)+len(st.RemoteKeys))
	for _, w := range st.Wrapped {
		rows = append(rows, JournalRow{
			Tool: c.Tool, ConfigPath: c.ConfigPath, EntryKey: w.Key,
			OrigCommand: w.Command, OrigArgs: append([]string(nil), w.Args...), OrigCwd: w.Cwd,
			OrigEnvRefs: append([]string(nil), w.EnvKeys...), VServer: w.VServer, RegistryServerID: w.ServerID,
		})
	}
	for _, k := range st.RemoteKeys {
		rows = append(rows, JournalRow{Tool: c.Tool, ConfigPath: c.ConfigPath, EntryKey: k, OrigCommand: RemoteOrigCommand})
	}
	if len(rows) == 0 {
		// A stage that changed bytes without naming an entry is a writer
		// bug: refuse rather than write something the journal cannot undo.
		r.Err = "stage: changed bytes but named no entry (nothing to journal); write refused"
		return r
	}
	for _, row := range rows {
		if _, ok := st.Owned[row.EntryKey]; !ok {
			// A stage that wrote an entry without its owned digest is a
			// writer bug: the row could never verify, so refuse the write.
			r.Err = fmt.Sprintf("stage: entry %s has no owned digest; write refused", row.EntryKey)
			return r
		}
	}
	// Step 1: the journal rows, durable before the write. A row's
	// generation bumps only when the entry the relay owns changes shape (a
	// different wrapper launch or remote entry); a retry of an UNLANDED
	// write carries the same owned digest and keeps its generation.
	written := make(map[string]bool, len(rows))
	for _, row := range rows {
		owned := st.Owned[row.EntryKey]
		var expected int64
		if ex, ok := existing[rowKey(row.Tool, row.ConfigPath, row.EntryKey)]; ok {
			row.ConfigGeneration, expected = ex.ConfigGeneration, ex.ConfigGeneration
			if ex.AppliedSHA256 != owned {
				row.ConfigGeneration = ex.ConfigGeneration + 1
			}
		} else {
			row.ConfigGeneration, expected = 1, 0
		}
		row.AppliedSHA256, row.AppliedAt = owned, now
		if err := p.Journal.Put(ctx, row, expected); err != nil {
			r.Err = fmt.Sprintf("journal %s: %v (write aborted)", row.EntryKey, err)
			return r
		}
		existing[rowKey(row.Tool, row.ConfigPath, row.EntryKey)] = row
		written[rowKey(row.Tool, row.ConfigPath, row.EntryKey)] = true
	}
	// Step 2: the atomic rewrite, CAS on the staged Before.
	if err := p.Writer.Commit(ctx, c, st); err != nil {
		r.Err = fmt.Sprintf("commit: %v (journal rows retained; re-run to re-apply)", err)
		return r
	}
	r.Changed = true
	r.Note = p.refreshOwned(ctx, c, st.Owned, now, existing, written)
	for _, w := range st.Wrapped {
		r.Wrapped = append(r.Wrapped, w.Key)
	}
	r.Remote = append(r.Remote, st.RemoteKeys...)
	return r
}

// refreshOwned brings the config's OTHER rows in line with what the stage
// saw in the file (T-397): a row whose entry is still the relay-owned one
// gets that entry's owned digest. This converts a row an older build wrote
// (a whole-file digest and a backup reference) on the next projection, and
// is a no-op for a current row. A row whose entry is no longer the
// relay-owned one is left alone and honestly reports not-applied. A failed
// refresh is a note: those rows under-claim until the next projection,
// never over-claim.
func (p *Projector) refreshOwned(ctx context.Context, c Client, owned map[string]string, now time.Time, existing map[string]JournalRow, written map[string]bool) string {
	keys := make([]string, 0, len(existing))
	for k, row := range existing {
		if written[k] || row.Tool != c.Tool || row.ConfigPath != c.ConfigPath {
			continue
		}
		d, ok := owned[row.EntryKey]
		if !ok || (row.AppliedSHA256 == d && row.BackupPath == "" && row.BackupSHA256 == "") {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var failed []string
	for _, k := range keys {
		row := existing[k]
		row.AppliedSHA256, row.AppliedAt = owned[row.EntryKey], now
		row.BackupPath, row.BackupSHA256 = "", ""
		if err := p.Journal.Put(ctx, row, row.ConfigGeneration); err != nil {
			failed = append(failed, row.EntryKey)
			continue
		}
		existing[k] = row
	}
	if len(failed) == 0 {
		return ""
	}
	return "owned-digest refresh failed for " + strings.Join(failed, ",") + " (they report not-applied until the next projection)"
}

// Preview stages every plannable client WITHOUT journaling or writing
// anything (Writer.Stage only - it never touches a file): the receipts name
// the stdio entries a projection would wrap NOW (Wrapped: bound, not yet
// the wrapper) and the ones it refuses (Refused), and Changed reports a
// write would happen. The coverage matrix uses it to know which stdio
// entries of a client are NOT mediated right now.
func (p *Projector) Preview(ctx context.Context, clients []Client, desired Desired) (Report, error) {
	if p.Writer == nil {
		return Report{}, ErrNoWriter
	}
	ops, err := Plan(clients, desired)
	if err != nil {
		return Report{}, err
	}
	rep := Report{Fingerprint: Fingerprint(desired)}
	for _, op := range ops {
		r := Receipt{Client: op.Client, Skipped: op.Skip}
		if op.Skip == "" {
			bd, refusals := bound(desired)
			st, serr := p.Writer.Stage(ctx, op.Client, bd)
			r.Refused = refusals()
			if serr != nil {
				r.Err = fmt.Sprintf("stage: %v", serr)
			} else {
				r.Changed = st.Changed
				for _, w := range st.Wrapped {
					r.Wrapped = append(r.Wrapped, w.Key)
				}
				r.Remote = append(r.Remote, st.RemoteKeys...)
			}
		}
		rep.Receipts = append(rep.Receipts, r)
	}
	return rep, nil
}

// group is every journal row of one (tool, config path).
type group struct {
	client Client
	rows   []JournalRow
}

// restoreRule is one ordered row of the restore decision table: the first
// rule whose match accepts the group decides the mode.
type restoreRule struct {
	name  string
	match func(g group) bool
	mode  string
}

// restoreRules is walked top-down, first match wins. Since T-397 there is
// no whole-file mode: no copy of a client config is kept (it can hold
// credentials, D-268 / D-270), so every group is the format-aware reversal
// of only the relay-owned entries, which also preserves any edit made after
// the projection. Legacy rows from an older build (a whole-file digest and
// a backup reference) take the same path; their backup is never read.
var restoreRules = []restoreRule{
	{name: "relay_owned_reversal", mode: RestoreReversal, match: func(group) bool { return true }},
}

// Restore puts every journaled client config back, one (tool, config)
// group at a time, in stable order, through the format-aware reversal of
// only the relay-owned entries (post-projection edits preserved). Rows are
// deleted only after their group restored; a refused group keeps its rows
// and reports why. Returns the first error after attempting every group.
func (p *Projector) Restore(ctx context.Context) ([]RestoreReceipt, error) {
	if p.Writer == nil {
		return nil, ErrNoWriter
	}
	if p.Journal == nil {
		return nil, ErrNoJournal
	}
	rows, err := p.Journal.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("project.Restore: journal list: %w", err)
	}
	groups := map[string]*group{}
	var order []string
	for _, r := range rows {
		k := r.Tool + "\x00" + r.ConfigPath
		g, ok := groups[k]
		if !ok {
			g = &group{client: Client{Tool: r.Tool, ConfigPath: r.ConfigPath, Verified: true}}
			groups[k] = g
			order = append(order, k)
		}
		g.rows = append(g.rows, r)
	}
	sort.Strings(order)
	var out []RestoreReceipt
	var first error
	for _, k := range order {
		rec := p.restoreGroup(ctx, groups[k])
		if rec.Err != "" && first == nil {
			first = fmt.Errorf("project.Restore %s: %s", rec.Client.Tool, rec.Err)
		}
		out = append(out, rec)
	}
	return out, first
}

func (p *Projector) restoreGroup(ctx context.Context, g *group) RestoreReceipt {
	rec := RestoreReceipt{Client: g.client, Rows: len(g.rows)}
	var rule restoreRule
	for _, r := range restoreRules {
		if r.match(*g) {
			rule = r
			break
		}
	}
	rec.Rule, rec.Mode = rule.name, rule.mode
	if err := p.Writer.Revert(ctx, g.client, g.rows); err != nil {
		rec.Err = err.Error()
		return rec
	}
	rec.Restored = true
	for _, r := range g.rows {
		if derr := p.Journal.Delete(ctx, r.Tool, r.ConfigPath, r.EntryKey); derr != nil && rec.Err == "" {
			rec.Err = fmt.Sprintf("restored, but journal row %s not deleted: %v", r.EntryKey, derr)
		}
	}
	return rec
}

// Applied reports the tools whose config carries at least one journal row
// on this node, of EITHER kind and WITHOUT verifying the row landed. It is
// NOT an applied-state predicate: a row whose commit failed is still
// listed (Sol P3+P4 fold finding 3). Coverage and the effective-state ACK
// use VerifyApplied.
func Applied(rows []JournalRow) map[string]bool {
	out := map[string]bool{}
	for _, r := range rows {
		out[r.Tool] = true
	}
	return out
}

// AppliedState is the VERIFIED applied predicate per client, separately
// per transport (Sol P3+P4 fold finding 3): Stdio[tool] is true when at
// least one RowStdioWrap row of the tool verified, Remote[tool] likewise
// for a RowRemote row. Each coverage transport rule consumes only its own.
type AppliedState struct {
	Stdio  map[string]bool
	Remote map[string]bool
}

// StdioApplied reports tool's verified stdio-wrap projection.
func (a AppliedState) StdioApplied(tool string) bool { return a.Stdio[tool] }

// RemoteApplied reports tool's verified remote-entry projection.
func (a AppliedState) RemoteApplied(tool string) bool { return a.Remote[tool] }

// Any reports whether any row of any tool verified (the relay point's
// `projection.applied` capability).
func (a AppliedState) Any() bool { return len(a.Stdio) > 0 || len(a.Remote) > 0 }

// Clients lists the tools with at least one verified row, sorted.
func (a AppliedState) Clients() []string {
	seen := map[string]bool{}
	for t := range a.Stdio {
		seen[t] = true
	}
	for t := range a.Remote {
		seen[t] = true
	}
	out := make([]string, 0, len(seen))
	for t := range seen {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// OwnedDigestsFunc returns, for one client config, the CURRENT owned digest
// of each journaled entry it can read (OwnedStdioDigest /
// OwnedRemoteDigest, keyed by entry key; an entry that is absent or no
// longer readable is missing from the map). ok=false when the config cannot
// be read at all. It is the Writer's Owned method in production.
type OwnedDigestsFunc func(client Client, rows []JournalRow) (map[string]string, bool)

// VerifyApplied derives the verified applied state from the journal rows
// and the CURRENT owned digests of their entries (T-397: the owned part
// only, never a whole-file digest). A row counts only when its entry's
// current owned digest equals the row's AppliedSHA256 - the relay's entry
// is what is on disk. A row whose commit failed (the entry is still the
// original), an entry edited since, an unreadable / absent config, a
// pre-132 row with no applied digest and a legacy row still carrying a
// whole-file digest all count as NOT applied (the honest direction:
// under-claim, never over-claim). owned is called once per (tool, config).
// Pure.
func VerifyApplied(rows []JournalRow, owned OwnedDigestsFunc) AppliedState {
	st := AppliedState{Stdio: map[string]bool{}, Remote: map[string]bool{}}
	if owned == nil {
		return st
	}
	type groupKey struct{ tool, path string }
	groups := map[groupKey][]JournalRow{}
	var order []groupKey
	for _, r := range rows {
		k := groupKey{r.Tool, r.ConfigPath}
		if _, ok := groups[k]; !ok {
			order = append(order, k)
		}
		groups[k] = append(groups[k], r)
	}
	for _, k := range order {
		g := groups[k]
		cur, ok := owned(Client{Tool: k.tool, ConfigPath: k.path, Verified: true}, g)
		if !ok {
			continue
		}
		for _, r := range g {
			if r.AppliedSHA256 == "" || cur[r.EntryKey] != r.AppliedSHA256 {
				continue
			}
			st.mark(r)
		}
	}
	return st
}

// mark records one verified row under its transport.
func (a AppliedState) mark(r JournalRow) {
	switch r.Kind() {
	case RowRemote:
		a.Remote[r.Tool] = true
	default:
		a.Stdio[r.Tool] = true
	}
}
