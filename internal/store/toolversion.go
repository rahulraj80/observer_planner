package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/marmutapp/superbased-observer/internal/models"
	"github.com/marmutapp/superbased-observer/internal/orgcontract"
)

// maxToolVersionRunes bounds a captured tool/CLI version string. A real
// version-shaped token ("0.150.0", "3.17.4-beta.1", "v0.130.0",
// "0.45.0+build.7") is well under this; anything longer is a mis-decoded
// free-text field, not a version, and is rejected rather than persisted.
// Re-exported here (rather than referenced as orgcontract.MaxToolVersionRunes
// at every call site) purely so this file's own doc comments and the
// existing test file keep their short, unqualified name; the VALUE is
// orgcontract's, not a second one.
const maxToolVersionRunes = orgcontract.MaxToolVersionRunes

// validToolVersion reports whether v is a plausible bounded, ASCII,
// version-shaped token store.SetSessionToolVersion will persist
// (TOOLVERSION-1, docs/security.md). It delegates to
// orgcontract.ValidToolVersion — the single grammar shared with this
// column's other boundary, internal/orgserver/ingest.go's re-validation of
// a pushed row, so a value that passes here is guaranteed to also pass
// there. See that function's doc comment for the exact grammar (an
// optional leading "v"/"V", then a digit, then up to 62 more
// alphanumeric/"."/"_"/"+"/"-" characters, AND at least one "." anywhere)
// and the shapes it excludes: a URL, an email address, a path-traversal
// segment need a character outside the charset (space, "/", ":", "@");
// a compact secret such as an AWS access key id or a GitHub PAT is
// alphanumeric noise with no leading-digit-then-dot structure, which a real
// vendor-stamped version always has (MAJOR.MINOR[.PATCH] is the one
// structural feature every version scheme shares). A rejected value is
// skipped, never stored, so the honest "unknown" empty is preserved.
func validToolVersion(v string) bool {
	return orgcontract.ValidToolVersion(v)
}

// SetSessionToolVersion stamps the captured tool/CLI version onto an
// existing session row. It is the ONE write path for BOTH version columns:
// sessions.tool_version (migration 125) and sessions.tool_version_last
// (migration 162). UpsertSession never touches either, so a re-parse that
// carries no version can never clear a captured value.
//
// SEMANTICS.
//
//   - tool_version is FIRST-WINS-UNLESS-EMPTY, mirroring SetSessionSurface:
//     the first grounded stamp sticks and a later parse can only FILL a
//     still-empty column. It is the build the session STARTED on.
//   - tool_version_last moves FORWARD on every valid stamp: it becomes the
//     incoming version when that version orders after the stored one
//     (compareToolVersions), and otherwise keeps what it holds. Its base is
//     tool_version while it is still empty (a row stamped before migration
//     162), so a valid stamp always leaves it set. "Orders after" rather
//     than "arrived later" keeps a whole-file re-parse, which re-stamps an
//     older version first, from rewinding a recorded upgrade; tool builds
//     move forward in practice, so the highest version seen is the latest.
//
// A session whose two columns differ was upgraded mid-session.
//
// The version is NOT an enum but IS a bounded token: validToolVersion
// length-caps it and rejects whitespace / control / prose. A value that
// fails validation is SKIPPED (returns false, nil) rather than persisted:
// a stray free-text field must never land in either column, but it must
// also never fail an ingest whose actions/tokens already landed.
//
// The returned bool honestly reports whether either column changed. An
// identical re-stamp, an older stamp, a rejected value, and an empty stamp
// return false. A missing session id is a silent no-op: the next parse
// re-stamps. The write is a compare-and-set on the values just read,
// retried a few times, so a concurrent stamp is never lost or moved back.
func (s *Store) SetSessionToolVersion(ctx context.Context, tv models.SessionToolVersion) (bool, error) {
	if tv.SessionID == "" {
		return false, errors.New("store.SetSessionToolVersion: SessionID is required")
	}
	if !validToolVersion(tv.Version) {
		// Empty or malformed: skip, not fail. The honest "unknown" empty
		// is preserved and the actions/tokens already written are kept.
		return false, nil
	}
	const attempts = 4
	for i := 0; i < attempts; i++ {
		var first, last string
		err := s.db.QueryRowContext(ctx,
			`SELECT COALESCE(tool_version, ''), COALESCE(tool_version_last, '')
			   FROM sessions WHERE id = ?`,
			tv.SessionID,
		).Scan(&first, &last)
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		if err != nil {
			return false, fmt.Errorf("store.SetSessionToolVersion: read: %w", err)
		}
		newFirst, newLast := nextToolVersions(first, last, tv.Version)
		if newFirst == first && newLast == last {
			return false, nil
		}
		res, err := s.db.ExecContext(ctx,
			`UPDATE sessions SET tool_version = ?, tool_version_last = ?
			 WHERE id = ?
			   AND COALESCE(tool_version, '') = ?
			   AND COALESCE(tool_version_last, '') = ?`,
			newFirst, newLast, tv.SessionID, first, last,
		)
		if err != nil {
			return false, fmt.Errorf("store.SetSessionToolVersion: %w", err)
		}
		if n, _ := res.RowsAffected(); n > 0 {
			return true, nil
		}
		// A concurrent stamp moved the row between the read and the write:
		// re-read and decide again.
	}
	return false, nil
}

