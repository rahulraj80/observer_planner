package telemetry

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/marmutapp/superbased-observer/internal/telemetry/contract"
)

// Bounds for the staging table (plan §D2: bounded local staging,
// drop-oldest with an honest dropped counter). MaxStagedEvents is the hard
// row ceiling; StagedMaxAge is the age ceiling. Both are enforced on every
// insert by enforceBounds, and the number of events evicted by the bounds
// is added to telemetry_state.dropped_count so the gap ships in batch_meta.
const (
	MaxStagedEvents = 10000
	StagedMaxAge    = 72 * time.Hour
)

// Collector derives and stages telemetry events from the node's own DB and
// drains them in bounded batches. Construct with NewCollector; the zero
// value is not usable.
type Collector struct {
	db *sql.DB
	// cfg is the resolved [telemetry] config snapshot.
	cfg CollectorConfig
	// now is the clock seam (tests).
	now func() time.Time
	// newUUID mints batch UUIDs (tests stub it for determinism).
	newUUID func() string
}

// CollectorConfig is the collector's resolved configuration — the validated
// subset of config.TelemetryConfig the daemon passes in.
type CollectorConfig struct {
	// Tier is the effective consent tier (already normalized via
	// config.TelemetryConfig.EffectiveTier).
	Tier string
	// ClientVersion is the running observer version (envelope field).
	ClientVersion string
	// BatchInterval is the target cadence; the collector jitters ±10%
	// around it and fires early when staged bytes reach MaxBatchBytes.
	BatchInterval time.Duration
	// MaxBatchBytes is the compressed-payload cap (plan §D3: ~256 KB).
	MaxBatchBytes int
	// Nudge is the segment classifier's signature (Step 5.6). The zero value
	// means DefaultNudgeRules; the daemon passes the defaults merged with the
	// local rules file (LoadNudgeRules).
	Nudge NudgeRules
}

// NewCollector opens a collector over the node's local DB handle. It never
// mutates schema — migration 115 owns telemetry_staged/telemetry_state.
func NewCollector(database *sql.DB, cfg CollectorConfig) *Collector {
	return &Collector{
		db:      database,
		cfg:     cfg,
		now:     time.Now,
		newUUID: randomUUID,
	}
}

// SetNow overrides the clock (tests).
func (c *Collector) SetNow(now func() time.Time) { c.now = now }

// Tier reports the collector's CURRENT consent tier. It diverges from the
// configured tier when the loop applies the server's consent ceiling as a
// live downgrade (SetTier); derive gating reads it on every pass.
func (c *Collector) Tier() string { return c.cfg.Tier }

// SetTier applies a runtime tier change. Used by the loop for the server's
// consent ceiling (max_tier is the HIGHEST admitted tier — plan §F3); the
// same semantics as the CLI downgrade path apply, so the caller purges
// staged rows the new tier may not emit (PurgeAboveTier).
func (c *Collector) SetTier(tier string) { c.cfg.Tier = tier }

