package store

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/models"
)

// TestSetSessionToolVersion pins the two captured version columns
// (migrations 125 and 162) through their one write path:
// tool_version is FIRST-WINS-UNLESS-EMPTY (a re-parse never rewrites the
// build the session started on) and tool_version_last moves forward to the
// highest version seen, so an upgrade mid-session shows as first != last
// and a re-parse that re-stamps the older build never rewinds it. The rows
// run in order against one session.
func TestSetSessionToolVersion(t *testing.T) {
	t.Parallel()
	s, database := newTestStore(t)
	ctx := context.Background()

	pid, _ := s.UpsertProject(ctx, "/tmp/toolver", "")
	if err := s.UpsertSession(ctx, models.Session{
		ID: "tv-sess", ProjectID: pid, Tool: models.ToolClaudeCode,
		StartedAt: time.Date(2026, 9, 21, 10, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatal(err)
	}

	read := func() (string, string) {
		var first, last string
		if err := database.QueryRowContext(ctx,
			`SELECT COALESCE(tool_version,''), COALESCE(tool_version_last,'') FROM sessions WHERE id='tv-sess'`,
		).Scan(&first, &last); err != nil {
			t.Fatal(err)
		}
		return first, last
	}

	cases := []struct {
		name        string
		in          models.SessionToolVersion
		wantChanged bool
		wantErr     bool
		wantFirst   string
		wantLast    string
	}{
		{
			name:        "first stamp sets both",
			in:          models.SessionToolVersion{SessionID: "tv-sess", Version: "0.150.0"},
			wantChanged: true, wantFirst: "0.150.0", wantLast: "0.150.0",
		},
		{
			name:        "identical re-stamp is a no-op",
			in:          models.SessionToolVersion{SessionID: "tv-sess", Version: "0.150.0"},
			wantChanged: false, wantFirst: "0.150.0", wantLast: "0.150.0",
		},
		{
			name:        "empty version preserves",
			in:          models.SessionToolVersion{SessionID: "tv-sess"},
			wantChanged: false, wantFirst: "0.150.0", wantLast: "0.150.0",
		},
		{
			name:        "malformed version preserves",
			in:          models.SessionToolVersion{SessionID: "tv-sess", Version: "https://x"},
			wantChanged: false, wantFirst: "0.150.0", wantLast: "0.150.0",
		},
		{
			// Mid-session upgrade: first stays the origin, last moves.
			name:        "newer version moves last only",
			in:          models.SessionToolVersion{SessionID: "tv-sess", Version: "0.151.0"},
			wantChanged: true, wantFirst: "0.150.0", wantLast: "0.151.0",
		},
		{
			// A whole-file re-parse re-stamps the older build first; it must
			// not rewind the recorded upgrade.
			name:        "older re-stamp does not rewind last",
			in:          models.SessionToolVersion{SessionID: "tv-sess", Version: "0.150.0"},
			wantChanged: false, wantFirst: "0.150.0", wantLast: "0.151.0",
		},
		{
			name:        "numeric ordering, not lexical",
			in:          models.SessionToolVersion{SessionID: "tv-sess", Version: "0.1000.0"},
			wantChanged: true, wantFirst: "0.150.0", wantLast: "0.1000.0",
		},
		{
			name:    "missing session id is an error",
			in:      models.SessionToolVersion{Version: "1.0.0"},
			wantErr: true, wantFirst: "0.150.0", wantLast: "0.1000.0",
		},
		{
			name:        "unknown session is a silent no-op",
			in:          models.SessionToolVersion{SessionID: "no-such-session", Version: "9.9.9"},
			wantChanged: false, wantFirst: "0.150.0", wantLast: "0.1000.0",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			changed, err := s.SetSessionToolVersion(ctx, tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
			} else if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !tc.wantErr && changed != tc.wantChanged {
				t.Errorf("changed = %v, want %v", changed, tc.wantChanged)
			}
			first, last := read()
			if first != tc.wantFirst || last != tc.wantLast {
				t.Errorf("stored (first, last) = (%q, %q), want (%q, %q)", first, last, tc.wantFirst, tc.wantLast)
			}
		})
	}

	first, last, err := s.LoadSessionToolVersions(ctx, "tv-sess")
	if err != nil || first != "0.150.0" || last != "0.1000.0" {
		t.Fatalf("LoadSessionToolVersions = (%q, %q, %v), want (0.150.0, 0.1000.0, nil)", first, last, err)
	}
}

// TestSetSessionToolVersionPre162Row pins the upgrade path for a session
// stamped before migration 162: tool_version is set and tool_version_last
// is NULL. Reads fall back to tool_version, and the next valid stamp takes
// tool_version as the base for "last", so an OLDER stamp leaves last at the
// first value rather than moving it backwards.
func TestSetSessionToolVersionPre162Row(t *testing.T) {
	t.Parallel()
	s, database := newTestStore(t)
	ctx := context.Background()
	pid, _ := s.UpsertProject(ctx, "/tmp/toolver-pre162", "")
	if err := s.UpsertSession(ctx, models.Session{
		ID: "tv-old", ProjectID: pid, Tool: models.ToolCline,
		StartedAt: time.Date(2026, 9, 21, 10, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `UPDATE sessions SET tool_version = '3.17.4' WHERE id = 'tv-old'`); err != nil {
		t.Fatal(err)
	}
	if first, last, err := s.LoadSessionToolVersions(ctx, "tv-old"); err != nil || first != "3.17.4" || last != "3.17.4" {
		t.Fatalf("NULL last must read as first: (%q, %q, %v)", first, last, err)
	}
	if _, err := s.SetSessionToolVersion(ctx, models.SessionToolVersion{SessionID: "tv-old", Version: "3.10.0"}); err != nil {
		t.Fatal(err)
	}
	if first, last, _ := s.LoadSessionToolVersions(ctx, "tv-old"); first != "3.17.4" || last != "3.17.4" {
		t.Fatalf("older stamp on a pre-162 row: (%q, %q), want (3.17.4, 3.17.4)", first, last)
	}
	if _, err := s.SetSessionToolVersion(ctx, models.SessionToolVersion{SessionID: "tv-old", Version: "3.18.0"}); err != nil {
		t.Fatal(err)
	}
	if first, last, _ := s.LoadSessionToolVersions(ctx, "tv-old"); first != "3.17.4" || last != "3.18.0" {
		t.Fatalf("newer stamp on a pre-162 row: (%q, %q), want (3.17.4, 3.18.0)", first, last)
	}
}

// TestCompareToolVersions pins the ordering the "last seen" column uses,
// one case per rule.
func TestCompareToolVersions(t *testing.T) {
	t.Parallel()
	cases := []struct {
		a, b string
		want int
	}{
		{"1.2.3", "1.2.3", 0},
		{"1.2.10", "1.2.9", 1},
		{"0.150.0", "0.1000.0", -1},
		{"v1.2.3", "1.2.3", 0},
		{"V2.0", "v1.9.9", 1},
		{"1.2", "1.2.0", -1},
		{"1.2.3", "1.2.3-rc.1", 1},
		{"1.2.3-beta.1", "1.2.3-rc.1", -1},
		{"1.2.3+build.7", "1.2.3", -1},
		{"2026.8.2", "2026.10.1", -1},
		{"2.1.0_beta", "2.1.0", 1},
		{"01.2", "1.2", 0},
	}
	for _, tc := range cases {
		if got := compareToolVersions(tc.a, tc.b); got != tc.want {
			t.Errorf("compareToolVersions(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
		if got := compareToolVersions(tc.b, tc.a); got != -tc.want {
			t.Errorf("compareToolVersions(%q, %q) = %d, want %d (antisymmetry)", tc.b, tc.a, got, -tc.want)
		}
	}
}

// TestSetSessionToolVersionRejectsMalformed pins the bounded-token
// validation: a value that is whitespace-bearing, control-bearing,
// prose, or over the length cap is SKIPPED (returns false, nil) and
// never persisted — the honest "unknown" empty is preserved.
func TestSetSessionToolVersionRejectsMalformed(t *testing.T) {
	t.Parallel()
	s, database := newTestStore(t)
	ctx := context.Background()

	pid, _ := s.UpsertProject(ctx, "/tmp/toolver-bad", "")
	if err := s.UpsertSession(ctx, models.Session{
		ID: "tv-bad", ProjectID: pid, Tool: models.ToolClaudeCode,
		StartedAt: time.Date(2026, 9, 21, 10, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatal(err)
	}
	read := func() string {
		var v string
		if err := database.QueryRowContext(ctx,
			`SELECT COALESCE(tool_version,'') FROM sessions WHERE id='tv-bad'`,
		).Scan(&v); err != nil {
			t.Fatal(err)
		}
		return v
	}

	bad := []struct {
		name    string
		version string
	}{
		{"has a space", "version 1.2.3"},
		{"has a tab", "1.2.3\t"},
		{"has a newline", "1.2.3\n"},
		{"has a control byte", "1.2.\x00"},
		{"prose", "the latest build as of tuesday"},
		{"over the length cap", strings.Repeat("9", maxToolVersionRunes+1)},
		{"empty", ""},
		// TOOLVERSION-1 (docs/security.md): the grammar rejects shapes a
		// URL/email/path/compact-secret needs but a real version never does.
		{"url scheme", "https://x"},
		{"url with host and path", "https://attacker.example/collect"},
		{"email-shaped", "user@example"},
		{"path traversal", "../../client"},
		{"absolute path", "/etc/passwd"},
		{"leading dot", ".hidden"},
		{"leading hyphen", "-1.2.3"},
		{"embedded colon", "sbo:secret-token"},
		{"embedded slash", "1.2/3"},
		{"compact base64-shaped secret with padding", "c2VjcmV0LXRva2VuLXZhbHVl="},
		// TOOLVERSION-1 follow-up (2026-09-23): the grammar now requires a
		// leading digit (after an optional "v"/"V") AND at least one ".",
		// which a compact secret's alphanumeric noise never has.
		{"AWS access key id", "AKIAIOSFODNN7EXAMPLE"},
		{"GitHub PAT", "ghp_abcdefghijklmnopqrstuvwxyz1234567890"},
		{"JWT", "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c"},
		{"hex sha (no dot)", "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"},
		{"hex sha, digit-leading (no dot)", "0d3b45c1a2f6e8b9c0d1e2f3a4b5c6d7e8f9a0b1"},
		{"UUID (no dot)", "550e8400-e29b-41d4-a716-446655440000"},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			changed, err := s.SetSessionToolVersion(ctx, models.SessionToolVersion{
				SessionID: "tv-bad", Version: tc.version,
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if changed {
				t.Errorf("malformed value reported changed=true; want false")
			}
			if got := read(); got != "" {
				t.Errorf("malformed value was persisted: %q", got)
			}
		})
	}

	// A value at exactly the length cap, version-shaped (leading "v", a
	// digit, and a "."), with no whitespace/control, IS accepted — the cap
	// is inclusive. "v9." (3 runes) + 61 more digits = 64 runes total.
	okVer := "v9." + strings.Repeat("9", maxToolVersionRunes-3)
	if len(okVer) != maxToolVersionRunes {
		t.Fatalf("test setup bug: okVer length = %d, want %d", len(okVer), maxToolVersionRunes)
	}
	changed, err := s.SetSessionToolVersion(ctx, models.SessionToolVersion{SessionID: "tv-bad", Version: okVer})
	if err != nil || !changed {
		t.Fatalf("value at length cap should be accepted: changed=%v err=%v", changed, err)
	}
	if got := read(); got != okVer {
		t.Errorf("stored = %q, want %q", got, okVer)
	}
}

// TestValidToolVersionAdmitsRealVendorShapes pins that the TOOLVERSION-1
// grammar (delegated to orgcontract.ValidToolVersion) admits every shape a
// real adapter actually stamps: plain semver, a "v"-prefixed CLI version, a
// date-based CalVer release, and semver with a prerelease or build-metadata
// suffix (grepped from internal/adapter/{claudecode,codex,cline,kilocode,
// qoder,copilotcli,qwencode} 2026-09-22).
func TestValidToolVersionAdmitsRealVendorShapes(t *testing.T) {
	t.Parallel()
	good := []string{
		"1.2.3",
		"2026.8.2",
		"v0.130.0",
		"1.8.1-rc.1",
		"0.45.0+build.7",
		"2.1.0_beta",
		"0.150.0",
		"3.17.4-beta.1",
	}
	for _, v := range good {
		if !validToolVersion(v) {
			t.Errorf("validToolVersion(%q) = false, want true (a real vendor-stamped shape)", v)
		}
	}
}
