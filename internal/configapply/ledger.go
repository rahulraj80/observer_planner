package configapply

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// CTA undo ledger (Stage 2 "Apply CTA", §undo/rollback): an append-only
// JSONL ledger of every briefing-CTA config apply and revert — the
// answer to "what did the briefing's one-click change and how do I undo
// it?". One event per line. It is a NODE-LOCAL file next to the
// observer DB — the same posture as the routingapply ledger; it never
// enters the database and therefore can never reach an org push.

// LedgerEvent is one recorded apply, revert, suspend, or resume. Revert
// events carry the restored From/To so the history reads as a coherent
// before/after.
type LedgerEvent struct {
	Time   time.Time `json:"time"`
	Kind   string    `json:"kind"`   // "apply" | "revert" | "suspend" | "resume"
	Source string    `json:"source"` // "briefing-cta"
	// Section/Key/From/To describe the written or restored lever.
	Section string `json:"section"`
	Key     string `json:"key"`
	From    string `json:"from"`
	To      string `json:"to"`
	// DedupKey/Detector tie the apply back to the opportunity card that
	// motivated it (empty on reverts). Title is the card's display title
	// (informational, recorded at suspend time so the strip can label it
	// without re-composing the briefing).
	DedupKey string `json:"dedup_key,omitempty"`
	Detector string `json:"detector,omitempty"`
	Title    string `json:"title,omitempty"`
	// ProjectedUSD is the monthly projection carried on the apply; it
	// lets Stage 3 realized-vs-estimated reporting cite the estimate.
	ProjectedUSD float64 `json:"projected_monthly_usd,omitempty"`
	// Stamp is the apply batch's backup stamp; Backup is the snapshot
	// file the apply created or the revert restored from.
	Stamp  string `json:"stamp,omitempty"`
	Backup string `json:"backup,omitempty"`
	// ChangeID discriminates which lever of a multi-lever card a
	// suspend/resume refers to.
	ChangeID string `json:"change_id,omitempty"`
	// Until is the snooze expiry for a suspend-snooze event; zero for a
	// pause (indefinite until resumed).
	Until time.Time `json:"until,omitempty"`
}

// Ledger appends and reads the CTA audit file. The zero value is
// unusable; construct with NewLedger.
type Ledger struct {
	path string
	now  func() time.Time
}

// NewLedger returns a ledger at path. The file (and its directory) are
// created on first append.
func NewLedger(path string) *Ledger {
	return &Ledger{path: path, now: time.Now}
}

// DefaultLedgerPath places the ledger next to the observer DB — the
// node-local data directory the dashboard already resolves.
func DefaultLedgerPath(dbPath string) string {
	return filepath.Join(filepath.Dir(dbPath), "briefing-cta-ledger.jsonl")
}

// Path returns the ledger's file path (for display surfaces).
func (l *Ledger) Path() string { return l.path }

// RecordApply appends an apply event for an executed change.
func (l *Ledger) RecordApply(c Change, dedupKey, detector string, projectedUSD float64, stamp, backup, source string) error {
	return l.append(LedgerEvent{
		Kind: "apply", Source: source,
		Section: c.Section, Key: c.Key, From: c.From, To: c.To,
		DedupKey: dedupKey, Detector: detector, ChangeID: c.ChangeID,
		ProjectedUSD: projectedUSD, Stamp: stamp, Backup: backup,
	})
}

// RecordRevert appends a revert event for a restored change, keyed by the
// apply's card (dedupKey) and lever (changeID) so per-card CardState
// derivations flip back to not-applied (empty dedupKey/changeID for
// lever-wide reverts that predate the per-card model).
func (l *Ledger) RecordRevert(c Change, dedupKey, changeID, backup, source string) error {
	return l.append(LedgerEvent{
		Kind: "revert", Source: source,
		DedupKey: dedupKey, ChangeID: changeID,
		Section: c.Section, Key: c.Key, From: c.From, To: c.To,
		Backup: backup,
	})
}

// RecordSuspend appends a suspend event. Two shapes:
//   - snooze (until non-zero): the card's recommendation is suppressed
//     until `until` — no config change, reversible via resume.
//   - pause (until zero, section/key/backup set): an applied lever was
//     restored to its pre-apply value; From = the restored value, To =
//     the value a resume re-applies, Backup = the apply's snapshot.
func (l *Ledger) RecordSuspend(dedupKey, detector, title, source, changeID, section, key, from, to, backup string, until time.Time) error {
	return l.append(LedgerEvent{
		Kind: "suspend", Source: source,
		DedupKey: dedupKey, Detector: detector, Title: title,
		ChangeID: changeID, Section: section, Key: key, From: from, To: to,
		Backup: backup, Until: until,
	})
}

