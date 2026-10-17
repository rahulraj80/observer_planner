package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// OrgIntelReceiptTombstonesApplied records one verified purge-tombstone set
// applied to the node's result cache. Its detail carries the set's cursor for
// the enrolment subject, so the newest such receipt is the node's DURABLE
// tombstone floor (review C156-29): a verified set whose cursor is below it is
// an older set replayed and is refused, across daemon restarts.
const OrgIntelReceiptTombstonesApplied = "tombstones_applied"

// orgIntelTombstoneFloorDetail is the receipt detail of a tombstones_applied
// row.
type orgIntelTombstoneFloorDetail struct {
	Subject string `json:"subject"`
	Cursor  string `json:"cursor"`
}

// OrgIntelTombstoneFloor returns the cursor of the newest tombstone set this
// node applied for (orgID, subject), or "" when it never applied one.
func (s *Store) OrgIntelTombstoneFloor(ctx context.Context, orgID, subject string) (string, error) {
	var detail string
	err := s.db.QueryRowContext(ctx, `
SELECT detail FROM org_intel_receipts
 WHERE kind = ? AND org_id = ? AND json_valid(detail) AND json_extract(detail, '$.subject') = ?
 ORDER BY id DESC LIMIT 1`, OrgIntelReceiptTombstonesApplied, orgID, subject).Scan(&detail)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("store.OrgIntelTombstoneFloor: %w", err)
	}
	var d orgIntelTombstoneFloorDetail
	if err := json.Unmarshal([]byte(detail), &d); err != nil {
		return "", fmt.Errorf("store.OrgIntelTombstoneFloor: %w", err)
	}
	return d.Cursor, nil
}

// RecordOrgIntelTombstonesApplied appends the tombstones_applied receipt that
// moves the (orgID, subject) floor to cursor; cleared is how many cached
// results the set removed.
func (s *Store) RecordOrgIntelTombstonesApplied(ctx context.Context, orgID, subject, cursor string, cleared int, now time.Time) error {
	raw, err := json.Marshal(orgIntelTombstoneFloorDetail{Subject: subject, Cursor: cursor})
	if err != nil {
		return fmt.Errorf("store.RecordOrgIntelTombstonesApplied: %w", err)
	}
	return s.AppendOrgIntelReceipt(ctx, OrgIntelReceipt{
		Kind: OrgIntelReceiptTombstonesApplied, OrgID: orgID, Count: cleared, Detail: string(raw), CreatedAt: now,
	})
}