// StagedCount reports how many events currently wait in the staging table
// (including already-acked rows awaiting the bounds' 72h expiry — the send
// watermark, not deletion, decides what ships).
func (c *Collector) StagedCount(ctx context.Context) (int, error) {
	var n int
	if err := c.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM telemetry_staged`).Scan(&n); err != nil {
		return 0, fmt.Errorf("telemetry.StagedCount: %w", err)
	}
	return n, nil
}

// SetUUIDFunc overrides the batch-UUID source (tests).
func (c *Collector) SetUUIDFunc(f func() string) { c.newUUID = f }

// randomUUID mints a random (never hardware-derived) RFC-4122 v4 UUID —
// plan §D2: stronger than NIC hashing and rotation-capable.
func randomUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand failing is a system-level fault; fall back to a
		// time-derived value rather than panicking (library code never
		// panics). The server rejects a malformed UUID, the batch is
		// dropped, and the next cycle retries.
		return fmt.Sprintf("%032x", time.Now().UnixNano())
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return hex.EncodeToString(b[0:4]) + "-" + hex.EncodeToString(b[4:6]) + "-" +
		hex.EncodeToString(b[6:8]) + "-" + hex.EncodeToString(b[8:10]) + "-" +
		hex.EncodeToString(b[10:16])
}

// InstallID returns the install identity, minting a random one on first
// use (plan §D2). Rotating it orphans history server-side — the documented
// trade-off behind `observer telemetry reset-id`.
func (c *Collector) InstallID(ctx context.Context) (string, error) {
	var iid sql.NullString
	err := c.db.QueryRowContext(ctx, `SELECT install_id FROM telemetry_state WHERE id = 1`).Scan(&iid)
	if err != nil {
		return "", fmt.Errorf("telemetry.InstallID: %w", err)
	}
	if iid.Valid && iid.String != "" {
		return iid.String, nil
	}
	// inst- + 12 hex chars (contract installRe).
	var raw [6]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("telemetry.InstallID: mint: %w", err)
	}
	fresh := "inst-" + hex.EncodeToString(raw[:])
	if _, err := c.db.ExecContext(ctx,
		`UPDATE telemetry_state SET install_id = ? WHERE id = 1 AND install_id IS NULL`, fresh); err != nil {
		return "", fmt.Errorf("telemetry.InstallID: store: %w", err)
	}
	// Re-read: a concurrent minter may have won; both readers then agree
	// on ONE identity.
	var stored sql.NullString
	if err := c.db.QueryRowContext(ctx,
		`SELECT install_id FROM telemetry_state WHERE id = 1`).Scan(&stored); err != nil {
		return "", fmt.Errorf("telemetry.InstallID: reread: %w", err)
	}
	if !stored.Valid || stored.String == "" {
		return "", fmt.Errorf("telemetry.InstallID: mint failed")
	}
	return stored.String, nil
}

// ResetInstallID mints a fresh random identity (the plan's "reset id"
// button). Staged rows keep the OLD install_id and are purged — the new id
// must never mix with the old identity's rows. Server-side the old
// identity's history is orphaned (documented trade-off, plan §D2).
func (c *Collector) ResetInstallID(ctx context.Context) (string, error) {
	if _, err := c.db.ExecContext(ctx, `DELETE FROM telemetry_staged`); err != nil {
		return "", fmt.Errorf("telemetry.ResetInstallID: purge staged: %w", err)
	}
	var raw [6]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("telemetry.ResetInstallID: mint: %w", err)
	}
	fresh := "inst-" + hex.EncodeToString(raw[:])
	if _, err := c.db.ExecContext(ctx,
		// The ack watermark belongs to the old identity: a new id restarts
		// its seq at 1, so a kept last_ack_seq would hide every new row from
		// BuildEnvelope until the seq passed the old watermark.
		`UPDATE telemetry_state SET install_id = ?, next_seq = 1, last_ack_seq = 0 WHERE id = 1`, fresh); err != nil {
		return "", fmt.Errorf("telemetry.ResetInstallID: store: %w", err)
	}
	return fresh, nil
}

// AdoptInstallID makes iid this node's install identity — the cloud telemetry
// lane derives it from the device key, so the server, not a local random draw,
// decides it. Staged rows are re-keyed to the new id (they are content-free
// and were never sent under the old one); the seq counter and ack watermark
// carry on. A no-op when iid already matches.
func (c *Collector) AdoptInstallID(ctx context.Context, iid string) error {
	// Read without minting: on the cloud lane a fresh local random id would
	// only be replaced here, so a NULL id is adopted directly.
	var stored sql.NullString
	if err := c.db.QueryRowContext(ctx, `SELECT install_id FROM telemetry_state WHERE id = 1`).Scan(&stored); err != nil {
		return fmt.Errorf("telemetry.AdoptInstallID: read: %w", err)
	}
	cur := stored.String
	if cur == iid {
		return nil
	}
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("telemetry.AdoptInstallID: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `UPDATE telemetry_staged SET install_id = ? WHERE install_id = ?`, iid, cur); err != nil {
		return fmt.Errorf("telemetry.AdoptInstallID: re-key staged: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE telemetry_state SET install_id = ? WHERE id = 1`, iid); err != nil {
		return fmt.Errorf("telemetry.AdoptInstallID: store: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("telemetry.AdoptInstallID: commit: %w", err)
	}
	return nil
}

