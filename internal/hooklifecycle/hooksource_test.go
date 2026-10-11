package hooklifecycle

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

// TestIsHookSource pins the T-563 boundary classification: the
// "<tool>:hook" source_file convention, read from the raw path on a node or
// a full-content org row and from source_file_hash on a metadata-only one.
func TestIsHookSource(t *testing.T) {
	t.Parallel()
	hash := func(s string) string { sum := sha256.Sum256([]byte(s)); return hex.EncodeToString(sum[:]) }
	tests := []struct {
		name               string
		tool, file, fileHs string
		want               bool
	}{
		{"raw hook path", "codex", "codex:hook", "", true},
		{"raw transcript path", "codex", "/home/u/.codex/sessions/r.jsonl", hash("/home/u/.codex/sessions/r.jsonl"), false},
		{"another tool's hook path", "codex", "cursor:hook", "", false},
		{"metadata-only row, hook hash", "codex", "", hash("codex:hook"), true},
		{"metadata-only row, transcript hash", "codex", "", hash("/x/r.jsonl"), false},
		{"raw path wins over a stale hash", "codex", "/x/r.jsonl", hash("codex:hook"), false},
		{"no tool", "", "codex:hook", "", false},
		{"nothing", "codex", "", "", false},
	}
	for _, tc := range tests {
		if got := IsHookSource(tc.tool, tc.file, tc.fileHs); got != tc.want {
			t.Errorf("%s: IsHookSource = %v, want %v", tc.name, got, tc.want)
		}
	}
}
