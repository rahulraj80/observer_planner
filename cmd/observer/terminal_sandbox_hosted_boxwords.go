//go:build !sbo_omit_terminals_sandbox && !sbo_omit_isolation && !sbo_omit_terminals

package main

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/marmutapp/superbased-observer/internal/dashapi"
	"github.com/marmutapp/superbased-observer/internal/sandboxrelay"
	"github.com/marmutapp/superbased-observer/internal/sandboxrelayd"
	"github.com/marmutapp/superbased-observer/internal/sandboxremote"
	vocab "github.com/marmutapp/superbased-observer/internal/sandboxvocab"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// A hosted run's box note (demo screenshots 2026-11-12, run_box_words.go):
// on a row whose node decides the box's isolation (Vendor.SeveralIsolations,
// the SandHold row), the host helper leaves what the node said it made, in
// the node's own words, as soon as the create is answered. It is keyed by
// the terminal run the relay run is bound to (the daemon binds it before the
// helper starts), so the run history finds it after the run ended and after
// a daemon restart; a run with no binding (a CLI launch without a daemon)
// keys it by the sandbox run id. Notes older than hostedBoxNoteKeep are swept
// when a new one is written.

// hostedBoxNote is one run's box note.
type hostedBoxNote struct {
	V         int    `json:"v"`
	Isolation string `json:"isolation"`
	Network   string `json:"network,omitempty"`
	RunsOn    string `json:"runs_on,omitempty"`
}

// hostedBoxNoteKeep is how long a box note is kept (well past the run
// history the Sandboxes tab and `observer terminals ls` read).
const hostedBoxNoteKeep = 90 * 24 * time.Hour

// hostedBoxIsolations and hostedBoxNetworks are the node words a note may
// hold (SandHold's tiers and network words); anything else is not kept.
var (
	hostedBoxIsolations = map[string]bool{"shared": true, "shielded": true, "sealed": true, "vault": true}
	hostedBoxNetworks   = map[string]bool{"none": true, "limited": true, "open": true}
)

// hostedBoxNoteDir is where the notes live.
func hostedBoxNoteDir(observerDir string) string {
	return filepath.Join(hostedStateDir(observerDir), "boxes")
}

// hostedBoxNotePath is one run's note.
func hostedBoxNotePath(observerDir, id string) string {
	return filepath.Join(hostedBoxNoteDir(observerDir), id+".json")
}

// hostedBoxRunsOnOK accepts a target name: a short token, never a sentence.
func hostedBoxRunsOnOK(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.') {
			return false
		}
	}
	return true
}

// hostedBoxNoteFrom is the note for a vendor's create answer (BoxInfo.Extra:
// isolation, network, runs_on); ok=false when the answer names no isolation
// in the node's words.
func hostedBoxNoteFrom(extra map[string]string) (hostedBoxNote, bool) {
	n := hostedBoxNote{V: 1, Isolation: strings.ToLower(strings.TrimSpace(extra["isolation"]))}
	if !hostedBoxIsolations[n.Isolation] {
		return hostedBoxNote{}, false
	}
	if w := strings.ToLower(strings.TrimSpace(extra["network"])); hostedBoxNetworks[w] {
		n.Network = w
	}
	if w := strings.TrimSpace(extra["runs_on"]); hostedBoxRunsOnOK(w) {
		n.RunsOn = w
	}
	return n, true
}

// hostedBoxNoteKey is the id a run's note is written under: the terminal run
// the relay run is bound to, else the sandbox run id.
func hostedBoxNoteKey(observerDir, sandboxRunID string) string {
	if rec, err := sandboxrelayd.ReadRun(observerDir, sandboxRunID); err == nil && sandboxrelay.ValidRunID(rec.TerminalRunID) {
		return rec.TerminalRunID
	}
	return sandboxRunID
}

// writeHostedBoxNote leaves what the node said it made for a run's box (best
// effort: a run whose note is missing reads as "the node has not said"). It
// returns the id the note is keyed by (the stored terminal run's) and the
// note; ok=false when the answer named no isolation in the node's words.
func writeHostedBoxNote(observerDir, sandboxRunID string, extra map[string]string) (string, hostedBoxNote, bool) {
	if observerDir == "" || !sandboxrelay.ValidRunID(sandboxRunID) {
		return "", hostedBoxNote{}, false
	}
	n, ok := hostedBoxNoteFrom(extra)
	if !ok {
		return "", hostedBoxNote{}, false
	}
	hostedSweepBoxNotes(observerDir, time.Now())
	key := hostedBoxNoteKey(observerDir, sandboxRunID)
	_ = hostedWriteOwned(hostedBoxNotePath(observerDir, key), n)
	return key, n, true
}

// The stored class of a run whose node decides its box (demo round
// 2026-11-12): the run is recorded at launch with the row's floor
// (process) and the launch's tier, because the box does not exist yet.
// Once the node answers, the stored terminal run takes the class and tier
// the node's words map to, so analytics, Security, exports and the org wire
// read the box the node made, not the floor. The note stays the source of
// the node's own words and target.