// RecordResume appends a resume event clearing a card's suspension. When
// the cleared suspension was a pause, Section/Key carry the re-applied
// lever (a sibling "apply" event records the actual config write).
func (l *Ledger) RecordResume(dedupKey, source, changeID, section, key string) error {
	return l.append(LedgerEvent{
		Kind: "resume", Source: source,
		DedupKey: dedupKey, ChangeID: changeID, Section: section, Key: key,
	})
}

func (l *Ledger) append(ev LedgerEvent) error {
	ev.Time = l.now().UTC()
	if err := os.MkdirAll(filepath.Dir(l.path), 0o755); err != nil {
		return fmt.Errorf("configapply: ensure ledger dir: %w", err)
	}
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("configapply: open ledger: %w", err)
	}
	defer f.Close()
	line, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("configapply: marshal ledger event: %w", err)
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("configapply: append ledger: %w", err)
	}
	return nil
}

// Read returns the ledger's events oldest-first. A missing file is an
// empty ledger, not an error. Unparseable lines are skipped and counted
// — a half-written line from a crashed process must not make the whole
// history unreadable.
func (l *Ledger) Read() (events []LedgerEvent, skipped int, err error) {
	f, err := os.Open(l.path)
	if os.IsNotExist(err) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, fmt.Errorf("configapply: open ledger: %w", err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var ev LedgerEvent
		if jsonErr := json.Unmarshal(line, &ev); jsonErr != nil {
			skipped++
			continue
		}
		events = append(events, ev)
	}
	if scanErr := sc.Err(); scanErr != nil {
		return events, skipped, fmt.Errorf("configapply: read ledger: %w", scanErr)
	}
	return events, skipped, nil
}

// LatestApply returns the newest apply event for a (section, key) lever
// that carries a backup — the undo target for POST /api/briefing/cta/
// revert. ok=false when no such apply exists.
func (l *Ledger) LatestApply(section, key string) (LedgerEvent, bool, error) {
	events, _, err := l.Read()
	if err != nil {
		return LedgerEvent{}, false, err
	}
	var best LedgerEvent
	found := false
	for _, ev := range events {
		if ev.Kind != "apply" || ev.Section != section || ev.Key != key || ev.Backup == "" {
			continue
		}
		if !found || ev.Time.After(best.Time) {
			best, found = ev, true
		}
	}
	return best, found, nil
}

// AppliedBackups returns every apply event that carries a backup,
// oldest-first — the full set a `{all: true}` revert unwinds. Duplicate
// backup paths (re-applies in the same stamp batch) are deduped.
func (l *Ledger) AppliedBackups() ([]LedgerEvent, error) {
	events, _, err := l.Read()
	if err != nil {
		return nil, err
	}
	var out []LedgerEvent
	seen := map[string]bool{}
	for _, ev := range events {
		if ev.Kind != "apply" || ev.Backup == "" || seen[ev.Backup] {
			continue
		}
		seen[ev.Backup] = true
		out = append(out, ev)
	}
	return out, nil
}

// Suspension is an ACTIVE (non-expired) suspension of one opportunity
// card lever, derived from the ledger. Kind "snooze" = recommendation
// muted until Until (ChangeID empty — whole card); kind "pause" = an
// applied lever currently restored (Section/Key/Backup identify the lever
// and its pre-apply snapshot; resume re-applies To).
type Suspension struct {
	DedupKey string    `json:"dedup_key"`
	Detector string    `json:"detector,omitempty"`
	Title    string    `json:"title,omitempty"`
	Kind     string    `json:"kind"` // "snooze" | "pause"
	Until    time.Time `json:"until,omitempty"`
	ChangeID string    `json:"change_id,omitempty"`
	Section  string    `json:"section,omitempty"`
	Key      string    `json:"key,omitempty"`
	From     string    `json:"from,omitempty"` // value restored by the pause
	To       string    `json:"to,omitempty"`   // applied value; resume target
	Backup   string    `json:"backup,omitempty"`
	Time     time.Time `json:"time"`
}

// AppliedLever is a currently-applied lever of a card, carrying the data
// resume needs to re-apply it after a pause.
type AppliedLever struct {
	ChangeID     string  `json:"change_id"`
	Section      string  `json:"section"`
	Key          string  `json:"key"`
	From         string  `json:"from"` // pre-apply value (the pause restores this)
	To           string  `json:"to"`   // applied value (resume re-applies this)
	Backup       string  `json:"backup"`
	Stamp        string  `json:"stamp,omitempty"`
	Detector     string  `json:"detector,omitempty"`
	ProjectedUSD float64 `json:"projected_monthly_usd,omitempty"`
}

// CardState is the derived CTA state of one opportunity card as of now:
// which levers are currently applied and which suspensions are active.
// A card is "applied" when it has ≥1 applied lever; "suspended" when it
// has ≥1 active suspension.
type CardState struct {
	DedupKey      string         `json:"dedup_key"`
	Applied       bool           `json:"applied"`
	AppliedLevers []AppliedLever `json:"applied_levers,omitempty"`
	Suspended     bool           `json:"suspended"`
	Suspensions   []Suspension   `json:"suspensions,omitempty"`
}

