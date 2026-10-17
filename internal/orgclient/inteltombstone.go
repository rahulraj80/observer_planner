package orgclient

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/marmutapp/superbased-observer/internal/orgcontract"
)

// errIntelTombstoneNoPin: the node has not pinned the org key yet (no verified
// requirement document for this enrolment), so it cannot verify a set.
var errIntelTombstoneNoPin = errors.New("no pinned org key for this enrolment yet")

// errIntelTombstoneReplayed: a verified set whose cursor is below the floor
// this node already applied (review C156-29): an older signed set replayed.
var errIntelTombstoneReplayed = errors.New("tombstone set is older than the one already applied")

// applyIntelTombstones verifies the signed purge-tombstone set the result rail
// carried (parity follow-up ruling 1, the D-192 withdrawal precedent) and
// clears every cached result it names. The set must verify against the org key
// this node pinned from the signed requirement document, its own org and its
// own enrolment subject; any refusal clears NOTHING and returns the reason (for
// logging; the rail stays fail-open). Returns how many cached rows it removed.
// An unchanged set that was already applied in full is skipped.
func (c *Client) applyIntelTombstones(ctx context.Context, orgID, subject string, doc *orgcontract.IntelTombstoneDoc) (int, error) {
	if doc == nil {
		return 0, nil
	}
	if doc.BodyHash != "" && doc.BodyHash == c.intelTombstoneApplied {
		return 0, nil
	}
	req, ok, err := c.store.GetOrgIntelRequirement(ctx)
	if err != nil {
		return 0, fmt.Errorf("orgclient.applyIntelTombstones: pinned key: %w", err)
	}
	if !ok || req.OrgID != orgID || req.ServerPubkey == "" {
		return 0, fmt.Errorf("orgclient.applyIntelTombstones: %w", errIntelTombstoneNoPin)
	}
	body, err := orgcontract.VerifyIntelTombstones(*doc, orgID, subject, req.ServerPubkey)
	if err != nil {
		return 0, fmt.Errorf("orgclient.applyIntelTombstones: %w", err)
	}
	floor, err := c.store.OrgIntelTombstoneFloor(ctx, orgID, subject)
	if err != nil {
		return 0, fmt.Errorf("orgclient.applyIntelTombstones: floor: %w", err)
	}
	if orgcontract.IntelTombstoneCursorOlder(body.Cursor, floor) {
		return 0, fmt.Errorf("orgclient.applyIntelTombstones: %w (set %q, floor %q)", errIntelTombstoneReplayed, body.Cursor, floor)
	}
	cleared := 0
	for _, e := range body.Entries {
		through, _ := strconv.ParseInt(e.JobID, 10, 64) // VerifyIntelTombstones checked it
		n, derr := c.store.DeleteOrgIntelResultsThrough(ctx, orgID, e.SessionID, through)
		if derr != nil {
			// Not marked applied: the next cycle retries the whole set.
			return cleared, fmt.Errorf("orgclient.applyIntelTombstones: %w", derr)
		}
		cleared += n
	}
	// The floor moves only after every entry cleared, so a failure above
	// retries the whole set. An unchanged cursor that cleared nothing new is
	// not recorded again (an unsettled tail is re-served on every page).
	if body.Cursor != floor || cleared > 0 {
		if rerr := c.store.RecordOrgIntelTombstonesApplied(ctx, orgID, subject, body.Cursor, cleared, time.Now().UTC()); rerr != nil {
			return cleared, fmt.Errorf("orgclient.applyIntelTombstones: floor: %w", rerr)
		}
	}
	c.intelTombstoneApplied = doc.BodyHash
	return cleared, nil
}
