package poolside

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestToolVersionFromClientMetadata pins T-784: the standalone Linux pool
// TUI's session_start.client_metadata.version ("1.0.16") becomes one
// SessionToolVersion keyed to the filename session id, and the JetBrains
// ACP capture (no client_metadata at all) stamps nothing.
func TestToolVersionFromClientMetadata(t *testing.T) {
	cases := []struct {
		name    string
		fixture string
		sid     string
		want    string // "" = no stamp
	}{
		{"linux pool TUI", linuxCLIFixture, "019a0c00-0000-7000-8000-000000000001", "1.0.16"},
		{"jetbrains acp", fixtureName, "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "poolside", "trajectories")
			if err := os.MkdirAll(root, 0o755); err != nil {
				t.Fatal(err)
			}
			body, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "poolside", filepath.FromSlash(tc.fixture)))
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, filepath.Base(tc.fixture))
			if err := os.WriteFile(path, body, 0o600); err != nil {
				t.Fatal(err)
			}
			res, err := NewWithOptions(nil, root).ParseSessionFile(context.Background(), path, 0)
			if err != nil {
				t.Fatalf("ParseSessionFile: %v", err)
			}
			if tc.want == "" {
				if len(res.SessionToolVersions) != 0 {
					t.Fatalf("SessionToolVersions = %+v, want none", res.SessionToolVersions)
				}
				return
			}
			if len(res.SessionToolVersions) != 1 {
				t.Fatalf("SessionToolVersions = %+v, want exactly one", res.SessionToolVersions)
			}
			if got := res.SessionToolVersions[0]; got.SessionID != tc.sid || got.Version != tc.want {
				t.Errorf("stamp = %+v, want {%s %s}", got, tc.sid, tc.want)
			}
		})
	}
}