// CardState derives the CTA state of one card from the ledger, as of
// now. Per (dedup_key, change_id) the NEWEST of {apply, revert,
// suspend, resume} decides applied-ness: apply/resume → applied,
// revert/suspend → not. Suspensions use the NEWEST of {suspend, resume}
// per (dedup_key, change_id), dropping resumed and expired-snoozed ones.
// Derived from the ledger (not a second file) so the ledger stays the
// single audit source of truth for the whole CTA lifecycle.
func (l *Ledger) CardState(dedupKey string, now time.Time) (CardState, error) {
	events, _, err := l.Read()
	if err != nil {
		return CardState{}, err
	}
	st := CardState{DedupKey: dedupKey}
	latest := map[string]LedgerEvent{}  // newest lifecycle event per change_id
	applied := map[string]LedgerEvent{} // newest apply per change_id
	susp := map[string]LedgerEvent{}    // newest suspend/resume per change_id
	for _, ev := range events {
		if ev.DedupKey != dedupKey {
			continue
		}
		if ev.ChangeID != "" {
			// Events arrive oldest-first; on equal timestamps the
			// later-appended event is the newer one (tests pin fixed
			// clocks), so ties must favor the last scanned.
			if prev, ok := latest[ev.ChangeID]; !ok || !ev.Time.Before(prev.Time) {
				latest[ev.ChangeID] = ev
			}
			if ev.Kind == "apply" {
				if prev, ok := applied[ev.ChangeID]; !ok || !ev.Time.Before(prev.Time) {
					applied[ev.ChangeID] = ev
				}
			}
		}
		// Whole-card snoozes live under the empty change_id key; per-lever
		// suspensions under their change_id.
		if ev.Kind == "suspend" || ev.Kind == "resume" {
			if prev, ok := susp[ev.ChangeID]; !ok || !ev.Time.Before(prev.Time) {
				susp[ev.ChangeID] = ev
			}
		}
	}
	for id, ap := range applied {
		if la, ok := latest[id]; ok && (la.Kind == "apply" || la.Kind == "resume") {
			st.Applied = true
			st.AppliedLevers = append(st.AppliedLevers, AppliedLever{
				ChangeID: id, Section: ap.Section, Key: ap.Key,
				From: ap.From, To: ap.To,
				Backup: ap.Backup, Stamp: ap.Stamp,
				Detector: ap.Detector, ProjectedUSD: ap.ProjectedUSD,
			})
		}
	}
	// Whole-card snoozes live under the empty change_id key.
	keys := keysOf(susp)
	if _, ok := susp[""]; !ok {
		keys = append(keys, "")
	}
	for _, key := range keys {
		ev, ok := susp[key]
		if !ok || ev.Kind == "resume" {
			continue
		}
		if !ev.Until.IsZero() && !ev.Until.After(now) {
			continue // expired snooze
		}
		kind := "snooze"
		if ev.Until.IsZero() {
			kind = "pause"
		}
		st.Suspensions = append(st.Suspensions, Suspension{
			DedupKey: ev.DedupKey, Detector: ev.Detector, Title: ev.Title, Kind: kind,
			Until: ev.Until, ChangeID: ev.ChangeID,
			Section: ev.Section, Key: ev.Key, From: ev.From, To: ev.To,
			Backup: ev.Backup, Time: ev.Time,
		})
	}
	st.Suspended = len(st.Suspensions) > 0
	sort.Slice(st.AppliedLevers, func(i, j int) bool { return st.AppliedLevers[i].ChangeID < st.AppliedLevers[j].ChangeID })
	sort.Slice(st.Suspensions, func(i, j int) bool { return st.Suspensions[i].ChangeID < st.Suspensions[j].ChangeID })
	return st, nil
}

// keysOf returns the sorted map keys (stability for Callers).
func keysOf(m map[string]LedgerEvent) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ActiveSuspensions returns every active suspension across all cards as
// of now, derived via CardState. Sorted by dedup key then change id.
func (l *Ledger) ActiveSuspensions(now time.Time) ([]Suspension, error) {
	events, _, err := l.Read()
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []Suspension
	for _, ev := range events {
		if ev.DedupKey == "" || seen[ev.DedupKey] {
			continue
		}
		seen[ev.DedupKey] = true
		st, err := l.CardState(ev.DedupKey, now)
		if err != nil {
			return nil, err
		}
		out = append(out, st.Suspensions...)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].DedupKey != out[j].DedupKey {
			return out[i].DedupKey < out[j].DedupKey
		}
		return out[i].ChangeID < out[j].ChangeID
	})
	return out, nil
}
