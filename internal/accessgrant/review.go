package accessgrant

import "time"

// PerpetualReviewEvery is D-080's re-review period: a grant with no expiry is
// flagged for a super-admin to re-confirm (extend it with no expiry, which
// re-issues it and restarts the clock) or revoke, every 90 days (review
// C156-30).
const PerpetualReviewEvery = 90 * 24 * time.Hour

// ReviewDue returns when a grant with no expiry falls due for its D-080
// re-review (its issue time plus PerpetualReviewEvery) and whether that is
// now. A grant with an expiry ends on its own and is never due (0, false).
func ReviewDue(issuedAtNs, expiresAtNs int64, now time.Time) (int64, bool) {
	if expiresAtNs != 0 || issuedAtNs <= 0 {
		return 0, false
	}
	due := issuedAtNs + int64(PerpetualReviewEvery)
	return due, now.UnixNano() >= due
}
