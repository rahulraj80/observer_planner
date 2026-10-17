package codex

import (
	"context"
	"testing"
)

// TestOpenInterpreterToolVersion pins Issue 2 (migration 125) for the
// Open Interpreter retag of the codex parser: both live-captured rollouts
// (the CLI store and the desktop app's embedded codex-home) carry a
// session_meta cli_version, which must surface as exactly one
// SessionToolVersion keyed to the same session id the tool rows use.
func TestOpenInterpreterToolVersion(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		dir     string
		path    string
		version string
	}{
		{"cli", oiFixtureDir(t), oiFixturePath(t), "0.0.28"},
		{"desktop", oiDesktopFixtureDir(t), oiDesktopFixturePath(t), "0.0.10"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res, err := NewOpenInterpreterWithOptions(nil, tc.dir).ParseSessionFile(context.Background(), tc.path, 0)
			if err != nil {
				t.Fatalf("ParseSessionFile: %v", err)
			}
			if len(res.SessionToolVersions) != 1 {
				t.Fatalf("SessionToolVersions = %+v, want exactly one", res.SessionToolVersions)
			}
			got := res.SessionToolVersions[0]
			if got.Version != tc.version {
				t.Errorf("version = %q, want %q", got.Version, tc.version)
			}
			if len(res.ToolEvents) == 0 {
				t.Fatal("no tool events parsed from the fixture")
			}
			if got.SessionID != res.ToolEvents[0].SessionID {
				t.Errorf("stamp session = %q, tool rows use %q", got.SessionID, res.ToolEvents[0].SessionID)
			}
		})
	}
}
