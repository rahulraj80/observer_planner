//go:build unix

package managedconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestReadOwnedFile is one row per ownership / symlink / size check of the
// unix reader (plan 6.2.2), on a temporary tree standing in for /etc.
func TestReadOwnedFile(t *testing.T) {
	cases := []struct {
		name   string
		setup  func(t *testing.T, etc string) string // returns the path to read
		found  bool
		failed string
	}{
		{
			name:  "absent is no source",
			setup: func(_ *testing.T, etc string) string { return filepath.Join(etc, "superbased", "managed.toml") },
		},
		{
			name: "absent parent is no source",
			setup: func(_ *testing.T, etc string) string {
				return filepath.Join(etc, "nothing-here", "managed.toml")
			},
		},
		{
			name:  "valid",
			setup: func(t *testing.T, etc string) string { return writeFile(t, etc, 0o644) },
			found: true,
		},
		{
			name:   "group writable file",
			setup:  func(t *testing.T, etc string) string { return writeFile(t, etc, 0o664) },
			found:  true,
			failed: "group_or_world_writable",
		},
		{
			name: "world writable parent",
			setup: func(t *testing.T, etc string) string {
				p := writeFile(t, etc, 0o644)
				if err := os.Chmod(filepath.Dir(p), 0o777); err != nil {
					t.Fatal(err)
				}
				return p
			},
			found:  true,
			failed: "parent_group_or_world_writable",
		},
		{
			name: "symlink is refused",
			setup: func(t *testing.T, etc string) string {
				target := writeFile(t, etc, 0o644)
				link := filepath.Join(filepath.Dir(target), "link.toml")
				if err := os.Symlink(target, link); err != nil {
					t.Fatal(err)
				}
				return link
			},
			found:  true,
			failed: "symlink",
		},
		{
			name: "a directory is not a file",
			setup: func(t *testing.T, etc string) string {
				p := filepath.Join(etc, "superbased", "managed.toml")
				if err := os.MkdirAll(p, 0o755); err != nil {
					t.Fatal(err)
				}
				return p
			},
			found:  true,
			failed: "not_a_regular_file",
		},
		{
			name: "too large",
			setup: func(t *testing.T, etc string) string {
				p := writeFile(t, etc, 0o644)
				if err := os.WriteFile(p, []byte(strings.Repeat("#", MaxBytes+1)), 0o644); err != nil {
					t.Fatal(err)
				}
				return p
			},
			found:  true,
			failed: "too_large",
		},
		{
			name: "wrong owner",
			setup: func(t *testing.T, etc string) string {
				requiredOwner = uint32(os.Getuid()) + 1
				return writeFile(t, etc, 0o644)
			},
			found:  true,
			failed: "parent_not_root_owned",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			etc := t.TempDir()
			oldRoot, oldOwner := trustRoot, requiredOwner
			trustRoot, requiredOwner = etc, uint32(os.Getuid())
			t.Cleanup(func() { trustRoot, requiredOwner = oldRoot, oldOwner })
			if err := os.Chmod(etc, 0o755); err != nil {
				t.Fatal(err)
			}
			p := readOwnedFile(tc.setup(t, etc), "toml")
			if p.Found != tc.found || !strings.HasPrefix(p.FailedCheck, tc.failed) || (tc.failed == "" && p.FailedCheck != "") {
				t.Fatalf("found=%v failed=%q, want found=%v failed=%q", p.Found, p.FailedCheck, tc.found, tc.failed)
			}
			if tc.found && tc.failed == "" {
				r := Decide(p, nil)
				if r.Status != StatusValid {
					t.Fatalf("decide: %+v", r)
				}
			}
		})
	}
}

func writeFile(t *testing.T, etc string, mode os.FileMode) string {
	t.Helper()
	dir := filepath.Join(etc, "superbased")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "managed.toml")
	if err := os.WriteFile(p, []byte(validDoc), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, mode); err != nil {
		t.Fatal(err)
	}
	return p
}
