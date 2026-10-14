package aider

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestToolVersionFromBanner pins T-784: the `> Aider vX.Y.Z` startup
// banner becomes exactly one SessionToolVersion per session that printed
// it, keyed to the same session key the rows use; a session without a
// banner (windows-multi's second session) gets no stamp.
func TestToolVersionFromBanner(t *testing.T) {
	cases := []struct {
		fixture string
		want    []string // per-session ordinal suffix that must be stamped
	}{
		{"edit-linux.md", []string{"-00"}},
		{"readonly.md", []string{"-00"}},
		{"windows-multi.md", []string{"-00"}},
	}
	for _, tc := range cases {
		t.Run(tc.fixture, func(t *testing.T) {
			p := newTestAdapter().parseTranscriptState(context.Background(), fixture(t, tc.fixture), "/proj", "/proj", "")
			if len(p.versions) != len(tc.want) {
				t.Fatalf("versions = %+v, want %d", p.versions, len(tc.want))
			}
			for i, tv := range p.versions {
				if tv.Version != "0.86.2" {
					t.Errorf("version = %q, want 0.86.2", tv.Version)
				}
				if !strings.HasSuffix(tv.SessionID, tc.want[i]) {
					t.Errorf("session = %q, want suffix %q", tv.SessionID, tc.want[i])
				}
			}
		})
	}
}

// TestToolVersionOncePerSessionViaParseSessionFile drives the real
// ParseSessionFile entry: a repeated banner inside one session still
// yields ONE stamp, and the stamp rides on ParseResult.
func TestToolVersionOncePerSessionViaParseSessionFile(t *testing.T) {
	body := "# aider chat started at 2026-07-09 04:37:13\n\n" +
		"> Aider v0.86.2  \n> Main model: gpt-4o with diff edit format  \n\n" +
		"#### hello\n\nHi there.\n\n" +
		"> Aider v0.86.2  \n"
	dir := t.TempDir()
	path := filepath.Join(dir, ".aider.chat.history.md")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	res, err := newTestAdapter().ParseSessionFile(context.Background(), path, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.SessionToolVersions) != 1 || res.SessionToolVersions[0].Version != "0.86.2" {
		t.Fatalf("SessionToolVersions = %+v, want one 0.86.2", res.SessionToolVersions)
	}
	if len(res.ToolEvents) == 0 || res.ToolEvents[0].SessionID != res.SessionToolVersions[0].SessionID {
		t.Errorf("stamp session does not match row session: %+v vs %+v", res.SessionToolVersions, res.ToolEvents)
	}
}