// nextToolVersions is the pure first/last decision SetSessionToolVersion
// applies to one valid incoming version. first is first-wins-unless-empty;
// last becomes incoming when incoming orders after the current last (or
// after first, while last is still empty), and otherwise stays.
func nextToolVersions(first, last, incoming string) (newFirst, newLast string) {
	newFirst = first
	if newFirst == "" {
		newFirst = incoming
	}
	base := last
	if base == "" {
		base = first
	}
	newLast = base
	if base == "" || compareToolVersions(incoming, base) > 0 {
		newLast = incoming
	}
	return newFirst, newLast
}

// compareToolVersions orders two version tokens that already passed
// validToolVersion: -1 when a sorts before b, 0 when equal, 1 after. An
// optional leading "v"/"V" is ignored. The core (everything before the
// first "-" or "+") is split on "." and "_" and compared segment by
// segment: numerically when both segments are all digits, else as strings,
// with a missing segment ordering first. On an equal core a release orders
// after a pre-release ("1.2.3" after "1.2.3-rc.1"), and two suffixes
// compare as strings. Not full SemVer precedence, but total and
// deterministic, which is all the "latest version seen" column needs.
func compareToolVersions(a, b string) int {
	ca, sa := splitToolVersion(a)
	cb, sb := splitToolVersion(b)
	pa := strings.FieldsFunc(ca, isToolVersionSep)
	pb := strings.FieldsFunc(cb, isToolVersionSep)
	for i := 0; i < len(pa) || i < len(pb); i++ {
		if i >= len(pa) {
			return -1
		}
		if i >= len(pb) {
			return 1
		}
		if c := compareToolVersionSegment(pa[i], pb[i]); c != 0 {
			return c
		}
	}
	switch {
	case sa == sb:
		return 0
	case sa == "":
		return 1
	case sb == "":
		return -1
	default:
		return strings.Compare(sa, sb)
	}
}

// splitToolVersion strips a leading "v"/"V" and splits a version token into
// its core and its pre-release/build suffix (from the first "-" or "+").
func splitToolVersion(v string) (core, suffix string) {
	v = strings.TrimPrefix(strings.TrimPrefix(v, "v"), "V")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		return v[:i], v[i:]
	}
	return v, ""
}

func isToolVersionSep(r rune) bool { return r == '.' || r == '_' }

// compareToolVersionSegment compares one core segment: numerically when
// both are digit-only (leading zeros ignored), else lexically.
func compareToolVersionSegment(a, b string) int {
	if isDigitString(a) && isDigitString(b) {
		a, b = strings.TrimLeft(a, "0"), strings.TrimLeft(b, "0")
		if len(a) != len(b) {
			if len(a) < len(b) {
				return -1
			}
			return 1
		}
	}
	return strings.Compare(a, b)
}

func isDigitString(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// LoadSessionToolVersion returns the stored first-seen tool/CLI version for
// a session. The result is "" when the session exists but no adapter has
// stamped a version (the honest unknown). sql.ErrNoRows propagates
// unwrapped when the session does not exist so callers can 404.
func (s *Store) LoadSessionToolVersion(ctx context.Context, sessionID string) (string, error) {
	first, _, err := s.LoadSessionToolVersions(ctx, sessionID)
	return first, err
}

// LoadSessionToolVersions returns the stored first and last tool/CLI
// versions for a session. last falls back to first when it was never
// stamped (a row from before migration 162), so last == first means no
// upgrade was seen. Both are "" when no adapter stamped a version (the
// honest unknown). sql.ErrNoRows propagates unwrapped when the session does
// not exist so callers can 404.
func (s *Store) LoadSessionToolVersions(ctx context.Context, sessionID string) (first, last string, err error) {
	if sessionID == "" {
		return "", "", errors.New("store.LoadSessionToolVersions: sessionID is required")
	}
	if err := s.db.QueryRowContext(
		ctx,
		`SELECT COALESCE(tool_version, ''),
		        COALESCE(NULLIF(tool_version_last, ''), tool_version, '')
		   FROM sessions WHERE id = ?`,
		sessionID,
	).Scan(&first, &last); err != nil {
		return "", "", err // sql.ErrNoRows propagates to the caller unwrapped.
	}
	return first, last, nil
}
