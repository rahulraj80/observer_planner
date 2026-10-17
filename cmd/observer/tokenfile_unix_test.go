//go:build !windows

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestResolveLocalTokenFileShape pins P3-27, one row per file shape: the
// listener token file is used only as a private regular file of ours. A
// symlink (another account could point it at a file it reads) is refused, a
// world-readable file is repaired to 0600 before use, and a missing or empty
// file is (re)created 0600.
func TestResolveLocalTokenFileShape(t *testing.T) {
	const tok = "0123456789abcdef0123456789abcdef"
	for _, tc := range []struct {
		name    string
		setup   func(t *testing.T, path string)
		wantErr string
		wantTok string
	}{
		{name: "missing: created", setup: func(*testing.T, string) {}},
		{name: "empty: regenerated", setup: func(t *testing.T, path string) {
			if err := os.WriteFile(path, nil, 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "world readable: repaired", wantTok: tok, setup: func(t *testing.T, path string) {
			if err := os.WriteFile(path, []byte(tok+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(path, 0o644); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "symlink: refused", wantErr: "not a regular file", setup: func(t *testing.T, path string) {
			target := filepath.Join(filepath.Dir(path), "elsewhere")
			if err := os.WriteFile(target, []byte(tok+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, path); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "directory: refused", wantErr: "not a regular file", setup: func(t *testing.T, path string) {
			if err := os.Mkdir(path, 0o700); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "otlp-ingest-token")
			tc.setup(t, path)
			got, err := resolveLocalTokenFile(path)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want %q (token %q)", err, tc.wantErr, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveLocalTokenFile: %v", err)
			}
			if tc.wantTok != "" && got != tc.wantTok {
				t.Fatalf("token = %q, want the stored one", got)
			}
			if len(got) != 64 && tc.wantTok == "" {
				t.Fatalf("generated token %q is not 32 random bytes", got)
			}
			info, err := os.Lstat(path)
			if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
				t.Fatalf("token file after use: %v mode %v", err, info.Mode())
			}
			again, err := resolveLocalTokenFile(path)
			if err != nil || again != got {
				t.Fatalf("second read = %q, %v; want the same token", again, err)
			}
		})
	}
}
