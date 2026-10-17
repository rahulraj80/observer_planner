package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/marmutapp/superbased-observer/internal/orgcontract"
)

// componentposture.go is the org-wire seam for the node's component posture
// (modular packaging plan 6.4, T-682). There is no component state table on
// the node: the posture is resolved live by cmd/observer (the one component
// resolver, governed) and handed to this file through a func seam, so
// orgpush.go names nothing it is derived from. The one table it reads is the
// purge plan table, through componentpurge.go's ScheduledPurgePosture (T-711),
// reduced to enums. The row is enum-only and is normalized here before it
// can reach the wire.

// componentPostureResendEvery bounds how long an unchanged posture is
// suppressed: it is re-sent at least this often, so a re-enrolment to
// another org (the digest is in-process and identity-blind) or a server that
// lost the row converges without waiting for a component change.
const componentPostureResendEvery = 6 * time.Hour

// ComponentPostureProvider returns this node's current component posture.
// ok=false means "nothing to report", and no row ships.
type ComponentPostureProvider func() (orgcontract.ComponentPostureRow, bool)

// SetComponentPostureProvider wires the posture seam. nil restores "no
// posture reported" (a byte-identical envelope).
func (s *Store) SetComponentPostureProvider(p ComponentPostureProvider) {
	s.componentPostureMu.Lock()
	defer s.componentPostureMu.Unlock()
	s.componentPosture = p
}

// composeComponentPosture resolves the posture for one push envelope and
// reports whether it differs from the last one the server accepted.
func (s *Store) composeComponentPosture(ctx context.Context) (*orgcontract.ComponentPostureRow, bool) {
	s.componentPostureMu.Lock()
	p := s.componentPosture
	s.componentPostureMu.Unlock()
	if p == nil {
		return nil, false
	}
	row, ok := p()
	if !ok {
		return nil, false
	}
	// The scheduled purges (T-711) come from this store's own plan table
	// (componentpurge.go, its one owner). A store without a database or a read
	// failure leaves the field nil, which the server reads as "not reported"
	// and keeps its stored list.
	row.ScheduledPurges = nil
	if s.db != nil {
		if purges, err := s.ScheduledPurgePosture(ctx, time.Now()); err == nil {
			row.ScheduledPurges = &purges
		}
	}
	row = orgcontract.NormalizeComponentPosture(row, nil)
	key, err := json.Marshal(row)
	if err != nil {
		return nil, false
	}
	s.componentPostureMu.Lock()
	defer s.componentPostureMu.Unlock()
	s.componentPostureDigestPending = string(key)
	stale := s.componentPostureCommittedAt.IsZero() || time.Since(s.componentPostureCommittedAt) >= componentPostureResendEvery
	return &row, stale || string(key) != s.componentPostureDigest
}

// commitComponentPostureDigest promotes the pending posture to delivered.
func (s *Store) commitComponentPostureDigest() {
	s.componentPostureMu.Lock()
	defer s.componentPostureMu.Unlock()
	if s.componentPostureDigestPending != "" {
		s.componentPostureDigest = s.componentPostureDigestPending
		s.componentPostureCommittedAt = time.Now()
	}
}
