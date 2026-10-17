package cowork

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// writeSubagentTree lays out n subagent transcripts, one assistant uuid each,
// under instanceDir/.claude/projects/<cwd>/<id>/subagents/.
func writeSubagentTree(t *testing.T, instanceDir string, n int) {
	t.Helper()
	dir := filepath.Join(instanceDir, ".claude", "projects", "-work", "sess-1", "subagents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < n; i++ {
		line := `{"type":"assistant","uuid":"u-` + string(rune('a'+i)) + `"}` + "\n"
		if err := os.WriteFile(filepath.Join(dir, "agent-"+string(rune('a'+i))+".jsonl"), []byte(line), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// TestCollectSidechainUUIDs_HonoursCancellation pins T-557: the per-parse
// subagent-tree walk stops on a cancelled context (daemon shutdown) instead
// of walking and decoding every transcript first, and reports the
// cancellation so ParseSessionFile returns it.
func TestCollectSidechainUUIDs_HonoursCancellation(t *testing.T) {
	t.Parallel()
	inst := t.TempDir()
	writeSubagentTree(t, inst, 5)

	got, err := collectSidechainUUIDs(context.Background(), inst)
	if err != nil || len(got) != 5 {
		t.Fatalf("live ctx: got %d uuids, err %v; want 5, nil", len(got), err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, err = collectSidechainUUIDs(ctx, inst)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled ctx: err = %v, want context.Canceled", err)
	}
	if len(got) != 0 {
		t.Fatalf("cancelled ctx: decoded %d transcripts, want none", len(got))
	}
}
