package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// briefing.go is the briefing_snapshots persistence seam (docs/design/
// standup-briefing.md §2.5, Phase A). The FACTS assembly lives in the
// dashboard package (it needs the cost engine's pricing for the per-turn
// scan); this file owns only the snapshot table's write/read.
//
// Node-local: the snapshot table is never selected by
// SelectUnpushedSince (see the migration's privacy note).

// SaveBriefingSnapshot upserts the composed briefing JSON for a local day.
// Idempotent: re-composing the same day overwrites, newest generated_at wins
// — the audit value is "the briefing as last composed", not every render.
func (s *Store) SaveBriefingSnapshot(ctx context.Context, date, briefingJSON string, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO briefing_snapshots (date, json, generated_at) VALUES (?, ?, ?)
ON CONFLICT(date) DO UPDATE SET json = excluded.json, generated_at = excluded.generated_at`,
		date, briefingJSON, now.UTC().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("store.SaveBriefingSnapshot: %w", err)
	}
	return nil
}

// LoadBriefingSnapshot returns the last-composed briefing JSON for a local
// day and whether a snapshot exists.
func (s *Store) LoadBriefingSnapshot(ctx context.Context, date string) (string, bool, error) {
	var b string
	err := s.db.QueryRowContext(ctx, `SELECT json FROM briefing_snapshots WHERE date = ?`, date).Scan(&b)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("store.LoadBriefingSnapshot: %w", err)
	}
	return b, true, nil
}

// LatestBriefingDate returns the most-recent local day that has a snapshot,
// or ("", false) when none exists.
func (s *Store) LatestBriefingDate(ctx context.Context) (string, bool, error) {
	var d string
	err := s.db.QueryRowContext(ctx, `SELECT date FROM briefing_snapshots ORDER BY date DESC LIMIT 1`).Scan(&d)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("store.LatestBriefingDate: %w", err)
	}
	return d, true, nil
}