// enforceBounds keeps the staging table bounded: hard row ceiling and age
// ceiling, drop-oldest, honest dropped counter (plan §D2). Runs inside the
// caller's transaction.
func (c *Collector) enforceBounds(ctx context.Context, tx sqlTx) error {
	res, err := tx.ExecContext(ctx,
		`DELETE FROM telemetry_staged WHERE id IN (
			SELECT id FROM telemetry_staged ORDER BY id DESC LIMIT -1 OFFSET ?
		)`, MaxStagedEvents)
	if err != nil {
		return fmt.Errorf("telemetry.enforceBounds: row ceiling: %w", err)
	}
	if n, _ := res.RowsAffected(); n > 0 {
		if _, err := tx.ExecContext(ctx,
			`UPDATE telemetry_state SET dropped_count = dropped_count + ? WHERE id = 1`, n); err != nil {
			return fmt.Errorf("telemetry.enforceBounds: count: %w", err)
		}
	}
	cutoff := c.now().UTC().Add(-StagedMaxAge).Format(time.RFC3339)
	res, err = tx.ExecContext(ctx, `DELETE FROM telemetry_staged WHERE created_at < ?`, cutoff)
	if err != nil {
		return fmt.Errorf("telemetry.enforceBounds: age ceiling: %w", err)
	}
	if n, _ := res.RowsAffected(); n > 0 {
		if _, err := tx.ExecContext(ctx,
			`UPDATE telemetry_state SET dropped_count = dropped_count + ? WHERE id = 1`, n); err != nil {
			return fmt.Errorf("telemetry.enforceBounds: count age: %w", err)
		}
	}
	return nil
}

// DropThrough deletes the staged rows in (lastAck, through] for the
// collector's install id — the permanent-rejection path (plan §D3/A3:
// bad_schema / unknown_version batches are dropped, never retried).
// Dropped rows count into telemetry_state.dropped_count so the gap ships
// in batch_meta. Rows at or below lastAck are already acked (never
// re-sent); rows above `through` stay staged for the next cycle.
func (c *Collector) DropThrough(ctx context.Context, lastAck, through int64) (int64, error) {
	iid, err := c.InstallID(ctx)
	if err != nil {
		return 0, err
	}
	res, err := c.db.ExecContext(ctx,
		`DELETE FROM telemetry_staged
		 WHERE install_id = ? AND seq > ? AND seq <= ?`, iid, lastAck, through)
	if err != nil {
		return 0, fmt.Errorf("telemetry.DropThrough: %w", err)
	}
	n, _ := res.RowsAffected()
	if n > 0 {
		if _, err := c.db.ExecContext(ctx,
			`UPDATE telemetry_state SET dropped_count = dropped_count + ? WHERE id = 1`, n); err != nil {
			return n, fmt.Errorf("telemetry.DropThrough: count: %w", err)
		}
	}
	return n, nil
}

// PurgeAboveTier removes staged rows the given tier may no longer emit —
// the tier-downgrade path (plan §D2: "staged deep events are purged on
// downgrade"). deep rows go unless the tier is deep; everything else stays
// for usage/error tiers that still admit it.
func (c *Collector) PurgeAboveTier(ctx context.Context, tier string) (int64, error) {
	rows, err := c.db.QueryContext(ctx, `SELECT seq, event FROM telemetry_staged`)
	if err != nil {
		return 0, fmt.Errorf("telemetry.PurgeAboveTier: list: %w", err)
	}
	defer rows.Close()
	var doomed []int64
	for rows.Next() {
		var seq int64
		var event string
		if err := rows.Scan(&seq, &event); err != nil {
			return 0, fmt.Errorf("telemetry.PurgeAboveTier: scan: %w", err)
		}
		if !contract.TierAllows(tier, event) {
			doomed = append(doomed, seq)
		}
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("telemetry.PurgeAboveTier: rows: %w", err)
	}
	var purged int64
	for _, seq := range doomed {
		res, err := c.db.ExecContext(ctx, `DELETE FROM telemetry_staged WHERE seq = ?`, seq)
		if err != nil {
			return purged, fmt.Errorf("telemetry.PurgeAboveTier: delete: %w", err)
		}
		n, _ := res.RowsAffected()
		purged += n
	}
	return purged, nil
}