// hostedBoxClasses and hostedBoxTiers map a note's words onto Observer's
// vocabulary: SandHold's one-to-one mapping (note 41 D4; the sandhold
// package's ClassForTier and TierForNetwork, which a test holds this to),
// kept here so a build without the SandHold driver still records a note's
// class.
var (
	hostedBoxClasses = map[string]vocab.IsoClass{"shared": vocab.ClassProcess, "shielded": vocab.ClassContainer, "sealed": vocab.ClassUserspaceKernel, "vault": vocab.ClassHWVM}
	hostedBoxTiers   = map[string]vocab.Tier{"none": vocab.TierNone, "limited": vocab.TierProxyOnly, "open": vocab.TierInternet}
)

// storeHostedBoxNote records a note's class and tier on the stored terminal
// run id of backend (only a row whose node decides its box: a local run is
// never touched, and the store only rewrites a row whose stack is exactly
// backend). changed=false: nothing to record, or already recorded.
func storeHostedBoxNote(ctx context.Context, st *store.Store, backend, id string, n hostedBoxNote) (bool, error) {
	v, ok := sandboxremote.VendorFor(backend)
	if !ok || !v.SeveralIsolations {
		return false, nil
	}
	class, ok := hostedBoxClasses[n.Isolation]
	if !ok {
		return false, nil
	}
	return st.SetTerminalRunNodeIsolation(ctx, id, backend, string(class), string(hostedBoxTiers[n.Network]), resealRunEvidence)
}

// hostedRecordBoxClass is the helper's write of a note's class onto the
// stored run, in the node database (best effort: a daemon start records
// any note this missed, backfillHostedBoxClasses).
func hostedRecordBoxClass(ctx context.Context, backend, id string, n hostedBoxNote) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	_, database, closeDB, err := loadConfigAndDB(ctx, daemonConfigPath())
	if err != nil {
		slog.Default().Warn("sandbox: the node's isolation was not recorded on the run; the next daemon start records it", "run", id, "err", err)
		return
	}
	defer closeDB()
	if _, err := storeHostedBoxNote(ctx, store.New(database), backend, id, n); err != nil {
		slog.Default().Warn("sandbox: the node's isolation was not recorded on the run; the next daemon start records it", "run", id, "err", err)
	}
}

// backfillHostedBoxClasses records every kept note's class on its stored
// run (a daemon start): runs recorded before the helper wrote the class,
// and any write the helper missed. Rows already right are left alone.
func backfillHostedBoxClasses(ctx context.Context, st *store.Store, observerDir string) (int, error) {
	entries, err := os.ReadDir(hostedBoxNoteDir(observerDir))
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	n := 0
	for _, e := range entries {
		id, ok := strings.CutSuffix(e.Name(), ".json")
		if !e.Type().IsRegular() || !ok {
			continue
		}
		note, ok := readHostedBoxNote(observerDir, id)
		if !ok {
			continue
		}
		for _, backend := range sandboxremote.VendorIDs() {
			changed, err := storeHostedBoxNote(ctx, st, backend, id, note)
			if err != nil {
				return n, err
			}
			if changed {
				n++
			}
		}
	}
	return n, nil
}

func init() {
	boxClassBackfills = append(boxClassBackfills, backfillHostedBoxClasses)
}

// readHostedBoxNote reads a run's note; ok=false when there is none or it
// does not read.
func readHostedBoxNote(observerDir, id string) (hostedBoxNote, bool) {
	if !sandboxrelay.ValidRunID(id) {
		return hostedBoxNote{}, false
	}
	b, err := hostedReadOwned(hostedBoxNotePath(observerDir, id))
	if err != nil {
		return hostedBoxNote{}, false
	}
	var n hostedBoxNote
	if json.Unmarshal(b, &n) != nil || n.V != 1 || !hostedBoxIsolations[n.Isolation] {
		return hostedBoxNote{}, false
	}
	if !hostedBoxNetworks[n.Network] {
		n.Network = ""
	}
	if !hostedBoxRunsOnOK(n.RunsOn) {
		n.RunsOn = ""
	}
	return n, true
}

// hostedSweepBoxNotes removes notes older than hostedBoxNoteKeep.
func hostedSweepBoxNotes(observerDir string, now time.Time) {
	dir := hostedBoxNoteDir(observerDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.Type().IsRegular() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		if fi, err := e.Info(); err == nil && now.Sub(fi.ModTime()) > hostedBoxNoteKeep {
			_ = os.Remove(filepath.Join(dir, e.Name())) //nolint:gosec // G703: a name listed from the daemon's own state directory
		}
	}
}

// hostedBoxWords is the box words source for a hosted row whose node decides
// the box's isolation: the note's words, or Pending while the run is open and
// the node has not answered. Every other backend reports none.
func hostedBoxWords(observerDir, runID, backend string, running bool) (dashapi.BoxWords, bool) {
	v, ok := sandboxremote.VendorFor(backend)
	if !ok || !v.SeveralIsolations {
		return dashapi.BoxWords{}, false
	}
	n, ok := readHostedBoxNote(observerDir, runID)
	if !ok {
		return dashapi.BoxWords{Pending: running}, true
	}
	return dashapi.BoxWords{Isolation: n.Isolation, Network: n.Network, RunsOn: n.RunsOn}, true
}

func init() { boxWordsSources = append(boxWordsSources, hostedBoxWords) }
