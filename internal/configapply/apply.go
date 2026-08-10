// Package configapply owns the briefing CTA apply mechanics: one
// evidence-backed write into the observer's OWN config.toml (the
// [routing].policy lever and future levers), with a stamped pre-write
// snapshot and a JSONL undo ledger so every applied change can be
// rolled back exactly. It is the config-apply analogue of
// internal/routingapply (Channel A), which edits per-AI-tool agent
// files; this package edits observer config instead.
//
// Layering: pure file mechanics + persistence. It imports neither
// internal/briefing nor internal/config — the dashboard composes
// evidence → Lever → Change and hands the resulting write here, so the
// two sides cannot drift on the actual write mechanics.
package configapply

import (
	"errors"
	"fmt"
	"os"
	"time"
)

// Change is one planned or executed config write: set Key within
// Section to To (From is the value it is currently).
type Change struct {
	Section         string `json:"section"`
	Key             string `json:"key"`
	From            string `json:"from"`
	To              string `json:"to"`
	RestartRequired bool   `json:"restart_required"`
	// ChangeID is a stable discriminator for multi-lever plans
	// (e.g. "mode", "policy", "stickiness"). Enables the apply
	// endpoint to select which lever to actuate.
	ChangeID string `json:"change_id,omitempty"`
	// AlreadyActive is set by the plan builder when the current
	// config already equals the target value — the apply is a no-op.
	AlreadyActive bool `json:"already_active,omitempty"`
}

// Lever describes which config knob realizes an opportunity detector's
// savings. Zero Section means the detector has NO config lever — its
// savings are behavioral (guidance only), so the CTA refuses to plan
// an apply for it.
type Lever struct {
	Section         string
	Key             string
	RestartRequired bool
	// ChangeID is the stable ID for this lever (mirrored into Change).
	ChangeID string
}

// LeverForDetector returns the PRIMARY lever for a detector (backward
// compat). For multi-lever detectors, use LeversForDetector.
func LeverForDetector(detector string) Lever {
	levers := LeversForDetector(detector)
	if len(levers) > 0 {
		return levers[0]
	}
	return Lever{}
}

// LeversForDetector returns ALL config levers that can realize a
// detector's savings. routing_shadow has three: mode (advise→enforce),
// policy (swap template), and stickiness (lower the coherence floor —
// gated by hold evidence in the CTA plan builder).
func LeversForDetector(detector string) []Lever {
	if detector == "routing_shadow" {
		return []Lever{
			{Section: "routing", Key: "mode", RestartRequired: true, ChangeID: "mode"},
			{Section: "routing", Key: "policy", RestartRequired: true, ChangeID: "policy"},
			{Section: "routing", Key: "stickiness.min_turns_between_switches", RestartRequired: true, ChangeID: "stickiness"},
		}
	}
	return nil
}

// BackupPrefix tags the stamped pre-write snapshots the CTA surface
// takes before every config write. config.WriteToml already writes a
// single-level .bak (one undo step, overwritten each save); multi-step
// undo needs stamped snapshots that are never overwritten, so a revert
// can restore ANY prior state, not just the last one.
const BackupPrefix = ".bak-observer-cta-"

// Stamp formats t as a collision-free snapshot stamp, mirroring the
// routingapply stamp shape.
func Stamp(t time.Time) string { return t.UTC().Format("20060102T150405Z") }

// BackupFile snapshots path to path+BackupPrefix+stamp and returns the
// snapshot path. It must run BEFORE the config write: the snapshot is
// the exact pre-write state a revert restores. The file itself is the
// snapshot source (not the struct), so hand-edited comments are
// preserved byte-for-byte on rollback.
func BackupFile(path, stamp string) (string, error) {
	existing, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("configapply: read current config for backup: %w", err)
	}
	backup := path + BackupPrefix + stamp
	//nolint:gosec // G306: a backup of the non-secret config.toml, mirroring config.writeBytesAtomic's 0644 perms.
	if err := os.WriteFile(backup, existing, 0o644); err != nil {
		return "", fmt.Errorf("configapply: write backup: %w", err)
	}
	return backup, nil
}

// RestoreFile restores path from backupPath — the path recorded in the
// ledger at apply time. Restoring the ledger-recorded snapshot (rather
// than recomputing one) is what makes the revert exact even after later
// writes accumulated more snapshots.
func RestoreFile(path, backupPath string) error {
	data, err := os.ReadFile(backupPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("configapply: backup %s missing: %w", backupPath, err)
		}
		return fmt.Errorf("configapply: read backup: %w", err)
	}
	//nolint:gosec // G306: restoring the non-secret config.toml from its own snapshot.
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("configapply: restore %s: %w", path, err)
	}
	return nil
}
