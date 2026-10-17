package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/marmutapp/superbased-observer/internal/dataauthority"
)

// orgIntelTitlesChunk bounds the IN-list of one LoadOrgIntelTitles query.
const orgIntelTitlesChunk = 400

// LoadOrgIntelTitles returns the newest cached org-intel title per session for
// the given ids: the Sessions list's third title source, after the
// developer's own title and the personal-plane AI title (Cloud Intelligence
// parity phase 3). The same org filter as OrgIntelResultsForSession applies;
// a session with no cached result, or an empty title, is absent from the map.
func (s *Store) LoadOrgIntelTitles(ctx context.Context, sessionIDs []string) (map[string]string, error) {
	titles := make(map[string]string, len(sessionIDs))
	for start := 0; start < len(sessionIDs); start += orgIntelTitlesChunk {
		ids := sessionIDs[start:min(start+orgIntelTitlesChunk, len(sessionIDs))]
		args := make([]any, 0, len(ids))
		for _, id := range ids {
			args = append(args, id)
		}
		placeholders := strings.TrimRight(strings.Repeat("?,", len(args)), ",")
		//nolint:gosec // G202: only the ?-placeholder list is concatenated; every value is bound.
		rows, err := s.db.QueryContext(ctx, `
			SELECT session_id, title FROM org_intel_cache
			 WHERE session_id IN (`+placeholders+`) AND title <> ''
			   AND org_id = COALESCE((SELECT org_id FROM org_enrolment WHERE id = 1), org_id)
			 ORDER BY fetched_at ASC, job_id ASC`, args...)
		if err != nil {
			return nil, fmt.Errorf("store.LoadOrgIntelTitles: %w", err)
		}
		for rows.Next() {
			var sid, title string
			if err := rows.Scan(&sid, &title); err != nil {
				_ = rows.Close()
				return nil, fmt.Errorf("store.LoadOrgIntelTitles: scan: %w", err)
			}
			// Ascending order: the newest pull is read last and wins.
			titles[sid] = title
		}
		err = rows.Err()
		_ = rows.Close()
		if err != nil {
			return nil, fmt.Errorf("store.LoadOrgIntelTitles: rows: %w", err)
		}
	}
	return titles, nil
}

// LoadOrgAuthoritySessions returns which of the given sessions carry the ORG
// data-authority stamp (captured while enrolled): the Sessions list's
// plane-aware Enrich button (Cloud Intelligence parity ruling Q3) sends those
// to the org plane and every other session to the personal plane. An unknown
// (NULL) stamp is not org.
func (s *Store) LoadOrgAuthoritySessions(ctx context.Context, sessionIDs []string) (map[string]bool, error) {
	out := make(map[string]bool, len(sessionIDs))
	for start := 0; start < len(sessionIDs); start += orgIntelTitlesChunk {
		ids := sessionIDs[start:min(start+orgIntelTitlesChunk, len(sessionIDs))]
		args := make([]any, 0, len(ids)+1)
		args = append(args, string(dataauthority.AuthorityOrg))
		for _, id := range ids {
			args = append(args, id)
		}
		placeholders := strings.TrimRight(strings.Repeat("?,", len(ids)), ",")
		//nolint:gosec // G202: only the ?-placeholder list is concatenated; every value is bound.
		rows, err := s.db.QueryContext(ctx, `SELECT id FROM sessions WHERE authority = ? AND id IN (`+placeholders+`)`, args...)
		if err != nil {
			return nil, fmt.Errorf("store.LoadOrgAuthoritySessions: %w", err)
		}
		for rows.Next() {
			var sid string
			if err := rows.Scan(&sid); err != nil {
				_ = rows.Close()
				return nil, fmt.Errorf("store.LoadOrgAuthoritySessions: scan: %w", err)
			}
			out[sid] = true
		}
		err = rows.Err()
		_ = rows.Close()
		if err != nil {
			return nil, fmt.Errorf("store.LoadOrgAuthoritySessions: rows: %w", err)
		}
	}
	return out, nil
}
